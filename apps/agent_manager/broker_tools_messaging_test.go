package main

import (
	"context"
	"strings"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
)

func TestCallMessagingToolsUnknownToolIsUnhandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	svc := data.NewAgentService(db, "msg-agent")

	handled, result, err := h.callMessagingTools(context.Background(), "msg-agent", svc, "not_a_messaging_tool", nil)
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

func TestCallMessagingToolsSendRequiresToAndContent(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	svc := data.NewAgentService(db, "msg-agent")

	handled, result, err := h.callMessagingTools(context.Background(), "msg-agent", svc, "send_agent_message", []byte(`{}`))
	if !handled {
		t.Fatalf("expected send_agent_message to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result on validation error, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "to_agent_id and content are required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallMessagingToolsReadRequiresPeerAgentID(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	svc := data.NewAgentService(db, "msg-agent")

	handled, result, err := h.callMessagingTools(context.Background(), "msg-agent", svc, "read_agent_messages", []byte(`{}`))
	if !handled {
		t.Fatalf("expected read_agent_messages to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result on validation error, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "peer_agent_id is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallMessagingToolsMarkReadRequiresPeerAgentID(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	svc := data.NewAgentService(db, "msg-agent")

	handled, result, err := h.callMessagingTools(context.Background(), "msg-agent", svc, "mark_agent_messages_read", []byte(`{}`))
	if !handled {
		t.Fatalf("expected mark_agent_messages_read to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result on validation error, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "peer_agent_id is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallMessagingToolsListPeersHandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	if _, err := NewAgentService(db).CreateAgent(context.Background(), "msg-agent"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	svc := data.NewAgentService(db, "msg-agent")

	handled, result, err := h.callMessagingTools(context.Background(), "msg-agent", svc, "list_peers", []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatalf("expected list_peers to be handled")
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	peersAny, ok := resultMap["peers"]
	if !ok {
		t.Fatalf("expected peers field in result")
	}
	if peers, ok := peersAny.([]map[string]any); ok {
		if len(peers) != 0 {
			t.Fatalf("expected no peers, got %d", len(peers))
		}
		return
	}
	if peers, ok := peersAny.([]any); ok {
		if len(peers) != 0 {
			t.Fatalf("expected no peers, got %d", len(peers))
		}
		return
	}
	t.Fatalf("unexpected peers type: %T", peersAny)
}

func TestIsMessagingToolRecognition(t *testing.T) {
	if !isMessagingTool("list_peers") {
		t.Fatalf("expected list_peers to be recognized")
	}
	if isMessagingTool("messaging_not_real") {
		t.Fatalf("expected unknown messaging tool to be rejected")
	}
}
