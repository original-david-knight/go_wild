package agentnode

import (
	"encoding/json"

	loop "github.com/original-david-knight/go_wild/agentic_loop"

	"google.golang.org/genai"
)

// NodeID uniquely identifies a node within a graph.
type NodeID string

// NodeStatus represents the execution state of a node.
type NodeStatus string

const (
	NodeDone    NodeStatus = "done"
	NodeFailed  NodeStatus = "failed"
	NodeSkipped NodeStatus = "skipped"
)

// NodeType selects the execution mode for a node.
type NodeType string

const (
	NodeTypeAuto       NodeType = ""              // infer from tools (backward compat)
	NodeTypeSingleShot NodeType = "single_shot"
	NodeTypeAgentic    NodeType = "agentic"
	NodeTypeResearch   NodeType = "deep_research"
)

// ResearchConfig holds options for deep_research nodes.
type ResearchConfig struct {
	MaxDepth       int      `json:"max_depth,omitempty"`       // default: 2
	Objectives     []string `json:"objectives,omitempty"`      // research objective keys
	Guidance       string   `json:"guidance,omitempty"`        // extra context
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"` // default: 300
}

// NodeDef defines a single work unit in a node graph.
type NodeDef struct {
	ID           NodeID          `json:"id"`
	Type         NodeType        `json:"type,omitempty"`
	DependsOn    []NodeID        `json:"depends_on,omitempty"`
	Prompt       string          `json:"prompt"`
	SystemPrompt string          `json:"system_prompt,omitempty"`
	Model        string          `json:"model,omitempty"`
	OutputSchema *genai.Schema   `json:"-"`                          // for single-shot structured output
	ToolNames    []string        `json:"tools,omitempty"`            // resolved at execution time
	MaxTurns     int             `json:"max_turns,omitempty"`        // for agentic nodes
	ResearchCfg  *ResearchConfig `json:"research_config,omitempty"`  // deep_research options
}

// ResolvedType returns the effective node type, inferring from tools when Type is empty.
func (n *NodeDef) ResolvedType() NodeType {
	if n.Type != NodeTypeAuto {
		return n.Type
	}
	if len(n.ToolNames) > 0 {
		return NodeTypeAgentic
	}
	return NodeTypeSingleShot
}

// NodeResult holds the output of a completed node.
type NodeResult struct {
	NodeID     NodeID          `json:"node_id"`
	Status     NodeStatus      `json:"status"`
	Output     json.RawMessage `json:"output,omitempty"` // structured JSON (single-shot)
	Text       string          `json:"text,omitempty"`   // free text (agentic)
	Error      string          `json:"error,omitempty"`
	Usage      loop.ModelUsage `json:"usage"`
	TurnCount  int             `json:"turn_count"`
	FullPrompt string          `json:"-"` // full prompt including parent context (not serialized)
}
