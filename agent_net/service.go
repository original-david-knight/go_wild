package gowild_agent_net

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/original-david-knight/go_wild/data"
	"github.com/google/uuid"
)

// Service provides operations for the agent network.
type Service struct {
	db                 gowild_data.Database
	rateLimiter        *RateLimiter
	nonceTracker       *NonceTracker
	difficultyManager  *DifficultyManager
	blockchainVerifier *BlockchainVerifier
	a2aMu              sync.Mutex
}

// NewService creates a new agent network service.
func NewService(db gowild_data.Database, verifier *BlockchainVerifier) *Service {
	return &Service{
		db:                 db,
		rateLimiter:        NewRateLimiter(db),
		nonceTracker:       NewNonceTracker(db),
		difficultyManager:  NewDifficultyManager(db, DefaultBaseDifficulty),
		blockchainVerifier: verifier,
	}
}

// DB returns the underlying database handle.
func (s *Service) DB() gowild_data.Database {
	return s.db
}

// IsPremium checks if an agent has premium status.
func (s *Service) IsPremium(ctx context.Context, publicKey string) (bool, error) {
	results, err := s.db.Table(PremiumAgent{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"public_key": publicKey},
		Limit: 1,
	})
	if err != nil || len(results) == 0 {
		return false, nil // Not found = not premium
	}
	agent, ok := results[0].(*PremiumAgent)
	if !ok {
		return false, nil
	}
	return !agent.Revoked, nil
}

// GetAgentTier returns the tier for an agent.
func (s *Service) GetAgentTier(ctx context.Context, publicKey string) (AgentTier, error) {
	isPremium, err := s.IsPremium(ctx, publicKey)
	if err != nil {
		return TierFree, err
	}
	if isPremium {
		return TierPremium, nil
	}
	return TierFree, nil
}

// IsKeyRevoked checks if a key has been revoked.
func (s *Service) IsKeyRevoked(ctx context.Context, publicKey string) (bool, error) {
	results, err := s.db.Table(RevokedKey{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"public_key": publicKey},
		Limit: 1,
	})
	if err != nil || len(results) == 0 {
		return false, nil // Not found = not revoked
	}
	return true, nil
}

// CreatePost creates a new post.
func (s *Service) CreatePost(ctx context.Context, publicKey, content, verificationMethod string, metadata map[string]any) (*Post, error) {
	post := &Post{
		ID:                 uuid.New().String(),
		PublicKey:          publicKey,
		Content:            content,
		VerificationMethod: verificationMethod,
		Metadata:           metadata,
		CreatedAt:          time.Now(),
	}

	if err := s.db.Table(Post{}).Insert(ctx, post); err != nil {
		return nil, fmt.Errorf("failed to create post: %w", err)
	}

	return post, nil
}

// GetPost retrieves a post by ID.
func (s *Service) GetPost(ctx context.Context, postID string) (*Post, error) {
	var post Post
	if err := s.db.Table(Post{}).Get(ctx, postID, &post); err != nil {
		return nil, fmt.Errorf("post not found: %w", err)
	}
	return &post, nil
}

// listPosts retrieves posts with pagination.
func (s *Service) listPosts(ctx context.Context, limit, offset int) ([]Post, error) {
	results, err := s.db.Table(Post{}).Query(ctx, gowild_data.QueryOpts{
		OrderBy:   "created_at",
		OrderDesc: true,
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list posts: %w", err)
	}

	posts := make([]Post, 0, len(results))
	for _, r := range results {
		if post, ok := r.(*Post); ok {
			posts = append(posts, *post)
		}
	}

	return posts, nil
}

// UpgradeAccount upgrades an agent to premium via blockchain verification.
func (s *Service) UpgradeAccount(ctx context.Context, publicKey, txHash, chain string) error {
	// Check if already premium
	isPremium, err := s.IsPremium(ctx, publicKey)
	if err != nil {
		return err
	}
	if isPremium {
		return fmt.Errorf("account already premium")
	}

	// Check if key is revoked
	isRevoked, err := s.IsKeyRevoked(ctx, publicKey)
	if err != nil {
		return err
	}
	if isRevoked {
		return fmt.Errorf("key has been revoked")
	}

	// Check tx_hash uniqueness
	results, err := s.db.Table(PremiumAgent{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"tx_hash": txHash},
	})
	if err == nil && len(results) > 0 {
		return fmt.Errorf("transaction already used for upgrade")
	}

	// Verify blockchain transaction
	if s.blockchainVerifier != nil {
		if err := s.blockchainVerifier.VerifyUpgradeTransaction(ctx, txHash, chain, publicKey); err != nil {
			return fmt.Errorf("blockchain verification failed: %w", err)
		}
	}

	// Create premium agent record
	now := time.Now()
	agent := &PremiumAgent{
		ID:           publicKey, // Use public key as ID
		PublicKey:    publicKey,
		TxHash:       txHash,
		Chain:        chain,
		UpgradedAt:   now,
		Revoked:      false,
		LastActiveAt: now,
	}

	if err := s.db.Table(PremiumAgent{}).Insert(ctx, agent); err != nil {
		return fmt.Errorf("failed to create premium agent: %w", err)
	}

	return nil
}

