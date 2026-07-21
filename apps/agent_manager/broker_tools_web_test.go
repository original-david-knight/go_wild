package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
)

func TestBrokerWebReaderToolReadsPageWhenEnabled(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	h := NewBrokerToolsHandler(db)

	agentID := "web-reader-enabled-agent"
	svc := data.NewAgentService(db, agentID)
	agent, err := svc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	agent.SetEnabledTools([]string{"web_reader"})
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><head><title>Research Page</title></head><body><h1>Headline</h1><p>Useful content.</p></body></html>"))
	}))
	defer page.Close()

	input, err := json.Marshal(map[string]any{"url": page.URL})
	if err != nil {
		t.Fatalf("Marshal input failed: %v", err)
	}

	resultAny, err := h.callTool(ctx, agentID, svc, "read_webpage", input)
	if err != nil {
		t.Fatalf("callTool(read_webpage) failed: %v", err)
	}

	result, ok := resultAny.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", resultAny)
	}
	if got := strings.TrimSpace(result["title"].(string)); got != "Research Page" {
		t.Fatalf("title = %q, want %q", got, "Research Page")
	}
	content, _ := result["content"].(string)
	if !strings.Contains(content, "Useful content.") {
		t.Fatalf("content missing fetched page text: %q", content)
	}
}

func TestBrokerWebReaderToolDeniedWhenGroupDisabled(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()
	h := NewBrokerToolsHandler(db)

	agentID := "web-reader-disabled-agent"
	svc := data.NewAgentService(db, agentID)
	agent, err := svc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	agent.SetEnabledTools([]string{"skills"})
	if err := svc.UpdateAgent(ctx, agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	input, err := json.Marshal(map[string]any{"url": "https://example.com"})
	if err != nil {
		t.Fatalf("Marshal input failed: %v", err)
	}

	_, err = h.callTool(ctx, agentID, svc, "read_webpage", input)
	if err == nil {
		t.Fatal("expected read_webpage to be denied when web_reader is disabled")
	}
	if !strings.Contains(err.Error(), `tool "read_webpage" is disabled`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
