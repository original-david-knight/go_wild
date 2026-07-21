package deepresearch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"
)

type geminiDeepResearchCompletenessChecker struct {
	client          *genai.Client
	model           string
	generateContent func(context.Context, string, []*genai.Content, *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error)
}

func NewGeminiCompletenessChecker() (*geminiDeepResearchCompletenessChecker, error) {
	apiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("Gemini API not configured (set GEMINI_API_KEY)")
	}
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini completeness-checker client: %w", err)
	}

	model := deepResearchModelFromEnv("DEEP_RESEARCH_CHECKER_MODEL", "FAST_MODEL")
	return &geminiDeepResearchCompletenessChecker{
		client: client,
		model:  model,
		generateContent: retryOnRateLimit(func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return client.Models.GenerateContent(ctx, model, contents, cfg)
		}),
	}, nil
}

func (c *geminiDeepResearchCompletenessChecker) Check(ctx context.Context, req CompletenessRequest) (CompletenessResult, error) {
	if len(req.Objectives) == 0 {
		return CompletenessResult{Complete: true}, nil
	}
	if c == nil || c.generateContent == nil {
		return CompletenessResult{}, fmt.Errorf("completeness checker model client is not configured")
	}

	prompt := deepResearchCompletenessPrompt(req)
	temp := float32(0)
	resp, err := c.generateContent(ctx, c.model, genai.Text(prompt), &genai.GenerateContentConfig{
		Temperature:      &temp,
		MaxOutputTokens:  16384,
		ResponseMIMEType: "application/json",
		ResponseSchema:   deepResearchCompletenessResponseSchema(),
	})
	if err != nil {
		return CompletenessResult{}, err
	}

	raw := strings.TrimSpace(deepResearchCandidateText(firstCandidateContent(resp)))
	if raw == "" {
		return CompletenessResult{}, fmt.Errorf("completeness checker returned empty response")
	}

	var parsed struct {
		Complete          bool   `json:"complete"`
		Reasoning         string `json:"reasoning"`
		MissingObjectives []struct {
			ObjectiveKey string `json:"objective_key"`
			Question     string `json:"question"`
		} `json:"missing_objectives"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		snippet := raw
		if len(snippet) > 280 {
			snippet = snippet[:280] + "..."
		}
		return CompletenessResult{}, fmt.Errorf("completeness checker returned invalid JSON: %w (raw=%q)", err, snippet)
	}

	out := CompletenessResult{
		Complete:  parsed.Complete,
		Reasoning: strings.TrimSpace(parsed.Reasoning),
	}
	for _, item := range parsed.MissingObjectives {
		key := strings.TrimSpace(item.ObjectiveKey)
		if key == "" {
			continue
		}
		out.MissingObjectives = append(out.MissingObjectives, MissingObjective{
			ObjectiveKey: key,
			Question:     strings.TrimSpace(item.Question),
		})
	}
	if !out.Complete && len(out.MissingObjectives) == 0 {
		for _, item := range req.ObjectiveResults {
			if item.Status == ObjectiveStatusSatisfied {
				continue
			}
			key := strings.TrimSpace(item.Objective.Key)
			if key == "" {
				continue
			}
			out.MissingObjectives = append(out.MissingObjectives, MissingObjective{
				ObjectiveKey: key,
				Question:     "Need stronger evidence for " + key,
			})
		}
	}
	return out, nil
}

func deepResearchCompletenessResponseSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"complete": {
				Type: genai.TypeBoolean,
			},
			"reasoning": {
				Type: genai.TypeString,
			},
			"missing_objectives": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"objective_key": {
							Type: genai.TypeString,
						},
						"question": {
							Type: genai.TypeString,
						},
					},
					Required: []string{"objective_key"},
				},
			},
		},
		Required: []string{"complete", "missing_objectives"},
	}
}

func deepResearchCompletenessPrompt(req CompletenessRequest) string {
	problem := strings.TrimSpace(req.Guidance)
	if problem == "" {
		problem = strings.TrimSpace(req.Query)
	}
	excludedDomains := strings.TrimSpace(strings.Join(req.ExcludedDomains, ", "))
	if excludedDomains == "" {
		excludedDomains = "none"
	}

	objectiveKey := "research"
	totalEvidence := 0
	for _, item := range req.ObjectiveResults {
		key := strings.TrimSpace(item.Objective.Key)
		if key != "" {
			objectiveKey = key
		}
		totalEvidence += item.EvidenceCount
	}

	schemaText := "{}"
	if len(req.Schema) > 0 {
		if blob, err := json.Marshal(req.Schema); err == nil {
			schemaText = string(blob)
		}
	}

	findingsText := "{}"
	if len(req.Findings) > 0 {
		type compactFinding struct {
			URL     string  `json:"url"`
			Title   string  `json:"title,omitempty"`
			Snippet string  `json:"snippet,omitempty"`
			Score   float64 `json:"score"`
			Date    string  `json:"published_at,omitempty"`
		}
		compact := map[string][]compactFinding{}
		for key, rows := range req.Findings {
			if len(rows) == 0 {
				continue
			}
			items := make([]compactFinding, 0, len(rows))
			for i, row := range rows {
				if i >= 5 {
					break
				}
				publishedAt := ""
				if !row.PublishedAt.IsZero() {
					publishedAt = row.PublishedAt.UTC().Format(time.RFC3339)
				}
				items = append(items, compactFinding{
					URL:     row.URL,
					Title:   row.Title,
					Snippet: row.Snippet,
					Score:   row.Score,
					Date:    publishedAt,
				})
			}
			compact[key] = items
		}
		if blob, err := json.Marshal(compact); err == nil {
			findingsText = string(blob)
		}
	}

	now := deepResearchCurrentTime()

	return fmt.Sprintf(`You are working on task within the following problem:
%s

Current date and time: %s

You are a strict JSON API for research completeness evaluation.
Return exactly one JSON object and nothing else.

JSON contract (MUST follow exactly):
{
  "complete": <boolean>,
  "reasoning": "<single-line string, max 200 chars>",
  "missing_objectives": [
    {"objective_key":"%s", "question":"<what evidence is still needed>"}
  ]
}

Your job is to evaluate whether we have collected enough evidence to fill ALL fields
of the target schema. The schema fields are interrelated — evaluate holistically,
not field-by-field. Consider whether the collected sources provide sufficient coverage
to produce a well-grounded answer for the complete schema.

Hard rules:
- Never wrap JSON in markdown code fences.
- Never add commentary before or after JSON.
- Always include missing_objectives (use [] when complete=true).
- objective_key must always be "%s".
- If complete=false, include one missing_objectives entry describing what gaps remain.
- Do not treat search snippets alone as sufficient evidence; require full-page excerpt evidence.
- Never rely on excluded domains as evidence. Excluded domains: %s
Base query:
%s

Round: %d
Total sources collected: %d

Current findings (compact JSON):
%s

Target schema (all fields must be coverable by evidence):
%s`,
		problem,
		now,
		objectiveKey,
		objectiveKey,
		excludedDomains,
		strings.TrimSpace(req.Query),
		req.Round,
		totalEvidence,
		findingsText,
		schemaText,
	)
}
