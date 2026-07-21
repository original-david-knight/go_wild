package deepresearch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"google.golang.org/genai"
)

type geminiDeepResearchPlanner struct {
	client          *genai.Client
	model           string
	generateContent func(context.Context, string, []*genai.Content, *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error)
}

func NewGeminiPlanner() (*geminiDeepResearchPlanner, error) {
	apiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("Gemini API not configured (set GEMINI_API_KEY)")
	}
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini planner client: %w", err)
	}

	model := deepResearchModelFromEnv("DEEP_RESEARCH_PLANNER_MODEL", "SMART_MODEL")
	return &geminiDeepResearchPlanner{
		client: client,
		model:  model,
		generateContent: retryOnRateLimit(func(ctx context.Context, model string, contents []*genai.Content, cfg *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return client.Models.GenerateContent(ctx, model, contents, cfg)
		}),
	}, nil
}

func (p *geminiDeepResearchPlanner) Plan(ctx context.Context, req PlanningRequest) (PlanningResult, error) {
	if len(req.MissingObjectives) == 0 {
		return PlanningResult{}, nil
	}
	if p == nil || p.generateContent == nil {
		return PlanningResult{}, fmt.Errorf("planner model client is not configured")
	}

	prompt := deepResearchPlannerPrompt(req)
	temp := float32(0.2)
	resp, err := p.generateContent(ctx, p.model, genai.Text(prompt), &genai.GenerateContentConfig{
		Temperature:      &temp,
		MaxOutputTokens:  16384,
		ResponseMIMEType: "application/json",
		ResponseSchema:   deepResearchPlannerResponseSchema(),
	})
	if err != nil {
		return PlanningResult{}, err
	}

	raw := deepResearchCandidateText(firstCandidateContent(resp))
	if strings.TrimSpace(raw) == "" {
		return PlanningResult{}, fmt.Errorf("planner returned empty response")
	}

	var parsed struct {
		Queries []struct {
			ObjectiveKey string `json:"objective_key"`
			Query        string `json:"query"`
			Rationale    string `json:"rationale"`
		} `json:"queries"`
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return PlanningResult{}, fmt.Errorf("planner returned invalid JSON: %w", err)
	}

	allowed := make(map[string]struct{}, len(req.MissingObjectives))
	for _, objective := range req.MissingObjectives {
		key := strings.TrimSpace(objective.Key)
		if key != "" {
			allowed[key] = struct{}{}
		}
	}

	out := PlanningResult{
		Queries:   make([]PlannedQuery, 0, len(parsed.Queries)),
		Reasoning: strings.TrimSpace(parsed.Reasoning),
	}
	for _, row := range parsed.Queries {
		key := strings.TrimSpace(row.ObjectiveKey)
		query := strings.TrimSpace(row.Query)
		if key == "" || query == "" {
			continue
		}
		if _, ok := allowed[key]; !ok {
			continue
		}
		out.Queries = append(out.Queries, PlannedQuery{
			ObjectiveKey: key,
			Query:        query,
			Rationale:    strings.TrimSpace(row.Rationale),
		})
	}
	return out, nil
}

func deepResearchPlannerResponseSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"queries": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"objective_key": {
							Type: genai.TypeString,
						},
						"query": {
							Type: genai.TypeString,
						},
						"rationale": {
							Type: genai.TypeString,
						},
					},
					Required: []string{"objective_key", "query"},
				},
			},
			"reasoning": {
				Type: genai.TypeString,
			},
		},
		Required: []string{"queries"},
	}
}

func deepResearchPlannerPrompt(req PlanningRequest) string {
	problem := strings.TrimSpace(req.Guidance)
	if problem == "" {
		problem = strings.TrimSpace(req.Query)
	}
	objectiveKey := "research"
	if len(req.MissingObjectives) > 0 {
		objectiveKey = strings.TrimSpace(req.MissingObjectives[0].Key)
	}

	sources := make([]string, 0, len(req.Sources))
	for i, source := range req.Sources {
		if i >= 8 {
			break
		}
		u := strings.TrimSpace(source.URL)
		if u == "" {
			continue
		}
		line := u
		if title := strings.TrimSpace(source.Title); title != "" {
			line = title + " | " + u
		}
		sources = append(sources, line)
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
			Score   float64 `json:"score"`
			Excerpt string  `json:"excerpt,omitempty"`
			Snippet string  `json:"snippet,omitempty"`
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
				items = append(items, compactFinding{
					URL:     row.URL,
					Title:   row.Title,
					Score:   row.Score,
					Excerpt: row.Excerpt,
					Snippet: row.Snippet,
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

You are the planner for a deep research agent.
Your job is to produce targeted web search queries that will gather enough evidence to fill every field of the target schema.

The schema fields are interrelated — do NOT treat each field as a separate research topic.
Instead, think about what sub-topics, angles, or source types would provide the best overall coverage.
A single good source may inform multiple schema fields.

Return JSON with fields: queries (array) and reasoning.
Every query in the array must use objective_key = "%s".

Base user query:
%s

Round: %d

Current top sources:
%s

Current findings (compact JSON):
%s

Target JSON schema:
%s

Rules:
- Return 3-6 diverse search queries that together cover the schema.
- Each query must be specific, web-searchable, and evidence-oriented.
- All queries must use objective_key = "%s".
- Vary query angles: try different phrasings, sub-topics, and source types.
- Avoid repeating queries that led to sources already found.
- Assume snippets are insufficient; plan queries that lead to full-page primary sources.
- Prefer recent and primary sources when appropriate.
- When the topic may be affected by current events, recent policy changes, market movements, or breaking news, include at least one query targeting very recent developments (e.g., add "2025" or "latest" or "today" to the query). Stale information can make answers wrong.`,
		problem,
		now,
		objectiveKey,
		strings.TrimSpace(req.Query),
		req.Round,
		strings.Join(sources, "\n"),
		findingsText,
		schemaText,
		objectiveKey,
	)
}

func firstCandidateContent(resp *genai.GenerateContentResponse) *genai.Content {
	if resp == nil || len(resp.Candidates) == 0 {
		return nil
	}
	if resp.Candidates[0] == nil {
		return nil
	}
	return resp.Candidates[0].Content
}
