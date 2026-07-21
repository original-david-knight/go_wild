package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

func TestCallWithInputDecodesJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	result, err := callWithInput[payload]([]byte(`{"name":"alice","age":30}`), func(in payload) (any, error) {
		if in.Name != "alice" || in.Age != 30 {
			t.Fatalf("unexpected decoded input: %#v", in)
		}
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("callWithInput failed: %v", err)
	}
	out, ok := result.(map[string]any)
	if !ok || out["ok"] != true {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestCallWithInputInvalidJSON(t *testing.T) {
	_, err := callWithInput[map[string]any]([]byte(`{"name":`), func(in map[string]any) (any, error) {
		return nil, errors.New("should not be called")
	})
	if err == nil {
		t.Fatalf("expected unmarshal error")
	}
	if !strings.Contains(err.Error(), "failed to unmarshal input") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolResultContent(t *testing.T) {
	out, err := toolResultContent(loop.NewSuccessResult(map[string]any{"ok": true}), nil)
	if err != nil {
		t.Fatalf("toolResultContent success failed: %v", err)
	}
	payload, ok := out.(map[string]any)
	if !ok || payload["ok"] != true {
		t.Fatalf("unexpected success payload: %#v", out)
	}

	_, err = toolResultContent(loop.NewErrorResult("tool failed"), nil)
	if err == nil {
		t.Fatalf("expected tool-level error")
	}
	if !strings.Contains(err.Error(), "tool failed") {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = toolResultContent(nil, errors.New("transport failed"))
	if err == nil {
		t.Fatalf("expected passthrough error")
	}
	if !strings.Contains(err.Error(), "transport failed") {
		t.Fatalf("unexpected passthrough error: %v", err)
	}
}

func TestCallToolUnknownToolReturnsError(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()
	agentID := "dispatch-calltool-unknown"
	svc := data.NewAgentService(db, agentID)
	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	_, err := h.callTool(ctx, agentID, svc, "not_a_real_tool", nil)
	if err == nil {
		t.Fatalf("expected unknown-tool error")
	}
	if !strings.Contains(err.Error(), "unknown tool: not_a_real_tool") {
		t.Fatalf("unexpected error: %v", err)
	}
}
