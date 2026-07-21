package agentnode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"google.golang.org/genai"
)

// PlanRequest is the input to the planner.
type PlanRequest struct {
	UserPrompt     string                     `json:"user_prompt"`
	CurrentState   map[string]json.RawMessage `json:"current_state,omitempty"`
	Round          int                        `json:"round"`
	AvailableTools string                     `json:"-"` // tool catalog text for the planner prompt
}

// PlanResult is the planner's output: a node graph and reasoning.
type PlanResult struct {
	Graph     NodeGraph `json:"graph"`
	Reasoning string    `json:"reasoning"`
}

// Planner decomposes a user prompt into a node graph.
type Planner interface {
	Plan(ctx context.Context, req PlanRequest) (*PlanResult, error)
}

// GeminiPlanner uses Gemini structured output to generate node graphs.
type GeminiPlanner struct {
	client *genai.Client
	model  string
}

// NewGeminiPlanner creates a planner backed by the Gemini API.
func NewGeminiPlanner(apiKey, model string) (*GeminiPlanner, error) {
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
		return nil, fmt.Errorf("failed to create planner client: %w", err)
	}

	return &GeminiPlanner{client: client, model: model}, nil
}

func (p *GeminiPlanner) Plan(ctx context.Context, req PlanRequest) (*PlanResult, error) {
	prompt := buildPlannerPrompt(req)
	temp := float32(0.2)

	resp, err := p.client.Models.GenerateContent(ctx, p.model, genai.Text(prompt), &genai.GenerateContentConfig{
		Temperature:      &temp,
		MaxOutputTokens:  4096,
		ResponseMIMEType: "application/json",
		ResponseSchema:   plannerResponseSchema(),
	})
	if err != nil {
		return nil, fmt.Errorf("planner generation error: %w", err)
	}

	text := extractCandidateText(resp)
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("planner returned empty response")
	}

	var result PlanResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("planner returned invalid JSON: %w", err)
	}

	return &result, nil
}

func plannerResponseSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"graph": {
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"nodes": {
						Type: genai.TypeArray,
						Items: &genai.Schema{
							Type: genai.TypeObject,
							Properties: map[string]*genai.Schema{
								"id": {
									Type:        genai.TypeString,
									Description: "Unique identifier for this node",
								},
								"depends_on": {
									Type:        genai.TypeArray,
									Items:       &genai.Schema{Type: genai.TypeString},
									Description: "IDs of nodes this node depends on",
								},
								"prompt": {
									Type:        genai.TypeString,
									Description: "The prompt/instruction for this node",
								},
								"system_prompt": {
									Type:        genai.TypeString,
									Description: "Optional system prompt for the node",
								},
								"model": {
									Type:        genai.TypeString,
									Description: "Optional model override",
								},
								"tools": {
									Type:        genai.TypeArray,
									Items:       &genai.Schema{Type: genai.TypeString},
									Description: "Tool names for agentic execution",
								},
								"max_turns": {
									Type:        genai.TypeInteger,
									Description: "Max turns for agentic nodes",
								},
								"type": {
									Type:        genai.TypeString,
									Description: "Execution mode: single_shot (default), agentic (multi-turn with tools), or deep_research (iterative web research engine)",
									Enum:        []string{"single_shot", "agentic", "deep_research"},
								},
								"research_config": {
									Type:        genai.TypeObject,
									Description: "Options for deep_research nodes",
									Properties: map[string]*genai.Schema{
										"max_depth": {
											Type:        genai.TypeInteger,
											Description: "Max research depth/rounds (default: 2)",
										},
										"objectives": {
											Type:        genai.TypeArray,
											Items:       &genai.Schema{Type: genai.TypeString},
											Description: "Research objective keys",
										},
										"guidance": {
											Type:        genai.TypeString,
											Description: "Additional research context or constraints",
										},
										"timeout_seconds": {
											Type:        genai.TypeInteger,
											Description: "Max time for research (default: 300)",
										},
									},
								},
							},
							Required: []string{"id", "prompt"},
						},
					},
				},
				Required: []string{"nodes"},
			},
			"reasoning": {
				Type:        genai.TypeString,
				Description: "Explanation of the decomposition strategy",
			},
		},
		Required: []string{"graph", "reasoning"},
	}
}

func buildPlannerPrompt(req PlanRequest) string {
	var sb strings.Builder
	sb.WriteString("You are a task decomposition planner. Given a user's request, decompose it into a directed acyclic graph (DAG) of work nodes.\n\n")
	sb.WriteString("Each node should be a focused, self-contained unit of work with a clear prompt.\n")
	sb.WriteString("Nodes can depend on other nodes — a node's prompt will be augmented with the outputs of its dependencies.\n")
	sb.WriteString("Independent nodes will execute in parallel.\n\n")

	// Node type guidelines
	sb.WriteString("Node types (set via the \"type\" field):\n")
	sb.WriteString("- \"single_shot\" (default): One LLM call. For analysis, synthesis, reasoning over existing data.\n")
	sb.WriteString("- \"agentic\": Multi-turn with tools. For targeted lookups, API calls, specific URL fetching.\n")
	sb.WriteString("- \"deep_research\": Iterative research engine with built-in search, fetch, and completeness checking.\n")
	sb.WriteString("  Best for comprehensive, multi-source research. Does NOT use the tools registry.\n\n")
	sb.WriteString("Use deep_research when you need thorough web research on a topic.\n")
	sb.WriteString("Use agentic when you need specific tool interactions.\n")
	sb.WriteString("Use single_shot for reasoning/analysis over available data.\n\n")

	// Tool catalog
	if req.AvailableTools != "" {
		sb.WriteString(req.AvailableTools)
		sb.WriteString("\n")
		sb.WriteString("Agentic nodes can use the tools listed above. Deep research nodes have built-in search and do not need tools.\n\n")
	}

	sb.WriteString("Rules:\n")
	sb.WriteString("- Each node ID must be unique and descriptive (e.g., \"research-topic\", \"analyze-data\", \"synthesize-report\")\n")
	sb.WriteString("- Dependencies must reference valid node IDs\n")
	sb.WriteString("- No cycles allowed\n")
	sb.WriteString("- Prefer parallel execution where possible\n")
	sb.WriteString("- Each node's prompt should be self-contained enough that an LLM can execute it\n")
	sb.WriteString("- Only assign tool names from the available tools list above\n\n")

	fmt.Fprintf(&sb, "Round: %d\n\n", req.Round)
	fmt.Fprintf(&sb, "User request:\n%s\n", req.UserPrompt)

	if len(req.CurrentState) > 0 {
		stateJSON, _ := json.MarshalIndent(req.CurrentState, "", "  ")
		fmt.Fprintf(&sb, "\nResults from previous rounds:\n%s\n", string(stateJSON))
	}

	return sb.String()
}
