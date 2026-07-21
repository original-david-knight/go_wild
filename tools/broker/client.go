// Package broker provides a Unix socket client for the broker API proxy.
// When an agent runs in a sandboxed container, it uses this client
// instead of direct API access to keep secrets on the host.
package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	gowild_crypto "github.com/original-david-knight/go_wild/crypto"
)

const (
	defaultBrokerSocketPath = "/tmp/gowild-broker/broker.sock"
	socketDialTimeout       = 5 * time.Second
	socketRetryAttempts     = 5
	socketRetryBaseDelay    = 500 * time.Millisecond
	socketRetryMaxDelay     = 5 * time.Second
)

// Client is a Unix socket client for the broker API.
type Client struct {
	socketPath    string
	sessionToken  string
	agentID       string
	ethPrivateKey string
	authMu        sync.Mutex
}

type contextKey string

const executionMethodContextKey contextKey = "broker_execution_method"

// NewClient creates a new broker API client using Unix socket transport.
func NewClient() *Client {
	return &Client{
		socketPath:    brokerSocketPath(),
		agentID:       brokerAgentID(),
		ethPrivateKey: strings.TrimSpace(os.Getenv(AgentEthPrivateKeyEnv)),
	}
}

// NewTestClient creates a broker client that connects to a Unix socket at the given path.
// Used only in tests where an httptest-style server listens on a temp socket.
func NewTestClient(socketPath, token string) *Client {
	agentID := brokerAgentID()
	if strings.TrimSpace(agentID) == "" {
		agentID = "test-agent"
	}
	return &Client{
		socketPath:   socketPath,
		sessionToken: token,
		agentID:      agentID,
	}
}

func brokerSocketPath() string {
	if raw := strings.TrimSpace(os.Getenv("BROKER_SOCKET_PATH")); raw != "" {
		return raw
	}
	return defaultBrokerSocketPath
}

func brokerAgentID() string {
	if raw := strings.TrimSpace(os.Getenv("GOWILD_AGENT_ID")); raw != "" {
		return raw
	}
	for i := 0; i+1 < len(os.Args); i++ {
		if strings.TrimSpace(os.Args[i]) == "-agent" {
			if v := strings.TrimSpace(os.Args[i+1]); v != "" {
				return v
			}
		}
	}
	return ""
}

// Endpoint returns a human-friendly broker endpoint description.
func (c *Client) Endpoint() string {
	if c == nil {
		return ""
	}
	return "unix://" + c.socketPath
}

