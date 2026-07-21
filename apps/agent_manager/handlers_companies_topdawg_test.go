package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleCompanyTopDawgCRUD(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	company, err := svc.CreateCompany(ctx, "TopDawg Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	putBody := map[string]any{
		"supplier_id": "supplier-123",
		"api_key":     "td-api-key-123",
		"enabled":     true,
	}
	var putBuf bytes.Buffer
	if err := json.NewEncoder(&putBuf).Encode(putBody); err != nil {
		t.Fatalf("encode put body failed: %v", err)
	}
	putReq := httptest.NewRequest(http.MethodPut, "/api/companies/"+company.ID+"/topdawg", &putBuf)
	putRec := httptest.NewRecorder()
	h.handleCompany(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from put, got %d body=%s", putRec.Code, putRec.Body.String())
	}
	var putResp map[string]any
	if err := json.NewDecoder(putRec.Body).Decode(&putResp); err != nil {
		t.Fatalf("decode put response failed: %v", err)
	}
	td, ok := putResp["topdawg"].(map[string]any)
	if !ok {
		t.Fatalf("expected topdawg object in put response, got %T", putResp["topdawg"])
	}
	if got, _ := td["supplier_id"].(string); got != "supplier-123" {
		t.Fatalf("unexpected supplier_id from put response: %q", got)
	}
	if got, _ := td["has_api_key"].(bool); !got {
		t.Fatalf("expected has_api_key=true in put response")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/companies/"+company.ID+"/topdawg", nil)
	getRec := httptest.NewRecorder()
	h.handleCompany(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from get, got %d body=%s", getRec.Code, getRec.Body.String())
	}
	var getResp map[string]any
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response failed: %v", err)
	}
	td, ok = getResp["topdawg"].(map[string]any)
	if !ok {
		t.Fatalf("expected topdawg object in get response, got %T", getResp["topdawg"])
	}
	if got, _ := td["supplier_id"].(string); got != "supplier-123" {
		t.Fatalf("unexpected supplier_id from get response: %q", got)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/companies/"+company.ID+"/topdawg", nil)
	delRec := httptest.NewRecorder()
	h.handleCompany(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from delete, got %d body=%s", delRec.Code, delRec.Body.String())
	}

	getReq2 := httptest.NewRequest(http.MethodGet, "/api/companies/"+company.ID+"/topdawg", nil)
	getRec2 := httptest.NewRecorder()
	h.handleCompany(getRec2, getReq2)
	if getRec2.Code != http.StatusOK {
		t.Fatalf("expected 200 from get after delete, got %d body=%s", getRec2.Code, getRec2.Body.String())
	}
	var getResp2 map[string]any
	if err := json.NewDecoder(getRec2.Body).Decode(&getResp2); err != nil {
		t.Fatalf("decode get after delete response failed: %v", err)
	}
	if got := getResp2["topdawg"]; got != nil {
		t.Fatalf("expected nil topdawg after delete, got %#v", got)
	}
}

func TestHandleCompanyTopDawgDisableWithoutAPIKey(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	company, err := svc.CreateCompany(ctx, "TopDawg Disable Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	// Create enabled connection with API key.
	createBody := map[string]any{
		"supplier_id": "supplier-abc",
		"api_key":     "td-key-abc",
		"enabled":     true,
	}
	var createBuf bytes.Buffer
	json.NewEncoder(&createBuf).Encode(createBody)
	createReq := httptest.NewRequest(http.MethodPut, "/api/companies/"+company.ID+"/topdawg", &createBuf)
	createRec := httptest.NewRecorder()
	h.handleCompany(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create: expected 200, got %d body=%s", createRec.Code, createRec.Body.String())
	}

	// Disable by sending enabled=false with blank api_key (mimics the UI form).
	disableBody := map[string]any{
		"supplier_id": "supplier-abc",
		"api_key":     "",
		"enabled":     false,
	}
	var disableBuf bytes.Buffer
	json.NewEncoder(&disableBuf).Encode(disableBody)
	disableReq := httptest.NewRequest(http.MethodPut, "/api/companies/"+company.ID+"/topdawg", &disableBuf)
	disableRec := httptest.NewRecorder()
	h.handleCompany(disableRec, disableReq)
	if disableRec.Code != http.StatusOK {
		t.Fatalf("disable: expected 200, got %d body=%s", disableRec.Code, disableRec.Body.String())
	}

	var disableResp map[string]any
	json.NewDecoder(disableRec.Body).Decode(&disableResp)
	td, ok := disableResp["topdawg"].(map[string]any)
	if !ok {
		t.Fatalf("expected topdawg in response, got %T", disableResp["topdawg"])
	}
	if enabled, _ := td["enabled"].(bool); enabled {
		t.Fatalf("expected enabled=false after disable, got true")
	}
	if hasKey, _ := td["has_api_key"].(bool); !hasKey {
		t.Fatalf("expected has_api_key=true (key should be preserved)")
	}
}

func TestHandleCompanyTopDawgTestRequiresConfig(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	company, err := svc.CreateCompany(ctx, "TopDawg Test Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	testReq := httptest.NewRequest(http.MethodPost, "/api/companies/"+company.ID+"/topdawg/test", nil)
	testRec := httptest.NewRecorder()
	h.handleCompany(testRec, testReq)
	if testRec.Code != http.StatusBadRequest {
		t.Fatalf("expected test status %d, got %d body=%s", http.StatusBadRequest, testRec.Code, testRec.Body.String())
	}
}
