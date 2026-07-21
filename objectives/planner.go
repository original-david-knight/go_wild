package objectives

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	agentnode "github.com/original-david-knight/go_wild/agent_node"
	"google.golang.org/genai"
)

// StrategicPlanner decomposes objectives into sub-goals and executable tasks
// using Gemini structured output.
type StrategicPlanner struct {
	client *genai.Client
	model  string
}

// NewStrategicPlanner creates a planner backed by the Gemini API.
func NewStrategicPlanner(apiKey, model string) (*StrategicPlanner, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key required for strategic planner")
	}
	if model == "" {
		model = "gemini-3-flash-preview"
	}

	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("create planner client: %w", err)
	}

	return &StrategicPlanner{client: client, model: model}, nil
}

// PlanTactical decomposes a single objective into tasks and produces an execution graph.
// priorContext is optional text summarizing previous execution results for this objective.
// validToolNames constrains which tool names may appear in execution nodes.
func (p *StrategicPlanner) PlanTactical(ctx context.Context, objective *Objective, children []*Objective, toolCatalog string, priorContext string, validToolNames map[string]bool) (*PlanOutput, *agentnode.NodeGraph, error) {
	prompt := buildTacticalPrompt(objective, children, toolCatalog, priorContext)
	return p.plan(ctx, prompt, validToolNames)
}

// PlanStrategic creates or restructures the full objective tree for a mission.
// memory is optional context from resolved escalations or prior planning cycles.
func (p *StrategicPlanner) PlanStrategic(ctx context.Context, mission string, tree []*Objective, toolCatalog string, memory string) (*PlanOutput, *agentnode.NodeGraph, error) {
	prompt := buildStrategicPrompt(mission, tree, toolCatalog, memory)
	return p.plan(ctx, prompt, nil)
}

// reviewMission evaluates whether a mission is complete or needs more work.
// Returns mutations for remaining work, or empty mutations if the mission is done.
func (p *StrategicPlanner) reviewMission(ctx context.Context, mission string, tree []*Objective, toolCatalog string, memory string) (*PlanOutput, error) {
	prompt := buildReviewPrompt(mission, tree, toolCatalog, memory)
	output, _, err := p.plan(ctx, prompt, nil)
	return output, err
}

func (p *StrategicPlanner) plan(ctx context.Context, prompt string, validToolNames map[string]bool) (*PlanOutput, *agentnode.NodeGraph, error) {
	const maxRetries = 2
	temp := float32(0.2)

	conversation := []*genai.Content{
		{Role: "user", Parts: []*genai.Part{{Text: prompt}}},
	}

	for attempt := range maxRetries + 1 {
		resp, err := p.client.Models.GenerateContent(ctx, p.model, conversation, &genai.GenerateContentConfig{
			Temperature:      &temp,
			MaxOutputTokens:  8192,
			ResponseMIMEType: "application/json",
			ResponseSchema:   tacticalPlanSchema(),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("planner generation error: %w", err)
		}

		text := extractText(resp)
		if strings.TrimSpace(text) == "" {
			return nil, nil, fmt.Errorf("planner returned empty response")
		}

		output, graph, parseErr := parsePlanResponse(text)
		if parseErr != nil {
			return nil, nil, parseErr
		}

		// Validate graph structure if present
		if graph != nil {
			if validErr := graph.Validate(); validErr != nil {
				if attempt < maxRetries {
					log.Printf("[planner] graph validation failed (attempt %d): %v — retrying", attempt+1, validErr)
					conversation = append(conversation,
						&genai.Content{Role: "model", Parts: []*genai.Part{{Text: text}}},
						&genai.Content{Role: "user", Parts: []*genai.Part{{Text: fmt.Sprintf(
							"ERROR: Your execution graph is invalid: %v. Every node must have a unique ID. Fix the graph and return the complete corrected JSON.", validErr)}}},
					)
					continue
				}
				return nil, nil, fmt.Errorf("planner produced invalid graph after %d attempts: %w", maxRetries+1, validErr)
			}

			// Validate tool names if a valid set was provided
			if len(validToolNames) > 0 {
				if unknownErr := validateGraphToolNames(graph, validToolNames); unknownErr != nil {
					if attempt < maxRetries {
						log.Printf("[planner] unknown tools in graph (attempt %d): %v — retrying", attempt+1, unknownErr)
						conversation = append(conversation,
							&genai.Content{Role: "model", Parts: []*genai.Part{{Text: text}}},
							&genai.Content{Role: "user", Parts: []*genai.Part{{Text: unknownErr.Error()}}},
						)
						continue
					}
					return nil, nil, fmt.Errorf("planner used unknown tools after %d attempts: %w", maxRetries+1, unknownErr)
				}
			}
		}

		return output, graph, nil
	}

	return nil, nil, fmt.Errorf("planner retry loop exhausted")
}

// parsePlanResponse extracts the PlanOutput and optional NodeGraph from raw JSON.
func parsePlanResponse(text string) (*PlanOutput, *agentnode.NodeGraph, error) {
	var raw struct {
		Reasoning           string               `json:"reasoning"`
		Mutations           []TreeMutation        `json:"mutations"`
		ExecutionNodes      []json.RawMessage     `json:"execution_nodes"`
		ClarifyingQuestions []ClarifyingQuestion   `json:"clarifying_questions"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, nil, fmt.Errorf("planner returned invalid JSON: %w", err)
	}

	output := &PlanOutput{
		Reasoning:           raw.Reasoning,
		Mutations:           raw.Mutations,
		ClarifyingQuestions: raw.ClarifyingQuestions,
	}

	var graph *agentnode.NodeGraph
	if len(raw.ExecutionNodes) > 0 {
		var nodes []agentnode.NodeDef
		for _, rawNode := range raw.ExecutionNodes {
			var nd agentnode.NodeDef
			if err := json.Unmarshal(rawNode, &nd); err != nil {
				continue
			}
			nodes = append(nodes, nd)
		}
		if len(nodes) > 0 {
			graph = &agentnode.NodeGraph{Nodes: nodes}
		}
	}

	return output, graph, nil
}

// validateGraphToolNames checks that all tool names in execution nodes exist
// in the provided set. Returns an error describing invalid tools and listing valid ones.
func validateGraphToolNames(graph *agentnode.NodeGraph, valid map[string]bool) error {
	var unknown []string
	seen := map[string]bool{}
	for _, node := range graph.Nodes {
		for _, name := range node.ToolNames {
			if !valid[name] && !seen[name] {
				unknown = append(unknown, name)
				seen[name] = true
			}
		}
	}
	if len(unknown) == 0 {
		return nil
	}

	validList := make([]string, 0, len(valid))
	for name := range valid {
		validList = append(validList, name)
	}
	return fmt.Errorf("ERROR: Execution nodes reference unknown tools: %v. "+
		"You MUST use EXACT tool names from this list: %v. "+
		"Fix the tool names and return the complete corrected JSON.", unknown, validList)
}

// extractText pulls the text content from a Gemini response.
func extractText(resp *genai.GenerateContentResponse) string {
	if resp == nil || len(resp.Candidates) == 0 {
		return ""
	}
	c := resp.Candidates[0]
	if c == nil || c.Content == nil {
		return ""
	}
	var text string
	for _, part := range c.Content.Parts {
		if part.Thought {
			continue
		}
		if part.Text != "" {
			text += part.Text
		}
	}
	return text
}
