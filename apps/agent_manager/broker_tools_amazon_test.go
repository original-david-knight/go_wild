package main

import (
	"context"
	"strings"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
)

func TestCallAmazonToolsUnknownToolIsUnhandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()

	handled, result, err := h.callAmazonTools(ctx, "amazon-agent-unknown", "not_a_real_amazon_tool", nil)
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

func TestCallAmazonToolsRecognizedToolRequiresCompanyMembership(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()
	agentID := "amazon-agent-no-company"

	svc := data.NewAgentService(db, agentID)
	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	handled, result, err := h.callAmazonTools(ctx, agentID, "amazon_search", []byte(`{"keywords":"test","limit":1}`))
	if !handled {
		t.Fatalf("expected recognized amazon tool to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result when membership resolution fails, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected company membership error")
	}
	if !strings.Contains(err.Error(), "amazon tools require company membership") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsAmazonToolRecognition(t *testing.T) {
	if !isAmazonTool("amazon_search") {
		t.Fatalf("expected amazon_search to be recognized")
	}
	if !isAmazonTool("amazon_get_product") {
		t.Fatalf("expected amazon_get_product to be recognized")
	}
	if isAmazonTool("amazon_not_real") {
		t.Fatalf("unexpectedly recognized unknown amazon tool")
	}
}

func TestAnnotateAmazonResult(t *testing.T) {
	mapPayload := map[string]any{"ok": true}
	annotated := annotateAmazonResult(mapPayload, "company-1")
	annotatedMap, ok := annotated.(map[string]any)
	if !ok {
		t.Fatalf("expected map annotation output, got %T", annotated)
	}
	if annotatedMap["identity_scope"] != "company" || annotatedMap["company_id"] != "company-1" {
		t.Fatalf("missing annotation fields: %#v", annotatedMap)
	}
	if annotatedMap["ok"] != true {
		t.Fatalf("expected original payload fields preserved: %#v", annotatedMap)
	}

	nonMap := annotateAmazonResult("payload", "company-2")
	nonMapPayload, ok := nonMap.(map[string]any)
	if !ok {
		t.Fatalf("expected wrapped map for non-map payload, got %T", nonMap)
	}
	if nonMapPayload["result"] != "payload" || nonMapPayload["company_id"] != "company-2" {
		t.Fatalf("unexpected wrapped payload: %#v", nonMapPayload)
	}
}
