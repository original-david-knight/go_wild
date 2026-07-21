package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/original-david-knight/go_wild/agent_data"
)

func TestCallCompanyKnowledgeToolsUnknownToolIsUnhandled(t *testing.T) {
	h := NewBrokerToolsHandler(setupManagerTestDB(t))
	handled, result, err := h.callCompanyKnowledgeTools(context.Background(), "agent-unknown", "not_a_company_knowledge_tool", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Fatalf("expected handled=false, got true with result=%#v", result)
	}
	if result != nil {
		t.Fatalf("expected nil result for unhandled tool, got %#v", result)
	}
}

func TestIsCompanyKnowledgeToolRecognition(t *testing.T) {
	if !isCompanyKnowledgeTool("company_knowledge_search") {
		t.Fatalf("expected company_knowledge_search to be recognized")
	}
	if isCompanyKnowledgeTool("company_knowledge_not_real") {
		t.Fatalf("expected unknown company knowledge tool to be rejected")
	}
}

func TestBrokerCompanyKnowledgeToolsRequireMembership(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)

	ctx := context.Background()
	agentID := "agent-no-company"
	agentSvc := data.NewAgentService(db, agentID)
	if _, err := agentSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	searchInput, _ := json.Marshal(map[string]any{"query": "x"})
	_, err := h.callTool(ctx, agentID, agentSvc, "company_knowledge_search", searchInput)
	if err == nil {
		t.Fatalf("expected membership error")
	}
	if !strings.Contains(err.Error(), "require company membership") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBrokerCompanyKnowledgeToolsCRUD(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)

	ctx := context.Background()
	agentID := "agent-in-company"
	agentSvc := data.NewAgentService(db, agentID)
	if _, err := agentSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	company, err := data.CreateCompany(ctx, db, "acme", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, agentID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}

	addInput, _ := json.Marshal(map[string]any{
		"kind":     "policy",
		"title":    "Refund policy",
		"content":  "Refunds require receipt.",
		"tags":     []string{"refunds", "ops"},
		"metadata": map[string]any{"source": "playbook"},
	})
	addResult, err := h.callTool(ctx, agentID, agentSvc, "company_knowledge_add", addInput)
	if err != nil {
		t.Fatalf("company_knowledge_add failed: %v", err)
	}
	addMap, ok := addResult.(map[string]any)
	if !ok {
		t.Fatalf("unexpected add result type: %T", addResult)
	}
	entryID, _ := addMap["id"].(string)
	if strings.TrimSpace(entryID) == "" {
		t.Fatalf("expected entry id in add response")
	}
	if got, _ := addMap["company_id"].(string); got != company.ID {
		t.Fatalf("expected company_id %q, got %q", company.ID, got)
	}

	searchInput, _ := json.Marshal(map[string]any{
		"query": "receipt",
		"kind":  "policy",
		"limit": 5,
	})
	searchResult, err := h.callTool(ctx, agentID, agentSvc, "company_knowledge_search", searchInput)
	if err != nil {
		t.Fatalf("company_knowledge_search failed: %v", err)
	}
	searchMap, ok := searchResult.(map[string]any)
	if !ok {
		t.Fatalf("unexpected search result type: %T", searchResult)
	}
	entries, ok := searchMap["entries"].([]map[string]any)
	if !ok {
		raw, ok := searchMap["entries"].([]any)
		if !ok {
			t.Fatalf("unexpected entries type: %T", searchMap["entries"])
		}
		entries = make([]map[string]any, 0, len(raw))
		for _, v := range raw {
			m, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("unexpected entry row type: %T", v)
			}
			entries = append(entries, m)
		}
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(entries))
	}

	getInput, _ := json.Marshal(map[string]any{"entry_id": entryID})
	getResult, err := h.callTool(ctx, agentID, agentSvc, "company_knowledge_get", getInput)
	if err != nil {
		t.Fatalf("company_knowledge_get failed: %v", err)
	}
	getMap, ok := getResult.(map[string]any)
	if !ok {
		t.Fatalf("unexpected get result type: %T", getResult)
	}
	if got, _ := getMap["title"].(string); got != "Refund policy" {
		t.Fatalf("unexpected title: %q", got)
	}

	updateInput, _ := json.Marshal(map[string]any{
		"entry_id": entryID,
		"kind":     "procedure",
		"title":    "Refund procedure",
		"tags":     []string{},
		"metadata": map[string]any{},
	})
	updateResult, err := h.callTool(ctx, agentID, agentSvc, "company_knowledge_update", updateInput)
	if err != nil {
		t.Fatalf("company_knowledge_update failed: %v", err)
	}
	updateMap, ok := updateResult.(map[string]any)
	if !ok {
		t.Fatalf("unexpected update result type: %T", updateResult)
	}
	if got, _ := updateMap["kind"].(string); got != "procedure" {
		t.Fatalf("expected updated kind procedure, got %q", got)
	}

	deleteInput, _ := json.Marshal(map[string]any{"entry_id": entryID})
	if _, err := h.callTool(ctx, agentID, agentSvc, "company_knowledge_delete", deleteInput); err != nil {
		t.Fatalf("company_knowledge_delete failed: %v", err)
	}
	if _, err := h.callTool(ctx, agentID, agentSvc, "company_knowledge_get", getInput); err == nil {
		t.Fatalf("expected get to fail after delete")
	}
}
