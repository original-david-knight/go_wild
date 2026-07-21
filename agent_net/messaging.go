package gowild_agent_net

import "time"

// DirectMessage represents an E2E encrypted direct message between two agents.
// The server is a blind relay — it never sees plaintext.
type DirectMessage struct {
	ID            string     `json:"id"`              // UUID
	FromPublicKey string     `json:"from_public_key"` // Sender Ed25519 pubkey (Base64URL)
	ToPublicKey   string     `json:"to_public_key"`   // Recipient Ed25519 pubkey (Base64URL)
	Ciphertext    string     `json:"ciphertext"`      // Base64URL NaCl box ciphertext
	Nonce         string     `json:"nonce"`           // Base64URL 24-byte encryption nonce
	CreatedAt     time.Time  `json:"created_at"`
	ReadAt        *time.Time `json:"read_at"`    // nil = unread
	ExpiresAt     *time.Time `json:"expires_at"` // nil = permanent
}

// GetID returns the primary key.
func (m DirectMessage) GetID() string {
	return m.ID
}

// TableName returns the database table name.
func (m DirectMessage) TableName() string {
	return "direct_messages"
}

// MaxMessageSize is the maximum ciphertext size in bytes (base64-encoded).
const MaxMessageSize = 8192

// Messaging rate limits (separate from post rate limits).
var MessageRateLimits = map[AgentTier]RateLimits{
	TierPremium: {PerMinute: 120, PerHour: 1000},
}

// Messaging rate limit window type constants.
const (
	WindowTypeMsgMinute = "msg_minute"
	WindowTypeMsgHour   = "msg_hour"
)

// ConversationSummary represents a conversation peer with metadata.
type ConversationSummary struct {
	PeerPublicKey string    `json:"peer_public_key"`
	LastMessageAt time.Time `json:"last_message_at"`
	UnreadCount   int       `json:"unread_count"`
	LastMessageID string    `json:"last_message_id"`
}
