package objectives_planner

import (
	"google.golang.org/genai"
)

// tacticalPlanSchema returns the Gemini structured output schema for tactical planning.
// The planner produces a list of tree mutations and an execution graph.
func tacticalPlanSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"reasoning": {
				Type:        genai.TypeString,
				Description: "Explanation of the planning strategy and decomposition rationale",
			},
			"mutations": {
				Type:        genai.TypeArray,
				Description: "Tree mutations to apply to the objective tree",
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"action": {
							Type:        genai.TypeString,
							Description: "The mutation type",
							Enum:        []string{"add", "remove", "update", "move"},
						},
						"objective_id": {
							Type:        genai.TypeString,
							Description: "ID of the objective to modify (for update/remove/move)",
						},
						"parent_id": {
							Type:        genai.TypeString,
							Description: "Parent objective ID (for add/move)",
						},
						"title": {
							Type:        genai.TypeString,
							Description: "Short title for the objective",
						},
						"description": {
							Type:        genai.TypeString,
							Description: "Detailed description with acceptance criteria",
						},
						"status": {
							Type:        genai.TypeString,
							Description: "Objective status",
							Enum:        []string{"pending", "active", "blocked", "completed", "failed", "paused"},
						},
						"priority": {
							Type:        genai.TypeInteger,
							Description: "Priority among siblings (lower = higher priority)",
						},
						"tool_allowlist": {
							Type:        genai.TypeArray,
							Items:       &genai.Schema{Type: genai.TypeString},
							Description: "Tools this objective and its children can use",
						},
					},
					Required: []string{"action"},
				},
			},
			"execution_nodes": {
				Type:        genai.TypeArray,
				Description: "DAG nodes to execute for leaf tasks. These are passed to gowild_agent_node.",
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"id": {
							Type:        genai.TypeString,
							Description: "Unique node identifier",
						},
						"depends_on": {
							Type:        genai.TypeArray,
							Items:       &genai.Schema{Type: genai.TypeString},
							Description: "IDs of nodes this depends on",
						},
						"prompt": {
							Type:        genai.TypeString,
							Description: "The instruction for this execution node",
						},
						"system_prompt": {
							Type:        genai.TypeString,
							Description: "Optional system prompt",
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
							Description: "Execution mode",
							Enum:        []string{"single_shot", "agentic", "deep_research"},
						},
					},
					Required: []string{"id", "prompt"},
				},
			},
			"clarifying_questions": {
				Type:        genai.TypeArray,
				Description: "Questions to ask the human when the mission or objective is too vague to decompose. If questions are present, mutations should be empty.",
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"question": {
							Type:        genai.TypeString,
							Description: "The specific question to ask",
						},
						"context": {
							Type:        genai.TypeString,
							Description: "Why this question matters for planning",
						},
					},
					Required: []string{"question"},
				},
			},
		},
		Required: []string{"reasoning"},
	}
}