// Post sends a JSON POST request and returns the decoded response.
// Responses that are JSON objects are returned as-is. Non-object responses
// (arrays, strings, etc.) are wrapped as {"result": value} so the return
// type stays map[string]any for all callers.
func (c *Client) Post(ctx context.Context, path string, body any) (map[string]any, error) {
	raw, statusCode, err := c.callSocket(ctx, "POST", path, body)
	if err != nil {
		return nil, err
	}

	// Try decoding as a JSON object first (most common case, also handles error responses).
	var mapResult map[string]any
	if err := json.Unmarshal(raw, &mapResult); err == nil {
		if statusCode >= 400 {
			if errMsg, ok := mapResult["error"].(string); ok {
				return nil, fmt.Errorf("broker error (%d): %s", statusCode, errMsg)
			}
			return nil, fmt.Errorf("broker error (%d)", statusCode)
		}
		return mapResult, nil
	}

	// Non-object response (e.g. array from kg_search, string from kg_delete).
	if statusCode >= 400 {
		return nil, fmt.Errorf("broker error (%d): %s", statusCode, string(raw))
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return map[string]any{"result": value}, nil
}

// Get sends a GET request and returns the decoded response.
func (c *Client) Get(ctx context.Context, path string) (map[string]any, error) {
	raw, statusCode, err := c.callSocket(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	if statusCode >= 400 {
		if errMsg, ok := result["error"].(string); ok {
			return nil, fmt.Errorf("broker error (%d): %s", statusCode, errMsg)
		}
		return nil, fmt.Errorf("broker error (%d)", statusCode)
	}
	return result, nil
}

// CallTool calls a tool through the broker's generic tool proxy.
// Returns the tool result exactly as the manager-side tool produced it.
func (c *Client) CallTool(ctx context.Context, toolName string, input any) (map[string]any, error) {
	return c.Post(ctx, "/broker/v1/tools/"+toolName, input)
}

// PostRaw sends a JSON POST request and returns the raw response body.
// Used for the LLM proxy where we need to deserialize into specific types.
func (c *Client) PostRaw(ctx context.Context, path string, body any) ([]byte, error) {
	raw, statusCode, err := c.callSocket(ctx, "POST", path, body)
	if err != nil {
		return nil, err
	}
	if statusCode >= 400 {
		var errResp map[string]any
		if json.Unmarshal(raw, &errResp) == nil {
			if errMsg, ok := errResp["error"].(string); ok {
				return nil, fmt.Errorf("broker error (%d): %s", statusCode, errMsg)
			}
		}
		return nil, fmt.Errorf("broker error (%d): %s", statusCode, string(raw))
	}
	return raw, nil
}

type socketRequest struct {
	Method          string          `json:"method"`
	Path            string          `json:"path"`
	Token           string          `json:"token,omitempty"`
	AgentID         string          `json:"agent_id,omitempty"`
	ExecutionMethod string          `json:"execution_method,omitempty"`
	Body            json.RawMessage `json:"body,omitempty"`
}

type socketResponse struct {
	StatusCode int             `json:"status_code"`
	Body       json.RawMessage `json:"body,omitempty"`
	Error      string          `json:"error,omitempty"`
}

func (c *Client) callSocket(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	if !isAuthPath(path) {
		if err := c.ensureAuthenticated(ctx); err != nil {
			return nil, 0, err
		}
	}

	respBody, statusCode, err := c.rawSocketCall(ctx, method, path, body, c.currentSessionToken())
	if err != nil || statusCode != httpStatusUnauthorized || isAuthPath(path) || !c.canAuthenticate() {
		return respBody, statusCode, err
	}

	c.clearSessionToken()
	if authErr := c.ensureAuthenticated(ctx); authErr != nil {
		return respBody, statusCode, authErr
	}
	return c.rawSocketCall(ctx, method, path, body, c.currentSessionToken())
}

func (c *Client) rawSocketCall(ctx context.Context, method, path string, body any, token string) ([]byte, int, error) {
	if c == nil {
		return nil, 0, fmt.Errorf("broker client is nil")
	}
	socketPath := strings.TrimSpace(c.socketPath)
	if socketPath == "" {
		return nil, 0, fmt.Errorf("broker socket path is empty")
	}

	var payload json.RawMessage
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to marshal request: %w", err)
		}
		payload = encoded
	}

	req := socketRequest{
		Method:          method,
		Path:            path,
		Token:           strings.TrimSpace(token),
		AgentID:         strings.TrimSpace(c.agentID),
		ExecutionMethod: ExecutionMethod(ctx),
		Body:            payload,
	}

	var lastErr error
	delay := socketRetryBaseDelay
	for attempt := 0; attempt < socketRetryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, 0, fmt.Errorf("broker socket request canceled: %w", ctx.Err())
			case <-time.After(delay):
			}
			delay *= 2
			if delay > socketRetryMaxDelay {
				delay = socketRetryMaxDelay
			}
		}

		respBody, statusCode, err := c.doSocketCall(ctx, socketPath, req)
		if err == nil {
			return respBody, statusCode, nil
		}
		if !isTransientSocketError(err) {
			return nil, 0, err
		}
		lastErr = err
	}
	return nil, 0, lastErr
}

const httpStatusUnauthorized = 401

func isAuthPath(path string) bool {
	path = strings.TrimSpace(path)
	return path == AuthChallengePath || path == AuthVerifyPath || strings.HasPrefix(path, "/broker/v1/auth/")
}

func (c *Client) canAuthenticate() bool {
	return c != nil &&
		strings.TrimSpace(c.agentID) != "" &&
		strings.TrimSpace(c.ethPrivateKey) != ""
}

func (c *Client) currentSessionToken() string {
	if c == nil {
		return ""
	}
	c.authMu.Lock()
	defer c.authMu.Unlock()
	return strings.TrimSpace(c.sessionToken)
}

func (c *Client) clearSessionToken() {
	if c == nil {
		return
	}
	c.authMu.Lock()
	c.sessionToken = ""
	c.authMu.Unlock()
}

