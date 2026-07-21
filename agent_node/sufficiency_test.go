package agentnode

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSufficiencyResponseSchemaShape(t *testing.T) {
	s := sufficiencyResponseSchema()
	if s == nil {
		t.Fatal("sufficiencyResponseSchema() returned nil")
	}
	if len(s.Required) != 2 {
		t.Fatalf("required fields = %v, want [sufficient reasoning]", s.Required)
	}
	if s.Properties["sufficient"] == nil {
		t.Fatal("schema missing sufficient property")
	}
	if s.Properties["reasoning"] == nil {
		t.Fatal("schema missing reasoning property")
	}
}

func TestBuildSufficiencyPrompt_WithState(t *testing.T) {
	req := SufficiencyRequest{
		UserPrompt: "Summarize the key findings.",
		CurrentState: map[string]json.RawMessage{
			"summary": json.RawMessage(`"final answer"`),
		},
		Round: 1,
	}

	prompt := buildSufficiencyPrompt(req)
	if !strings.Contains(prompt, "Round: 1") {
		t.Fatalf("prompt missing round number: %q", prompt)
	}
	if !strings.Contains(prompt, "Summarize the key findings.") {
		t.Fatalf("prompt missing user prompt: %q", prompt)
	}
	if !strings.Contains(prompt, "summary") {
		t.Fatalf("prompt missing state entry: %q", prompt)
	}
	if !strings.Contains(prompt, "Recency policy") {
		t.Fatalf("prompt missing recency policy: %q", prompt)
	}
}

func TestBuildSufficiencyPrompt_NoState(t *testing.T) {
	req := SufficiencyRequest{
		UserPrompt:   "Anything else needed?",
		CurrentState: nil,
		Round:        0,
	}

	prompt := buildSufficiencyPrompt(req)
	if !strings.Contains(prompt, "Final node outputs: (none)") {
		t.Fatalf("prompt should include empty-state marker: %q", prompt)
	}
}
