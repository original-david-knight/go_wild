package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
)

func setupCompanyMethodToolTest(t *testing.T, method string) (context.Context, *BrokerToolsHandler, string, *data.AgentService, string, *localA2AQueue) {
	t.Helper()
	db := setupManagerTestDB(t)
	ctx := context.Background()
	h := NewBrokerToolsHandler(db)

	callerID := "company-method-caller"
	providerID := "company-method-provider"

	callerSvc := data.NewAgentService(db, callerID)
	if _, err := callerSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(caller) failed: %v", err)
	}
	providerSvc := data.NewAgentService(db, providerID)
	if _, err := providerSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent(provider) failed: %v", err)
	}

	company, err := data.CreateCompany(ctx, db, "company-method-co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, callerID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(caller) failed: %v", err)
	}
	if err := data.AddAgentToCompany(ctx, db, company.ID, providerID, "member"); err != nil {
		t.Fatalf("AddAgentToCompany(provider) failed: %v", err)
	}

	systemSvc := data.NewAgentService(db, "system")
	if _, err := systemSvc.CreateA2AMethodWithInstructions(
		ctx,
		method,
		"Fulfill an order",
		"Confirm the order_id and return a JSON object with ok=true when complete.",
		`{"type":"object","required":["order_id"],"properties":{"order_id":{"type":"string"}},"additionalProperties":false}`,
		`{"type":"object","required":["ok"],"properties":{"ok":{"type":"boolean"},"order_id":{"type":"string"}},"additionalProperties":false}`,
	); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}
	if err := providerSvc.RegisterCapability(ctx, "fulfillment", method); err != nil {
		t.Fatalf("RegisterCapability failed: %v", err)
	}

	caller, err := callerSvc.GetAgent(ctx)
	if err != nil {
		t.Fatalf("GetAgent(caller) failed: %v", err)
	}
	toolName := companyMethodToolName(method)
	caller.SetEnabledTools([]string{toolName})
	if err := callerSvc.UpdateAgent(ctx, caller); err != nil {
		t.Fatalf("UpdateAgent(caller) failed: %v", err)
	}

	return ctx, h, callerID, callerSvc, providerID, newLocalA2AQueue(db)
}

func TestBrokerCompanyMethodToolSubmitsQueuedLocalJob(t *testing.T) {
	ctx, h, callerID, callerSvc, providerID, queue := setupCompanyMethodToolTest(t, "fulfill_order")
	toolName := companyMethodToolName("fulfill_order")
	var providerHeartbeat string
	h.sendHeartbeatFn = func(agentID, message string) error {
		if strings.TrimSpace(agentID) == providerID {
			providerHeartbeat = message
		}
		return nil
	}

	prevWait := companyMethodToolWaitTimeout
	companyMethodToolWaitTimeout = 50 * time.Millisecond
	defer func() { companyMethodToolWaitTimeout = prevWait }()

	input, _ := json.Marshal(map[string]any{"order_id": "A-123"})
	resultAny, err := h.callTool(ctx, callerID, callerSvc, toolName, input)
	if err != nil {
		t.Fatalf("callTool(%s) failed: %v", toolName, err)
	}

	result, ok := resultAny.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resultAny)
	}
	if pending, _ := result["pending"].(bool); !pending {
		t.Fatalf("expected pending=true, got %#v", result["pending"])
	}
	if !strings.Contains(providerHeartbeat, "This is a heartbeat for a company method call.") {
		t.Fatalf("expected method-call heartbeat, got %q", providerHeartbeat)
	}
	if !strings.Contains(providerHeartbeat, "Method Instructions:") {
		t.Fatalf("expected method instructions in heartbeat, got %q", providerHeartbeat)
	}
	if strings.Contains(providerHeartbeat, "Claim and complete available A2A jobs.") {
		t.Fatalf("did not expect generic claim instruction in heartbeat: %q", providerHeartbeat)
	}
	if strings.Contains(strings.ToLower(providerHeartbeat), "a2a_claim_jobs") {
		t.Fatalf("did not expect claim tool instruction in method-call heartbeat: %q", providerHeartbeat)
	}
	if !strings.Contains(providerHeartbeat, `"order_id": "A-123"`) {
		t.Fatalf("expected params in heartbeat, got %q", providerHeartbeat)
	}

	jobID, _ := result["job_id"].(string)
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		t.Fatalf("expected pending response to include job_id")
	}

	claimed, err := queue.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	if got, _ := claimed["status"].(string); got != "claimed" {
		t.Fatalf("expected job to be auto-claimed, got status %q", got)
	}

	jobs, err := queue.ClaimJobs(ctx, providerID, 1, 120)
	if err != nil {
		t.Fatalf("ClaimJobs failed: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected no queued jobs after auto-claim, got %d", len(jobs))
	}

	request, ok := claimed["request"].(map[string]any)
	if !ok {
		t.Fatalf("expected request map in claimed job, got %T", claimed["request"])
	}
	if got, _ := request["method"].(string); got != "fulfill_order" {
		t.Fatalf("expected claimed request method fulfill_order, got %q", got)
	}
	params, ok := request["params"].(map[string]any)
	if !ok {
		t.Fatalf("expected params map, got %T", request["params"])
	}
	if got, _ := params["order_id"].(string); got != "A-123" {
		t.Fatalf("expected order_id A-123, got %q", got)
	}
}

func TestBrokerCompanyMethodToolValidatesInputSchema(t *testing.T) {
	ctx, h, callerID, callerSvc, _, _ := setupCompanyMethodToolTest(t, "fulfill_order_schema")
	toolName := companyMethodToolName("fulfill_order_schema")

	input, _ := json.Marshal(map[string]any{"unexpected": true})
	_, err := h.callTool(ctx, callerID, callerSvc, toolName, input)
	if err == nil {
		t.Fatalf("expected schema validation error")
	}
	if !strings.Contains(err.Error(), "input schema") {
		t.Fatalf("expected input schema error, got %v", err)
	}
}

func TestBrokerCompanyMethodToolRequeuesWhenDeliveryFails(t *testing.T) {
	ctx, h, callerID, callerSvc, providerID, queue := setupCompanyMethodToolTest(t, "fulfill_order_requeue")
	toolName := companyMethodToolName("fulfill_order_requeue")
	h.sendHeartbeatFn = func(agentID, message string) error {
		return fmt.Errorf("session unavailable for %s", strings.TrimSpace(agentID))
	}

	prevWait := companyMethodToolWaitTimeout
	companyMethodToolWaitTimeout = 50 * time.Millisecond
	defer func() { companyMethodToolWaitTimeout = prevWait }()

	input, _ := json.Marshal(map[string]any{"order_id": "B-456"})
	resultAny, err := h.callTool(ctx, callerID, callerSvc, toolName, input)
	if err != nil {
		t.Fatalf("callTool(%s) failed: %v", toolName, err)
	}
	result, ok := resultAny.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resultAny)
	}
	if pending, _ := result["pending"].(bool); !pending {
		t.Fatalf("expected pending=true, got %#v", result["pending"])
	}

	jobID, _ := result["job_id"].(string)
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		t.Fatalf("expected pending response to include job_id")
	}

	job, err := queue.GetJob(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	if got, _ := job["status"].(string); got != "queued" {
		t.Fatalf("expected queued job after delivery failure, got %q", got)
	}

	claimable, err := queue.ClaimJobs(ctx, providerID, 1, 120)
	if err != nil {
		t.Fatalf("ClaimJobs failed: %v", err)
	}
	if len(claimable) != 1 {
		t.Fatalf("expected one requeued job to be claimable, got %d", len(claimable))
	}
}
