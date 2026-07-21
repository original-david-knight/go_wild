package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleCompanyCJDropshippingCRUD(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	company, err := svc.CreateCompany(ctx, "CJ Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	putBody := map[string]any{
		"api_key":                   "cj-api-key-123",
		"default_from_country_code": "cn",
		"enabled":                   true,
	}
	var putBuf bytes.Buffer
	if err := json.NewEncoder(&putBuf).Encode(putBody); err != nil {
		t.Fatalf("encode put body failed: %v", err)
	}
	putReq := httptest.NewRequest(http.MethodPut, "/api/companies/"+company.ID+"/cjdropshipping", &putBuf)
	putRec := httptest.NewRecorder()
	h.handleCompany(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from put, got %d body=%s", putRec.Code, putRec.Body.String())
	}
	var putResp map[string]any
	if err := json.NewDecoder(putRec.Body).Decode(&putResp); err != nil {
		t.Fatalf("decode put response failed: %v", err)
	}
	cj, ok := putResp["cjdropshipping"].(map[string]any)
	if !ok {
		t.Fatalf("expected cjdropshipping object in put response, got %T", putResp["cjdropshipping"])
	}
	if got, _ := cj["default_from_country_code"].(string); got != "CN" {
		t.Fatalf("expected normalized default country CN, got %q", got)
	}
	if got, _ := cj["has_api_key"].(bool); !got {
		t.Fatalf("expected has_api_key=true in put response")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/companies/"+company.ID+"/cjdropshipping", nil)
	getRec := httptest.NewRecorder()
	h.handleCompany(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from get, got %d body=%s", getRec.Code, getRec.Body.String())
	}
	var getResp map[string]any
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response failed: %v", err)
	}
	cj, ok = getResp["cjdropshipping"].(map[string]any)
	if !ok {
		t.Fatalf("expected cjdropshipping object in get response, got %T", getResp["cjdropshipping"])
	}
	if got, _ := cj["has_access_token"].(bool); got {
		t.Fatalf("did not expect access token after initial put")
	}

	putBody2 := map[string]any{
		"access_token":  "cj-access-token-1",
		"refresh_token": "cj-refresh-token-1",
		"enabled":       false,
	}
	var putBuf2 bytes.Buffer
	if err := json.NewEncoder(&putBuf2).Encode(putBody2); err != nil {
		t.Fatalf("encode second put body failed: %v", err)
	}
	putReq2 := httptest.NewRequest(http.MethodPut, "/api/companies/"+company.ID+"/cjdropshipping", &putBuf2)
	putRec2 := httptest.NewRecorder()
	h.handleCompany(putRec2, putReq2)
	if putRec2.Code != http.StatusOK {
		t.Fatalf("expected 200 from second put, got %d body=%s", putRec2.Code, putRec2.Body.String())
	}
	var putResp2 map[string]any
	if err := json.NewDecoder(putRec2.Body).Decode(&putResp2); err != nil {
		t.Fatalf("decode second put response failed: %v", err)
	}
	cj, ok = putResp2["cjdropshipping"].(map[string]any)
	if !ok {
		t.Fatalf("expected cjdropshipping object in second put response, got %T", putResp2["cjdropshipping"])
	}
	if got, _ := cj["has_access_token"].(bool); !got {
		t.Fatalf("expected has_access_token=true in second put response")
	}
	if got, _ := cj["enabled"].(bool); got {
		t.Fatalf("expected enabled=false after second put")
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/companies/"+company.ID+"/cjdropshipping", nil)
	delRec := httptest.NewRecorder()
	h.handleCompany(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from delete, got %d body=%s", delRec.Code, delRec.Body.String())
	}

	getReq2 := httptest.NewRequest(http.MethodGet, "/api/companies/"+company.ID+"/cjdropshipping", nil)
	getRec2 := httptest.NewRecorder()
	h.handleCompany(getRec2, getReq2)
	if getRec2.Code != http.StatusOK {
		t.Fatalf("expected 200 from get after delete, got %d body=%s", getRec2.Code, getRec2.Body.String())
	}
	var getResp2 map[string]any
	if err := json.NewDecoder(getRec2.Body).Decode(&getResp2); err != nil {
		t.Fatalf("decode get after delete response failed: %v", err)
	}
	if got := getResp2["cjdropshipping"]; got != nil {
		t.Fatalf("expected nil cjdropshipping after delete, got %#v", got)
	}
}

func TestHandleCompanyCJDropshippingTestRequiresConfig(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	company, err := svc.CreateCompany(ctx, "CJ Test Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	testReq := httptest.NewRequest(http.MethodPost, "/api/companies/"+company.ID+"/cjdropshipping/test", nil)
	testRec := httptest.NewRecorder()
	h.handleCompany(testRec, testReq)
	if testRec.Code != http.StatusBadRequest {
		t.Fatalf("expected test status %d, got %d body=%s", http.StatusBadRequest, testRec.Code, testRec.Body.String())
	}
}
