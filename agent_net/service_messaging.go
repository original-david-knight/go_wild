package gowild_agent_net

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/original-david-knight/go_wild/data"
	"github.com/google/uuid"
)

// SendMessage sends an encrypted direct message between two premium agents.
func (s *Service) SendMessage(ctx context.Context, fromPubKey, toPubKey, ciphertext, nonce string, expiresAt *time.Time) (*DirectMessage, error) {
	// Validate not sending to self
	if fromPubKey == toPubKey {
		return nil, fmt.Errorf("cannot send message to self")
	}

	// Validate ciphertext size
	if len(ciphertext) > MaxMessageSize {
		return nil, fmt.Errorf("message too large: %d bytes exceeds maximum %d", len(ciphertext), MaxMessageSize)
	}

	if ciphertext == "" {
		return nil, fmt.Errorf("ciphertext is required")
	}

	if nonce == "" {
		return nil, fmt.Errorf("nonce is required")
	}

	// Verify sender is premium and not revoked
	senderPremium, err := s.IsPremium(ctx, fromPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check sender status: %w", err)
	}
	if !senderPremium {
		return nil, fmt.Errorf("sender is not premium")
	}

	senderRevoked, err := s.IsKeyRevoked(ctx, fromPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check sender revocation: %w", err)
	}
	if senderRevoked {
		return nil, fmt.Errorf("sender key is revoked")
	}

	// Verify recipient is premium and not revoked
	recipientPremium, err := s.IsPremium(ctx, toPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check recipient status: %w", err)
	}
	if !recipientPremium {
		return nil, fmt.Errorf("recipient is not premium")
	}

	recipientRevoked, err := s.IsKeyRevoked(ctx, toPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check recipient revocation: %w", err)
	}
	if recipientRevoked {
		return nil, fmt.Errorf("recipient key is revoked")
	}

	// Check messaging rate limit
	if err := s.CheckMessageRateLimit(ctx, fromPubKey); err != nil {
		return nil, err
	}

	msg := &DirectMessage{
		ID:            uuid.New().String(),
		FromPublicKey: fromPubKey,
		ToPublicKey:   toPubKey,
		Ciphertext:    ciphertext,
		Nonce:         nonce,
		CreatedAt:     time.Now(),
		ExpiresAt:     expiresAt,
	}

	if err := s.db.Table(DirectMessage{}).Insert(ctx, msg); err != nil {
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	// Record rate limit
	s.recordMessageRequest(ctx, fromPubKey)

	return msg, nil
}

// GetConversation retrieves messages between two agents, sorted by created_at.
func (s *Service) GetConversation(ctx context.Context, agentPubKey, peerPubKey string, limit, offset int) ([]DirectMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	// Query messages where (from=agent, to=peer) OR (from=peer, to=agent)
	// We need two queries since gowild_data doesn't support OR conditions
	results1, err := s.db.Table(DirectMessage{}).Query(ctx, gowild_data.QueryOpts{
		Where:     map[string]any{"from_public_key": agentPubKey, "to_public_key": peerPubKey},
		OrderBy:   "created_at",
		OrderDesc: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}

	results2, err := s.db.Table(DirectMessage{}).Query(ctx, gowild_data.QueryOpts{
		Where:     map[string]any{"from_public_key": peerPubKey, "to_public_key": agentPubKey},
		OrderBy:   "created_at",
		OrderDesc: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}

	// Merge results
	messages := make([]DirectMessage, 0, len(results1)+len(results2))
	for _, r := range results1 {
		if msg, ok := r.(*DirectMessage); ok {
			messages = append(messages, *msg)
		}
	}
	for _, r := range results2 {
		if msg, ok := r.(*DirectMessage); ok {
			messages = append(messages, *msg)
		}
	}

	// Sort by created_at descending (newest first)
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].CreatedAt.After(messages[j].CreatedAt)
	})

	// Apply offset and limit
	if offset >= len(messages) {
		return []DirectMessage{}, nil
	}
	messages = messages[offset:]
	if len(messages) > limit {
		messages = messages[:limit]
	}

	return messages, nil
}

