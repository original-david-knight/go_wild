package main

import (
	"context"
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
)

func TestListAgentDynamicTools_DeepResearch(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()

	agentID := "agent-dynamic-tools-dr"
	svc := data.NewAgentService(db, agentID)
	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent: %v", err)
	}

	managerSvc := NewAgentService(db)
	if _, err := managerSvc.CreateDeepResearchMethod(
		ctx,
		"deep_research_report",
		"Generate deep research report",
		"",
		"Research {{topic}}",
		`{"type":"object","required":["topic"],"properties":{"topic":{"type":"string","description":"Topic to research"}}}`,
		`{"type":"object","properties":{"report":{"type":"string"}}}`,
		`{"max_depth":3}`,
		true,
	); err != nil {
		t.Fatalf("CreateDeepResearchMethod: %v", err)
	}
	if _, err := managerSvc.CreateDeepResearchMethod(
		ctx,
		"deep_research_answer",
		"Quick research answer",
		"",
		"Answer {{question}}",
		`{"type":"object","required":["question"],"properties":{"question":{"type":"string"}}}`,
		`{}`,
		`{}`,
		true,
	); err != nil {
		t.Fatalf("CreateDeepResearchMethod(answer): %v", err)
	}
	if _, err := managerSvc.CreateDeepResearchMethod(
		ctx,
		"deep_research_disabled",
		"Disabled method",
		"",
		"",
		`{}`,
		`{}`,
		`{}`,
		false,
	); err != nil {
		t.Fatalf("CreateDeepResearchMethod(disabled): %v", err)
	}

	// Enable only deep_research_report for this agent (not deep_research_answer).
	agent, err := svc.GetAgent(ctx)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	agent.EnabledToolsJSON = `["deep_research_report"]`
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}

	tools := h.listAgentDynamicTools(ctx, agentID)

	var reportFound, answerFound, disabledFound bool
	for _, tool := range tools {
		switch tool.Name {
		case "deep_research_report":
			reportFound = true
			if tool.Route != "broker" {
				t.Errorf("expected route=broker, got %q", tool.Route)
			}
			if tool.Description != "Generate deep research report" {
				t.Errorf("expected description, got %q", tool.Description)
			}
			if tool.InputSchema == nil {
				t.Error("expected input_schema to be non-nil")
			}
		case "deep_research_answer":
			answerFound = true
		case "deep_research_disabled":
			disabledFound = true
		}
	}
	if !reportFound {
		t.Error("expected deep_research_report in dynamic tools")
	}
	if answerFound {
		t.Error("deep_research_answer should not appear (not in agent's enabled tools)")
	}
	if disabledFound {
		t.Error("disabled deep research method should not appear")
	}
}

func TestListAgentDynamicTools_NoMCPHost(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	// Replace mcpHost with nil to simulate unavailable manager.
	h.mcpHost = nil
	ctx := context.Background()

	agentID := "agent-no-mcp"
	svc := data.NewAgentService(db, agentID)
	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent: %v", err)
	}

	// Should not panic when mcpHost is nil.
	tools := h.listAgentDynamicTools(ctx, agentID)
	// May have deep research tools but no MCP tools.
	for _, tool := range tools {
		if tool.Route == "mcp" {
			t.Errorf("should not have MCP-routed tools when mcpHost is nil, got %q", tool.Name)
		}
	}
}

func TestNormalizeToolPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Reuters News", "reuters_news"},
		{"deep-research", "deep_research"},
		{"MyServer", "myserver"},
		{"foo__bar", "foo_bar"},
		{"  spaces  ", "spaces"},
		{"UPPER_CASE", "upper_case"},
	}
	for _, tt := range tests {
		got := normalizeToolPrefix(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeToolPrefix(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
