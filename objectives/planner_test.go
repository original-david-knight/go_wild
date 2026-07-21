package objectives

import (
	"strings"
	"testing"
)

func TestTacticalPlanSchema(t *testing.T) {
	schema := tacticalPlanSchema()

	if schema.Type != "OBJECT" {
		t.Fatalf("expected OBJECT type, got %s", schema.Type)
	}

	// Check required fields
	required := map[string]bool{}
	for _, r := range schema.Required {
		required[r] = true
	}
	if !required["reasoning"] {
		t.Error("reasoning should be required")
	}

	// mutations is optional — planner may return only clarifying_questions
	if _, ok := schema.Properties["clarifying_questions"]; !ok {
		t.Error("missing clarifying_questions property")
	}

	// Check mutations schema
	mutations, ok := schema.Properties["mutations"]
	if !ok {
		t.Fatal("missing mutations property")
	}
	if mutations.Type != "ARRAY" {
		t.Fatalf("expected mutations to be ARRAY, got %s", mutations.Type)
	}

	// Check mutation item has action enum
	action, ok := mutations.Items.Properties["action"]
	if !ok {
		t.Fatal("missing action property in mutation")
	}
	if len(action.Enum) != 4 {
		t.Fatalf("expected 4 action enum values, got %d", len(action.Enum))
	}

	// Check execution_nodes schema
	execNodes, ok := schema.Properties["execution_nodes"]
	if !ok {
		t.Fatal("missing execution_nodes property")
	}
	if execNodes.Type != "ARRAY" {
		t.Fatalf("expected execution_nodes to be ARRAY, got %s", execNodes.Type)
	}
}

func TestBuildTacticalPrompt(t *testing.T) {
	obj := &Objective{
		ID:          "test-123",
		Title:       "Audit product listings",
		Description: "Review all product listings for quality",
		Status:      StatusActive,
		Depth:       2,
	}

	children := []*Objective{
		{ID: "child-1", Title: "Scrape competitors", Status: StatusPending, Priority: 1},
		{ID: "child-2", Title: "Analyze data", Status: StatusPending, Priority: 2},
	}

	prompt := buildTacticalPrompt(obj, children, "Available tools:\n- web_search\n- http_request\n", "")

	if !strings.Contains(prompt, "tactical planner") {
		t.Error("prompt should mention tactical planner")
	}
	if !strings.Contains(prompt, "test-123") {
		t.Error("prompt should contain objective ID")
	}
	if !strings.Contains(prompt, "Audit product listings") {
		t.Error("prompt should contain objective title")
	}
	if !strings.Contains(prompt, "Scrape competitors") {
		t.Error("prompt should contain child titles")
	}
	if !strings.Contains(prompt, "web_search") {
		t.Error("prompt should contain tool catalog")
	}
}

func TestBuildStrategicPrompt(t *testing.T) {
	tree := []*Objective{
		{ID: "root", Title: "Run a store", Status: StatusActive, Depth: 0},
		{ID: "obj-1", Title: "Optimize catalog", Status: StatusPending, Depth: 1},
	}

	prompt := buildStrategicPrompt("Run a profitable Shopify store", tree, "", "")

	if !strings.Contains(prompt, "strategic planner") {
		t.Error("prompt should mention strategic planner")
	}
	if !strings.Contains(prompt, "Run a profitable Shopify store") {
		t.Error("prompt should contain mission")
	}
	if !strings.Contains(prompt, "Run a store") {
		t.Error("prompt should contain existing tree")
	}
	if !strings.Contains(prompt, "Optimize catalog") {
		t.Error("prompt should contain tree children")
	}
}
