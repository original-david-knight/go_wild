package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestA2AJobLifecycle(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()
	handler := srv.handler()

	_, makeSenderReq := makePremiumAgent(t, srv)
	recipientID, makeRecipientReq := makePremiumAgent(t, srv)

	submitBody := []byte(`{
		"to_public_key":"` + recipientID + `",
		"idempotency_key":"idem-1",
		"request":{"method":"summarize","params":{"text":"hello"}}
	}`)
	req := makeSenderReq(http.MethodPost, "/api/v1/a2a/jobs", submitBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("submit expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var submitResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&submitResp); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	jobID, _ := submitResp["job_id"].(string)
	if jobID == "" {
		t.Fatal("expected non-empty job_id")
	}

	req = makeRecipientReq(http.MethodPost, "/api/v1/a2a/jobs/claim", []byte(`{"max_jobs":1}`))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("claim expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var claimResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&claimResp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	jobs, _ := claimResp["jobs"].([]any)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 claimed job, got %d", len(jobs))
	}

	completeBody := []byte(`{
		"status":"succeeded",
		"result":{"summary":"done"}
	}`)
	req = makeRecipientReq(http.MethodPost, "/api/v1/a2a/jobs/"+jobID+"/complete", completeBody)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("complete expected 200, got %d: %s", w.Code, w.Body.String())
	}

	req = makeSenderReq(http.MethodGet, "/api/v1/a2a/jobs/"+jobID, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var getResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if status, _ := getResp["status"].(string); status != "succeeded" {
		t.Fatalf("expected succeeded status, got %v", getResp["status"])
	}
}

func TestA2AJobForbiddenForUnrelatedAgent(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()
	handler := srv.handler()

	_, makeSenderReq := makePremiumAgent(t, srv)
	recipientID, _ := makePremiumAgent(t, srv)
	_, makeThirdReq := makePremiumAgent(t, srv)

	submitBody := []byte(`{
		"to_public_key":"` + recipientID + `",
		"request":{"method":"ping","params":{"n":1}}
	}`)
	req := makeSenderReq(http.MethodPost, "/api/v1/a2a/jobs", submitBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("submit expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var submitResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&submitResp); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	jobID, _ := submitResp["job_id"].(string)

	req = makeThirdReq(http.MethodGet, "/api/v1/a2a/jobs/"+jobID, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unrelated agent, got %d: %s", w.Code, w.Body.String())
	}
}