// RevokeKey revokes an agent's key (self-revocation for premium accounts).
func (s *Service) RevokeKey(ctx context.Context, publicKey, reason, txHash string) error {
	// Add to revoked keys
	revoked := &RevokedKey{
		ID:        publicKey, // Use public key as ID
		PublicKey: publicKey,
		RevokedAt: time.Now(),
		Reason:    reason,
		TxHash:    txHash,
	}

	if err := s.db.Table(RevokedKey{}).Insert(ctx, revoked); err != nil {
		return fmt.Errorf("failed to revoke key: %w", err)
	}

	// Mark premium agent as revoked if exists
	results, err := s.db.Table(PremiumAgent{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"public_key": publicKey},
		Limit: 1,
	})
	if err == nil && len(results) > 0 {
		if agent, ok := results[0].(*PremiumAgent); ok {
			agent.Revoked = true
			s.db.Table(PremiumAgent{}).Update(ctx, agent)
		}
	}

	return nil
}

// UpdateLastActive updates the last active timestamp for an agent.
func (s *Service) UpdateLastActive(ctx context.Context, publicKey string) error {
	results, err := s.db.Table(PremiumAgent{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"public_key": publicKey},
		Limit: 1,
	})
	if err != nil || len(results) == 0 {
		return nil // Not premium, nothing to update
	}

	agent, ok := results[0].(*PremiumAgent)
	if !ok {
		return nil
	}

	agent.LastActiveAt = time.Now()
	return s.db.Table(PremiumAgent{}).Update(ctx, agent)
}

// CheckRateLimit checks if a request is within rate limits.
func (s *Service) CheckRateLimit(ctx context.Context, publicKey string, tier AgentTier) error {
	return s.rateLimiter.CheckLimit(ctx, publicKey, tier)
}

// RecordRequest records a request for rate limiting.
func (s *Service) RecordRequest(ctx context.Context, publicKey string) error {
	return s.rateLimiter.RecordRequest(ctx, publicKey)
}

// CheckNonce checks and records a nonce for replay protection.
func (s *Service) CheckNonce(ctx context.Context, publicKey, nonce string, timestamp time.Time) error {
	return s.nonceTracker.CheckAndRecord(ctx, publicKey, nonce, timestamp)
}

// GetCurrentDifficulty returns the current PoW difficulty.
func (s *Service) GetCurrentDifficulty(ctx context.Context) (*PoWDifficulty, error) {
	return s.difficultyManager.GetCurrentDifficulty(ctx)
}

// VerifyPoW verifies a Proof of Work hash.
func (s *Service) VerifyPoW(powHashHex string, payloadJSON []byte, timestamp, nonce string) (bool, error) {
	ctx := context.Background()
	difficulty, err := s.difficultyManager.GetCurrentDifficulty(ctx)
	if err != nil {
		return false, err
	}
	return VerifyPoW(powHashHex, payloadJSON, timestamp, nonce, difficulty.CurrentDifficulty)
}

// GetPremiumAgent retrieves premium agent details.
func (s *Service) GetPremiumAgent(ctx context.Context, publicKey string) (*PremiumAgent, error) {
	results, err := s.db.Table(PremiumAgent{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"public_key": publicKey},
		Limit: 1,
	})
	if err != nil || len(results) == 0 {
		return nil, fmt.Errorf("agent not found")
	}
	agent, ok := results[0].(*PremiumAgent)
	if !ok {
		return nil, fmt.Errorf("agent not found")
	}
	return agent, nil
}

// cleanup performs periodic cleanup of expired data.
func (s *Service) cleanup(ctx context.Context) (noncesDeleted, rateLimitsDeleted int, err error) {
	noncesDeleted, err = s.nonceTracker.Cleanup(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("nonce cleanup failed: %w", err)
	}

	rateLimitsDeleted, err = s.rateLimiter.Cleanup(ctx)
	if err != nil {
		return noncesDeleted, 0, fmt.Errorf("rate limit cleanup failed: %w", err)
	}

	return noncesDeleted, rateLimitsDeleted, nil
}

// StartCleanupWorker starts background cleanup workers.
func (s *Service) StartCleanupWorker(ctx context.Context) {
	// Cleanup nonces every 5 minutes
	s.nonceTracker.StartCleanupWorker(ctx, 5*time.Minute)
}

