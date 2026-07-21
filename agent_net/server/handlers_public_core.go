package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/original-david-knight/go_wild/agent_net"
)

func (h *Handlers) HandleGetDifficulty(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBadRequest(w, "Method not allowed")
		return
	}

	difficulty, err := h.service.GetCurrentDifficulty(r.Context())
	if err != nil {
		writeInternalError(w, "Failed to get difficulty: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, DifficultyResponse{
		BaseDifficulty:    difficulty.BaseDifficulty,
		CurrentDifficulty: difficulty.CurrentDifficulty,
		PostsLastHour:     difficulty.PostsLastHour,
	})
}

// DifficultyResponse represents current difficulty settings.
type DifficultyResponse struct {
	BaseDifficulty    int `json:"base_difficulty"`
	CurrentDifficulty int `json:"current_difficulty"`
	PostsLastHour     int `json:"posts_last_hour"`
}

// TreasuryResponse represents treasury addresses.
type TreasuryResponse struct {
	Addresses map[string]string `json:"addresses"`
	Amounts   map[string]string `json:"amounts"`
}

// HandleGetTreasury handles GET /api/v1/treasury.
func (h *Handlers) HandleGetTreasury(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBadRequest(w, "Method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, TreasuryResponse{
		Addresses: map[string]string{
			"solana":   h.treasury.Solana,
			"ethereum": h.treasury.Ethereum,
			"base":     h.treasury.Base,
		},
		Amounts: gowild_agent_net.UpgradeAmounts,
	})
}

// HealthResponse represents health check response.
type HealthResponse struct {
	Status string `json:"status"`
}

// HandleHealth handles GET /health.
func (h *Handlers) HandleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok"})
}

// PoWTestRequest is the request body for testing PoW computation.
type PoWTestRequest struct {
	Payload   any    `json:"payload"`
	Timestamp string `json:"timestamp"`
	Nonce     string `json:"nonce"`
	PoWHash   string `json:"pow_hash,omitempty"`
}

// PoWTestResponse is the response for PoW testing.
type PoWTestResponse struct {
	CanonicalJSON   string `json:"canonical_json"`
	Challenge       string `json:"challenge_hex"`
	ExpectedHash    string `json:"expected_hash"`
	ProvidedHash    string `json:"provided_hash,omitempty"`
	HashMatches     bool   `json:"hash_matches"`
	LeadingZeroBits int    `json:"leading_zero_bits"`
	RequiredBits    int    `json:"required_bits"`
	MeetsDifficulty bool   `json:"meets_difficulty"`
	Valid           bool   `json:"valid"`
}

// HandlePoWTest handles POST /api/v1/pow/test - helps debug PoW computation.
func (h *Handlers) HandlePoWTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeBadRequest(w, "Method not allowed - use POST")
		return
	}

	var req PoWTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	// Get canonical JSON
	canonicalJSON, err := gowild_agent_net.CanonicalJSON(req.Payload)
	if err != nil {
		writeBadRequest(w, "Failed to canonicalize JSON: "+err.Error())
		return
	}

	// Compute challenge
	challenge := gowild_agent_net.ComputePoWChallenge(canonicalJSON, req.Timestamp, req.Nonce)

	// Compute expected hash
	expectedHash := gowild_agent_net.ComputePoWHash(challenge)
	expectedHashHex := fmt.Sprintf("%x", expectedHash)

	// Get current difficulty
	difficulty, _ := h.service.GetCurrentDifficulty(r.Context())
	requiredBits := difficulty.CurrentDifficulty

	// Count leading zero bits
	leadingZeros := gowild_agent_net.CountLeadingZeroBits(expectedHash)

	resp := PoWTestResponse{
		CanonicalJSON:   string(canonicalJSON),
		Challenge:       fmt.Sprintf("%x", challenge),
		ExpectedHash:    expectedHashHex,
		LeadingZeroBits: leadingZeros,
		RequiredBits:    requiredBits,
		MeetsDifficulty: leadingZeros >= requiredBits,
	}

	// If client provided a hash, check if it matches
	if req.PoWHash != "" {
		resp.ProvidedHash = req.PoWHash
		resp.HashMatches = (req.PoWHash == expectedHashHex)
		resp.Valid = resp.HashMatches && resp.MeetsDifficulty
	} else {
		resp.Valid = resp.MeetsDifficulty
	}

	writeJSON(w, http.StatusOK, resp)
}
