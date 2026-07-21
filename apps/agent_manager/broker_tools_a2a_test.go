package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

func TestCallA2AToolsUnknownToolIsUnhandled(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	svc := data.NewAgentService(db, "a2a-agent")

	handled, result, err := h.callA2ATools(context.Background(), "a2a-agent", svc, "not_an_a2a_tool", nil)
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

func TestCallA2AToolsRemovedToolReturnsError(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	svc := data.NewAgentService(db, "a2a-agent")

	handled, result, err := h.callA2ATools(context.Background(), "a2a-agent", svc, "a2a_claim_jobs", []byte(`{}`))
	if !handled {
		t.Fatalf("expected removed tool to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result for removed tool, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected removed-tool error")
	}
	if !strings.Contains(err.Error(), "has been removed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallA2AToolsJobResultRequiresJobID(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	svc := data.NewAgentService(db, "a2a-agent")

	handled, result, err := h.callA2ATools(context.Background(), "a2a-agent", svc, "job_result", []byte(`{}`))
	if !handled {
		t.Fatalf("expected job_result to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result on validation error, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected job_id validation error")
	}
	if !strings.Contains(err.Error(), "job_id is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallA2AToolsJobResultRequiresValidStatus(t *testing.T) {
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	svc := data.NewAgentService(db, "a2a-agent")

	handled, result, err := h.callA2ATools(context.Background(), "a2a-agent", svc, "job_result", []byte(`{"job_id":"j1","status":"queued"}`))
	if !handled {
		t.Fatalf("expected job_result to be handled")
	}
	if result != nil {
		t.Fatalf("expected nil result on validation error, got %#v", result)
	}
	if err == nil {
		t.Fatalf("expected status validation error")
	}
	if !strings.Contains(err.Error(), "status must be succeeded or failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsA2AToolRecognition(t *testing.T) {
	if !isA2ATool("job_result") {
		t.Fatalf("expected job_result to be recognized")
	}
	if !isA2ATool("a2a_claim_jobs") {
		t.Fatalf("expected removed legacy a2a tool to still be recognized")
	}
	if isA2ATool("a2a_not_real") {
		t.Fatalf("expected unknown a2a tool to be rejected")
	}
}

func TestCallA2AToolsJobResultAddsMarketNoteOnFailureWhenEnabled(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	systemSvc := data.NewAgentService(db, "system")
	workerSvc := NewAgentService(db)

	if _, err := workerSvc.CreateAgent(ctx, "poly-position-checker"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	company, err := data.CreateCompany(ctx, db, "Polymarket Pipeline", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, "poly-position-checker", "trader"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}
	if _, err := systemSvc.CreateA2AMethodWithConfig(ctx, "polymarket_check_position", "check a market", "", "", "", true, false, false, false, false); err != nil {
		t.Fatalf("CreateA2AMethodWithConfig failed: %v", err)
	}

	jobID := prepareClaimedMethodJobForTest(t, db, "poly-position-checker", "polymarket_check_position", map[string]any{
		"market": map[string]any{
			"condition_id": "cond-1",
			"question":     "Will OpenAI have the best AI model at the end of March 2026?",
		},
	})

	input := map[string]any{
		"job_id": jobID,
		"status": "failed",
		"error": map[string]any{
			"message": "method response must be a JSON object",
			"details": map[string]any{
				"response_preview": `{"condition_id":"cond-1","action_taken":"PASSED","reason":"Resolution date is in the past.","current_position":0}`,
			},
		},
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	handled, result, err := h.callA2ATools(ctx, "poly-position-checker", data.NewAgentService(db, "poly-position-checker"), "job_result", inputJSON)
	if err != nil {
		t.Fatalf("callA2ATools failed: %v", err)
	}
	if !handled {
		t.Fatalf("expected job_result to be handled")
	}
	if result == nil {
		t.Fatalf("expected completion result")
	}

	notes, err := data.ListMarketNotes(ctx, db, company.ID, "cond-1", 10)
	if err != nil {
		t.Fatalf("ListMarketNotes failed: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 market note, got %d", len(notes))
	}
	if notes[0].CreatedByAgentID != "poly-position-checker" {
		t.Fatalf("expected note to be attributed to completing agent, got %q", notes[0].CreatedByAgentID)
	}
	if !strings.Contains(notes[0].Content, "Status: FAILED") {
		t.Fatalf("expected failed status in note, got %q", notes[0].Content)
	}
	if !strings.Contains(notes[0].Content, "Action: PASSED") {
		t.Fatalf("expected parsed response preview action in note, got %q", notes[0].Content)
	}
	if !strings.Contains(notes[0].Content, "Error: method response must be a JSON object") {
		t.Fatalf("expected error message in note, got %q", notes[0].Content)
	}
}

func TestCallA2AToolsJobResultSkipsMarketNoteForLowVolumeMarket(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	systemSvc := data.NewAgentService(db, "system")
	workerSvc := NewAgentService(db)

	if _, err := workerSvc.CreateAgent(ctx, "poly-position-checker"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	company, err := data.CreateCompany(ctx, db, "Polymarket Pipeline", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, "poly-position-checker", "trader"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}
	if _, err := systemSvc.CreateA2AMethodWithConfig(ctx, "polymarket_check_position", "check a market", "", "", "", true, false, false, false, false); err != nil {
		t.Fatalf("CreateA2AMethodWithConfig failed: %v", err)
	}

	jobID := prepareClaimedMethodJobForTest(t, db, "poly-position-checker", "polymarket_check_position", map[string]any{
		"market": map[string]any{
			"condition_id": "cond-low-volume",
			"question":     "Will OpenAI have the best AI model at the end of March 2026?",
			"volume":       49999.99,
		},
	})

	inputJSON := []byte(fmt.Sprintf(`{"job_id":%q,"status":"failed","error":{"message":"tool failure"}}`, jobID))
	handled, result, err := h.callA2ATools(ctx, "poly-position-checker", data.NewAgentService(db, "poly-position-checker"), "job_result", inputJSON)
	if err != nil {
		t.Fatalf("callA2ATools failed: %v", err)
	}
	if !handled {
		t.Fatalf("expected job_result to be handled")
	}
	if result == nil {
		t.Fatalf("expected completion result")
	}

	notes, err := data.ListMarketNotes(ctx, db, company.ID, "cond-low-volume", 10)
	if err != nil {
		t.Fatalf("ListMarketNotes failed: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("expected no market notes for low-volume market, got %d", len(notes))
	}
}

func TestCallA2AToolsJobResultSkipsMarketNoteWhenDisabled(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	systemSvc := data.NewAgentService(db, "system")
	workerSvc := NewAgentService(db)

	if _, err := workerSvc.CreateAgent(ctx, "poly-position-checker"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	company, err := data.CreateCompany(ctx, db, "Polymarket Pipeline", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, "poly-position-checker", "trader"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}
	if _, err := systemSvc.CreateA2AMethodWithConfig(ctx, "polymarket_check_position", "check a market", "", "", "", false, false, false, false, false); err != nil {
		t.Fatalf("CreateA2AMethodWithConfig failed: %v", err)
	}

	jobID := prepareClaimedMethodJobForTest(t, db, "poly-position-checker", "polymarket_check_position", map[string]any{
		"market": map[string]any{
			"condition_id": "cond-2",
		},
	})

	inputJSON := []byte(fmt.Sprintf(`{"job_id":%q,"status":"failed","error":{"message":"tool failure"}}`, jobID))
	handled, result, err := h.callA2ATools(ctx, "poly-position-checker", data.NewAgentService(db, "poly-position-checker"), "job_result", inputJSON)
	if err != nil {
		t.Fatalf("callA2ATools failed: %v", err)
	}
	if !handled {
		t.Fatalf("expected job_result to be handled")
	}
	if result == nil {
		t.Fatalf("expected completion result")
	}

	notes, err := data.ListMarketNotes(ctx, db, company.ID, "cond-2", 10)
	if err != nil {
		t.Fatalf("ListMarketNotes failed: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("expected no market notes, got %d", len(notes))
	}
}

func TestCallA2AToolsJobResultSkipsRedundantResearchFailureMarketNoteForGenericMethod(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	h := NewBrokerToolsHandler(db)
	systemSvc := data.NewAgentService(db, "system")
	workerSvc := NewAgentService(db)

	if _, err := workerSvc.CreateAgent(ctx, "poly-researcher"); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	company, err := data.CreateCompany(ctx, db, "Polymarket Pipeline", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, "poly-researcher", "polymarket_researcher"); err != nil {
		t.Fatalf("AddAgentToCompany failed: %v", err)
	}
	if _, err := systemSvc.CreateA2AMethodWithConfig(ctx, "custom_market_review", "review a market", "", "", "", true, false, true, false, false); err != nil {
		t.Fatalf("CreateA2AMethodWithConfig failed: %v", err)
	}

	jobID := prepareClaimedMethodJobForTest(t, db, "poly-researcher", "custom_market_review", map[string]any{
		"payload": map[string]any{
			"condition_id": "cond-reeval",
			"question":     "Will test happen?",
		},
	})

	inputJSON := []byte(fmt.Sprintf(`{"job_id":%q,"status":"FAILED","error":{"message":"This market position has already been completely researched and evaluated by poly-researcher on March 7, 2026. There are no new developments or changes to the thesis that warrant further redundant re-evaluation today."}}`, jobID))
	handled, result, err := h.callA2ATools(ctx, "poly-researcher", data.NewAgentService(db, "poly-researcher"), "job_result", inputJSON)
	if err != nil {
		t.Fatalf("callA2ATools failed: %v", err)
	}
	if !handled {
		t.Fatalf("expected job_result to be handled")
	}
	if result == nil {
		t.Fatalf("expected completion result")
	}

	notes, err := data.ListMarketNotes(ctx, db, company.ID, "cond-reeval", 10)
	if err != nil {
		t.Fatalf("ListMarketNotes failed: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("expected redundant re-research failure to skip market notes, got %d", len(notes))
	}
}

func prepareClaimedMethodJobForTest(t *testing.T, db gowild_data.Database, agentID, method string, params map[string]any) string {
	t.Helper()

	queue := newLocalA2AQueue(db)
	job, reused, err := queue.Submit(context.Background(), "pipeline", "", "", localA2ARequest{
		Method: method,
		Params: params,
	})
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}
	if reused {
		t.Fatalf("expected unique test job")
	}

	jobID := fmt.Sprint(job["job_id"])
	if _, err := queue.ClaimJob(context.Background(), agentID, jobID, 60); err != nil {
		t.Fatalf("ClaimJob failed: %v", err)
	}
	return jobID
}
