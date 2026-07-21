package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateCapabilityWithSchemas(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	if _, err := svc.CreateAgent(context.Background(), "agent-cap"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	h := NewHandlers(svc, nil, nil, nil, nil)

	// Create the method definition (schemas live on the method, not the capability).
	methodBody := `{
		"method":"fulfill_order",
		"description":"Process incoming orders",
		"instructions":"Use the warehouse fulfillment API and include tracking details.",
		"input_schema":{
			"type":"object",
			"required":["order_id"],
			"properties":{"order_id":{"type":"string"}},
			"additionalProperties":false
		},
		"output_schema":{
			"type":"object",
			"required":["status"],
			"properties":{"status":{"type":"string"}},
			"additionalProperties":false
		}
	}`
	methodReq := httptest.NewRequest(http.MethodPost, "/api/a2a-methods", strings.NewReader(methodBody))
	methodRec := httptest.NewRecorder()
	h.handleA2AMethods(methodRec, methodReq)
	if methodRec.Code != http.StatusCreated {
		t.Fatalf("create method status = %d, body = %s", methodRec.Code, methodRec.Body.String())
	}

	body := `{"role":"fulfiller","method":"fulfill_order"}`
	req := httptest.NewRequest(http.MethodPost, "/api/agents/agent-cap/capabilities", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.handleCapabilities(rec, req, "agent-cap", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/agents/agent-cap/capabilities", nil)
	listRec := httptest.NewRecorder()
	h.handleCapabilities(listRec, listReq, "agent-cap", "")
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRec.Code, listRec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(listRec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode list response failed: %v", err)
	}
	rawCaps, _ := payload["capabilities"].([]any)
	if len(rawCaps) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(rawCaps))
	}
	capability, ok := rawCaps[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected capability payload type: %T", rawCaps[0])
	}
	if capability["input_schema"] == nil {
		t.Fatalf("expected input_schema in list response")
	}
	if capability["output_schema"] == nil {
		t.Fatalf("expected output_schema in list response")
	}
	if got, _ := capability["instructions"].(string); !strings.Contains(got, "warehouse fulfillment API") {
		t.Fatalf("expected instructions metadata in list response, got %q", got)
	}
}

func TestCreateMethodRejectsInvalidSchema(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	if _, err := svc.CreateAgent(context.Background(), "agent-cap"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	h := NewHandlers(svc, nil, nil, nil, nil)

	body := `{
		"method":"fulfill_order",
		"description":"Process incoming orders",
		"output_schema":{"type":"not-a-real-type"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/a2a-methods", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.handleA2AMethods(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteA2AMethodReturnsNotFoundForUnknownMethod(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/a2a-methods/unknown_method", nil)
	rec := httptest.NewRecorder()
	h.handleA2AMethods(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown method delete, got %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteA2AMethodExisting(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)

	createReq := httptest.NewRequest(http.MethodPost, "/api/a2a-methods", strings.NewReader(`{
		"method":"delete_me",
		"description":"temp"
	}`))
	createRec := httptest.NewRecorder()
	h.handleA2AMethods(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/a2a-methods/delete_me", nil)
	deleteRec := httptest.NewRecorder()
	h.handleA2AMethods(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleteRec.Code, deleteRec.Body.String())
	}
}

func TestDeleteCapabilityExisting(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	agent, err := svc.CreateAgent(ctx, "agent-delete-cap")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	if _, err := svc.CreateA2AMethod(ctx, "delete_cap_method", "", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}
	capability, err := svc.AddCapability(ctx, agent.ID, "fulfiller", "delete_cap_method")
	if err != nil {
		t.Fatalf("AddCapability failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/agents/"+agent.ID+"/capabilities/"+capability.ID, nil)
	rec := httptest.NewRecorder()
	h.handleCapabilities(rec, req, agent.ID, capability.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete capability status = %d, body = %s", rec.Code, rec.Body.String())
	}

	caps, err := svc.GetCapabilities(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetCapabilities failed: %v", err)
	}
	if len(caps) != 0 {
		t.Fatalf("expected 0 capabilities after delete, got %d", len(caps))
	}
}

func TestDeleteCapabilityIsIdempotentForMissingID(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	agent, err := svc.CreateAgent(ctx, "agent-delete-cap-404")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/agents/"+agent.ID+"/capabilities/missing", nil)
	rec := httptest.NewRecorder()
	h.handleCapabilities(rec, req, agent.ID, "missing")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete missing capability status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCapabilitiesWithCapIDMethodNotAllowed(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	agent, err := svc.CreateAgent(ctx, "agent-cap-method-check")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+agent.ID+"/capabilities/some-cap", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.handleCapabilities(rec, req, agent.ID, "some-cap")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleCapabilitiesCollectionMethodNotAllowed(t *testing.T) {
	h := &Handlers{}

	req := httptest.NewRequest(http.MethodPatch, "/api/agents/agent-x/capabilities", nil)
	rec := httptest.NewRecorder()
	h.handleCapabilities(rec, req, "agent-x", "")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCapabilityRouteRecognitionHelpers(t *testing.T) {
	if !isCapabilityCollectionMethod(http.MethodGet) || !isCapabilityCollectionMethod(http.MethodPost) {
		t.Fatalf("expected GET/POST to be recognized capability collection methods")
	}
	if isCapabilityCollectionMethod(http.MethodPatch) {
		t.Fatalf("expected PATCH to be rejected capability collection method")
	}

	if !isCapabilityMethod(http.MethodDelete) {
		t.Fatalf("expected DELETE to be recognized capability resource method")
	}
	if isCapabilityMethod(http.MethodGet) {
		t.Fatalf("expected GET to be rejected capability resource method")
	}
}
