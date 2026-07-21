package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

func TestClaimedMethodJobIDFromHeartbeat(t *testing.T) {
	msg := `This is a heartbeat for a company method call.

Job ID: abc-123
Method: fulfill_order`

	jobID, ok := claimedMethodJobIDFromHeartbeat(msg)
	if !ok {
		t.Fatalf("expected method-call heartbeat to parse")
	}
	if jobID != "abc-123" {
		t.Fatalf("expected job id abc-123, got %q", jobID)
	}
}

func TestClaimedMethodCallConfigFromHeartbeat_FreshContext(t *testing.T) {
	msg := `This is a heartbeat for a company method call.

Job ID: abc-123
Fresh Context: true
Method: fulfill_order`

	cfg, ok := claimedMethodCallConfigFromHeartbeat(msg)
	if !ok {
		t.Fatalf("expected method-call heartbeat to parse")
	}
	if cfg.JobID != "abc-123" {
		t.Fatalf("expected job id abc-123, got %q", cfg.JobID)
	}
	if cfg.Method != "fulfill_order" {
		t.Fatalf("expected method fulfill_order, got %q", cfg.Method)
	}
	if !cfg.FreshContext {
		t.Fatalf("expected fresh context to be parsed")
	}
}

func TestClaimedMethodJobIDFromHeartbeat_NoMatch(t *testing.T) {
	if _, ok := claimedMethodJobIDFromHeartbeat("This is a normal heartbeat."); ok {
		t.Fatalf("expected non-method heartbeat to be ignored")
	}
}

func TestHistoryForClaimedMethodCall(t *testing.T) {
	base := []loop.Message{
		loop.NewUserMessage("existing context"),
	}

	withExisting := historyForClaimedMethodCall(base, "do the work", false)
	if len(withExisting) != 2 {
		t.Fatalf("expected existing history to be preserved, got %d messages", len(withExisting))
	}

	fresh := historyForClaimedMethodCall(base, "do the work", true)
	if len(fresh) != 1 {
		t.Fatalf("expected fresh method call history to contain only the method prompt, got %d messages", len(fresh))
	}
	if text := loop.ExtractText(fresh[0].Content); text != "do the work" {
		t.Fatalf("expected fresh method call prompt to be preserved, got %q", text)
	}
	if len(base) != 1 {
		t.Fatalf("expected base history to remain unchanged, got %d messages", len(base))
	}
}

func TestFinalizeClaimedMethodCallHistory(t *testing.T) {
	base := []loop.Message{loop.NewUserMessage("existing context")}
	updated := []loop.Message{loop.NewUserMessage("new context")}

	if got := finalizeClaimedMethodCallHistory(base, updated, false); len(got) != len(updated) {
		t.Fatalf("expected non-fresh method call to keep updated history")
	}
	if got := finalizeClaimedMethodCallHistory(base, updated, true); len(got) != len(base) {
		t.Fatalf("expected fresh method call to restore previous history")
	}
}

func TestParseMethodResultJSONMap(t *testing.T) {
	out, err := parseMethodResultJSONMap(`{"ok":true,"order_id":"A-123"}`)
	if err != nil {
		t.Fatalf("parseMethodResultJSONMap failed: %v", err)
	}
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got %#v", out["ok"])
	}
}

func TestParseMethodResultJSONMap_CodeFence(t *testing.T) {
	out, err := parseMethodResultJSONMap("```json\n{\"ok\":true}\n```")
	if err != nil {
		t.Fatalf("parseMethodResultJSONMap failed: %v", err)
	}
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got %#v", out["ok"])
	}
}

func TestParseMethodResultJSONMap_Invalid(t *testing.T) {
	if _, err := parseMethodResultJSONMap("not json"); err == nil {
		t.Fatalf("expected parse error for invalid json")
	}
}

func TestAutoCompleteClaimedMethodJob_Succeeds(t *testing.T) {
	var callCount int
	var gotBody map[string]any

	prevClient := globalBrokerClient
	globalBrokerClient = newTestBrokerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path != "/broker/v1/tools/job_result" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body failed: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer func() { globalBrokerClient = prevClient }()

	autoCompleteClaimedMethodJob(context.Background(), "job-1", `{"ok":true,"order_id":"A-123"}`, "")

	if callCount != 1 {
		t.Fatalf("expected 1 broker call, got %d", callCount)
	}
	if got, _ := gotBody["job_id"].(string); got != "job-1" {
		t.Fatalf("expected job_id job-1, got %q", got)
	}
	if got, _ := gotBody["status"].(string); got != "succeeded" {
		t.Fatalf("expected status succeeded, got %q", got)
	}
	result, ok := gotBody["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", gotBody["result"])
	}
	if got, _ := result["ok"].(bool); !got {
		t.Fatalf("expected result.ok true, got %#v", result["ok"])
	}
}

func TestAutoCompleteClaimedMethodJob_ParseErrorMarksFailed(t *testing.T) {
	var callCount int
	var gotBody map[string]any

	prevClient := globalBrokerClient
	globalBrokerClient = newTestBrokerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path != "/broker/v1/tools/job_result" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body failed: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer func() { globalBrokerClient = prevClient }()

	autoCompleteClaimedMethodJob(context.Background(), "job-2", "not-json-output", "")

	if callCount != 1 {
		t.Fatalf("expected 1 broker call, got %d", callCount)
	}
	if got, _ := gotBody["status"].(string); got != "failed" {
		t.Fatalf("expected status failed, got %q", got)
	}
	errPayload, ok := gotBody["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error map, got %T", gotBody["error"])
	}
	msg, _ := errPayload["message"].(string)
	if !strings.Contains(msg, "must be a JSON object") {
		t.Fatalf("expected parse error message, got %q", msg)
	}
}

