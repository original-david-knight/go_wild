package agentnode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"google.golang.org/genai"
)

// OrchestratorConfig configures the plan-execute-check loop.
type OrchestratorConfig struct {
	Planner     Planner
	Checker     SufficiencyChecker
	Executor    *GraphExecutor
	MaxRounds   int // default: 7
	Events      chan<- GraphEvent
	ToolCatalog string // tool descriptions for the planner prompt
	APIKey      string // for synthesis client; falls back to GEMINI_API_KEY
	SynthModel  string // model for answer synthesis; defaults to executor's model
}

// OrchestratorResult is the final output of the orchestrator.
type OrchestratorResult struct {
	Answer    string                     `json:"answer,omitempty"`
	State     map[string]json.RawMessage `json:"state"`
	Rounds    int                        `json:"rounds"`
	NodeCount int                        `json:"node_count"`
}

// Orchestrator runs the plan → execute → check loop.
type Orchestrator struct {
	config OrchestratorConfig
	client *genai.Client // shared client for answer synthesis
	model  string
}

// NewOrchestrator creates a new orchestrator.
func NewOrchestrator(ctx context.Context, config OrchestratorConfig) (*Orchestrator, error) {
	if config.Planner == nil {
		return nil, fmt.Errorf("planner is required")
	}
	if config.Checker == nil {
		return nil, fmt.Errorf("checker is required")
	}
	if config.Executor == nil {
		return nil, fmt.Errorf("executor is required")
	}
	if config.MaxRounds <= 0 {
		config.MaxRounds = 7
	}
	apiKey := config.APIKey
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	}
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY not set (needed for answer synthesis)")
	}
	model := config.SynthModel
	if model == "" {
		model = config.Executor.config.DefaultModel
	}
	if model == "" {
		model = "gemini-3-flash-preview"
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("failed to create synthesis client: %w", err)
	}

	return &Orchestrator{config: config, client: client, model: model}, nil
}

// Run executes the full orchestration loop for the given user prompt.
func (o *Orchestrator) Run(ctx context.Context, userPrompt string) (*OrchestratorResult, error) {
	state := NewSharedState()
	totalNodes := 0

	for round := 0; round < o.config.MaxRounds; round++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Plan
		planResult, err := o.config.Planner.Plan(ctx, PlanRequest{
			UserPrompt:     userPrompt,
			CurrentState:   state.Snapshot(),
			Round:          round,
			AvailableTools: o.config.ToolCatalog,
		})
		if err != nil {
			return nil, fmt.Errorf("planning failed (round %d): %w", round, err)
		}
		if planResult == nil {
			return nil, fmt.Errorf("planning failed (round %d): planner returned nil result", round)
		}
		if o.config.Events != nil {
			o.config.Events <- PlanEvent{Round: round, Graph: &planResult.Graph}
		}

		if len(planResult.Graph.Nodes) == 0 {
			break
		}

		// Execute
		totalNodes += len(planResult.Graph.Nodes)
		if err := o.config.Executor.Execute(ctx, &planResult.Graph, state); err != nil {
			return nil, fmt.Errorf("execution failed (round %d): %w", round, err)
		}

		// Check sufficiency using only leaf node outputs (terminal nodes that
		// already incorporate upstream results). This keeps the checker's context
		// small even when intermediate research nodes produce huge outputs.
		leafIDs := planResult.Graph.leafNodeIDs()
		checkResult, err := o.config.Checker.Check(ctx, SufficiencyRequest{
			UserPrompt:   userPrompt,
			CurrentState: state.snapshotOnly(leafIDs),
			Round:        round,
		})
		if err != nil {
			if o.config.Events != nil {
				o.config.Events <- SufficiencyCheckEvent{
					Round:      round,
					Sufficient: false,
					Reasoning:  fmt.Sprintf("checker error: %v", err),
				}
			}
			continue
		}
		if checkResult == nil {
			if o.config.Events != nil {
				o.config.Events <- SufficiencyCheckEvent{
					Round:      round,
					Sufficient: false,
					Reasoning:  "checker returned nil result",
				}
			}
			continue
		}

		if o.config.Events != nil {
			o.config.Events <- SufficiencyCheckEvent{
				Round:      round,
				Sufficient: checkResult.Sufficient,
				Reasoning:  checkResult.Reasoning,
			}
		}

		if checkResult.Sufficient {
			result := &OrchestratorResult{
				State:     state.Snapshot(),
				Rounds:    round + 1,
				NodeCount: totalNodes,
			}
			leafState := state.snapshotOnly(planResult.Graph.leafNodeIDs())
			result.Answer = o.synthesizeAnswer(ctx, userPrompt, leafState)
			return result, nil
		}
	}

	result := &OrchestratorResult{
		State:     state.Snapshot(),
		Rounds:    o.config.MaxRounds,
		NodeCount: totalNodes,
	}
	result.Answer = o.synthesizeAnswer(ctx, userPrompt, state.Snapshot())
	return result, nil
}

// synthesizeAnswer makes a final Gemini call to produce a coherent answer
// from the leaf node outputs. Falls back to concatenation on failure.
func (o *Orchestrator) synthesizeAnswer(ctx context.Context, userPrompt string, leafState map[string]json.RawMessage) string {
	if len(leafState) == 0 {
		return ""
	}

	stateJSON, _ := json.MarshalIndent(leafState, "", "  ")

	prompt := fmt.Sprintf(`You are a research synthesizer. The user asked the following question:

%s

Below are the outputs from several research/analysis nodes that worked on parts of this question. Synthesize them into a single coherent, well-structured answer that directly addresses the user's question. Do not mention "nodes" or the research process — just provide the final answer.

Node outputs:
%s`, userPrompt, string(stateJSON))

	resp, err := o.client.Models.GenerateContent(ctx, o.model, genai.Text(prompt), &genai.GenerateContentConfig{
		MaxOutputTokens: 8192,
	})
	if err != nil {
		return concatenateLeafOutputs(leafState)
	}

	text := extractCandidateText(resp)
	if strings.TrimSpace(text) == "" {
		return concatenateLeafOutputs(leafState)
	}
	return text
}

// concatenateLeafOutputs is the fallback when synthesis fails.
func concatenateLeafOutputs(leafState map[string]json.RawMessage) string {
	var parts []string
	for id, raw := range leafState {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			parts = append(parts, fmt.Sprintf("## %s\n%s", id, s))
		} else {
			parts = append(parts, fmt.Sprintf("## %s\n%s", id, string(raw)))
		}
	}
	return strings.Join(parts, "\n\n")
}
