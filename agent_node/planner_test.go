package agentnode

import (
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/genai"
)

func TestPlannerResponseSchemaShape(t *testing.T) {
	s := plannerResponseSchema()
	if s == nil {
		t.Fatal("plannerResponseSchema() returned nil")
	}
	if s.Type != genai.TypeObject {
		t.Fatalf("schema type = %q, want %q", s.Type, genai.TypeObject)
	}
	if len(s.Required) != 2 {
		t.Fatalf("required fields = %v, want [graph reasoning]", s.Required)
	}
	if s.Properties["graph"] == nil {
		t.Fatal("schema missing graph property")
	}
	if s.Properties["reasoning"] == nil {
		t.Fatal("schema missing reasoning property")
	}

	nodeType := s.Properties["graph"].Properties["nodes"].Items.Properties["type"]
	if nodeType == nil {
		t.Fatal("schema missing node type property")
	}
	if len(nodeType.Enum) != 3 {
		t.Fatalf("node type enum = %v, want 3 values", nodeType.Enum)
	}
}

func TestBuildPlannerPrompt_IncludesRoundToolsAndState(t *testing.T) {
	state := map[string]json.RawMessage{
		"research-node": json.RawMessage(`{"key":"value"}`),
	}
	req := PlanRequest{
		UserPrompt:     "Compare A and B.",
		CurrentState:   state,
		Round:          2,
		AvailableTools: "Available tools:\n- read_webpage: fetch page content",
	}

	prompt := buildPlannerPrompt(req)
	if !strings.Contains(prompt, "Round: 2") {
		t.Fatalf("prompt missing round number: %q", prompt)
	}
	if !strings.Contains(prompt, "Compare A and B.") {
		t.Fatalf("prompt missing user prompt: %q", prompt)
	}
	if !strings.Contains(prompt, "read_webpage") {
		t.Fatalf("prompt missing tool catalog: %q", prompt)
	}
	if !strings.Contains(prompt, "research-node") {
		t.Fatalf("prompt missing prior state key: %q", prompt)
	}
	// Freshness rules were removed; just verify the prompt was generated.
}