func TestAutoCompleteClaimedMethodJob_RejectedResultFallsBackToFailed(t *testing.T) {
	var callCount int
	var statuses []string

	prevClient := globalBrokerClient
	globalBrokerClient = newTestBrokerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path != "/broker/v1/tools/job_result" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body failed: %v", err)
		}
		status, _ := body["status"].(string)
		statuses = append(statuses, status)
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "schema mismatch"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer func() { globalBrokerClient = prevClient }()

	autoCompleteClaimedMethodJob(context.Background(), "job-3", `{"ok":"bad-type"}`, "")

	if callCount != 2 {
		t.Fatalf("expected 2 broker calls (succeeded then failed), got %d", callCount)
	}
	if len(statuses) != 2 || statuses[0] != "succeeded" || statuses[1] != "failed" {
		t.Fatalf("unexpected status sequence: %#v", statuses)
	}
}

func TestAutoCompleteClaimedMethodJob_AlreadyFinalizedSkipsFallback(t *testing.T) {
	var callCount int
	var statuses []string

	prevClient := globalBrokerClient
	globalBrokerClient = newTestBrokerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path != "/broker/v1/tools/job_result" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body failed: %v", err)
		}
		status, _ := body["status"].(string)
		statuses = append(statuses, status)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": `invalid job state "failed"`})
	}))
	defer func() { globalBrokerClient = prevClient }()

	autoCompleteClaimedMethodJob(context.Background(), "job-3b", `{"ok":true}`, "")

	if callCount != 1 {
		t.Fatalf("expected 1 broker call for already-finalized job, got %d", callCount)
	}
	if len(statuses) != 1 || statuses[0] != "succeeded" {
		t.Fatalf("unexpected status sequence: %#v", statuses)
	}
}

func TestAutoCompleteClaimedMethodJob_ResultStatusFailedMarksFailed(t *testing.T) {
	var callCount int
	var gotBody map[string]any

	prevClient := globalBrokerClient
	globalBrokerClient = newTestBrokerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path != "/broker/v1/tools/job_result" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body failed: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer func() { globalBrokerClient = prevClient }()

	autoCompleteClaimedMethodJob(context.Background(), "job-4", `{"status":"FAILED","reason":"policy blocked"}`, "")

	if callCount != 1 {
		t.Fatalf("expected 1 broker call, got %d", callCount)
	}
	if got, _ := gotBody["status"].(string); got != "failed" {
		t.Fatalf("expected status failed, got %q", got)
	}
	errPayload, ok := gotBody["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error map, got %T", gotBody["error"])
	}
	msg, _ := errPayload["message"].(string)
	if msg != "policy blocked" {
		t.Fatalf("expected policy blocked message, got %q", msg)
	}
}

func TestMethodResultMarkedFailed_WrappedStatus(t *testing.T) {
	failed, reason := methodResultMarkedFailed(map[string]any{
		"result": map[string]any{
			"status": "FAILED",
			"reason": "bad input",
		},
	})
	if !failed {
		t.Fatalf("expected wrapped failed status to be detected")
	}
	if reason != "bad input" {
		t.Fatalf("expected reason bad input, got %q", reason)
	}
}

func TestAutoCompleteClaimedMethodJob_UnwrapsSucceededEnvelopeObject(t *testing.T) {
	var gotBody map[string]any

	prevClient := globalBrokerClient
	globalBrokerClient = newTestBrokerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/broker/v1/tools/job_result" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body failed: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer func() { globalBrokerClient = prevClient }()

	autoCompleteClaimedMethodJob(context.Background(), "job-5", `{"status":"succeeded","result":{"markets":[{"id":"m1"}]}}`, "")

	if got, _ := gotBody["status"].(string); got != "succeeded" {
		t.Fatalf("expected status succeeded, got %q", got)
	}
	result, ok := gotBody["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", gotBody["result"])
	}
	if _, hasStatus := result["status"]; hasStatus {
		t.Fatalf("expected unwrapped payload without status key")
	}
	if _, hasMarkets := result["markets"]; !hasMarkets {
		t.Fatalf("expected unwrapped payload to include markets")
	}
}

func TestAutoCompleteClaimedMethodJob_UnwrapsSucceededEnvelopeStringResult(t *testing.T) {
	var gotBody map[string]any

	prevClient := globalBrokerClient
	globalBrokerClient = newTestBrokerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/broker/v1/tools/job_result" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body failed: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer func() { globalBrokerClient = prevClient }()

	autoCompleteClaimedMethodJob(context.Background(), "job-6", `{"status":"succeeded","result":"{\"markets\":[{\"id\":\"m2\"}]}"}`, "")

	result, ok := gotBody["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", gotBody["result"])
	}
	markets, ok := result["markets"].([]any)
	if !ok || len(markets) != 1 {
		t.Fatalf("expected one market in unwrapped result, got %#v", result["markets"])
	}
}

func TestIsMethodJobAlreadyFinalizedError(t *testing.T) {
	if !isMethodJobAlreadyFinalizedError(fmt.Errorf(`broker error (500): invalid job state "failed"`)) {
		t.Fatalf("expected failed-state error to be recognized")
	}
	if !isMethodJobAlreadyFinalizedError(fmt.Errorf(`broker error (500): invalid job state "succeeded"`)) {
		t.Fatalf("expected succeeded-state error to be recognized")
	}
	if isMethodJobAlreadyFinalizedError(fmt.Errorf("broker error (500): invalid job state \"claimed\"")) {
		t.Fatalf("did not expect claimed-state error to be treated as finalized")
	}
	if isMethodJobAlreadyFinalizedError(nil) {
		t.Fatalf("nil error should not be treated as finalized")
	}
}
