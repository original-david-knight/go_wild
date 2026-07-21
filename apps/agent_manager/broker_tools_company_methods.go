package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

const (
	companyMethodToolPollEvery = 300 * time.Millisecond
)

var companyMethodToolWaitTimeout = 20 * time.Second

func (h *BrokerToolsHandler) callCompanyMethodTools(ctx context.Context, agentID string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
	_ = svc
	toolName = strings.TrimSpace(toolName)
	spec, ok, err := companyMethodToolSpecForAgent(ctx, h.db, agentID, toolName)
	if err != nil {
		return true, nil, err
	}
	if !ok {
		return false, nil, nil
	}
	if len(spec.ProviderAgentIDs) == 0 {
		return true, nil, fmt.Errorf("no company providers available for method %q", spec.Method)
	}
	// Resolve method name from the first provider (all providers share the same method).
	targetMethod := strings.TrimSpace(spec.targetMethodForProvider(strings.TrimSpace(spec.ProviderAgentIDs[0])))
	if targetMethod == "" {
		targetMethod = strings.TrimSpace(spec.Method)
	}

	params := map[string]any{}
	if len(inputJSON) > 0 {
		if err := json.Unmarshal(inputJSON, &params); err != nil {
			return true, nil, fmt.Errorf("failed to unmarshal input: %w", err)
		}
		if params == nil {
			params = map[string]any{}
		}
	}

	if err := validatePayloadForMethod(ctx, h.db, targetMethod, capabilitySchemaInput, params); err != nil {
		return true, nil, err
	}

	// Submit as pool job (no target agent). The queue assigns agents at claim time.
	queue := newLocalA2AQueue(h.db)
	job, _, err := queue.Submit(ctx, strings.TrimSpace(agentID), "", "", localA2ARequest{
		Method: targetMethod,
		Params: params,
	})
	if err != nil {
		return true, nil, err
	}
	jobID, _ := job["job_id"].(string)
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return true, nil, fmt.Errorf("failed to enqueue company method job")
	}

	methodDef, _ := h.loadA2AMethodDefinition(ctx, targetMethod)

	// Try each provider in order; deliver to the first free one.
	for _, providerID := range spec.ProviderAgentIDs {
		providerID = strings.TrimSpace(providerID)
		if providerID == "" {
			continue
		}
		if queue.IsAgentBusy(ctx, providerID) {
			continue
		}
		h.tryDeliverCompanyMethodJob(ctx, queue, agentID, providerID, jobID, spec, methodDef)
		break
	}

	result, pending, err := waitForCompanyMethodResult(ctx, queue, jobID, spec.Method, companyMethodToolWaitTimeout)
	if err != nil {
		return true, nil, err
	}
	if pending {
		return true, result, nil
	}
	return true, result, nil
}

func (h *BrokerToolsHandler) sendCompanyMethodHeartbeat(agentID, message string) {
	if err := h.sendAgentHeartbeat(agentID, message); err != nil {
		log.Printf("Company method: failed to send heartbeat to %s: %v", agentID, err)
	}
}

func (h *BrokerToolsHandler) sendAgentHeartbeat(agentID, message string) error {
	agentID = strings.TrimSpace(agentID)
	message = strings.TrimSpace(message)
	if agentID == "" || message == "" {
		return nil
	}
	if h.sendHeartbeatFn != nil {
		return h.sendHeartbeatFn(agentID, message)
	}
	if h.workerManager != nil {
		return h.workerManager.SendHeartbeat(agentID, message)
	}
	return nil
}

func (h *BrokerToolsHandler) isWorkerModeAgent(ctx context.Context, agentID string) (bool, error) {
	_ = ctx
	_ = agentID
	// Worker mode has been removed.
	return false, nil
}

func (h *BrokerToolsHandler) loadA2AMethodDefinition(ctx context.Context, method string) (*data.A2AMethod, error) {
	method = strings.TrimSpace(method)
	if method == "" {
		return nil, fmt.Errorf("method is required")
	}
	return data.NewAgentService(h.db, "system").GetA2AMethod(ctx, method)
}

