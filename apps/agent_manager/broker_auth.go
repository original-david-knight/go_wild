package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	agentauth "github.com/original-david-knight/go_wild/agent_auth"
	gowild_crypto "github.com/original-david-knight/go_wild/crypto"
	gowild_data "github.com/original-david-knight/go_wild/data"
)

type contextKey string

const brokerAgentIDKey contextKey = "broker_agent_id"
const brokerAgentAddressKey contextKey = "broker_agent_address"
const brokerExecutionMethodKey contextKey = "broker_execution_method"
const brokerExecutionMethodHeader = "X-Gowild-Execution-Method"

const (
	brokerAuthDomain    = "gowild-agent-manager"
	brokerAuthStatement = "Authenticate this agent with the Gowild broker."
	brokerAuthURI       = "gowild://broker"
	brokerAuthChainID   = 1
	brokerChallengeTTL  = 2 * time.Minute
	brokerSessionTTL    = 12 * time.Hour
)

// Keep broker authentication on a separate Ethereum account from the wallet
// tools' spend address, which uses index 0.
const agentAuthDerivationIndex uint32 = 1

type AgentSession = agentauth.Session

type BrokerAuthHandler struct {
	service      *AgentService
	secret       []byte
	mu           sync.Mutex
	challenges   map[string]agentauth.Challenge
	now          func() time.Time
	challengeTTL time.Duration
	sessionTTL   time.Duration
}

func NewBrokerAuthHandler(service *AgentService, secret []byte) *BrokerAuthHandler {
	return &BrokerAuthHandler{
		service:      service,
		secret:       secret,
		challenges:   make(map[string]agentauth.Challenge),
		now:          time.Now,
		challengeTTL: brokerChallengeTTL,
		sessionTTL:   brokerSessionTTL,
	}
}

// BrokerAgentID extracts the agent ID from the request context (set by broker auth middleware).
func BrokerAgentID(ctx context.Context) string {
	if id, ok := ctx.Value(brokerAgentIDKey).(string); ok {
		return id
	}
	return ""
}

func BrokerAgentAddress(ctx context.Context) string {
	if address, ok := ctx.Value(brokerAgentAddressKey).(string); ok {
		return strings.TrimSpace(address)
	}
	return ""
}

func BrokerExecutionMethod(ctx context.Context) string {
	if method, ok := ctx.Value(brokerExecutionMethodKey).(string); ok {
		return strings.TrimSpace(method)
	}
	return ""
}

func (h *BrokerAuthHandler) handleChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req agentauth.ChallengeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid challenge request: "+err.Error())
		return
	}

	agentID, err := agentauth.NormalizeAgentID(req.AgentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	address, err := agentauth.NormalizeAddress(req.Address)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	registeredAddress, err := h.registeredAgentAddress(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "agent authentication unavailable: "+err.Error())
		return
	}
	if !agentauth.SameAddress(address, registeredAddress) {
		writeError(w, http.StatusUnauthorized, "ethereum address is not registered for this agent")
		return
	}

	now := h.now().UTC().Truncate(time.Second)
	expiresAt := now.Add(h.challengeTTL).UTC()
	challenge, err := agentauth.NewChallenge(agentauth.ChallengeOptions{
		AgentID:   agentID,
		Address:   registeredAddress,
		Domain:    brokerAuthDomain,
		Statement: brokerAuthStatement,
		URI:       brokerAuthURI,
		ChainID:   brokerAuthChainID,
		IssuedAt:  now,
		ExpiresAt: expiresAt,
		RequestID: agentID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build challenge message")
		return
	}

	h.mu.Lock()
	h.pruneExpiredChallengesLocked(now)
	h.challenges[challenge.Nonce] = challenge
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, challenge)
}

func (h *BrokerAuthHandler) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req agentauth.VerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid verify request: "+err.Error())
		return
	}

	nonce := strings.TrimSpace(req.Nonce)
	if nonce == "" {
		writeError(w, http.StatusBadRequest, "nonce is required")
		return
	}

	now := h.now().UTC()
	h.mu.Lock()
	challenge, ok := h.challenges[nonce]
	delete(h.challenges, nonce)
	h.pruneExpiredChallengesLocked(now)
	h.mu.Unlock()
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid or expired nonce")
		return
	}
	if err := agentauth.VerifyChallenge(challenge, req, now); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	registeredAddress, err := h.registeredAgentAddress(r.Context(), challenge.AgentID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "agent authentication unavailable: "+err.Error())
		return
	}
	if !agentauth.SameAddress(challenge.Address, registeredAddress) {
		writeError(w, http.StatusUnauthorized, "registered agent address changed")
		return
	}

	sessionToken, expiresAt, err := agentauth.GenerateSessionToken(h.secret, challenge.AgentID, challenge.Address, now, h.sessionTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate session token")
		return
	}

	writeJSON(w, http.StatusOK, agentauth.VerifyResponse{
		SessionToken: sessionToken,
		TokenType:    agentauth.BearerTokenType,
		AgentID:      challenge.AgentID,
		Address:      challenge.Address,
		ExpiresAt:    expiresAt.UTC().Format(time.RFC3339),
	})
}

