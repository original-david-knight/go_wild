package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/agent_net"
)

// HandleA2ACallback receives terminal A2A completion callbacks from agent_net.
// Route: POST /pipeline/callbacks/a2a
func (pe *PipelineEngine) HandleA2ACallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read callback body")
		return
	}
	if err := pe.verifyA2ACallbackRequest(r, body); err != nil {
		log.Printf("Pipeline callback rejected: %v", err)
		writeError(w, http.StatusUnauthorized, "invalid callback signature")
		return
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid callback payload")
		return
	}

	jobID := pe.extractJobID(payload)
	if jobID == "" {
		writeError(w, http.StatusBadRequest, "missing job_id")
		return
	}
	if headerJobID := strings.TrimSpace(r.Header.Get("X-A2A-Job-ID")); headerJobID != "" && headerJobID != jobID {
		writeError(w, http.StatusBadRequest, "job_id does not match X-A2A-Job-ID header")
		return
	}

	// Callback payloads from agent_net intentionally omit request details; fetch the
	// canonical job document so pipeline matching can inspect request.method.
	if _, ok := payload["request"]; !ok {
		client, err := pe.callbackClientForPayload(r.Context(), payload)
		if err != nil {
			log.Printf("Pipeline callback: unable to load A2A client for job %s: %v", jobID, err)
		} else {
			jobDoc, err := client.doJSON(r.Context(), http.MethodGet, "/api/v1/a2a/jobs/"+url.PathEscape(jobID), nil)
			if err != nil {
				log.Printf("Pipeline callback: failed to hydrate job %s: %v", jobID, err)
			} else {
				for k, v := range jobDoc {
					if _, exists := payload[k]; !exists {
						payload[k] = v
					}
				}
			}
		}
	}

	pe.RecordCompletion(payload)
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

func (pe *PipelineEngine) normalizeCompletionJob(job map[string]any) map[string]any {
	if job == nil {
		return nil
	}

	normalized := make(map[string]any, len(job)+3)
	for k, v := range job {
		normalized[k] = v
	}

	if _, ok := normalized["id"]; !ok {
		if jobID := pe.extractJobID(job); jobID != "" {
			normalized["id"] = jobID
		}
	}

	if _, ok := normalized["request"]; !ok {
		if requestJSON, _ := normalized["request_json"].(string); requestJSON != "" {
			var request map[string]any
			if err := json.Unmarshal([]byte(requestJSON), &request); err == nil {
				normalized["request"] = request
			}
		}
	}
	if _, ok := normalized["request_json"]; !ok {
		if request, ok := normalized["request"].(map[string]any); ok && request != nil {
			if b, err := json.Marshal(request); err == nil {
				normalized["request_json"] = string(b)
			}
		}
	}

	if _, ok := normalized["result"]; !ok {
		if resultJSON, _ := normalized["result_json"].(string); resultJSON != "" {
			var result map[string]any
			if err := json.Unmarshal([]byte(resultJSON), &result); err == nil {
				normalized["result"] = result
			}
		}
	}
	if _, ok := normalized["result_json"]; !ok {
		if result, ok := normalized["result"].(map[string]any); ok && result != nil {
			if b, err := json.Marshal(result); err == nil {
				normalized["result_json"] = string(b)
			}
		}
	}

	return normalized
}

func (pe *PipelineEngine) extractJobID(job map[string]any) string {
	if job == nil {
		return ""
	}
	if id, _ := job["id"].(string); strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id)
	}
	if id, _ := job["job_id"].(string); strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id)
	}
	return ""
}

func (pe *PipelineEngine) extractJobRequestMethod(job map[string]any) string {
	if job == nil {
		return ""
	}
	normalized := pe.normalizeCompletionJob(job)
	if request, ok := normalized["request"].(map[string]any); ok {
		if method, _ := request["method"].(string); strings.TrimSpace(method) != "" {
			return strings.TrimSpace(method)
		}
	}
	if requestJSON, _ := normalized["request_json"].(string); requestJSON != "" {
		var req struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal([]byte(requestJSON), &req); err == nil && strings.TrimSpace(req.Method) != "" {
			return strings.TrimSpace(req.Method)
		}
	}
	return ""
}

