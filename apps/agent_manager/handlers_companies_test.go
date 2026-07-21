package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleCompanyAddMemberConflictReturns409(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	if _, err := svc.CreateAgent(ctx, "agent-a"); err != nil {
		t.Fatalf("CreateAgent(agent-a) failed: %v", err)
	}

	companyA, err := svc.CreateCompany(ctx, "Company A", "", "")
	if err != nil {
		t.Fatalf("CreateCompany(company A) failed: %v", err)
	}
	companyB, err := svc.CreateCompany(ctx, "Company B", "", "")
	if err != nil {
		t.Fatalf("CreateCompany(company B) failed: %v", err)
	}
	if err := svc.AddAgentToCompany(ctx, companyA.ID, "agent-a", "member"); err != nil {
		t.Fatalf("AddAgentToCompany(company A) failed: %v", err)
	}

	body := map[string]any{
		"agent_id": "agent-a",
		"role":     "member",
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode body failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/companies/"+companyB.ID+"/members", &buf)
	rec := httptest.NewRecorder()
	h.handleCompany(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusConflict, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "already belongs") {
		t.Fatalf("expected conflict error body, got %s", rec.Body.String())
	}
}

func TestHandleCompanyPatchMemberRole(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	if _, err := svc.CreateAgent(ctx, "agent-role"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	company, err := svc.CreateCompany(ctx, "Role Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := svc.AddAgentToCompany(ctx, company.ID, "agent-role", "member"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]any{"role": "analyst"}); err != nil {
		t.Fatalf("encode patch body failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/companies/"+company.ID+"/members/agent-role", &buf)
	rec := httptest.NewRecorder()
	h.handleCompany(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
	}

	member, err := svc.GetCompanyMemberForAgent(ctx, "agent-role")
	if err != nil {
		t.Fatalf("GetCompanyMemberForAgent failed: %v", err)
	}
	if member == nil {
		t.Fatalf("expected member to exist")
	}
	if member.Role != "analyst" {
		t.Fatalf("expected updated role analyst, got %q", member.Role)
	}
}

func TestHandleAgentCompanyMapping(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	if _, err := svc.CreateAgent(ctx, "agent-company-map"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	company, err := svc.CreateCompany(ctx, "Mapping Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := svc.AddAgentToCompany(ctx, company.ID, "agent-company-map", "member"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agents/agent-company-map/company", nil)
	rec := httptest.NewRecorder()
	h.handleAgent(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	companyResp, ok := payload["company"].(map[string]any)
	if !ok {
		t.Fatalf("expected company object, got %T", payload["company"])
	}
	if got, _ := companyResp["id"].(string); got != company.ID {
		t.Fatalf("expected company id %q, got %q", company.ID, got)
	}
	memberResp, ok := payload["member"].(map[string]any)
	if !ok {
		t.Fatalf("expected member object, got %T", payload["member"])
	}
	if got, _ := memberResp["agent_id"].(string); got != "agent-company-map" {
		t.Fatalf("expected member agent_id agent-company-map, got %q", got)
	}
}

func TestHandleAgentCompanyMethodToolsListsTeammateMethods(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	requester, err := svc.CreateAgent(ctx, "agent-company-tools-requester")
	if err != nil {
		t.Fatalf("CreateAgent(requester) failed: %v", err)
	}
	provider, err := svc.CreateAgent(ctx, "agent-company-tools-provider")
	if err != nil {
		t.Fatalf("CreateAgent(provider) failed: %v", err)
	}
	company, err := svc.CreateCompany(ctx, "Tools Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := svc.AddAgentToCompany(ctx, company.ID, requester.ID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(requester) failed: %v", err)
	}
	if err := svc.AddAgentToCompany(ctx, company.ID, provider.ID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(provider) failed: %v", err)
	}

	if _, err := svc.CreateA2AMethod(
		ctx,
		"fulfill_order",
		"Fulfill an order",
		`{"type":"object","required":["order_id"],"properties":{"order_id":{"type":"string"}}}`,
		`{"type":"object","required":["ok"],"properties":{"ok":{"type":"boolean"}}}`,
	); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}
	if _, err := svc.AddCapability(ctx, provider.ID, "fulfillment", "fulfill_order"); err != nil {
		t.Fatalf("AddCapability failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agents/"+requester.ID+"/company-method-tools", nil)
	rec := httptest.NewRecorder()
	h.handleAgent(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	rawTools, ok := payload["tools"].([]any)
	if !ok {
		t.Fatalf("expected tools array, got %T", payload["tools"])
	}
	if len(rawTools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(rawTools))
	}
	tool, ok := rawTools[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected tool row type: %T", rawTools[0])
	}
	if got, _ := tool["method"].(string); got != "fulfill_order" {
		t.Fatalf("expected method fulfill_order, got %q", got)
	}
	if got, _ := tool["tool_name"].(string); got != companyMethodToolName("fulfill_order") {
		t.Fatalf("unexpected tool_name %q", got)
	}
	if got, _ := tool["provider_agent_count"].(float64); got != 1 {
		t.Fatalf("expected provider_agent_count 1, got %v", got)
	}
}

func TestHandleCompaniesCRUDAndValidation(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	if _, err := svc.CreateAgent(ctx, "ceo-crud"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	var badCreate bytes.Buffer
	if err := json.NewEncoder(&badCreate).Encode(map[string]any{"name": "   "}); err != nil {
		t.Fatalf("encode bad create body failed: %v", err)
	}
	badCreateReq := httptest.NewRequest(http.MethodPost, "/api/companies", &badCreate)
	badCreateRec := httptest.NewRecorder()
	h.handleCompanies(badCreateRec, badCreateReq)
	if badCreateRec.Code != http.StatusBadRequest {
		t.Fatalf("expected bad create status %d, got %d body=%s", http.StatusBadRequest, badCreateRec.Code, badCreateRec.Body.String())
	}

	var createBuf bytes.Buffer
	if err := json.NewEncoder(&createBuf).Encode(map[string]any{
		"name":         "CRUD Co",
		"description":  "initial",
		"ceo_agent_id": "ceo-crud",
	}); err != nil {
		t.Fatalf("encode create body failed: %v", err)
	}
	createReq := httptest.NewRequest(http.MethodPost, "/api/companies", &createBuf)
	createRec := httptest.NewRecorder()
	h.handleCompanies(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create status %d, got %d body=%s", http.StatusCreated, createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response failed: %v", err)
	}
	companyID, _ := created["id"].(string)
	if strings.TrimSpace(companyID) == "" {
		t.Fatalf("expected company id in create response")
	}
	seedPhrase, _ := created["wallet_seed_phrase"].(string)
	if strings.TrimSpace(seedPhrase) == "" {
		t.Fatalf("expected wallet_seed_phrase in create response")
	}
	publicKeys, ok := created["wallet_public_keys"].(map[string]any)
	if !ok {
		t.Fatalf("expected wallet_public_keys object in create response, got %T", created["wallet_public_keys"])
	}
	ethKey, _ := publicKeys["ethereum"].(string)
	if strings.TrimSpace(ethKey) == "" {
		t.Fatalf("expected non-empty ethereum public key in create response")
	}
	solKey, _ := publicKeys["solana"].(string)
	if strings.TrimSpace(solKey) == "" {
		t.Fatalf("expected non-empty solana public key in create response")
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/companies", nil)
	listRec := httptest.NewRecorder()
	h.handleCompanies(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list status %d, got %d body=%s", http.StatusOK, listRec.Code, listRec.Body.String())
	}
	var listed map[string]any
	if err := json.NewDecoder(listRec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response failed: %v", err)
	}
	companiesRaw, ok := listed["companies"].([]any)
	if !ok || len(companiesRaw) == 0 {
		t.Fatalf("expected companies list in response, got %#v", listed["companies"])
	}
	firstCompany, ok := companiesRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first company object, got %T", companiesRaw[0])
	}
	listSeed, _ := firstCompany["wallet_seed_phrase"].(string)
	if strings.TrimSpace(listSeed) == "" {
		t.Fatalf("expected wallet_seed_phrase in list response")
	}

	var badPatch bytes.Buffer
	if err := json.NewEncoder(&badPatch).Encode(map[string]any{"name": "   "}); err != nil {
		t.Fatalf("encode bad patch body failed: %v", err)
	}
	badPatchReq := httptest.NewRequest(http.MethodPatch, "/api/companies/"+companyID, &badPatch)
	badPatchRec := httptest.NewRecorder()
	h.handleCompany(badPatchRec, badPatchReq)
	if badPatchRec.Code != http.StatusBadRequest {
		t.Fatalf("expected bad patch status %d, got %d body=%s", http.StatusBadRequest, badPatchRec.Code, badPatchRec.Body.String())
	}

	var patchBuf bytes.Buffer
	if err := json.NewEncoder(&patchBuf).Encode(map[string]any{
		"name":        "CRUD Co Updated",
		"description": "updated",
	}); err != nil {
		t.Fatalf("encode patch body failed: %v", err)
	}
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/companies/"+companyID, &patchBuf)
	patchRec := httptest.NewRecorder()
	h.handleCompany(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("expected patch status %d, got %d body=%s", http.StatusOK, patchRec.Code, patchRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/companies/"+companyID, nil)
	getRec := httptest.NewRecorder()
	h.handleCompany(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected get status %d, got %d body=%s", http.StatusOK, getRec.Code, getRec.Body.String())
	}
	var got map[string]any
	if err := json.NewDecoder(getRec.Body).Decode(&got); err != nil {
		t.Fatalf("decode get response failed: %v", err)
	}
	gotSeed, _ := got["wallet_seed_phrase"].(string)
	if strings.TrimSpace(gotSeed) == "" {
		t.Fatalf("expected wallet_seed_phrase in get response")
	}
	gotPublicKeys, ok := got["wallet_public_keys"].(map[string]any)
	if !ok {
		t.Fatalf("expected wallet_public_keys object in get response, got %T", got["wallet_public_keys"])
	}
	if s, _ := gotPublicKeys["ethereum"].(string); strings.TrimSpace(s) == "" {
		t.Fatalf("expected ethereum key in get response")
	}
	if s, _ := gotPublicKeys["solana"].(string); strings.TrimSpace(s) == "" {
		t.Fatalf("expected solana key in get response")
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/companies/"+companyID, nil)
	deleteRec := httptest.NewRecorder()
	h.handleCompany(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected delete status %d, got %d body=%s", http.StatusOK, deleteRec.Code, deleteRec.Body.String())
	}
}
