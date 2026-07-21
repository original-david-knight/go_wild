package main

import (
	"context"
	"strings"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
)

func TestCallRecurringToolsUnknownToolIsUnhandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	if _, err := NewAgentService(db).CreateAgent(context.Background(), "recurring-agent"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	svc := data.NewAgentService(db, "recurring-agent")

	handled, result, err := h.callRecurringTools(context.Background(), svc, "not_a_recurring_tool", nil)
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

func TestCallRecurringToolsAddRecurringTaskRequiresDescription(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	if _, err := NewAgentService(db).CreateAgent(context.Background(), "recurring-agent"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	svc := data.NewAgentService(db, "recurring-agent")

	handled, result, err := h.callRecurringTools(context.Background(), svc, "add_recurring_task", []byte(`{"interval_minutes":15}`))
	if !handled {
		t.Fatalf("expected add_recurring_task to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result on validation error, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected validation error for missing description")
	}
	if !strings.Contains(err.Error(), "description is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallRecurringToolsAddRecurringTaskRequiresPositiveInterval(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	if _, err := NewAgentService(db).CreateAgent(context.Background(), "recurring-agent"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	svc := data.NewAgentService(db, "recurring-agent")

	handled, result, err := h.callRecurringTools(context.Background(), svc, "add_recurring_task", []byte(`{"description":"x","interval_minutes":0}`))
	if !handled {
		t.Fatalf("expected add_recurring_task to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result on validation error, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected validation error for interval_minutes")
	}
	if !strings.Contains(err.Error(), "interval_minutes must be positive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsRecurringToolRecognition(t *testing.T) {
	if !isRecurringTool("add_recurring_task") {
		t.Fatalf("expected add_recurring_task to be recognized")
	}
	if isRecurringTool("recurring_not_real") {
		t.Fatalf("expected unknown recurring tool to be rejected")
	}
}

func TestCallCacheToolsUnknownToolIsUnhandled(t *testing.T) {
	h := NewBrokerToolsHandler(setupManagerTestDB(t))

	handled, result, err := h.callCacheTools(context.Background(), "not_a_cache_tool", nil)
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

func TestCallCacheToolsCacheGetRequiresKey(t *testing.T) {
	h := NewBrokerToolsHandler(setupManagerTestDB(t))

	handled, result, err := h.callCacheTools(context.Background(), "cache_get", []byte(`{}`))
	if !handled {
		t.Fatalf("expected cache_get to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result on validation error, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected validation error for missing key")
	}
	if !strings.Contains(err.Error(), "key is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallCacheToolsSetThenGet(t *testing.T) {
	h := NewBrokerToolsHandler(setupManagerTestDB(t))

	setHandled, setResult, setErr := h.callCacheTools(context.Background(), "cache_set", []byte(`{"key":"k1","value":"v1","ttl_secs":60}`))
	if setErr != nil {
		t.Fatalf("unexpected cache_set error: %v", setErr)
	}
	if !setHandled {
		t.Fatalf("expected cache_set to be handled")
	}
	setMap, ok := setResult.(map[string]any)
	if !ok {
		t.Fatalf("unexpected cache_set result type: %T", setResult)
	}
	if setMap["stored"] != true {
		t.Fatalf("expected stored=true, got %#v", setMap["stored"])
	}

	getHandled, getResult, getErr := h.callCacheTools(context.Background(), "cache_get", []byte(`{"key":"k1"}`))
	if getErr != nil {
		t.Fatalf("unexpected cache_get error: %v", getErr)
	}
	if !getHandled {
		t.Fatalf("expected cache_get to be handled")
	}
	getMap, ok := getResult.(map[string]any)
	if !ok {
		t.Fatalf("unexpected cache_get result type: %T", getResult)
	}
	if getMap["found"] != true {
		t.Fatalf("expected found=true, got %#v", getMap["found"])
	}
	if getMap["value"] != "v1" {
		t.Fatalf("expected value=v1, got %#v", getMap["value"])
	}
}

func TestIsCacheToolRecognition(t *testing.T) {
	if !isCacheTool("cache_set") {
		t.Fatalf("expected cache_set to be recognized")
	}
	if isCacheTool("cache_not_real") {
		t.Fatalf("expected unknown cache tool to be rejected")
	}
}
