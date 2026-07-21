package objectives

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"google.golang.org/genai"
)

// EvaluationResult holds the output of a post-execution evaluation.
type EvaluationResult struct {
	Sufficient     bool              `json:"sufficient"`
	ReplanLevel    string            `json:"replan_level"`
	Reasoning      string            `json:"reasoning"`
	Decision       *DecisionEntry    `json:"decision,omitempty"`
	ExtractedFacts []*KnowledgeEntry `json:"extracted_facts,omitempty"`
}

// PostExecutionEvaluator analyzes execution results and extracts knowledge.
type PostExecutionEvaluator struct {
	client *genai.Client
	model  string
	memory *MemoryStore
}

// NewPostExecutionEvaluator creates an evaluator backed by the Gemini API.
func NewPostExecutionEvaluator(apiKey, model string, memory *MemoryStore) (*PostExecutionEvaluator, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key required for evaluator")
	}
	if model == "" {
		model = "gemini-3-flash-preview"
	}

	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("create evaluator client: %w", err)
	}

	return &PostExecutionEvaluator{client: client, model: model, memory: memory}, nil
}

// Evaluate performs a post-execution evaluation of the objective's results.
func (e *PostExecutionEvaluator) Evaluate(ctx context.Context, objective *Objective, results map[string]json.RawMessage) (*EvaluationResult, error) {
	prompt := buildEvaluationPrompt(objective, results)

	temp := float32(0.2)
	resp, err := e.client.Models.GenerateContent(ctx, e.model, genai.Text(prompt), &genai.GenerateContentConfig{
		Temperature:      &temp,
		MaxOutputTokens:  4096,
		ResponseMIMEType: "application/json",
		ResponseSchema:   evaluationSchema(),
	})
	if err != nil {
		return nil, fmt.Errorf("evaluator generation error: %w", err)
	}

	text := extractText(resp)
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("evaluator returned empty response")
	}

	var raw struct {
		Sufficient     bool   `json:"sufficient"`
		Reasoning      string `json:"reasoning"`
		ReplanLevel    string `json:"replan_level"`
		ExtractedFacts []struct {
			Fact          string   `json:"fact"`
			Tags          []string `json:"tags"`
			Confidence    float64  `json:"confidence"`
			ExpiresInDays int      `json:"expires_in_days"`
		} `json:"extracted_facts"`
		DecisionSummary *struct {
			Decision  string `json:"decision"`
			Reasoning string `json:"reasoning"`
			Outcome   string `json:"outcome"`
		} `json:"decision_summary"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("evaluator returned invalid JSON: %w", err)
	}

	result := &EvaluationResult{
		Sufficient:  raw.Sufficient,
		ReplanLevel: raw.ReplanLevel,
		Reasoning:   raw.Reasoning,
	}

	// Store extracted facts as knowledge entries
	now := time.Now().UTC()
	for _, f := range raw.ExtractedFacts {
		entry := &KnowledgeEntry{
			ObjectiveID: objective.ID,
			Fact:        f.Fact,
			Source:       "evaluator",
			Tags:        f.Tags,
			Confidence:  f.Confidence,
		}
		if f.ExpiresInDays > 0 {
			entry.ExpiresAt = now.Add(time.Duration(f.ExpiresInDays) * 24 * time.Hour)
		}
		if e.memory != nil {
			if err := e.memory.addKnowledge(ctx, entry); err != nil {
				return nil, fmt.Errorf("store extracted fact: %w", err)
			}
		}
		result.ExtractedFacts = append(result.ExtractedFacts, entry)
	}

	// Store decision entry
	if raw.DecisionSummary != nil {
		decision := &DecisionEntry{
			ObjectiveID: objective.ID,
			Decision:    raw.DecisionSummary.Decision,
			Reasoning:   raw.DecisionSummary.Reasoning,
			Outcome:     raw.DecisionSummary.Outcome,
		}
		if e.memory != nil {
			if err := e.memory.addDecision(ctx, decision); err != nil {
				return nil, fmt.Errorf("store decision: %w", err)
			}
		}
		result.Decision = decision
	}

	return result, nil
}

// buildEvaluationPrompt constructs the prompt for post-execution evaluation.
func buildEvaluationPrompt(objective *Objective, results map[string]json.RawMessage) string {
	var sb strings.Builder

	sb.WriteString("You are a post-execution evaluator for an autonomous objective management system.\n")
	sb.WriteString("Analyze the execution results and determine if the objective was achieved.\n\n")

	sb.WriteString("## Objective\n")
	fmt.Fprintf(&sb, "Title: %s\n", objective.Title)
	fmt.Fprintf(&sb, "Description: %s\n\n", objective.Description)

	sb.WriteString("## Execution Results\n")
	if len(results) == 0 {
		sb.WriteString("No results were produced.\n")
	} else {
		for nodeID, result := range results {
			fmt.Fprintf(&sb, "### Node: %s\n", nodeID)
			// Try to unmarshal as string for readability
			var text string
			if err := json.Unmarshal(result, &text); err == nil {
				fmt.Fprintf(&sb, "%s\n\n", text)
			} else {
				fmt.Fprintf(&sb, "%s\n\n", string(result))
			}
		}
	}

	sb.WriteString("## Instructions\n")
	sb.WriteString("1. Determine if the execution results are sufficient to consider the objective achieved.\n")
	sb.WriteString("2. Extract any useful facts discovered during execution (e.g., API endpoints, pricing, patterns).\n")
	sb.WriteString("   - Assign tags for categorization and a confidence score (0.0-1.0).\n")
	sb.WriteString("   - Set expires_in_days for time-sensitive facts (0 = never expires).\n")
	sb.WriteString("3. Summarize the key decision that was made and its outcome.\n")
	sb.WriteString("4. Recommend a replan level:\n")
	sb.WriteString("   - none: Objective achieved, no replanning needed.\n")
	sb.WriteString("   - reactive: Minor adjustment to the current task (retry with tweaks).\n")
	sb.WriteString("   - tactical: Re-decompose this objective into different tasks.\n")
	sb.WriteString("   - strategic: Rethink the entire objective tree from the mission level.\n")

	return sb.String()
}
