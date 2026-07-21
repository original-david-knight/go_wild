package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mr-tron/base58"
	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/agent_net"
	"github.com/original-david-knight/go_wild/crypto"
)

type a2aAgentNetClient struct {
	baseURL    string
	agentID    string
	privateKey ed25519.PrivateKey
	httpClient *http.Client
}

type a2aToolHandlerFunc func(h *BrokerToolsHandler, ctx context.Context, agentID string, svc *data.AgentService, toolName string, inputJSON []byte) (any, error)

var a2aToolHandlers = map[string]a2aToolHandlerFunc{
	"a2a_submit_request": removedA2AToolHandler,
	"a2a_get_job":        removedA2AToolHandler,
	"a2a_claim_jobs":     removedA2AToolHandler,
	"a2a_complete_job":   removedA2AToolHandler,
	"a2a_get_public_key": removedA2AToolHandler,
	"a2a_extend_lease":   removedA2AToolHandler,
	"job_result": func(h *BrokerToolsHandler, ctx context.Context, agentID string, _ *data.AgentService, _ string, inputJSON []byte) (any, error) {
		queue := newLocalA2AQueue(h.db)
		var input struct {
			JobID  string                             `json:"job_id"`
			Status string                             `json:"status"`
			Result map[string]any                     `json:"result,omitempty"`
			Error  *gowild_agent_net.A2AErrorEnvelope `json:"error,omitempty"`
		}
		if len(inputJSON) > 0 {
			if err := json.Unmarshal(inputJSON, &input); err != nil {
				return nil, fmt.Errorf("failed to unmarshal input: %w", err)
			}
		}
		if strings.TrimSpace(input.JobID) == "" {
			return nil, fmt.Errorf("job_id is required")
		}
		status := strings.ToLower(strings.TrimSpace(input.Status))
		if status == "" {
			return nil, fmt.Errorf("status is required")
		}
		if status != "succeeded" && status != "failed" {
			return nil, fmt.Errorf("status must be succeeded or failed")
		}
		if status == "failed" {
			if input.Error == nil || strings.TrimSpace(input.Error.Message) == "" {
				return nil, fmt.Errorf("error.message is required when status=failed")
			}
		}

		if status == "succeeded" {
			if err := h.validateA2ACompletionResultAgainstCapability(ctx, strings.TrimSpace(input.JobID), input.Result); err != nil {
				return nil, err
			}
		}

		result, err := queue.CompleteJob(ctx, strings.TrimSpace(agentID), strings.TrimSpace(input.JobID), status, input.Result, input.Error)
		if err == nil {
			h.maybeAddAutomaticMethodMarketNote(ctx, strings.TrimSpace(agentID), result)
			h.maybeSetCompletionMarketProperties(ctx, strings.TrimSpace(agentID), result)
		}
		if err == nil && h.pipelineEngine != nil {
			h.pipelineEngine.RecordCompletion(result)
		}
		if err == nil {
			h.sendA2ACompletionHeartbeat(ctx, result, strings.TrimSpace(agentID))
		}
		// Agent is now free — dispatch next queued pool job to it.
		if err == nil {
			go h.deliverQueuedCompanyMethodJobs(context.Background(), strings.TrimSpace(agentID), 1)
		}
		return result, err
	},
}

func removedA2AToolHandler(_ *BrokerToolsHandler, _ context.Context, _ string, _ *data.AgentService, toolName string, _ []byte) (any, error) {
	return nil, fmt.Errorf("tool %q has been removed; use company method tools", strings.TrimSpace(toolName))
}

func isA2ATool(toolName string) bool {
	_, ok := a2aToolHandlers[toolName]
	return ok
}

