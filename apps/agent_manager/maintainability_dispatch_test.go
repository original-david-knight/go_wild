package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
)

func TestCallDataAccessToolsUnknownToolIsUnhandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()
	agentID := "dispatch-unknown-tool"
	svc := data.NewAgentService(db, agentID)
	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	handled, result, err := h.callDataAccessTools(ctx, agentID, svc, "not_a_data_access_tool", nil)
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

func TestCallDataAccessToolsGetEnabledTools_DefaultAndConfigured(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	ctx := context.Background()
	agentID := "dispatch-enabled-tools"
	svc := data.NewAgentService(db, agentID)
	if _, err := svc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	handled, result, err := h.callDataAccessTools(ctx, agentID, svc, "get_enabled_tools", nil)
	if err != nil {
		t.Fatalf("get_enabled_tools failed: %v", err)
	}
	if !handled {
		t.Fatalf("expected get_enabled_tools to be handled")
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if _, ok := resultMap["enabled_tools"]; !ok {
		t.Fatalf("expected enabled_tools key in response")
	}
	if resultMap["enabled_tools"] != nil {
		t.Fatalf("expected nil enabled_tools for default state, got %#v", resultMap["enabled_tools"])
	}

	agent, err := svc.GetAgent(ctx)
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	agent.SetEnabledTools([]string{"web_search", "wallet"})
	if err := db.Table(data.Agent{}).Update(ctx, agent); err != nil {
		t.Fatalf("update agent failed: %v", err)
	}

	_, result, err = h.callDataAccessTools(ctx, agentID, svc, "get_enabled_tools", nil)
	if err != nil {
		t.Fatalf("get_enabled_tools (configured) failed: %v", err)
	}
	enabledSet := enabledToolsSetFromResult(t, result)
	if len(enabledSet) != 2 {
		t.Fatalf("expected 2 enabled tools, got %d (%#v)", len(enabledSet), enabledSet)
	}
	if !enabledSet["web_search"] || !enabledSet["wallet"] {
		t.Fatalf("expected enabled tools to include web_search and wallet, got %#v", enabledSet)
	}
}

func TestHandleCompanyUnknownActionReturnsNotFound(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewHandlers(NewAgentService(db), nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/companies/company-1/unknown-action", nil)
	rec := httptest.NewRecorder()
	h.handleCompany(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusNotFound, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown company action") {
		t.Fatalf("expected unknown company action error, got %s", rec.Body.String())
	}
}

func TestHandleCompanyShopifyTestRequiresPost(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewHandlers(NewAgentService(db), nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/companies/company-1/shopify/test", nil)
	rec := httptest.NewRecorder()
	h.handleCompany(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusMethodNotAllowed, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "method not allowed") {
		t.Fatalf("expected method not allowed error, got %s", rec.Body.String())
	}
}

func TestNormalizeShopifyToolNameAndRecognition(t *testing.T) {
	normalized := normalizeShopifyToolName(" company_commerce_shopify_list_products ")
	if normalized != "shopify_list_products" {
		t.Fatalf("expected normalized name shopify_list_products, got %q", normalized)
	}
	if !isShopifyTool(normalized) {
		t.Fatalf("expected normalized Shopify tool to be recognized")
	}
	if isShopifyTool("shopify_not_a_real_tool") {
		t.Fatalf("unexpectedly recognized unknown Shopify tool")
	}
}

func TestIsCompanyConnectionMethodRecognition(t *testing.T) {
	if !isCompanyConnectionMethod(http.MethodGet) {
		t.Fatalf("expected GET to be recognized for company connection routes")
	}
	if !isCompanyConnectionMethod(http.MethodPut) {
		t.Fatalf("expected PUT to be recognized for company connection routes")
	}
	if !isCompanyConnectionMethod(http.MethodDelete) {
		t.Fatalf("expected DELETE to be recognized for company connection routes")
	}
	if isCompanyConnectionMethod(http.MethodPatch) {
		t.Fatalf("expected PATCH to be rejected for company connection routes")
	}
}

func TestIsCompanyActionRecognition(t *testing.T) {
	if !isCompanyAction("members") {
		t.Fatalf("expected members action to be recognized")
	}
	if !isCompanyAction("missions") {
		t.Fatalf("expected missions action to be recognized")
	}
	if isCompanyAction("company-not-real") {
		t.Fatalf("expected unknown company action to be rejected")
	}
}

func TestCompanyMethodRecognitionHelpers(t *testing.T) {
	if !isCompanyCollectionMethod(http.MethodGet) || !isCompanyCollectionMethod(http.MethodPost) {
		t.Fatalf("expected GET/POST to be recognized company collection methods")
	}
	if isCompanyCollectionMethod(http.MethodPatch) {
		t.Fatalf("expected PATCH to be rejected company collection method")
	}

	if !isCompanyMethod(http.MethodGet) || !isCompanyMethod(http.MethodPatch) || !isCompanyMethod(http.MethodDelete) {
		t.Fatalf("expected GET/PATCH/DELETE to be recognized company resource methods")
	}
	if isCompanyMethod(http.MethodPost) {
		t.Fatalf("expected POST to be rejected company resource method")
	}

	if !isCompanyMembersCollectionMethod(http.MethodGet) || !isCompanyMembersCollectionMethod(http.MethodPost) {
		t.Fatalf("expected GET/POST to be recognized company members collection methods")
	}
	if isCompanyMembersCollectionMethod(http.MethodPatch) {
		t.Fatalf("expected PATCH to be rejected company members collection method")
	}

	if !isCompanyMemberMethod(http.MethodDelete) || !isCompanyMemberMethod(http.MethodPatch) {
		t.Fatalf("expected DELETE/PATCH to be recognized company member methods")
	}
	if isCompanyMemberMethod(http.MethodGet) {
		t.Fatalf("expected GET to be rejected company member method")
	}

	if !isCompanyWebhookMethod(http.MethodGet) || !isCompanyWebhookMethod(http.MethodPut) {
		t.Fatalf("expected GET/PUT to be recognized company webhook methods")
	}
	if isCompanyWebhookMethod(http.MethodDelete) {
		t.Fatalf("expected DELETE to be rejected company webhook method")
	}

	if !isCompanyKnowledgeCollectionMethod(http.MethodGet) || !isCompanyKnowledgeCollectionMethod(http.MethodPost) {
		t.Fatalf("expected GET/POST to be recognized company knowledge collection methods")
	}
	if isCompanyKnowledgeCollectionMethod(http.MethodPatch) {
		t.Fatalf("expected PATCH to be rejected company knowledge collection method")
	}

	if !isCompanyKnowledgeEntryMethod(http.MethodGet) || !isCompanyKnowledgeEntryMethod(http.MethodPatch) || !isCompanyKnowledgeEntryMethod(http.MethodDelete) {
		t.Fatalf("expected GET/PATCH/DELETE to be recognized company knowledge entry methods")
	}
	if isCompanyKnowledgeEntryMethod(http.MethodPost) {
		t.Fatalf("expected POST to be rejected company knowledge entry method")
	}
}

func TestHandleCompanyWebhooksUnknownSubpathReturnsNotFound(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewHandlers(NewAgentService(db), nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/companies/company-1/webhooks/not-real", nil)
	rec := httptest.NewRecorder()
	h.handleCompany(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusNotFound, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown company action") {
		t.Fatalf("expected unknown company action error, got %s", rec.Body.String())
	}
}

func enabledToolsSetFromResult(t *testing.T, result any) map[string]bool {
	t.Helper()
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	raw, ok := resultMap["enabled_tools"]
	if !ok {
		t.Fatalf("expected enabled_tools key in result")
	}

	set := make(map[string]bool)
	switch v := raw.(type) {
	case []string:
		for _, id := range v {
			set[id] = true
		}
	case []any:
		for _, item := range v {
			id, ok := item.(string)
			if !ok {
				t.Fatalf("unexpected enabled_tools item type: %T", item)
			}
			set[id] = true
		}
	default:
		t.Fatalf("unexpected enabled_tools type: %T", raw)
	}

	return set
}