func (c *Client) ensureAuthenticated(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("broker client is nil")
	}

	c.authMu.Lock()
	if strings.TrimSpace(c.sessionToken) != "" {
		c.authMu.Unlock()
		return nil
	}
	c.authMu.Unlock()

	if !c.canAuthenticate() {
		if strings.TrimSpace(c.agentID) == "" {
			return fmt.Errorf("missing agent id for broker authentication")
		}
		return fmt.Errorf("missing %s for broker authentication", AgentEthPrivateKeyEnv)
	}

	wallet, err := gowild_crypto.NewWallet(gowild_crypto.WalletConfig{EthPrivateKey: strings.TrimSpace(c.ethPrivateKey)})
	if err != nil {
		return fmt.Errorf("failed to load broker authentication key: %w", err)
	}
	addressInfo, err := wallet.GetAddress(gowild_crypto.ChainEthereum)
	if err != nil {
		return fmt.Errorf("failed to derive broker authentication address: %w", err)
	}
	address := strings.TrimSpace(addressInfo.Address)

	challengeReq := AuthChallengeRequest{
		AgentID: strings.TrimSpace(c.agentID),
		Address: address,
	}
	rawChallenge, statusCode, err := c.rawSocketCall(ctx, "POST", AuthChallengePath, challengeReq, "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return brokerStatusError(statusCode, rawChallenge)
	}

	var challenge AuthChallengeResponse
	if err := json.Unmarshal(rawChallenge, &challenge); err != nil {
		return fmt.Errorf("failed to decode broker authentication challenge: %w", err)
	}
	if strings.TrimSpace(challenge.AgentID) != strings.TrimSpace(c.agentID) {
		return fmt.Errorf("broker authentication challenge agent mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(challenge.Address), address) {
		return fmt.Errorf("broker authentication challenge address mismatch")
	}
	message, err := BuildSignInMessage(challenge)
	if err != nil {
		return fmt.Errorf("invalid broker authentication challenge: %w", err)
	}
	if strings.TrimSpace(challenge.Message) != "" && challenge.Message != message {
		return fmt.Errorf("broker authentication challenge message mismatch")
	}

	signed, err := wallet.SignMessage(gowild_crypto.ChainEthereum, message)
	if err != nil {
		return fmt.Errorf("failed to sign broker authentication challenge: %w", err)
	}

	verifyReq := AuthVerifyRequest{
		AgentID:   strings.TrimSpace(c.agentID),
		Address:   address,
		Nonce:     strings.TrimSpace(challenge.Nonce),
		Message:   message,
		Signature: signed.Signature,
	}
	rawVerify, statusCode, err := c.rawSocketCall(ctx, "POST", AuthVerifyPath, verifyReq, "")
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return brokerStatusError(statusCode, rawVerify)
	}

	var verify AuthVerifyResponse
	if err := json.Unmarshal(rawVerify, &verify); err != nil {
		return fmt.Errorf("failed to decode broker authentication session: %w", err)
	}
	sessionToken := strings.TrimSpace(verify.SessionToken)
	if sessionToken == "" {
		return fmt.Errorf("broker authentication did not return a session token")
	}

	c.authMu.Lock()
	c.sessionToken = sessionToken
	c.authMu.Unlock()
	return nil
}

func brokerStatusError(statusCode int, raw []byte) error {
	var errResp map[string]any
	if json.Unmarshal(raw, &errResp) == nil {
		if errMsg, ok := errResp["error"].(string); ok && strings.TrimSpace(errMsg) != "" {
			return fmt.Errorf("broker error (%d): %s", statusCode, strings.TrimSpace(errMsg))
		}
	}
	return fmt.Errorf("broker error (%d): %s", statusCode, string(raw))
}

func WithExecutionMethod(ctx context.Context, method string) context.Context {
	method = strings.TrimSpace(method)
	if method == "" {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, executionMethodContextKey, method)
}

func ExecutionMethod(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if method, ok := ctx.Value(executionMethodContextKey).(string); ok {
		return strings.TrimSpace(method)
	}
	return ""
}

// isTransientSocketError returns true for connection errors that indicate the
// broker socket is temporarily unavailable (e.g. during manager restart).
// NOTE: EOF errors are NOT retried — they typically indicate the broker handler
// crashed or timed out mid-request, and retrying expensive calls (e.g. deep
// research) would waste time and tokens.
func isTransientSocketError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "connection reset")
}

func (c *Client) doSocketCall(ctx context.Context, socketPath string, req socketRequest) ([]byte, int, error) {
	dialer := &net.Dialer{Timeout: socketDialTimeout}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, 0, fmt.Errorf("broker socket request failed: %w", err)
	}
	defer conn.Close()

	// Deadline must exceed the longest handler timeout (deep research = 5 min)
	// plus overhead for Gemini API calls, planning, synthesis, etc.
	if err := conn.SetDeadline(time.Now().Add(10 * time.Minute)); err != nil {
		return nil, 0, fmt.Errorf("failed to set socket deadline: %w", err)
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, 0, fmt.Errorf("failed to send socket request: %w", err)
	}

	var resp socketResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, 0, fmt.Errorf("failed to decode socket response: %w", err)
	}
	if strings.TrimSpace(resp.Error) != "" {
		return nil, 0, fmt.Errorf("broker socket error: %s", strings.TrimSpace(resp.Error))
	}

	if len(resp.Body) == 0 {
		return []byte(`{}`), resp.StatusCode, nil
	}
	return resp.Body, resp.StatusCode, nil
}