func (h *BrokerToolsHandler) tryDeliverCompanyMethodJob(
	ctx context.Context,
	queue *localA2AQueue,
	callerAgentID,
	targetAgentID,
	jobID string,
	spec companyMethodToolSpec,
	methodDef *data.A2AMethod,
) {
	if h.sendHeartbeatFn == nil && h.workerManager == nil {
		return
	}

	claimedJob, err := queue.ClaimJob(ctx, strings.TrimSpace(targetAgentID), strings.TrimSpace(jobID), localA2AMaxClaimLeaseSeconds)
	if err != nil {
		log.Printf("Company method: failed to claim job %s for %s: %v", strings.TrimSpace(jobID), strings.TrimSpace(targetAgentID), err)
		return
	}

	message := buildClaimedCompanyMethodHeartbeat(callerAgentID, claimedJob, spec, methodDef)
	if err := h.sendAgentHeartbeat(targetAgentID, message); err != nil {
		log.Printf("Company method: failed to deliver claimed job %s to %s: %v", strings.TrimSpace(jobID), strings.TrimSpace(targetAgentID), err)
		if requeueErr := queue.RequeueClaimedJob(ctx, strings.TrimSpace(targetAgentID), strings.TrimSpace(jobID)); requeueErr != nil {
			log.Printf("Company method: failed to requeue claimed job %s for %s after delivery error: %v", strings.TrimSpace(jobID), strings.TrimSpace(targetAgentID), requeueErr)
		}
		return
	}
}

// deliverQueuedCompanyMethodJobs finds a single queued pool job matching the
// agent's capabilities, claims it, and delivers a heartbeat. Returns 1 if a
// job was delivered, 0 otherwise. Only delivers if the agent is free.
func (h *BrokerToolsHandler) deliverQueuedCompanyMethodJobs(ctx context.Context, targetAgentID string, _ int) (int, error) {
	targetAgentID = strings.TrimSpace(targetAgentID)
	if targetAgentID == "" {
		return 0, fmt.Errorf("agent_id is required")
	}
	if h.sendHeartbeatFn == nil && h.workerManager == nil {
		return 0, nil
	}

	queue := newLocalA2AQueue(h.db)

	// Single-job-per-agent: skip if already working.
	if queue.IsAgentBusy(ctx, targetAgentID) {
		return 0, nil
	}

	// Discover which methods this agent can handle.
	svc := data.NewAgentService(h.db, targetAgentID)
	caps, err := svc.GetCapabilities(ctx)
	if err != nil || len(caps) == 0 {
		return 0, nil
	}
	methods := make(map[string]bool, len(caps))
	for _, c := range caps {
		if m := strings.TrimSpace(c.Method); m != "" {
			methods[m] = true
		}
	}

	// Find queued pool jobs and pick the first one this agent can handle.
	dao := h.db.Table(localA2AJob{})
	results, err := dao.Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{
			"status":      localA2AStatusQueued,
			"to_agent_id": "",
		},
		OrderBy: "created_at",
		Limit:   50,
	})
	if err != nil {
		return 0, err
	}

	for _, row := range results {
		job := row.(*localA2AJob)
		var req localA2ARequest
		_ = json.Unmarshal([]byte(job.RequestJSON), &req)
		if !methods[strings.TrimSpace(req.Method)] {
			continue
		}

		claimedJob, err := queue.ClaimJob(ctx, targetAgentID, job.ID, localA2AMaxClaimLeaseSeconds)
		if err != nil {
			continue
		}

		method := strings.TrimSpace(req.Method)
		methodDef, _ := h.loadA2AMethodDefinition(ctx, method)
		message := buildClaimedCompanyMethodHeartbeat("", claimedJob, companyMethodToolSpec{Method: method}, methodDef)
		if err := h.sendAgentHeartbeat(targetAgentID, message); err != nil {
			log.Printf("Company method: failed to deliver pool job %s to %s: %v", job.ID, targetAgentID, err)
			_ = queue.RequeueClaimedJob(ctx, targetAgentID, job.ID)
			continue
		}
		return 1, nil
	}
	return 0, nil
}

