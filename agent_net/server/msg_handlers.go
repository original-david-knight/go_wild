package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/agent_net"
)

// SendMessageRequest represents the request body for sending a message.
type SendMessageRequest struct {
	ToPublicKey string  `json:"to_public_key"`
	Ciphertext  string  `json:"ciphertext"`
	Nonce       string  `json:"nonce"`
	ExpiresAt   *string `json:"expires_at,omitempty"` // ISO8601 timestamp or nil
}

// MessageResponse represents a direct message in API responses.
type MessageResponse struct {
	ID            string  `json:"id"`
	FromPublicKey string  `json:"from_public_key"`
	ToPublicKey   string  `json:"to_public_key"`
	Ciphertext    string  `json:"ciphertext"`
	Nonce         string  `json:"nonce"`
	CreatedAt     string  `json:"created_at"`
	ReadAt        *string `json:"read_at,omitempty"`
	ExpiresAt     *string `json:"expires_at,omitempty"`
}

// ConversationListResponse represents the response for listing conversations.
type ConversationListResponse struct {
	Conversations []ConversationResponse `json:"conversations"`
	NextOffset    int                    `json:"next_offset,omitempty"`
}

// ConversationResponse represents a single conversation summary.
type ConversationResponse struct {
	PeerPublicKey string `json:"peer_public_key"`
	LastMessageAt string `json:"last_message_at"`
	UnreadCount   int    `json:"unread_count"`
	LastMessageID string `json:"last_message_id"`
}

// ConversationMessagesResponse represents the response for getting messages in a conversation.
type ConversationMessagesResponse struct {
	Messages   []MessageResponse `json:"messages"`
	NextOffset int               `json:"next_offset,omitempty"`
}

// HandleSendMessage handles POST /api/v1/messages.
func (h *Handlers) HandleSendMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeBadRequest(w, "Method not allowed")
		return
	}

	agentID := GetAgentID(r.Context())

	body := GetCachedBody(r.Context())
	if body == nil {
		writeBadRequest(w, "Request body is required")
		return
	}

	var req SendMessageRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeBadRequest(w, "Invalid JSON: "+err.Error())
		return
	}

	if req.ToPublicKey == "" {
		writeBadRequest(w, "to_public_key is required")
		return
	}

	if req.Ciphertext == "" {
		writeMessageError(w, http.StatusBadRequest, ErrCodeBadRequest, "ciphertext is required")
		return
	}

	if req.Nonce == "" {
		writeBadRequest(w, "nonce is required")
		return
	}

	if len(req.Ciphertext) > gowild_agent_net.MaxMessageSize {
		writeMessageError(w, http.StatusBadRequest, ErrCodeMessageTooLarge,
			"Message too large: exceeds maximum size")
		return
	}

	// Check for self-message
	if agentID == req.ToPublicKey {
		writeMessageError(w, http.StatusBadRequest, ErrCodeSelfMessage,
			"Cannot send a message to yourself")
		return
	}

	// Parse optional expiry
	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			writeBadRequest(w, "Invalid expires_at format: must be RFC3339")
			return
		}
		if parsed.Before(time.Now()) {
			writeBadRequest(w, "expires_at must be in the future")
			return
		}
		expiresAt = &parsed
	}

	msg, err := h.service.SendMessage(r.Context(), agentID, req.ToPublicKey, req.Ciphertext, req.Nonce, expiresAt)
	if err != nil {
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "not premium"):
			if strings.Contains(errMsg, "recipient") {
				writeMessageError(w, http.StatusBadRequest, ErrCodeRecipientNotPremium,
					"Recipient is not a premium agent")
			} else {
				writePremiumRequired(w, "Messaging requires a premium account")
			}
		case strings.Contains(errMsg, "revoked"):
			writeForbidden(w, errMsg)
		case strings.Contains(errMsg, "rate limit"):
			writeRateLimited(w, "Messaging rate limit exceeded", 60, "120/min")
		default:
			writeInternalError(w, "Failed to send message: "+errMsg)
		}
		return
	}

	// Notify recipient via WebSocket if connected
	if h.wsHub != nil {
		h.wsHub.NotifyNewMessage(req.ToPublicKey, msg.ID, agentID, msg.CreatedAt)
	}

	writeJSON(w, http.StatusCreated, messageToResponse(msg))
}

