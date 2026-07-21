package gowild_agent_net

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// NonceTTL defines how long a nonce is considered "used" to prevent replay.
const NonceTTL = 10 * time.Minute

// NonceTracker tracks used nonces to prevent replay attacks.
type NonceTracker struct {
	db gowild_data.Database
}

// NewNonceTracker creates a new nonce tracker.
func NewNonceTracker(db gowild_data.Database) *NonceTracker {
	return &NonceTracker{db: db}
}

// CheckAndRecord checks if a nonce has been used and records it if not.
// Returns nil if the nonce is valid (not used), error if it's a replay.
func (n *NonceTracker) CheckAndRecord(ctx context.Context, publicKey, nonce string, timestamp time.Time) error {
	id := n.makeID(publicKey, timestamp, nonce)

	// Check if nonce exists
	var existing UsedNonce
	err := n.db.Table(UsedNonce{}).Get(ctx, id, &existing)
	if err == nil {
		// Nonce found - check if still valid (not expired)
		if time.Now().Before(existing.ExpiresAt) {
			return fmt.Errorf("nonce already used (replay attack detected)")
		}
		// Expired, can reuse - delete and continue
		n.db.Table(UsedNonce{}).Delete(ctx, id)
	}

	// Record the nonce
	usedNonce := &UsedNonce{
		ID:        id,
		PublicKey: publicKey,
		Nonce:     nonce,
		Timestamp: timestamp,
		ExpiresAt: timestamp.Add(NonceTTL),
	}

	if err := n.db.Table(UsedNonce{}).Insert(ctx, usedNonce); err != nil {
		// If insert fails due to race condition, that's also a replay
		return fmt.Errorf("failed to record nonce: %w", err)
	}

	return nil
}

// isUsed checks if a nonce has been used (without recording).
func (n *NonceTracker) isUsed(ctx context.Context, publicKey, nonce string, timestamp time.Time) bool {
	id := n.makeID(publicKey, timestamp, nonce)

	var existing UsedNonce
	err := n.db.Table(UsedNonce{}).Get(ctx, id, &existing)
	if err != nil {
		return false
	}

	// Check if still valid
	return time.Now().Before(existing.ExpiresAt)
}

// Cleanup removes expired nonces.
func (n *NonceTracker) Cleanup(ctx context.Context) (int, error) {
	results, err := n.db.Table(UsedNonce{}).GetAll(ctx)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	deleted := 0

	for _, result := range results {
		nonce, ok := result.(*UsedNonce)
		if !ok {
			continue
		}

		if now.After(nonce.ExpiresAt) {
			if err := n.db.Table(UsedNonce{}).Delete(ctx, nonce.ID); err == nil {
				deleted++
			}
		}
	}

	return deleted, nil
}

// makeID creates a unique ID for a nonce record.
func (n *NonceTracker) makeID(publicKey string, timestamp time.Time, nonce string) string {
	data := fmt.Sprintf("%s:%s:%s", publicKey, timestamp.Format(time.RFC3339), nonce)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash[:16])
}

// StartCleanupWorker starts a background goroutine that periodically cleans up expired nonces.
func (n *NonceTracker) StartCleanupWorker(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n.Cleanup(ctx)
			}
		}
	}()
}
