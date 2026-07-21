package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleCompanyPolymarketCRUD(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	company, err := svc.CreateCompany(ctx, "Poly Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	putBody := map[string]any{
		"proxy_url":       "socks5://127.0.0.1:9050",
		"onchain_rpc_url": "https://polygon-rpc.example",
		"funder_address":  "0xabc",
		"signature_type":  1,
		"chain_id":        137,
		"enabled":         true,
	}
	var putBuf bytes.Buffer
	if err := json.NewEncoder(&putBuf).Encode(putBody); err != nil {
		t.Fatalf("encode put body failed: %v", err)
	}
	putReq := httptest.NewRequest(http.MethodPut, "/api/companies/"+company.ID+"/polymarket", &putBuf)
	putRec := httptest.NewRecorder()
	h.handleCompany(putRec, putReq)

	if putRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from put, got %d body=%s", putRec.Code, putRec.Body.String())
	}
	var putResp map[string]any
	if err := json.NewDecoder(putRec.Body).Decode(&putResp); err != nil {
		t.Fatalf("decode put response failed: %v", err)
	}
	poly, ok := putResp["polymarket"].(map[string]any)
	if !ok {
		t.Fatalf("expected polymarket object in put response, got %T", putResp["polymarket"])
	}
	if got, _ := poly["proxy_url"].(string); got != "socks5://127.0.0.1:9050" {
		t.Fatalf("unexpected proxy_url from put response: %q", got)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/companies/"+company.ID+"/polymarket", nil)
	getRec := httptest.NewRecorder()
	h.handleCompany(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from get, got %d body=%s", getRec.Code, getRec.Body.String())
	}
	var getResp map[string]any
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response failed: %v", err)
	}
	poly, ok = getResp["polymarket"].(map[string]any)
	if !ok {
		t.Fatalf("expected polymarket object in get response, got %T", getResp["polymarket"])
	}
	if got, _ := poly["chain_id"].(float64); int(got) != 137 {
		t.Fatalf("expected chain_id 137, got %v", poly["chain_id"])
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/companies/"+company.ID+"/polymarket", nil)
	delRec := httptest.NewRecorder()
	h.handleCompany(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from delete, got %d body=%s", delRec.Code, delRec.Body.String())
	}

	getReq2 := httptest.NewRequest(http.MethodGet, "/api/companies/"+company.ID+"/polymarket", nil)
	getRec2 := httptest.NewRecorder()
	h.handleCompany(getRec2, getReq2)
	if getRec2.Code != http.StatusOK {
		t.Fatalf("expected 200 from get after delete, got %d body=%s", getRec2.Code, getRec2.Body.String())
	}
	var getResp2 map[string]any
	if err := json.NewDecoder(getRec2.Body).Decode(&getResp2); err != nil {
		t.Fatalf("decode get after delete response failed: %v", err)
	}
	if got := getResp2["polymarket"]; got != nil {
		t.Fatalf("expected nil polymarket after delete, got %#v", got)
	}
}
