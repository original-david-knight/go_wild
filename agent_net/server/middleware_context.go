package server

import (
	"context"
	"crypto/ed25519"
	"time"

	"github.com/original-david-knight/go_wild/agent_net"
)

// Context keys for request data.
type contextKey string

const (
	ctxKeyAgentID   contextKey = "agent_id"
	ctxKeyAgentTier contextKey = "agent_tier"
	ctxKeyTimestamp contextKey = "timestamp"
	ctxKeyBody      contextKey = "body"
	ctxKeyPublicKey contextKey = "public_key"
)

// Header names.
const (
	HeaderAgentID        = "X-Agent-ID"
	HeaderAgentSig       = "X-Agent-Sig"
	HeaderAgentTimestamp = "X-Agent-Timestamp"
	HeaderAgentPoW       = "X-Agent-PoW"
	HeaderAgentNonce     = "X-Agent-Nonce"
)

// Timestamp tolerance.
const TimestampTolerance = 5 * time.Minute

// MaxBodySize limits request body size (1 MB).
const MaxBodySize = 1 << 20

// GetAgentID retrieves the agent ID from context.
func GetAgentID(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyAgentID).(string)
	return v
}

// GetAgentTier retrieves the agent tier from context.
func GetAgentTier(ctx context.Context) gowild_agent_net.AgentTier {
	v, _ := ctx.Value(ctxKeyAgentTier).(gowild_agent_net.AgentTier)
	return v
}

// GetTimestamp retrieves the timestamp from context.
func GetTimestamp(ctx context.Context) time.Time {
	v, _ := ctx.Value(ctxKeyTimestamp).(time.Time)
	return v
}

// GetBody retrieves the cached request body from context.
func GetBody(ctx context.Context) []byte {
	v, _ := ctx.Value(ctxKeyBody).([]byte)
	return v
}

// GetCachedBody is an alias for GetBody.
func GetCachedBody(ctx context.Context) []byte {
	return GetBody(ctx)
}

// GetAgentPubkey retrieves the decoded Ed25519 public key from context.
func GetAgentPubkey(ctx context.Context) ed25519.PublicKey {
	v, _ := ctx.Value(ctxKeyPublicKey).(ed25519.PublicKey)
	return v
}