// UpdateProfile creates or updates an agent's profile.
func (s *Service) UpdateProfile(ctx context.Context, publicKey, name, description, url string) (*AgentProfile, error) {
	now := time.Now()

	// Check if profile exists
	var existing AgentProfile
	err := s.db.Table(AgentProfile{}).Get(ctx, publicKey, &existing)

	if err != nil {
		// Create new profile
		profile := &AgentProfile{
			ID:          publicKey,
			PublicKey:   publicKey,
			Name:        name,
			Description: description,
			URL:         url,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := s.db.Table(AgentProfile{}).Insert(ctx, profile); err != nil {
			return nil, fmt.Errorf("failed to create profile: %w", err)
		}
		return profile, nil
	}

	// Update existing profile
	existing.Name = name
	existing.Description = description
	existing.URL = url
	existing.UpdatedAt = now

	if err := s.db.Table(AgentProfile{}).Update(ctx, &existing); err != nil {
		return nil, fmt.Errorf("failed to update profile: %w", err)
	}

	return &existing, nil
}

// GetProfile retrieves an agent's profile by public key.
func (s *Service) GetProfile(ctx context.Context, publicKey string) (*AgentProfile, error) {
	var profile AgentProfile
	if err := s.db.Table(AgentProfile{}).Get(ctx, publicKey, &profile); err != nil {
		return nil, fmt.Errorf("profile not found: %w", err)
	}
	return &profile, nil
}

// GetProfilesMap retrieves profiles for multiple public keys, returning a map.
func (s *Service) GetProfilesMap(ctx context.Context, publicKeys []string) (map[string]*AgentProfile, error) {
	result := make(map[string]*AgentProfile)
	if len(publicKeys) == 0 {
		return result, nil
	}

	// Get all profiles and filter in memory (simpler than multiple queries)
	profiles, err := s.db.Table(AgentProfile{}).Query(ctx, gowild_data.QueryOpts{})
	if err != nil {
		return nil, fmt.Errorf("failed to query profiles: %w", err)
	}

	// Build a set of requested keys for fast lookup
	keySet := make(map[string]bool)
	for _, key := range publicKeys {
		keySet[key] = true
	}

	for _, r := range profiles {
		profile, ok := r.(*AgentProfile)
		if !ok {
			continue
		}
		if keySet[profile.PublicKey] {
			result[profile.PublicKey] = profile
		}
	}

	return result, nil
}

// AgentWithProfile combines agent info with their profile (if available).
type AgentWithProfile struct {
	PublicKey string
	Tier      AgentTier
	Revoked   bool
	Agent     *PremiumAgent // nil for free tier agents
	Profile   *AgentProfile
}

// ListAllAgents retrieves all agents (premium and those with profiles).
func (s *Service) ListAllAgents(ctx context.Context) ([]AgentWithProfile, error) {
	agentMap := make(map[string]*AgentWithProfile)

	// Get all premium agents
	premiumResults, err := s.db.Table(PremiumAgent{}).Query(ctx, gowild_data.QueryOpts{
		OrderBy:   "upgraded_at",
		OrderDesc: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list premium agents: %w", err)
	}

	for _, r := range premiumResults {
		agent, ok := r.(*PremiumAgent)
		if !ok {
			continue
		}

		tier := TierPremium
		if agent.Revoked {
			tier = TierFree // Revoked agents lose premium
		}

		agentMap[agent.PublicKey] = &AgentWithProfile{
			PublicKey: agent.PublicKey,
			Tier:      tier,
			Revoked:   agent.Revoked,
			Agent:     agent,
		}
	}

	// Get all profiles (includes free tier agents who set up profiles)
	profileResults, err := s.db.Table(AgentProfile{}).Query(ctx, gowild_data.QueryOpts{
		OrderBy:   "created_at",
		OrderDesc: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list profiles: %w", err)
	}

	for _, r := range profileResults {
		profile, ok := r.(*AgentProfile)
		if !ok {
			continue
		}

		if existing, exists := agentMap[profile.PublicKey]; exists {
			// Add profile to existing premium agent
			existing.Profile = profile
		} else {
			// Free tier agent with a profile
			agentMap[profile.PublicKey] = &AgentWithProfile{
				PublicKey: profile.PublicKey,
				Tier:      TierFree,
				Revoked:   false,
				Agent:     nil,
				Profile:   profile,
			}
		}
	}

	// Convert map to slice, premium first then free
	premiumAgents := make([]AgentWithProfile, 0)
	freeAgents := make([]AgentWithProfile, 0)

	for _, awp := range agentMap {
		if awp.Agent != nil {
			premiumAgents = append(premiumAgents, *awp)
		} else {
			freeAgents = append(freeAgents, *awp)
		}
	}

	// Sort premium by upgraded_at desc, free by profile created_at desc
	slices.SortFunc(premiumAgents, func(a, b AgentWithProfile) int {
		return b.Agent.UpgradedAt.Compare(a.Agent.UpgradedAt)
	})
	slices.SortFunc(freeAgents, func(a, b AgentWithProfile) int {
		if a.Profile == nil || b.Profile == nil {
			return 0
		}
		return b.Profile.CreatedAt.Compare(a.Profile.CreatedAt)
	})

	return append(premiumAgents, freeAgents...), nil
}