func (h *BrokerAuthHandler) ValidateSessionToken(ctx context.Context, token string) (*AgentSession, error) {
	if h == nil {
		return nil, fmt.Errorf("broker auth unavailable")
	}
	session, err := agentauth.ValidateSessionToken(h.secret, token, h.now().UTC())
	if err != nil {
		return nil, err
	}
	registeredAddress, err := h.registeredAgentAddress(ctx, session.AgentID)
	if err != nil {
		return nil, err
	}
	if !agentauth.SameAddress(session.Address, registeredAddress) {
		return nil, fmt.Errorf("session address is no longer registered for this agent")
	}
	return session, nil
}

func (h *BrokerAuthHandler) registeredAgentAddress(ctx context.Context, agentID string) (string, error) {
	if h == nil || h.service == nil {
		return "", fmt.Errorf("agent service is not configured")
	}
	agent, err := h.service.GetAgent(ctx, agentID)
	if err != nil {
		return "", err
	}
	seedPhrase := strings.TrimSpace(agent.WalletSeedPhrase)
	if seedPhrase == "" {
		return "", fmt.Errorf("agent has no wallet seed phrase")
	}
	derived, err := gowild_crypto.DeriveKeysFromMnemonic(seedPhrase, agentAuthDerivationIndex)
	if err != nil {
		return "", fmt.Errorf("failed to derive registered ethereum address: %w", err)
	}
	return agentauth.NormalizeAddress(derived.EthAddress)
}

func (h *BrokerAuthHandler) pruneExpiredChallengesLocked(now time.Time) {
	for nonce, challenge := range h.challenges {
		expiresAt, err := agentauth.ParseChallengeExpiration(challenge)
		if err != nil || !now.Before(expiresAt) {
			delete(h.challenges, nonce)
		}
	}
}

// brokerSessionAuthMiddleware validates agent session tokens and injects the agent identity into the request context.
func brokerSessionAuthMiddleware(auth *BrokerAuthHandler, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			writeError(w, http.StatusUnauthorized, "invalid authorization format (expected Bearer token)")
			return
		}

		session, err := auth.ValidateSessionToken(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid agent session token: "+err.Error())
			return
		}

		ctx := context.WithValue(r.Context(), brokerAgentIDKey, session.AgentID)
		ctx = context.WithValue(ctx, brokerAgentAddressKey, session.Address)
		if executionMethod := strings.TrimSpace(r.Header.Get(brokerExecutionMethodHeader)); executionMethod != "" {
			ctx = context.WithValue(ctx, brokerExecutionMethodKey, executionMethod)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// loadOrGenerateBrokerSecret loads BROKER_SECRET from env, database, or generates one.
func loadOrGenerateBrokerSecret(db gowild_data.Database) []byte {
	ctx := context.Background()
	const settingKey = "broker_secret"

	// 1. Check environment variable first (takes precedence)
	if s := os.Getenv("BROKER_SECRET"); s != "" {
		decoded, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			log.Fatalf("Invalid BROKER_SECRET (must be base64): %v", err)
		}
		return decoded
	}

	// 2. Check database for stored secret
	if db != nil {
		stored, _ := GetSetting(ctx, db, settingKey)
		if stored != "" {
			decoded, err := base64.StdEncoding.DecodeString(stored)
			if err == nil {
				log.Println("Loaded broker secret from database")
				return decoded
			}
		}
	}

	// 3. Generate a new 32-byte secret
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		log.Fatalf("Failed to generate broker secret: %v", err)
	}

	encoded := base64.StdEncoding.EncodeToString(secret)

	// 4. Store in database for persistence
	if db != nil {
		if err := SetSetting(ctx, db, settingKey, encoded); err != nil {
			log.Printf("Warning: Failed to persist broker secret to database: %v", err)
		} else {
			log.Println("Generated and stored new broker secret in database")
		}
	} else {
		fmt.Printf("Generated BROKER_SECRET=%s\n", encoded)
		fmt.Println("Set this in your environment to persist across restarts.")
	}

	return secret
}