func (pe *PipelineEngine) extractJobResult(job map[string]any) map[string]any {
	if job == nil {
		return nil
	}
	normalized := pe.normalizeCompletionJob(job)
	if result, ok := normalized["result"].(map[string]any); ok {
		return result
	}
	if resultJSON, _ := normalized["result_json"].(string); resultJSON != "" {
		var result map[string]any
		if err := json.Unmarshal([]byte(resultJSON), &result); err == nil {
			return result
		}
	}
	return nil
}

func (pe *PipelineEngine) resolveSourceRole(ctx context.Context, job map[string]any, method string) string {
	if role, _ := job["from_role"].(string); strings.TrimSpace(role) != "" {
		return strings.TrimSpace(role)
	}
	fromPubKey, _ := job["from_public_key"].(string)
	if strings.TrimSpace(fromPubKey) == "" || strings.TrimSpace(method) == "" {
		return ""
	}
	agentID, err := pe.agentIDForPublicKey(ctx, strings.TrimSpace(fromPubKey))
	if err != nil {
		return ""
	}

	svc := data.NewAgentService(pe.db, agentID)
	caps, err := svc.GetCapabilities(ctx)
	if err != nil {
		return ""
	}
	for _, cap := range caps {
		if cap.Method == method && strings.TrimSpace(cap.Role) != "" {
			return strings.TrimSpace(cap.Role)
		}
	}
	return ""
}

func (pe *PipelineEngine) callbackClientForPayload(ctx context.Context, payload map[string]any) (*a2aAgentNetClient, error) {
	if toPub, _ := payload["to_public_key"].(string); strings.TrimSpace(toPub) != "" {
		if client, err := pe.getA2AClientForPublicKey(ctx, strings.TrimSpace(toPub)); err == nil {
			return client, nil
		}
	}
	if fromPub, _ := payload["from_public_key"].(string); strings.TrimSpace(fromPub) != "" {
		if client, err := pe.getA2AClientForPublicKey(ctx, strings.TrimSpace(fromPub)); err == nil {
			return client, nil
		}
	}
	return pe.getA2AClient(ctx)
}

func (pe *PipelineEngine) getA2AClientForPublicKey(ctx context.Context, pubKey string) (*a2aAgentNetClient, error) {
	agentID, err := pe.agentIDForPublicKey(ctx, pubKey)
	if err != nil {
		return nil, err
	}
	return newA2AAgentNetClient(ctx, data.NewAgentService(pe.db, agentID))
}

func (pe *PipelineEngine) agentIDForPublicKey(ctx context.Context, pubKey string) (string, error) {
	agents, err := pe.service.ListAgents(ctx)
	if err != nil {
		return "", err
	}
	for _, agent := range agents {
		agentPubKey, err := pe.getAgentPublicKey(ctx, agent)
		if err != nil {
			continue
		}
		if agentPubKey == pubKey {
			return agent.ID, nil
		}
	}
	return "", fmt.Errorf("no local agent key for public key %s", pubKey)
}

type a2aCallbackVerifier struct {
	allowedKeys  map[string]ed25519.PublicKey
	maxClockSkew time.Duration
}

func (pe *PipelineEngine) verifyA2ACallbackRequest(r *http.Request, body []byte) error {
	if pe.callbackVerifierErr != nil {
		return pe.callbackVerifierErr
	}
	if pe.callbackVerifier == nil {
		return errors.New("callback verifier is not configured")
	}
	return pe.callbackVerifier.Verify(r, body)
}

func (v *a2aCallbackVerifier) Verify(r *http.Request, body []byte) error {
	if v == nil {
		return errors.New("callback verifier is nil")
	}
	keyID := strings.TrimSpace(r.Header.Get("X-A2A-Key-ID"))
	if keyID == "" {
		return errors.New("missing X-A2A-Key-ID")
	}
	pubKey, ok := v.allowedKeys[keyID]
	if !ok {
		return fmt.Errorf("untrusted X-A2A-Key-ID %q", keyID)
	}

	timestamp := strings.TrimSpace(r.Header.Get("X-A2A-Timestamp"))
	if timestamp == "" {
		return errors.New("missing X-A2A-Timestamp")
	}
	parsedTS, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return fmt.Errorf("invalid X-A2A-Timestamp: %w", err)
	}
	skew := time.Since(parsedTS)
	if skew < -v.maxClockSkew || skew > v.maxClockSkew {
		return fmt.Errorf("callback timestamp skew %s exceeds allowed %s", skew, v.maxClockSkew)
	}

	encodedSig := strings.TrimSpace(r.Header.Get("X-A2A-Sig"))
	if encodedSig == "" {
		return errors.New("missing X-A2A-Sig")
	}
	signature, err := gowild_agent_net.DecodeSignature(encodedSig)
	if err != nil {
		return fmt.Errorf("invalid X-A2A-Sig: %w", err)
	}

	path := callbackSignaturePath(r)
	if !gowild_agent_net.VerifySignature(pubKey, r.Method, path, timestamp, body, signature) {
		return errors.New("signature verification failed")
	}
	return nil
}

