package objectives_planner

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEvaluationSchema(t *testing.T) {
	schema := evaluationSchema()

	if schema.Type != "OBJECT" {
		t.Fatalf("expected OBJECT type, got %s", schema.Type)
	}

	// Check required fields
	required := map[string]bool{}
	for _, r := range schema.Required {
		required[r] = true
	}
	if !required["sufficient"] {
		t.Error("sufficient should be required")
	}
	if !required["reasoning"] {
		t.Error("reasoning should be required")
	}
	if !required["replan_level"] {
		t.Error("replan_level should be required")
	}

	// Check sufficient is boolean
	sufficient, ok := schema.Properties["sufficient"]
	if !ok {
		t.Fatal("missing sufficient property")
	}
	if sufficient.Type != "BOOLEAN" {
		t.Fatalf("expected sufficient to be BOOLEAN, got %s", sufficient.Type)
	}

	// Check extracted_facts is array of objects
	facts, ok := schema.Properties["extracted_facts"]
	if !ok {
		t.Fatal("missing extracted_facts property")
	}
	if facts.Type != "ARRAY" {
		t.Fatalf("expected extracted_facts to be ARRAY, got %s", facts.Type)
	}
	if facts.Items == nil {
		t.Fatal("extracted_facts items schema is nil")
	}
	factProps := facts.Items.Properties
	for _, field := range []string{"fact", "tags", "confidence"} {
		if _, ok := factProps[field]; !ok {
			t.Errorf("missing %s in extracted_facts item", field)
		}
	}
	// Check expires_in_days exists
	if _, ok := factProps["expires_in_days"]; !ok {
		t.Error("missing expires_in_days in extracted_facts item")
	}

	// Check decision_summary
	decision, ok := schema.Properties["decision_summary"]
	if !ok {
		t.Fatal("missing decision_summary property")
	}
	if decision.Type != "OBJECT" {
		t.Fatalf("expected decision_summary to be OBJECT, got %s", decision.Type)
	}
	for _, field := range []string{"decision", "reasoning", "outcome"} {
		if _, ok := decision.Properties[field]; !ok {
			t.Errorf("missing %s in decision_summary", field)
		}
	}

	// Check replan_level enum
	replan, ok := schema.Properties["replan_level"]
	if !ok {
		t.Fatal("missing replan_level property")
	}
	if len(replan.Enum) != 4 {
		t.Fatalf("expected 4 replan_level enum values, got %d", len(replan.Enum))
	}
	expectedEnums := map[string]bool{"none": true, "reactive": true, "tactical": true, "strategic": true}
	for _, e := range replan.Enum {
		if !expectedEnums[e] {
			t.Errorf("unexpected replan_level enum value: %s", e)
		}
	}
}

func TestBuildEvaluationPrompt(t *testing.T) {
	obj := &Objective{
		ID:          "eval-obj-1",
		Title:       "Scrape product listings",
		Description: "Scrape top 10 product listings from competitor site",
	}

	results := map[string]json.RawMessage{
		"scrape-node": json.RawMessage(`"Found 8 product listings with prices ranging from $5 to $50"`),
		"parse-node":  json.RawMessage(`{"products": 8, "status": "partial"}`),
	}

	prompt := buildEvaluationPrompt(obj, results)

	if !strings.Contains(prompt, "post-execution evaluator") {
		t.Error("prompt should mention post-execution evaluator")
	}
	if !strings.Contains(prompt, "Scrape product listings") {
		t.Error("prompt should contain objective title")
	}
	if !strings.Contains(prompt, "Scrape top 10 product listings") {
		t.Error("prompt should contain objective description")
	}
	if !strings.Contains(prompt, "scrape-node") {
		t.Error("prompt should contain node IDs")
	}
	if !strings.Contains(prompt, "parse-node") {
		t.Error("prompt should contain all node IDs")
	}
	if !strings.Contains(prompt, "replan level") {
		t.Error("prompt should mention replan levels")
	}
}

func TestBuildEvaluationPromptEmpty(t *testing.T) {
	obj := &Objective{
		Title:       "Empty task",
		Description: "Task with no results",
	}

	prompt := buildEvaluationPrompt(obj, nil)

	if !strings.Contains(prompt, "No results were produced") {
		t.Error("prompt should indicate no results when results are empty")
	}
}
