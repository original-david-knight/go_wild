package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConvertInputSchema(t *testing.T) {
	t.Run("nil returns empty object schema", func(t *testing.T) {
		s := convertInputSchema(nil)
		if s == nil || s.Type != "object" {
			t.Fatal("expected non-nil schema with type=object")
		}
	})

	t.Run("converts map to mcpSchema", func(t *testing.T) {
		input := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"topic": map[string]any{
					"type":        "string",
					"description": "Topic to research",
				},
			},
			"required": []any{"topic"},
		}
		s := convertInputSchema(input)
		if s == nil {
			t.Fatal("expected non-nil schema")
		}
		if s.Type != "object" {
			t.Errorf("expected type=object, got %q", s.Type)
		}
		if s.Properties == nil || s.Properties["topic"] == nil {
			t.Fatal("expected topic property")
		}
		if s.Properties["topic"].Type != "string" {
			t.Errorf("expected topic.type=string, got %q", s.Properties["topic"].Type)
		}
	})
}

func TestFetchDynamicTools(t *testing.T) {
	// Set up a mock broker server.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/broker/v1/mcp-tools/list" {
			http.Error(w, "not found", 404)
			return
		}

		resp := map[string]any{
			"tools": []map[string]any{
				{
					"name":        "deep_research_report",
					"description": "Generate a deep research report",
					"input_schema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"topic": map[string]any{
								"type":        "string",
								"description": "Topic to research",
							},
						},
						"required": []string{"topic"},
					},
					"route": "broker",
				},
				{
					"name":          "reuters__search_news",
					"description":   "Search Reuters news",
					"route":         "mcp",
					"mcp_server_id": "reuters",
					"mcp_tool_name": "search_news",
					"input_schema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"query": map[string]any{"type": "string"},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	tools, routes, err := fetchDynamicTools(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("fetchDynamicTools: %v", err)
	}

	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	// Check deep research tool.
	if tools[0].Name != "deep_research_report" {
		t.Errorf("expected first tool name deep_research_report, got %q", tools[0].Name)
	}
	if tools[0].InputSchema == nil || tools[0].InputSchema.Properties["topic"] == nil {
		t.Error("expected input schema with topic property for deep_research_report")
	}

	// Check MCP-routed tool.
	if tools[1].Name != "reuters__search_news" {
		t.Errorf("expected second tool name reuters__search_news, got %q", tools[1].Name)
	}

	route, ok := routes["reuters__search_news"]
	if !ok {
		t.Fatal("expected MCP route for reuters__search_news")
	}
	if route.ServerID != "reuters" {
		t.Errorf("expected server_id=reuters, got %q", route.ServerID)
	}
	if route.ToolName != "search_news" {
		t.Errorf("expected tool_name=search_news, got %q", route.ToolName)
	}

	// Deep research tool should not have an MCP route.
	if _, ok := routes["deep_research_report"]; ok {
		t.Error("deep_research_report should not have an MCP route")
	}
}

func TestAllToolsIncludesReadWebpage(t *testing.T) {
	tools := allTools()
	for _, tool := range tools {
		if tool.Name != "read_webpage" {
			continue
		}
		if tool.InputSchema == nil {
			t.Fatal("read_webpage should define an input schema")
		}
		if tool.InputSchema.Properties["url"] == nil {
			t.Fatal("read_webpage schema should include url")
		}
		return
	}
	t.Fatal("expected read_webpage in static MCP tool catalog")
}

func TestAllToolsWithDynamic_Dedup(t *testing.T) {
	// Use a fast-failing mock broker that returns an empty tool list.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"tools": []any{}})
	}))
	defer srv.Close()

	s := &Server{
		brokerURL: srv.URL,
		token:     "test",
	}
	// Trigger dynamic tool loading (gets empty list from mock).
	s.loadDynamicTools()

	// Now manually set dynamic tools that overlap with static.
	s.dynamicTools = []mcpTool{
		{Name: "read_soul", Description: "duplicate of static"},
		{Name: "new_dynamic_tool", Description: "unique dynamic tool", InputSchema: &mcpSchema{Type: "object"}},
	}
	s.mcpRoutes = map[string]mcpRoute{}

	tools := s.allToolsWithDynamic()

	// read_soul should appear only once.
	count := 0
	var foundDynamic bool
	for _, t := range tools {
		if t.Name == "read_soul" {
			count++
		}
		if t.Name == "new_dynamic_tool" {
			foundDynamic = true
		}
	}
	if count != 1 {
		t.Errorf("expected read_soul once, got %d", count)
	}
	if !foundDynamic {
		t.Error("expected new_dynamic_tool in merged tools")
	}
}