func callbackSignaturePath(r *http.Request) string {
	path := r.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	return path
}

func loadA2ACallbackVerifierFromEnv() (*a2aCallbackVerifier, error) {
	allowedKeys, err := loadA2ACallbackAllowedKeysFromEnv()
	if err != nil {
		return nil, err
	}
	if len(allowedKeys) == 0 {
		return nil, nil
	}
	maxClockSkew, err := parseA2ACallbackMaxClockSkew()
	if err != nil {
		return nil, err
	}
	return &a2aCallbackVerifier{
		allowedKeys:  allowedKeys,
		maxClockSkew: maxClockSkew,
	}, nil
}

func loadA2ACallbackAllowedKeysFromEnv() (map[string]ed25519.PublicKey, error) {
	raw := strings.TrimSpace(os.Getenv("A2A_CALLBACK_ALLOWED_KEY_IDS"))
	if raw == "" {
		return map[string]ed25519.PublicKey{}, nil
	}

	parts := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', ';', ' ', '\n', '\r', '\t':
			return true
		default:
			return false
		}
	})

	allowedKeys := make(map[string]ed25519.PublicKey, len(parts))
	for _, part := range parts {
		keyID := strings.TrimSpace(part)
		if keyID == "" {
			continue
		}
		pubKey, err := gowild_agent_net.DecodePublicKey(keyID)
		if err != nil {
			return nil, fmt.Errorf("invalid key ID in A2A_CALLBACK_ALLOWED_KEY_IDS: %q: %w", keyID, err)
		}
		allowedKeys[keyID] = pubKey
	}
	return allowedKeys, nil
}

func parseA2ACallbackMaxClockSkew() (time.Duration, error) {
	const defaultSkew = 5 * time.Minute
	raw := strings.TrimSpace(os.Getenv("A2A_CALLBACK_MAX_SKEW_SECONDS"))
	if raw == "" {
		return defaultSkew, nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("A2A_CALLBACK_MAX_SKEW_SECONDS must be an integer: %w", err)
	}
	if seconds <= 0 {
		return 0, fmt.Errorf("A2A_CALLBACK_MAX_SKEW_SECONDS must be > 0")
	}
	return time.Duration(seconds) * time.Second, nil
}

func validatePipelineCallbackConfiguration() (string, int, error) {
	callbackURL, err := pipelineA2ACallbackURLWithError()
	if err != nil {
		return "", 0, err
	}

	allowedKeys, err := loadA2ACallbackAllowedKeysFromEnv()
	if err != nil {
		return "", 0, err
	}
	if callbackURL != "" && len(allowedKeys) == 0 {
		return "", 0, fmt.Errorf("callback URL is configured, but A2A_CALLBACK_ALLOWED_KEY_IDS is empty")
	}
	return callbackURL, len(allowedKeys), nil
}

func pipelineA2ACallbackURL() string {
	callbackURL, err := pipelineA2ACallbackURLWithError()
	if err != nil {
		log.Printf("Pipeline engine: %v", err)
		return ""
	}
	return callbackURL
}

func pipelineA2ACallbackURLWithError() (string, error) {
	// Explicit full callback URL takes precedence.
	if raw := strings.TrimSpace(os.Getenv("PIPELINE_CALLBACK_URL")); raw != "" {
		if _, err := parseHTTPSURL(raw); err != nil {
			return "", fmt.Errorf("invalid PIPELINE_CALLBACK_URL: %w", err)
		}
		return raw, nil
	}

	// Fallback: derive from ingress public base URL.
	if base, err := ingressPublicBaseURLWithError(); err != nil {
		return "", err
	} else if base != "" {
		return base + "/ingress/callbacks/a2a", nil
	}
	return "", nil
}

func parseHTTPSURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return nil, fmt.Errorf("URL scheme must be https")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return nil, fmt.Errorf("URL host is required")
	}
	return parsed, nil
}
