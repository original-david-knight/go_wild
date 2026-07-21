package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agent_net"
)

// makePremiumAgent creates a premium agent and returns its public key string.
func makePremiumAgent(t *testing.T, srv *Server) (string, func(method, path string, body []byte) *http.Request) {
	t.Helper()
	pubkey, privkey, err := gowild_agent_net.GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate keypair: %v", err)
	}
	agentID := gowild_agent_net.EncodePublicKey(pubkey)

	// Upgrade to premium
	if err := srv.service.UpgradeAccount(context.Background(), agentID, "tx_"+agentID[:8], gowild_agent_net.ChainSolana); err != nil {
		t.Fatalf("Failed to upgrade agent: %v", err)
	}

	// Return a helper that creates signed requests for this agent
	makeReq := func(method, path string, body []byte) *http.Request {
		timestamp := time.Now().UTC().Format(time.RFC3339)
		signature := gowild_agent_net.SignRequest(privkey, method, path, timestamp, body)

		var reqBody io.Reader
		if body != nil {
			reqBody = strings.NewReader(string(body))
		}

		req := httptest.NewRequest(method, path, reqBody)
		req.Header.Set(HeaderAgentID, agentID)
		req.Header.Set(HeaderAgentTimestamp, timestamp)
		req.Header.Set(HeaderAgentSig, gowild_agent_net.EncodeSignature(signature))
		return req
	}

	return agentID, makeReq
}

func TestHandleSendMessage(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()
	handler := srv.handler()

	senderID, makeSenderReq := makePremiumAgent(t, srv)
	recipientID, _ := makePremiumAgent(t, srv)
	_ = senderID

	body := []byte(`{"to_public_key":"` + recipientID + `","ciphertext":"encrypted_data","nonce":"test_nonce_24"}`)
	req := makeSenderReq("POST", "/api/v1/messages", body)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected 201, got %d: %s", w.Code, w.Body.String())
		return
	}

	var resp MessageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.ID == "" {
		t.Error("Message should have an ID")
	}
	if resp.Ciphertext != "encrypted_data" {
		t.Error("Ciphertext mismatch")
	}
}

func TestHandleSendMessage_SelfMessage(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()
	handler := srv.handler()

	agentID, makeReq := makePremiumAgent(t, srv)

	body := []byte(`{"to_public_key":"` + agentID + `","ciphertext":"data","nonce":"n"}`)
	req := makeReq("POST", "/api/v1/messages", body)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for self-message, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleSendMessage_NonPremiumSender(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()
	handler := srv.handler()

	_, makeReq := makePremiumAgent(t, srv)
	recipientID, _ := makePremiumAgent(t, srv)

	// Create a non-premium request (different agent, not upgraded)
	// The PremiumAuthChain middleware will reject this
	body := []byte(`{"to_public_key":"` + recipientID + `","ciphertext":"data","nonce":"n"}`)

	// Use makeSignedRequest which generates a NEW agent (not premium)
	req, _, _ := makeSignedRequest(t, "POST", "/api/v1/messages", body)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should get 402 from PremiumOnlyMiddleware
	if w.Code != http.StatusPaymentRequired {
		t.Errorf("Expected 402 for non-premium sender, got %d: %s", w.Code, w.Body.String())
	}

	_ = makeReq
}

func TestHandleListConversations(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()
	handler := srv.handler()

	aliceID, makeAliceReq := makePremiumAgent(t, srv)
	bobID, makeBobReq := makePremiumAgent(t, srv)
	_ = aliceID
	_ = bobID

	// Alice sends to Bob
	body := []byte(`{"to_public_key":"` + bobID + `","ciphertext":"hi_bob","nonce":"n1"}`)
	req := makeAliceReq("POST", "/api/v1/messages", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Send failed: %d %s", w.Code, w.Body.String())
	}

	// Bob sends to Alice
	body = []byte(`{"to_public_key":"` + aliceID + `","ciphertext":"hi_alice","nonce":"n2"}`)
	req = makeBobReq("POST", "/api/v1/messages", body)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Send failed: %d %s", w.Code, w.Body.String())
	}

	// Alice lists conversations
	req = makeAliceReq("GET", "/api/v1/messages", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
		return
	}

	var resp ConversationListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if len(resp.Conversations) != 1 {
		t.Errorf("Expected 1 conversation, got %d", len(resp.Conversations))
	}
}

func TestHandleGetConversation(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()
	handler := srv.handler()

	_, makeAliceReq := makePremiumAgent(t, srv)
	bobID, _ := makePremiumAgent(t, srv)

	// Alice sends to Bob
	body := []byte(`{"to_public_key":"` + bobID + `","ciphertext":"hello","nonce":"n1"}`)
	req := makeAliceReq("POST", "/api/v1/messages", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Send failed: %d %s", w.Code, w.Body.String())
	}

	// Alice gets conversation with Bob
	req = makeAliceReq("GET", "/api/v1/messages/"+bobID, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
		return
	}

	var resp ConversationMessagesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if len(resp.Messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(resp.Messages))
	}
}

func TestHandleMarkRead(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()
	handler := srv.handler()

	_, makeAliceReq := makePremiumAgent(t, srv)
	bobID, makeBobReq := makePremiumAgent(t, srv)

	// Alice sends to Bob
	body := []byte(`{"to_public_key":"` + bobID + `","ciphertext":"read_me","nonce":"n1"}`)
	req := makeAliceReq("POST", "/api/v1/messages", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var sendResp MessageResponse
	json.NewDecoder(w.Body).Decode(&sendResp)
	msgID := sendResp.ID

	// Bob marks as read
	req = makeBobReq("PUT", "/api/v1/messages/"+msgID+"/read", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteMessage(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()
	handler := srv.handler()

	_, makeAliceReq := makePremiumAgent(t, srv)
	bobID, _ := makePremiumAgent(t, srv)

	// Alice sends to Bob
	body := []byte(`{"to_public_key":"` + bobID + `","ciphertext":"delete_me","nonce":"n1"}`)
	req := makeAliceReq("POST", "/api/v1/messages", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var sendResp MessageResponse
	json.NewDecoder(w.Body).Decode(&sendResp)
	msgID := sendResp.ID

	// Alice deletes (she's the sender)
	req = makeAliceReq("DELETE", "/api/v1/messages/"+msgID, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteMessage_NotSender(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()
	handler := srv.handler()

	_, makeAliceReq := makePremiumAgent(t, srv)
	bobID, makeBobReq := makePremiumAgent(t, srv)

	// Alice sends to Bob
	body := []byte(`{"to_public_key":"` + bobID + `","ciphertext":"data","nonce":"n1"}`)
	req := makeAliceReq("POST", "/api/v1/messages", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var sendResp MessageResponse
	json.NewDecoder(w.Body).Decode(&sendResp)
	msgID := sendResp.ID

	// Bob tries to delete (should fail - he's not the sender)
	req = makeBobReq("DELETE", "/api/v1/messages/"+msgID, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