func buildClaimedCompanyMethodHeartbeat(callerAgentID string, claimedJob map[string]any, spec companyMethodToolSpec, methodDef *data.A2AMethod) string {
	jobID, _ := claimedJob["job_id"].(string)
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		jobID = "(unknown)"
	}

	method := strings.TrimSpace(spec.Method)
	if req, ok := claimedJob["request"].(map[string]any); ok {
		if requestMethod, _ := req["method"].(string); strings.TrimSpace(requestMethod) != "" {
			method = strings.TrimSpace(requestMethod)
		}
	}
	if method == "" && methodDef != nil {
		method = strings.TrimSpace(methodDef.Method)
	}
	if method == "" {
		method = "(unknown)"
	}

	var sb strings.Builder
	sb.WriteString("This is a heartbeat for a company method call.\n\n")
	sb.WriteString("Execute this method call now.\n\n")
	fmt.Fprintf(&sb, "Job ID: %s\n", jobID)
	fmt.Fprintf(&sb, "From Agent: %s\n", strings.TrimSpace(callerAgentID))
	fmt.Fprintf(&sb, "Method: %s\n", method)

	description := strings.TrimSpace(spec.Description)
	if methodDef != nil && strings.TrimSpace(methodDef.Description) != "" {
		description = strings.TrimSpace(methodDef.Description)
	}
	if description != "" {
		fmt.Fprintf(&sb, "Description: %s\n", description)
	}
	if methodDef != nil && methodDef.FreshContext {
		sb.WriteString("Fresh Context: true\n")
	}
	if methodDef != nil && methodDef.DisableMarketNotes {
		sb.WriteString("Market Notes Access: disabled\n")
	}
	if methodDef != nil && methodDef.PolymarketNoteAugmentationDisabled() {
		sb.WriteString("Polymarket Note Augmentation: disabled\n")
	}

	instructions := ""
	if methodDef != nil {
		instructions = strings.TrimSpace(methodDef.Instructions)
	}
	if instructions != "" {
		sb.WriteString("\nMethod Instructions:\n")
		sb.WriteString(instructions)
		sb.WriteString("\n")
	}

	params := map[string]any{}
	if req, ok := claimedJob["request"].(map[string]any); ok {
		if parsed, ok := req["params"].(map[string]any); ok && parsed != nil {
			params = parsed
		}
	}
	sb.WriteString("\nInput Parameters (JSON):\n")
	sb.WriteString(formatA2AHeartbeatJSON(params))
	sb.WriteString("\n")

	inputSchema := spec.InputSchema
	if inputSchema == nil && methodDef != nil {
		inputSchema = decodeMethodSchemaJSON(methodDef.InputSchemaJSON)
	}
	if inputSchema != nil {
		sb.WriteString("\nInput Schema (JSON):\n")
		sb.WriteString(formatA2AHeartbeatJSON(inputSchema))
		sb.WriteString("\n")
	}

	outputSchema := spec.OutputSchema
	if outputSchema == nil && methodDef != nil {
		outputSchema = decodeMethodSchemaJSON(methodDef.OutputSchemaJSON)
	}
	if outputSchema != nil {
		sb.WriteString("\nOutput Schema (JSON):\n")
		sb.WriteString(formatA2AHeartbeatJSON(outputSchema))
		sb.WriteString("\n")
	}

	sb.WriteString("\nCompletion Rules:\n")
	sb.WriteString("- Return exactly one JSON object as your final response.\n")
	sb.WriteString("- The JSON object should match the output schema when one is provided.\n")
	sb.WriteString("- To fail this branch intentionally, return {\"status\":\"FAILED\",\"reason\":\"<why>\"} in that final JSON object.\n")
	sb.WriteString("- Do not include tool-lifecycle or queue-management text in your response.\n")
	return sb.String()
}

func decodeMethodSchemaJSON(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func formatA2AHeartbeatJSON(value any) string {
	if value == nil {
		return "  {}"
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "  {}"
	}
	lines := strings.Split(string(data), "\n")
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n")
}

func waitForCompanyMethodResult(ctx context.Context, queue *localA2AQueue, jobID, method string, timeout time.Duration) (any, bool, error) {
	if timeout <= 0 {
		timeout = companyMethodToolWaitTimeout
	}
	deadline := time.Now().Add(timeout)
	lastStatus := localA2AStatusQueued

	for {
		job, err := queue.GetJob(ctx, jobID)
		if err != nil {
			return nil, false, err
		}
		status, _ := job["status"].(string)
		status = strings.TrimSpace(status)
		if status != "" {
			lastStatus = status
		}

		switch status {
		case localA2AStatusSucceeded:
			if result, ok := job["result"]; ok {
				return result, false, nil
			}
			return map[string]any{}, false, nil
		case localA2AStatusFailed:
			errMsg := "unknown error"
			if payload, ok := job["error"].(map[string]any); ok {
				if msg, _ := payload["message"].(string); strings.TrimSpace(msg) != "" {
					errMsg = strings.TrimSpace(msg)
				}
			}
			return nil, false, fmt.Errorf("company method %q failed: %s", method, errMsg)
		}

		if time.Now().After(deadline) {
			return map[string]any{
				"status":  lastStatus,
				"pending": true,
				"job_id":  jobID,
				"method":  method,
			}, true, nil
		}

		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-time.After(companyMethodToolPollEvery):
		}
	}
}
