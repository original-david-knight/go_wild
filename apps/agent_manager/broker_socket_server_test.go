package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	gowild_crypto "github.com/original-david-knight/go_wild/crypto"
	brokerclient "github.com/original-david-knight/go_wild/tools/broker"
)

func TestBrokerSocketServerHandlesToolCalls(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()

	agentID := "socket-agent"
	agentSvc := data.NewAgentService(db, agentID)
	agent, err := agentSvc.EnsureAgent(ctx)
	if err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}
	derived, err := gowild_crypto.DeriveKeysFromMnemonic(agent.WalletSeedPhrase, agentAuthDerivationIndex)
	if err != nil {
		t.Fatalf("DeriveKeysFromMnemonic failed: %v", err)
	}

	socketPath := shortTestUnixSocketPath(t)
	secret := []byte("01234567890123456789012345678901")
	broker := &BrokerHandlers{
		auth:   NewBrokerAuthHandler(NewAgentService(db), secret),
		tools:  NewBrokerToolsHandler(db),
		secret: secret,
	}
	socketServer := NewBrokerSocketServer(socketPath, broker)
	defer socketServer.Close()

	done := make(chan error, 1)
	go func() {
		done <- socketServer.ListenAndServe()
	}()

	if err := waitForBrokerSocket(socketPath, 2*time.Second); err != nil {
		t.Fatalf("waitForBrokerSocket failed: %v", err)
	}

	t.Setenv("BROKER_SOCKET_PATH", socketPath)
	t.Setenv("GOWILD_AGENT_ID", agentID)
	t.Setenv("GOWILD_AGENT_ETH_PRIVATE_KEY", derived.EthPrivateKey)
	client := brokerclient.NewClient()

	result, err := client.CallTool(ctx, "get_agent_name", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool(get_agent_name) failed: %v", err)
	}
	if got, _ := result["name"].(string); got != agent.Name {
		t.Fatalf("expected name %q, got %q", agent.Name, got)
	}

	if err := socketServer.Close(); err != nil {
		t.Fatalf("socket server close failed: %v", err)
	}
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatalf("socket server did not stop")
	}
}

func TestBrokerSocketServerRejectsMissingTokenWhenSecretConfigured(t *testing.T) {
	db := setupManagerTestDB(t)
	ctx := context.Background()

	agentID := "socket-agent-missing-token"
	agentSvc := data.NewAgentService(db, agentID)
	if _, err := agentSvc.EnsureAgent(ctx); err != nil {
		t.Fatalf("EnsureAgent failed: %v", err)
	}

	socketPath := shortTestUnixSocketPath(t)
	secret := []byte("01234567890123456789012345678901")
	broker := &BrokerHandlers{
		auth:   NewBrokerAuthHandler(NewAgentService(db), secret),
		tools:  NewBrokerToolsHandler(db),
		secret: secret,
	}
	socketServer := NewBrokerSocketServer(socketPath, broker)
	defer socketServer.Close()

	go func() {
		_ = socketServer.ListenAndServe()
	}()
	if err := waitForBrokerSocket(socketPath, 2*time.Second); err != nil {
		t.Fatalf("waitForBrokerSocket failed: %v", err)
	}

	resp := callBrokerSocketRaw(t, socketPath, brokerSocketRequest{
		Method:  http.MethodPost,
		Path:    "/broker/v1/tools/get_agent_name",
		AgentID: agentID,
		Body:    json.RawMessage(`{}`),
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", resp.StatusCode, string(resp.Body))
	}
	if !strings.Contains(string(resp.Body), "missing agent session token") {
		t.Fatalf("expected missing session token error, got %s", string(resp.Body))
	}
}

func TestBrokerSocketServerRejectsTokenAgentMismatch(t *testing.T) {
	db := setupManagerTestDB(t)

	socketPath := shortTestUnixSocketPath(t)
	secret := []byte("01234567890123456789012345678901")
	broker := &BrokerHandlers{
		auth:   NewBrokerAuthHandler(NewAgentService(db), secret),
		tools:  NewBrokerToolsHandler(db),
		secret: secret,
	}
	socketServer := NewBrokerSocketServer(socketPath, broker)
	defer socketServer.Close()

	go func() {
		_ = socketServer.ListenAndServe()
	}()
	if err := waitForBrokerSocket(socketPath, 2*time.Second); err != nil {
		t.Fatalf("waitForBrokerSocket failed: %v", err)
	}

	tokenAgentID := "socket-agent-token"
	token := testSessionTokenForAgent(t, db, secret, tokenAgentID)
	resp := callBrokerSocketRaw(t, socketPath, brokerSocketRequest{
		Method:  http.MethodPost,
		Path:    "/broker/v1/tools/get_agent_name",
		Token:   token,
		AgentID: "socket-agent-request",
		Body:    json.RawMessage(`{}`),
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", resp.StatusCode, string(resp.Body))
	}
	if !strings.Contains(string(resp.Body), "does not match session token identity") {
		t.Fatalf("expected session/agent mismatch error, got %s", string(resp.Body))
	}
}

func callBrokerSocketRaw(t *testing.T, socketPath string, req brokerSocketRequest) brokerSocketResponse {
	t.Helper()
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatalf("encode socket request: %v", err)
	}
	var resp brokerSocketResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode socket response: %v", err)
	}
	return resp
}
