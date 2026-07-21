package server

import (
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/original-david-knight/go_wild/agent_net"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for agent-to-agent communication
	},
}

// HandleWSUpgrade handles GET /api/v1/messages/ws — upgrades to WebSocket.
// Auth via query params since WebSocket upgrades can't use custom headers.
// Query params: agent_id, timestamp, signature
func (h *Handlers) HandleWSUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBadRequest(w, "Method not allowed")
		return
	}

	query := r.URL.Query()
	agentID := query.Get("agent_id")
	timestampStr := query.Get("timestamp")
	signatureStr := query.Get("signature")

	// Validate required params
	if agentID == "" || timestampStr == "" || signatureStr == "" {
		writeBadRequest(w, "Missing required query params: agent_id, timestamp, signature")
		return
	}

	// Validate agent_id format (43 chars Base64URL)
	if len(agentID) != 43 {
		writeBadRequest(w, "Invalid agent_id: expected 43 character Base64URL encoded Ed25519 public key")
		return
	}

	// Decode public key
	pubkey, err := gowild_agent_net.DecodePublicKey(agentID)
	if err != nil {
		writeBadRequest(w, "Invalid agent_id: "+err.Error())
		return
	}

	// Validate timestamp
	timestamp, err := time.Parse(time.RFC3339, timestampStr)
	if err != nil {
		writeBadRequest(w, "Invalid timestamp format: must be RFC3339")
		return
	}

	diff := time.Since(timestamp)
	if diff < 0 {
		diff = -diff
	}
	if diff > TimestampTolerance {
		writeTimestampError(w, "Timestamp out of range")
		return
	}

	// Validate signature
	// Signature input: GET:/api/v1/messages/ws:TIMESTAMP:SHA256("")
	if len(signatureStr) != 86 {
		writeUnauthorized(w, "Invalid signature: expected 86 character Base64URL encoded signature")
		return
	}

	sig, err := gowild_agent_net.DecodeSignature(signatureStr)
	if err != nil {
		writeUnauthorized(w, "Invalid signature: "+err.Error())
		return
	}

	// Build signature input — empty body for GET
	emptyBodyHash := sha256.Sum256([]byte{})
	signInput := fmt.Sprintf("GET:/api/v1/messages/ws:%s:%x", timestampStr, emptyBodyHash)
	if !gowild_agent_net.VerifySignature(pubkey, "GET", "/api/v1/messages/ws", timestampStr, []byte{}, sig) {
		writeUnauthorized(w, "Signature verification failed")
		return
	}

	_ = signInput // signInput used for documentation — actual verification done above

	// Check premium status
	isPremium, err := h.service.IsPremium(r.Context(), agentID)
	if err != nil || !isPremium {
		writePremiumRequired(w, "WebSocket messaging requires a premium account")
		return
	}

	// Check not revoked
	isRevoked, err := h.service.IsKeyRevoked(r.Context(), agentID)
	if err != nil || isRevoked {
		writeForbidden(w, "Key has been revoked")
		return
	}

	// Upgrade connection
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws: upgrade failed for %s: %v", agentID[:12], err)
		return
	}

	log.Printf("ws: agent %s connected", agentID[:12])

	// Register with hub
	if h.wsHub == nil {
		conn.Close()
		return
	}

	client := h.wsHub.Register(agentID, conn)

	// Start read/write pumps
	go client.WritePump()
	go client.ReadPump()
}
