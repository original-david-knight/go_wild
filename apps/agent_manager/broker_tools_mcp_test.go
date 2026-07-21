package main

import (
	"context"
	"strings"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/tools"
)

func TestCallMCPToolsUnknownToolIsUnhandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	svc := data.NewAgentService(db, "mcp-agent")

	handled, result, err := h.callMCPTools(context.Background(), "mcp-agent", svc, "not_an_mcp_tool", nil)
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

func TestCallMCPToolsListServersHandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	if _, err := NewAgentService(db).CreateAgent(context.Background(), "mcp-agent"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	svc := data.NewAgentService(db, "mcp-agent")

	handled, result, err := h.callMCPTools(context.Background(), "mcp-agent", svc, "mcp_list_servers", []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatalf("expected mcp_list_servers to be handled")
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if _, ok := resultMap["servers"]; !ok {
		t.Fatalf("expected servers field in result")
	}
}

func TestCallMCPToolsListToolsRequiresServerID(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	svc := data.NewAgentService(db, "mcp-agent")

	handled, result, err := h.callMCPTools(context.Background(), "mcp-agent", svc, "mcp_list_tools", []byte(`{}`))
	if !handled {
		t.Fatalf("expected mcp_list_tools to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result on validation error, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected server_id validation error")
	}
	if !strings.Contains(err.Error(), "server_id is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallMCPToolsCallToolRequiresServerID(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	svc := data.NewAgentService(db, "mcp-agent")

	handled, result, err := h.callMCPTools(context.Background(), "mcp-agent", svc, "mcp_call_tool", []byte(`{}`))
	if !handled {
		t.Fatalf("expected mcp_call_tool to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result on validation error, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected server_id validation error")
	}
	if !strings.Contains(err.Error(), "server_id is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallMCPToolsCallToolRequiresToolName(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	svc := data.NewAgentService(db, "mcp-agent")

	handled, result, err := h.callMCPTools(context.Background(), "mcp-agent", svc, "mcp_call_tool", []byte(`{"server_id":"s1"}`))
	if !handled {
		t.Fatalf("expected mcp_call_tool to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result on validation error, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected tool_name validation error")
	}
	if !strings.Contains(err.Error(), "tool_name is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsMCPToolRecognition(t *testing.T) {
	if !isMCPTool("mcp_list_servers") {
		t.Fatalf("expected mcp_list_servers to be recognized")
	}
	if isMCPTool("mcp_not_real") {
		t.Fatalf("expected unknown mcp tool to be rejected")
	}
}

func TestSetMCPServerEnabledInvalidScope(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	svc := data.NewAgentService(db, "mcp-agent")
	if _, err := svc.EnsureAgent(context.Background()); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	_, err := h.setMCPServerEnabled(context.Background(), "mcp-agent", svc, tools.MCPSetServerEnabledInput{
		Scope:    "invalid",
		ServerID: "server-1",
		Enabled:  true,
	})
	if err == nil {
		t.Fatalf("expected invalid scope error")
	}
	if !strings.Contains(err.Error(), "scope must be 'local' or 'host'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetMCPServerEnabledLocalRequiresServerID(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	svc := data.NewAgentService(db, "mcp-agent")
	if _, err := svc.EnsureAgent(context.Background()); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	_, err := h.setMCPServerEnabled(context.Background(), "mcp-agent", svc, tools.MCPSetServerEnabledInput{
		Scope:   "local",
		Enabled: true,
	})
	if err == nil {
		t.Fatalf("expected server_id required error")
	}
	if !strings.Contains(err.Error(), "server_id is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMCPSetServerEnabledScopeRecognition(t *testing.T) {
	if !isMCPSetServerEnabledScope("local") || !isMCPSetServerEnabledScope("host") {
		t.Fatalf("expected local/host to be recognized scopes")
	}
	if isMCPSetServerEnabledScope("not-real") {
		t.Fatalf("expected unknown scope to be rejected")
	}
}
