package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agent_net"
)

func TestGetPostByID(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()

	post, err := srv.service.CreatePost(context.Background(), "pubkey123", "hello", gowild_agent_net.VerificationMethodPoW, nil)
	if err != nil {
		t.Fatalf("Failed to create post: %v", err)
	}

	handler := srv.handler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts/"+post.ID, nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp CreatePostResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.ID != post.ID {
		t.Errorf("Expected post ID %s, got %s", post.ID, resp.ID)
	}
}

func TestListPostsTrailingSlash(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()

	handler := srv.handler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ListPostsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
}

func TestUpgradeAccountSuccess(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()

	body := []byte(`{"tx_signature":"tx_abc","chain":"solana"}`)
	req, pubkey, _ := makeSignedRequest(t, http.MethodPost, "/api/v1/account/upgrade", body)
	w := httptest.NewRecorder()

	srv.handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp UpgradeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Fatalf("Expected success true, got false")
	}

	if resp.PublicKey != gowild_agent_net.EncodePublicKey(pubkey) {
		t.Errorf("Expected public key %s, got %s", gowild_agent_net.EncodePublicKey(pubkey), resp.PublicKey)
	}

	isPremium, err := srv.service.IsPremium(context.Background(), resp.PublicKey)
	if err != nil {
		t.Fatalf("Failed to check premium status: %v", err)
	}
	if !isPremium {
		t.Errorf("Expected premium status true, got false")
	}
}

func TestUpgradeAccountInvalidChain(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()

	body := []byte(`{"tx_signature":"tx_abc","chain":"bitcoin"}`)
	req, _, _ := makeSignedRequest(t, http.MethodPost, "/api/v1/account/upgrade", body)
	w := httptest.NewRecorder()

	srv.handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAndGetProfile(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()

	publicKey, makeReq := makePremiumAgent(t, srv)

	body := []byte(`{"name":"Agent Alpha","description":"Testing profile","url":"https://example.com"}`)
	updateReq := makeReq(http.MethodPut, "/api/v1/profile", body)
	updateW := httptest.NewRecorder()

	srv.handler().ServeHTTP(updateW, updateReq)

	if updateW.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", updateW.Code, updateW.Body.String())
	}

	var updateResp ProfileResponse
	if err := json.NewDecoder(updateW.Body).Decode(&updateResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if updateResp.PublicKey != publicKey {
		t.Errorf("Expected public key %s, got %s", publicKey, updateResp.PublicKey)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/profile/"+publicKey, nil)
	getW := httptest.NewRecorder()

	srv.handler().ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", getW.Code, getW.Body.String())
	}

	var getResp ProfileResponse
	if err := json.NewDecoder(getW.Body).Decode(&getResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if getResp.Name != "Agent Alpha" {
		t.Errorf("Expected name 'Agent Alpha', got %s", getResp.Name)
	}
}

func TestDeleteAccountPremium(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()

	publicKey, makeReq := makePremiumAgent(t, srv)
	deleteReq := makeReq(http.MethodDelete, "/api/v1/account", nil)
	deleteW := httptest.NewRecorder()

	srv.handler().ServeHTTP(deleteW, deleteReq)

	if deleteW.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", deleteW.Code, deleteW.Body.String())
	}

	revoked, err := srv.service.IsKeyRevoked(context.Background(), publicKey)
	if err != nil {
		t.Fatalf("Failed to check revoked status: %v", err)
	}
	if !revoked {
		t.Errorf("Expected key to be revoked")
	}
}

func TestDeleteAccountNonPremium(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()

	req, _, _ := makeSignedRequest(t, http.MethodDelete, "/api/v1/account", nil)
	w := httptest.NewRecorder()

	srv.handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("Expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPoWTestEndpoint(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()

	timestamp := time.Now().UTC().Format(time.RFC3339)
	body := []byte(`{"payload":{"foo":"bar"},"timestamp":"` + timestamp + `","nonce":"abc"}`)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pow/test", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	srv.handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp PoWTestResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.CanonicalJSON == "" || resp.ExpectedHash == "" {
		t.Errorf("Expected canonical_json and expected_hash to be set")
	}
}

func TestHelpEndpoint(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()

	req := httptest.NewRequest(http.MethodGet, "/help", nil)
	w := httptest.NewRecorder()

	srv.handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	if !strings.Contains(w.Body.String(), "Agent402 Protocol v3") {
		t.Fatalf("Expected help content to include protocol header")
	}
}

func TestA2ASkillEndpoint(t *testing.T) {
	srv, db := setupTestServer(t)
	defer db.Close()

	req := httptest.NewRequest(http.MethodGet, "/a2a_skill.md", nil)
	w := httptest.NewRecorder()

	srv.handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Agent402 A2A Skill") {
		t.Fatalf("Expected A2A skill content header")
	}
	if !strings.Contains(body, "/api/v1/a2a/jobs") {
		t.Fatalf("Expected A2A job endpoint in skill doc")
	}
	if strings.Contains(body, "/api/v1/posts") {
		t.Fatalf("A2A skill doc should not include posts endpoint")
	}
	if strings.Contains(body, "/api/v1/messages") {
		t.Fatalf("A2A skill doc should not include messages endpoint")
	}
}
