package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestA2AMethodConfigPersistsInstructions(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	if _, err := svc.CreateAgent(context.Background(), "agent-method-config"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	h := NewHandlers(svc, nil, nil, nil, nil)

	createBody := `{
		"method":"fulfill_order",
		"description":"Process incoming orders",
		"instructions":"Validate inventory before fulfillment and include tracking in result",
		"auto_market_note":true,
		"fresh_context":true,
		"redact_market_prices":true,
		"disable_market_notes":true,
		"disable_polymarket_note_augmentation":true,
		"input_schema":{"type":"object"},
		"output_schema":{"type":"object"}
	}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/a2a-methods", strings.NewReader(createBody))
	createRec := httptest.NewRecorder()
	h.handleA2AMethods(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	var createResp map[string]any
	if err := json.NewDecoder(createRec.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode create response failed: %v", err)
	}
	if got, _ := createResp["instructions"].(string); !strings.Contains(got, "tracking") {
		t.Fatalf("expected instructions in create response, got %q", got)
	}
	if got, _ := createResp["auto_market_note"].(bool); !got {
		t.Fatalf("expected auto_market_note in create response")
	}
	if got, _ := createResp["fresh_context"].(bool); !got {
		t.Fatalf("expected fresh_context in create response")
	}
	if got, _ := createResp["redact_market_prices"].(bool); !got {
		t.Fatalf("expected redact_market_prices in create response")
	}
	if got, _ := createResp["disable_market_notes"].(bool); !got {
		t.Fatalf("expected disable_market_notes in create response")
	}
	if got, _ := createResp["disable_polymarket_note_augmentation"].(bool); !got {
		t.Fatalf("expected disable_polymarket_note_augmentation in create response")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/a2a-methods/fulfill_order", nil)
	getRec := httptest.NewRecorder()
	h.handleA2AMethods(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
	var getResp map[string]any
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response failed: %v", err)
	}
	if got, _ := getResp["instructions"].(string); !strings.Contains(got, "Validate inventory") {
		t.Fatalf("expected instructions in get response, got %q", got)
	}
	if got, _ := getResp["auto_market_note"].(bool); !got {
		t.Fatalf("expected auto_market_note in get response")
	}
	if got, _ := getResp["fresh_context"].(bool); !got {
		t.Fatalf("expected fresh_context in get response")
	}
	if got, _ := getResp["redact_market_prices"].(bool); !got {
		t.Fatalf("expected redact_market_prices in get response")
	}
	if got, _ := getResp["disable_market_notes"].(bool); !got {
		t.Fatalf("expected disable_market_notes in get response")
	}
	if got, _ := getResp["disable_polymarket_note_augmentation"].(bool); !got {
		t.Fatalf("expected disable_polymarket_note_augmentation in get response")
	}

	updateBody := `{
		"description":"Process incoming orders",
		"instructions":"Use carrier API and include shipment_id in the result",
		"auto_market_note":false,
		"fresh_context":false,
		"redact_market_prices":false,
		"disable_market_notes":false,
		"disable_polymarket_note_augmentation":false
	}`
	updateReq := httptest.NewRequest(http.MethodPut, "/api/a2a-methods/fulfill_order", strings.NewReader(updateBody))
	updateRec := httptest.NewRecorder()
	h.handleA2AMethods(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updateRec.Code, updateRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/a2a-methods", nil)
	listRec := httptest.NewRecorder()
	h.handleA2AMethods(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	var listPayload map[string]any
	if err := json.NewDecoder(listRec.Body).Decode(&listPayload); err != nil {
		t.Fatalf("decode list response failed: %v", err)
	}
	rawMethods, _ := listPayload["methods"].([]any)
	if len(rawMethods) != 1 {
		t.Fatalf("expected 1 method in list, got %d", len(rawMethods))
	}
	methodRow, ok := rawMethods[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected method row type: %T", rawMethods[0])
	}
	if got, _ := methodRow["instructions"].(string); !strings.Contains(got, "carrier API") {
		t.Fatalf("expected updated instructions in list response, got %q", got)
	}
	if got, _ := methodRow["auto_market_note"].(bool); got {
		t.Fatalf("expected updated auto_market_note=false in list response")
	}
	if got, _ := methodRow["fresh_context"].(bool); got {
		t.Fatalf("expected updated fresh_context=false in list response")
	}
	if got, _ := methodRow["redact_market_prices"].(bool); got {
		t.Fatalf("expected updated redact_market_prices=false in list response")
	}
	if got, _ := methodRow["disable_market_notes"].(bool); got {
		t.Fatalf("expected updated disable_market_notes=false in list response")
	}
	if got, _ := methodRow["disable_polymarket_note_augmentation"].(bool); got {
		t.Fatalf("expected updated disable_polymarket_note_augmentation=false in list response")
	}
}

func TestHandleA2AMethodsMethodGuards(t *testing.T) {
	h := &Handlers{}

	collectionReq := httptest.NewRequest(http.MethodPatch, "/api/a2a-methods", nil)
	collectionRec := httptest.NewRecorder()
	h.handleA2AMethods(collectionRec, collectionReq)
	if collectionRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected collection wrong-method status %d, got %d body=%s", http.StatusMethodNotAllowed, collectionRec.Code, collectionRec.Body.String())
	}

	methodReq := httptest.NewRequest(http.MethodPost, "/api/a2a-methods/method-x", nil)
	methodRec := httptest.NewRecorder()
	h.handleA2AMethods(methodRec, methodReq)
	if methodRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected method wrong-method status %d, got %d body=%s", http.StatusMethodNotAllowed, methodRec.Code, methodRec.Body.String())
	}
}

func TestA2AMethodRouteRecognitionHelpers(t *testing.T) {
	if !isA2AMethodCollectionMethod(http.MethodGet) || !isA2AMethodCollectionMethod(http.MethodPost) {
		t.Fatalf("expected GET/POST to be recognized A2A method collection methods")
	}
	if isA2AMethodCollectionMethod(http.MethodPatch) {
		t.Fatalf("expected PATCH to be rejected A2A method collection method")
	}

	if !isA2AMethodMethod(http.MethodGet) || !isA2AMethodMethod(http.MethodPut) || !isA2AMethodMethod(http.MethodDelete) {
		t.Fatalf("expected GET/PUT/DELETE to be recognized A2A method methods")
	}
	if isA2AMethodMethod(http.MethodPost) {
		t.Fatalf("expected POST to be rejected A2A method method")
	}
}

func TestParseA2AMethodRoute(t *testing.T) {
	if got := parseA2AMethodRoute("/api/a2a-methods"); got != "" {
		t.Fatalf("expected collection route to parse empty method, got %q", got)
	}
	if got := parseA2AMethodRoute("/api/a2a-methods/fulfill_order"); got != "fulfill_order" {
		t.Fatalf("expected method route to parse method, got %q", got)
	}
	if got := parseA2AMethodRoute("/api/a2a-methods/fulfill_order/extra"); got != "fulfill_order" {
		t.Fatalf("expected extra route parts to preserve first method segment, got %q", got)
	}
}