func newA2AAgentNetClient(ctx context.Context, svc *data.AgentService) (*a2aAgentNetClient, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("AGENT_NET_URL")), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("AGENT_NET_URL is not configured on the manager")
	}

	seedPhrase, err := resolveAgentNetSeedPhrase(ctx, svc)
	if err != nil {
		return nil, err
	}

	derived, err := gowild_crypto.DeriveKeysFromMnemonic(seedPhrase, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to derive agent keys: %w", err)
	}

	keypair, err := base58.Decode(derived.SolPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode solana keypair: %w", err)
	}
	if len(keypair) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid derived solana keypair length: got %d", len(keypair))
	}

	privateKey := ed25519.PrivateKey(keypair)
	pubKey := privateKey.Public().(ed25519.PublicKey)
	agentID := gowild_agent_net.EncodePublicKey(pubKey)

	return &a2aAgentNetClient{
		baseURL:    baseURL,
		agentID:    agentID,
		privateKey: privateKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// resolveAgentNetSeedPhrase returns the seed phrase to use for agent_net identity.
// If the agent belongs to a company, the company's seed phrase is used so that
// the agent_net identity matches what the agent sees via wallet tools.
// If the agent is in a company but the company seed phrase is missing, this is
// an error — we never silently fall back to the agent's own key.
func resolveAgentNetSeedPhrase(ctx context.Context, svc *data.AgentService) (string, error) {
	companyPhrase, err := svc.GetCompanyWalletSeedPhrase(ctx)
	if err == nil && strings.TrimSpace(companyPhrase) != "" {
		return companyPhrase, nil
	}
	// If the agent is in a company but we got an error (e.g. missing seed phrase),
	// surface it instead of silently falling back to the agent's own key.
	if err != nil && !strings.Contains(err.Error(), "not in a company") {
		return "", fmt.Errorf("agent is in a company but company seed phrase is unavailable: %w", err)
	}

	// Agent is not in a company — use their own seed phrase.
	seedPhrase, err := svc.GetWalletSeedPhrase(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to load wallet seed phrase: %w", err)
	}
	if strings.TrimSpace(seedPhrase) == "" {
		return "", fmt.Errorf("agent wallet seed phrase is empty")
	}
	return seedPhrase, nil
}

// sign returns a timestamp and encoded Ed25519 signature for an agent_net request.
func (c *a2aAgentNetClient) sign(method, path string, body []byte) (timestamp, signature string) {
	timestamp = time.Now().UTC().Format(time.RFC3339)
	sig := gowild_agent_net.SignRequest(c.privateKey, method, path, timestamp, body)
	signature = gowild_agent_net.EncodeSignature(sig)
	return
}

func (c *a2aAgentNetClient) doJSON(ctx context.Context, method, path string, body any) (map[string]any, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}

	var reqBody []byte
	var bodyReader io.Reader
	if method != http.MethodGet {
		if body == nil {
			reqBody = []byte("{}")
		} else {
			var err error
			reqBody, err = json.Marshal(body)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal request body: %w", err)
			}
		}
		bodyReader = bytes.NewReader(reqBody)
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	signature := gowild_agent_net.SignRequest(c.privateKey, method, path, timestamp, reqBody)

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Agent-ID", c.agentID)
	req.Header.Set("X-Agent-Timestamp", timestamp)
	req.Header.Set("X-Agent-Sig", gowild_agent_net.EncodeSignature(signature))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agent_net request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent_net response: %w", err)
	}

	var mapResult map[string]any
	if err := json.Unmarshal(data, &mapResult); err == nil {
		if resp.StatusCode >= 400 {
			msg, _ := mapResult["message"].(string)
			if msg == "" {
				msg, _ = mapResult["error"].(string)
			}
			if msg == "" {
				msg = strings.TrimSpace(string(data))
			}
			return nil, fmt.Errorf("agent_net error (%d): %s", resp.StatusCode, msg)
		}
		return mapResult, nil
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("agent_net error (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil, fmt.Errorf("unexpected non-object agent_net response")
}

func (h *BrokerToolsHandler) callA2ATools(ctx context.Context, agentID string, svc *data.AgentService, toolName string, inputJSON []byte) (bool, any, error) {
	if !isA2ATool(toolName) {
		return false, nil, nil
	}

	handler, ok := a2aToolHandlers[toolName]
	if !ok {
		return false, nil, nil
	}
	result, err := handler(h, ctx, agentID, svc, toolName, inputJSON)
	return true, result, err
}

func (h *BrokerToolsHandler) validateA2ACompletionResultAgainstCapability(ctx context.Context, jobID string, result map[string]any) error {
	job, err := newLocalA2AQueue(h.db).GetJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("failed to load job for schema validation: %w", err)
	}

	method, err := a2aRequestMethod(job)
	if err != nil {
		return fmt.Errorf("failed to resolve method for output validation: %w", err)
	}

	payload := any(result)
	if payload == nil {
		payload = map[string]any{}
	}
	if err := validatePayloadForMethod(ctx, h.db, method, capabilitySchemaOutput, payload); err != nil {
		return err
	}
	return nil
}

