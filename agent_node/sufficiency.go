package agentnode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"google.golang.org/genai"
)

// SufficiencyRequest is the input to the sufficiency checker.
type SufficiencyRequest struct {
	UserPrompt   string                     `json:"user_prompt"`
	CurrentState map[string]json.RawMessage `json:"current_state"`
	Round        int                        `json:"round"`
}

// SufficiencyResult is the checker's verdict.
type SufficiencyResult struct {
	Sufficient bool   `json:"sufficient"`
	Reasoning  string `json:"reasoning"`
}

// SufficiencyChecker determines whether accumulated results satisfy the user's request.
type SufficiencyChecker interface {
	Check(ctx context.Context, req SufficiencyRequest) (*SufficiencyResult, error)
}

// GeminiChecker uses Gemini structured output to assess result sufficiency.
type GeminiChecker struct {
	client *genai.Client
	model  string
}

// NewGeminiChecker creates a sufficiency checker backed by the Gemini API.
func NewGeminiChecker(apiKey, model string) (*GeminiChecker, error) {
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	}
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY not set")
	}
	if model == "" {
		model = "gemini-3-flash-preview"
	}

	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("failed to create checker client: %w", err)
	}

	return &GeminiChecker{client: client, model: model}, nil
}

func (c *GeminiChecker) Check(ctx context.Context, req SufficiencyRequest) (*SufficiencyResult, error) {
	prompt := buildSufficiencyPrompt(req)
	temp := float32(0.1)

	resp, err := c.client.Models.GenerateContent(ctx, c.model, genai.Text(prompt), &genai.GenerateContentConfig{
		Temperature:      &temp,
		MaxOutputTokens:  1024,
		ResponseMIMEType: "application/json",
		ResponseSchema:   sufficiencyResponseSchema(),
	})
	if err != nil {
		return nil, fmt.Errorf("sufficiency check error: %w", err)
	}

	text := extractCandidateText(resp)
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("checker returned empty response")
	}

	var result SufficiencyResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("checker returned invalid JSON: %w", err)
	}

	return &result, nil
}

func sufficiencyResponseSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"sufficient": {
				Type:        genai.TypeBoolean,
				Description: "Whether the accumulated results fully satisfy the user's request",
			},
			"reasoning": {
				Type:        genai.TypeString,
				Description: "Explanation of the sufficiency assessment",
			},
		},
		Required: []string{"sufficient", "reasoning"},
	}
}

func buildSufficiencyPrompt(req SufficiencyRequest) string {
	var sb strings.Builder
	sb.WriteString("You are a quality assessor. Determine whether the accumulated node results sufficiently answer the user's original request.\n\n")
	sb.WriteString("Be strict: results should comprehensively address the request, not just partially.\n")
	sb.WriteString("If key information is missing, incomplete, or if additional analysis would meaningfully improve the answer, mark as insufficient.\n")
	sb.WriteString("You are seeing only the final/leaf node outputs — these already incorporate all upstream research and analysis.\n\n")
	sb.WriteString("Recency policy:\n")
	sb.WriteString("- If the request is time-sensitive (e.g., current/latest/recent/by a specific date), require explicit dates and up-to-date evidence.\n")
	sb.WriteString("- If the outputs appear stale, undated, or likely based on old model memory, mark as insufficient.\n\n")

	fmt.Fprintf(&sb, "Round: %d\n\n", req.Round)
	fmt.Fprintf(&sb, "User request:\n%s\n\n", req.UserPrompt)

	if len(req.CurrentState) > 0 {
		stateJSON, _ := json.MarshalIndent(req.CurrentState, "", "  ")
		fmt.Fprintf(&sb, "Final node outputs:\n%s\n", string(stateJSON))
	} else {
		sb.WriteString("Final node outputs: (none)\n")
	}

	return sb.String()
}