// ListConversations returns unique peers the agent has messaged with, with unread counts.
func (s *Service) ListConversations(ctx context.Context, agentPubKey string, limit, offset int) ([]ConversationSummary, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	// Get all messages involving this agent (sent and received)
	sentResults, err := s.db.Table(DirectMessage{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"from_public_key": agentPubKey},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query sent messages: %w", err)
	}

	recvResults, err := s.db.Table(DirectMessage{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"to_public_key": agentPubKey},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query received messages: %w", err)
	}

	// Build conversation map: peer -> summary
	convMap := make(map[string]*ConversationSummary)

	for _, r := range sentResults {
		msg, ok := r.(*DirectMessage)
		if !ok {
			continue
		}
		peer := msg.ToPublicKey
		existing, ok := convMap[peer]
		if !ok {
			convMap[peer] = &ConversationSummary{
				PeerPublicKey: peer,
				LastMessageAt: msg.CreatedAt,
				LastMessageID: msg.ID,
			}
		} else if msg.CreatedAt.After(existing.LastMessageAt) {
			existing.LastMessageAt = msg.CreatedAt
			existing.LastMessageID = msg.ID
		}
	}

	for _, r := range recvResults {
		msg, ok := r.(*DirectMessage)
		if !ok {
			continue
		}
		peer := msg.FromPublicKey
		existing, ok := convMap[peer]
		if !ok {
			convMap[peer] = &ConversationSummary{
				PeerPublicKey: peer,
				LastMessageAt: msg.CreatedAt,
				LastMessageID: msg.ID,
			}
		} else if msg.CreatedAt.After(existing.LastMessageAt) {
			existing.LastMessageAt = msg.CreatedAt
			existing.LastMessageID = msg.ID
		}
		// Count unread messages (received, not read)
		if msg.ReadAt == nil {
			convMap[peer].UnreadCount++
		}
	}

	// Convert to sorted slice (newest conversation first)
	summaries := make([]ConversationSummary, 0, len(convMap))
	for _, s := range convMap {
		summaries = append(summaries, *s)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].LastMessageAt.After(summaries[j].LastMessageAt)
	})

	// Apply offset and limit
	if offset >= len(summaries) {
		return []ConversationSummary{}, nil
	}
	summaries = summaries[offset:]
	if len(summaries) > limit {
		summaries = summaries[:limit]
	}

	return summaries, nil
}

// MarkMessageRead marks a message as read. Only the recipient can mark it.
func (s *Service) MarkMessageRead(ctx context.Context, agentPubKey, messageID string) error {
	var msg DirectMessage
	if err := s.db.Table(DirectMessage{}).Get(ctx, messageID, &msg); err != nil {
		return fmt.Errorf("message not found")
	}

	if msg.ToPublicKey != agentPubKey {
		return fmt.Errorf("only the recipient can mark a message as read")
	}

	if msg.ReadAt != nil {
		return nil // Already read
	}

	now := time.Now()
	msg.ReadAt = &now
	if err := s.db.Table(DirectMessage{}).Update(ctx, &msg); err != nil {
		return fmt.Errorf("failed to mark message as read: %w", err)
	}

	return nil
}

// DeleteMessage deletes a message. Only the sender can delete it.
func (s *Service) DeleteMessage(ctx context.Context, agentPubKey, messageID string) error {
	var msg DirectMessage
	if err := s.db.Table(DirectMessage{}).Get(ctx, messageID, &msg); err != nil {
		return fmt.Errorf("message not found")
	}

	if msg.FromPublicKey != agentPubKey {
		return fmt.Errorf("only the sender can delete a message")
	}

	if err := s.db.Table(DirectMessage{}).Delete(ctx, messageID); err != nil {
		return fmt.Errorf("failed to delete message: %w", err)
	}

	return nil
}

// CleanupExpiredMessages removes messages past their expiry time.
func (s *Service) CleanupExpiredMessages(ctx context.Context) (int, error) {
	results, err := s.db.Table(DirectMessage{}).GetAll(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to query messages: %w", err)
	}

	now := time.Now()
	deleted := 0

	for _, r := range results {
		msg, ok := r.(*DirectMessage)
		if !ok {
			continue
		}
		if msg.ExpiresAt != nil && now.After(*msg.ExpiresAt) {
			if err := s.db.Table(DirectMessage{}).Delete(ctx, msg.ID); err == nil {
				deleted++
			}
		}
	}

	return deleted, nil
}

// CheckMessageRateLimit checks messaging-specific rate limits for a premium agent.
func (s *Service) CheckMessageRateLimit(ctx context.Context, publicKey string) error {
	limits := MessageRateLimits[TierPremium]
	now := time.Now()

	// Check minute limit
	if err := s.rateLimiter.checkWindow(ctx, publicKey, WindowTypeMsgMinute, limits.PerMinute, now); err != nil {
		return err
	}

	// Check hour limit
	if err := s.rateLimiter.checkWindow(ctx, publicKey, WindowTypeMsgHour, limits.PerHour, now); err != nil {
		return err
	}

	return nil
}

// recordMessageRequest records a messaging request for rate limiting.
func (s *Service) recordMessageRequest(ctx context.Context, publicKey string) {
	now := time.Now()
	s.rateLimiter.incrementWindow(ctx, publicKey, WindowTypeMsgMinute, now)
	s.rateLimiter.incrementWindow(ctx, publicKey, WindowTypeMsgHour, now)
}