// HandleListConversations handles GET /api/v1/messages.
func (h *Handlers) HandleListConversations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeBadRequest(w, "Method not allowed")
		return
	}

	agentID := GetAgentID(r.Context())
	query := r.URL.Query()

	limit := 50
	offset := 0

	if l := query.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	if o := query.Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	convs, err := h.service.ListConversations(r.Context(), agentID, limit, offset)
	if err != nil {
		writeInternalError(w, "Failed to list conversations: "+err.Error())
		return
	}

	resp := ConversationListResponse{
		Conversations: make([]ConversationResponse, 0, len(convs)),
	}
	for _, c := range convs {
		resp.Conversations = append(resp.Conversations, ConversationResponse{
			PeerPublicKey: c.PeerPublicKey,
			LastMessageAt: c.LastMessageAt.Format(time.RFC3339),
			UnreadCount:   c.UnreadCount,
			LastMessageID: c.LastMessageID,
		})
	}
	if len(convs) == limit {
		resp.NextOffset = offset + limit
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleGetConversation handles GET /api/v1/messages/{pubkey}.
func (h *Handlers) HandleGetConversation(w http.ResponseWriter, r *http.Request, peerPubKey string) {
	if r.Method != http.MethodGet {
		writeBadRequest(w, "Method not allowed")
		return
	}

	agentID := GetAgentID(r.Context())
	query := r.URL.Query()

	limit := 50
	offset := 0

	if l := query.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	if o := query.Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	msgs, err := h.service.GetConversation(r.Context(), agentID, peerPubKey, limit, offset)
	if err != nil {
		writeInternalError(w, "Failed to get conversation: "+err.Error())
		return
	}

	resp := ConversationMessagesResponse{
		Messages: make([]MessageResponse, 0, len(msgs)),
	}
	for _, m := range msgs {
		resp.Messages = append(resp.Messages, messageToResponse(&m))
	}
	if len(msgs) == limit {
		resp.NextOffset = offset + limit
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleMarkRead handles PUT /api/v1/messages/{id}/read.
func (h *Handlers) HandleMarkRead(w http.ResponseWriter, r *http.Request, messageID string) {
	if r.Method != http.MethodPut {
		writeBadRequest(w, "Method not allowed")
		return
	}

	agentID := GetAgentID(r.Context())

	if err := h.service.MarkMessageRead(r.Context(), agentID, messageID); err != nil {
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "not found"):
			writeMessageError(w, http.StatusNotFound, ErrCodeMessageNotFound, "Message not found")
		case strings.Contains(errMsg, "recipient"):
			writeMessageError(w, http.StatusForbidden, ErrCodeNotRecipient,
				"Only the recipient can mark a message as read")
		default:
			writeInternalError(w, "Failed to mark message as read: "+errMsg)
		}
		return
	}

	// Notify sender via WebSocket
	if h.wsHub != nil {
		// We need to look up the message to find the sender
		msgs, err := h.service.GetConversation(r.Context(), agentID, "", 1, 0)
		// Best effort notification — we just use the agentID as "from" since
		// the read receipt is from the reader to the message sender
		_ = msgs
		_ = err
		// The hub notify method handles the lookup
		h.wsHub.NotifyMessageRead(agentID, messageID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"message_id": messageID,
	})
}

// HandleDeleteMessage handles DELETE /api/v1/messages/{id}.
func (h *Handlers) HandleDeleteMessage(w http.ResponseWriter, r *http.Request, messageID string) {
	if r.Method != http.MethodDelete {
		writeBadRequest(w, "Method not allowed")
		return
	}

	agentID := GetAgentID(r.Context())

	if err := h.service.DeleteMessage(r.Context(), agentID, messageID); err != nil {
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "not found"):
			writeMessageError(w, http.StatusNotFound, ErrCodeMessageNotFound, "Message not found")
		case strings.Contains(errMsg, "sender"):
			writeMessageError(w, http.StatusForbidden, ErrCodeNotSender,
				"Only the sender can delete a message")
		default:
			writeInternalError(w, "Failed to delete message: "+errMsg)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"message_id": messageID,
	})
}

// messageToResponse converts a DirectMessage to a MessageResponse.
func messageToResponse(m *gowild_agent_net.DirectMessage) MessageResponse {
	resp := MessageResponse{
		ID:            m.ID,
		FromPublicKey: m.FromPublicKey,
		ToPublicKey:   m.ToPublicKey,
		Ciphertext:    m.Ciphertext,
		Nonce:         m.Nonce,
		CreatedAt:     m.CreatedAt.Format(time.RFC3339),
	}
	if m.ReadAt != nil {
		s := m.ReadAt.Format(time.RFC3339)
		resp.ReadAt = &s
	}
	if m.ExpiresAt != nil {
		s := m.ExpiresAt.Format(time.RFC3339)
		resp.ExpiresAt = &s
	}
	return resp
}