func (h *BrokerToolsHandler) resolveLocalA2ATargetAgentID(ctx context.Context, toPublicKey string) (string, error) {
	toPublicKey = strings.TrimSpace(toPublicKey)
	if toPublicKey == "" {
		return "", fmt.Errorf("to_public_key is required")
	}

	// Manager-local A2A uses agent IDs as addresses.
	var agent data.Agent
	if err := h.db.Table(data.Agent{}).Get(ctx, toPublicKey, &agent); err == nil {
		return toPublicKey, nil
	}
	return "", fmt.Errorf("unknown recipient %q: manager-local A2A expects target agent_id", toPublicKey)
}

func a2aRequestMethod(job map[string]any) (string, error) {
	if job == nil {
		return "", fmt.Errorf("job payload is empty")
	}
	if request, ok := job["request"].(map[string]any); ok {
		if method, _ := request["method"].(string); strings.TrimSpace(method) != "" {
			return strings.TrimSpace(method), nil
		}
	}
	if requestJSON, _ := job["request_json"].(string); strings.TrimSpace(requestJSON) != "" {
		var request struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal([]byte(requestJSON), &request); err == nil && strings.TrimSpace(request.Method) != "" {
			return strings.TrimSpace(request.Method), nil
		}
	}
	return "", fmt.Errorf("job is missing request.method")
}

func (h *BrokerToolsHandler) sendA2ACompletionHeartbeat(ctx context.Context, completedJob map[string]any, completedByAgentID string) {
	fromAgentID, _ := completedJob["from_public_key"].(string)
	fromAgentID = strings.TrimSpace(fromAgentID)
	completedByAgentID = strings.TrimSpace(completedByAgentID)
	if fromAgentID == "" {
		return
	}
	// Only inject into real agent sessions, not pipeline/webhook pseudo-senders.
	var caller data.Agent
	if err := h.db.Table(data.Agent{}).Get(ctx, fromAgentID, &caller); err != nil {
		return
	}
	msg := buildA2ACompletionHeartbeat(completedJob, completedByAgentID)
	if err := h.sendAgentHeartbeat(fromAgentID, msg); err != nil {
		log.Printf("A2A completion: failed to send heartbeat to caller %s: %v", fromAgentID, err)
	}
}

func buildA2ACompletionHeartbeat(completedJob map[string]any, completedByAgentID string) string {
	jobID, _ := completedJob["job_id"].(string)
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		jobID = "(unknown)"
	}
	status, _ := completedJob["status"].(string)
	status = strings.TrimSpace(status)
	if status == "" {
		status = "unknown"
	}
	method := "(unknown)"
	if m, err := a2aRequestMethod(completedJob); err == nil && strings.TrimSpace(m) != "" {
		method = strings.TrimSpace(m)
	}

	var sb strings.Builder
	sb.WriteString("This is a heartbeat for a completed method call.\n\n")
	fmt.Fprintf(&sb, "Job ID: %s\n", jobID)
	fmt.Fprintf(&sb, "Method: %s\n", method)
	if strings.TrimSpace(completedByAgentID) != "" {
		fmt.Fprintf(&sb, "Handled By: %s\n", strings.TrimSpace(completedByAgentID))
	}
	fmt.Fprintf(&sb, "Status: %s\n", status)

	if status == localA2AStatusSucceeded {
		sb.WriteString("\nResult (JSON):\n")
		sb.WriteString(formatA2AHeartbeatJSON(completedJob["result"]))
		sb.WriteString("\n")
	} else if status == localA2AStatusFailed {
		sb.WriteString("\nError (JSON):\n")
		sb.WriteString(formatA2AHeartbeatJSON(completedJob["error"]))
		sb.WriteString("\n")
	}

	sb.WriteString("\nUse this method-call outcome in your next step.\n")
	return sb.String()
}
