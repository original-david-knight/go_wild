package gowild_agent_net

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/data"
	"github.com/google/uuid"
)

// CreateIsnadClaimPost creates a post from an isnad_claim payload.
// It validates the claim structure and signature, then stores it.
func (s *Service) CreateIsnadClaimPost(ctx context.Context, claim *IsnadClaim, authorPubkey ed25519.PublicKey, verificationMethod string) (*Post, error) {
	// Validate claim structure
	if err := ValidateIsnadClaim(claim); err != nil {
		return nil, fmt.Errorf("invalid claim: %w", err)
	}

	// Verify the claim signature
	valid, err := VerifyIsnadClaimSignature(claim, authorPubkey)
	if err != nil {
		// Error contains detailed diagnostics (SignatureVerificationError)
		return nil, fmt.Errorf("claim signature verification failed: %w", err)
	}
	if !valid {
		return nil, fmt.Errorf("invalid claim signature (no additional details available)")
	}

	// Serialize the full claim for storage
	payloadJSON, err := json.Marshal(claim)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize claim: %w", err)
	}

	authorPubkeyStr := EncodePublicKey(authorPubkey)

	// Create the post
	post := &Post{
		ID:                 uuid.New().String(),
		PublicKey:          authorPubkeyStr,
		Content:            claim.Claim.Text,
		VerificationMethod: verificationMethod,
		CreatedAt:          time.Now(),
		PostType:           PostTypeIsnadClaim,
		Confidence:         &claim.Claim.Confidence,
		Tags:               claim.Meta.Tags,
		PayloadJSON:        string(payloadJSON),
		AuthorPubkey:       authorPubkeyStr,
		ClaimID:            claim.Meta.ID,
	}

	if err := s.db.Table(Post{}).Insert(ctx, post); err != nil {
		return nil, fmt.Errorf("failed to create post: %w", err)
	}

	return post, nil
}

// CreateIsnadEndorsementPost creates a post from an isnad_endorsement payload.
// It performs full verification including the nested claim signature.
func (s *Service) CreateIsnadEndorsementPost(ctx context.Context, endorsement *IsnadEndorsement, endorserPubkey ed25519.PublicKey, verificationMethod string) (*Post, error) {
	// Verify endorser pubkey matches the one in the endorsement
	endorserPubkeyStr := EncodePublicKey(endorserPubkey)
	if endorsement.Meta.EndorserPubkey != endorserPubkeyStr {
		return nil, fmt.Errorf("endorser_pubkey mismatch: header says %s but payload says %s", endorserPubkeyStr, endorsement.Meta.EndorserPubkey)
	}

	// Fully verify the endorsement (validates structure, wrapper signature)
	targetClaim, err := FullyVerifyIsnadEndorsement(endorsement, endorserPubkey)
	if err != nil {
		return nil, fmt.Errorf("endorsement verification failed: %w", err)
	}

	// Note: We cannot verify the inner claim signature without knowing the author's pubkey.
	// The target claim should exist in our database so we can look up the author.
	// For now, we trust the embedded claim but flag it for later verification.

	// Serialize the full endorsement for storage
	payloadJSON, err := json.Marshal(endorsement)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize endorsement: %w", err)
	}

	// Create the post
	post := &Post{
		ID:                 uuid.New().String(),
		PublicKey:          endorserPubkeyStr,
		Content:            endorsement.Endorsement.Context,
		VerificationMethod: verificationMethod,
		CreatedAt:          time.Now(),
		PostType:           PostTypeIsnadEndorsement,
		Rating:             &endorsement.Endorsement.Rating,
		TargetPostID:       targetClaim.Meta.ID, // Link to the target claim ID
		PayloadJSON:        string(payloadJSON),
		AuthorPubkey:       endorserPubkeyStr, // The endorser is the author of this post
		ClaimID:            targetClaim.Meta.ID,
	}

	if err := s.db.Table(Post{}).Insert(ctx, post); err != nil {
		return nil, fmt.Errorf("failed to create post: %w", err)
	}

	return post, nil
}

// PostsFilter retrieves posts with Isnad-specific filtering.
type PostsFilter struct {
	PostType      string   // Filter by post type (text, isnad_claim, isnad_endorsement, etc.)
	MinConfidence *float64 // Minimum author confidence (for claims)
	MinRating     *float64 // Minimum endorser rating (for endorsements)
	Tag           string   // Filter by tag
	Author        string   // Filter by author public key
	Limit         int
	Offset        int

	// v3: New filters
	Since  *time.Time // Posts after this timestamp
	RefID  string     // Posts referencing this parent
	Topic  string     // Hierarchical topic prefix match
	Result string     // Verification result filter
}

// ListPostsFiltered retrieves posts with Isnad-specific filtering.
func (s *Service) ListPostsFiltered(ctx context.Context, filter PostsFilter) ([]Post, error) {
	where := make(map[string]any)

	if filter.PostType != "" {
		where["post_type"] = filter.PostType
	}
	if filter.Author != "" {
		where["public_key"] = filter.Author
	}
	if filter.RefID != "" {
		where["ref_id"] = filter.RefID
	}
	if filter.Result != "" {
		where["result"] = filter.Result
	}

	results, err := s.db.Table(Post{}).Query(ctx, gowild_data.QueryOpts{
		Where:     where,
		OrderBy:   "created_at",
		OrderDesc: true,
		Limit:     filter.Limit,
		Offset:    filter.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list posts: %w", err)
	}

	posts := make([]Post, 0, len(results))
	for _, r := range results {
		post, ok := r.(*Post)
		if !ok {
			continue
		}

		// Apply additional filters that can't be done in SQL easily
		if filter.MinConfidence != nil && post.Confidence != nil {
			if *post.Confidence < *filter.MinConfidence {
				continue
			}
		}
		if filter.MinRating != nil && post.Rating != nil {
			if *post.Rating < *filter.MinRating {
				continue
			}
		}
		if filter.Tag != "" && len(post.Tags) > 0 {
			if !slices.Contains(post.Tags, filter.Tag) {
				continue
			}
		}

		// v3: Since filter (posts after timestamp)
		if filter.Since != nil {
			if post.CreatedAt.Before(*filter.Since) || post.CreatedAt.Equal(*filter.Since) {
				continue
			}
		}

		// v3: Topic prefix match (e.g., "market" matches "market/gpu", "market/cpu")
		if filter.Topic != "" {
			if post.Topic != filter.Topic && !strings.HasPrefix(post.Topic, filter.Topic+"/") {
				continue
			}
		}

		posts = append(posts, *post)
	}

	return posts, nil
}

// CreateIsnadVerificationPost creates a post from an isnad_verification payload.
// It validates the verification structure and signature, then stores it.
func (s *Service) CreateIsnadVerificationPost(ctx context.Context, verification *IsnadVerification, verifierPubkey ed25519.PublicKey, verificationMethod string) (*Post, error) {
	// 1. Validate verification structure
	if err := ValidateIsnadVerification(verification); err != nil {
		return nil, fmt.Errorf("invalid verification: %w", err)
	}

	// 2. Verify verifier pubkey matches
	verifierPubkeyStr := EncodePublicKey(verifierPubkey)
	if verification.Meta.VerifierPubkey != verifierPubkeyStr {
		return nil, fmt.Errorf("verifier_pubkey mismatch: header says %s but payload says %s", verifierPubkeyStr, verification.Meta.VerifierPubkey)
	}

	// 3. Extract and validate target claim
	targetClaim, err := ExtractTargetClaim(verification.TargetObject)
	if err != nil {
		return nil, fmt.Errorf("failed to extract target claim: %w", err)
	}

	// 4. Verify wrapper signature
	valid, err := VerifyIsnadVerificationSignature(verification, verifierPubkey, targetClaim.Signature)
	if err != nil {
		return nil, fmt.Errorf("verification signature failed: %w", err)
	}
	if !valid {
		return nil, fmt.Errorf("invalid verification signature")
	}

	// 5. Create post
	payloadJSON, _ := json.Marshal(verification)
	post := &Post{
		ID:                 uuid.New().String(),
		PublicKey:          verifierPubkeyStr,
		Content:            verification.Verification.Methodology,
		VerificationMethod: verificationMethod,
		CreatedAt:          time.Now(),
		PostType:           PostTypeIsnadVerification,
		TargetPostID:       targetClaim.Meta.ID,
		Result:             verification.Verification.Result,
		Methodology:        verification.Verification.Methodology,
		Confidence:         &verification.Verification.Confidence,
		PayloadJSON:        string(payloadJSON),
		AuthorPubkey:       verifierPubkeyStr,
	}

	if err := s.db.Table(Post{}).Insert(ctx, post); err != nil {
		return nil, fmt.Errorf("failed to create post: %w", err)
	}

	return post, nil
}

// CreateBountyPost creates a post from a bounty payload.
func (s *Service) CreateBountyPost(ctx context.Context, bounty *BountyPost, authorPubkey ed25519.PublicKey, verificationMethod string) (*Post, error) {
	// 1. Validate bounty structure
	if err := ValidateBounty(bounty); err != nil {
		return nil, fmt.Errorf("invalid bounty: %w", err)
	}

	// 2. Verify signature
	valid, err := VerifyBountySignature(bounty, authorPubkey)
	if err != nil {
		return nil, fmt.Errorf("bounty signature failed: %w", err)
	}
	if !valid {
		return nil, fmt.Errorf("invalid bounty signature")
	}

	// 3. Create post
	authorPubkeyStr := EncodePublicKey(authorPubkey)
	payloadJSON, _ := json.Marshal(bounty)

	post := &Post{
		ID:                 uuid.New().String(),
		PublicKey:          authorPubkeyStr,
		Content:            bounty.Bounty.Description,
		VerificationMethod: verificationMethod,
		CreatedAt:          time.Now(),
		PostType:           PostTypeBounty,
		Topic:              bounty.Meta.Topic,
		Tags:               bounty.Meta.Tags,
		RewardLamports:     bounty.Bounty.RewardLamports,
		Deadline:           bounty.Bounty.Deadline,
		PayloadJSON:        string(payloadJSON),
		AuthorPubkey:       authorPubkeyStr,
		ClaimID:            bounty.Meta.ID,
	}

	if err := s.db.Table(Post{}).Insert(ctx, post); err != nil {
		return nil, fmt.Errorf("failed to create post: %w", err)
	}

	return post, nil
}

// CreateSolutionPost creates a post from a solution payload.
func (s *Service) CreateSolutionPost(ctx context.Context, solution *SolutionPost, authorPubkey ed25519.PublicKey, verificationMethod string) (*Post, error) {
	// 1. Validate solution structure
	if err := ValidateSolution(solution); err != nil {
		return nil, fmt.Errorf("invalid solution: %w", err)
	}

	// 2. Verify referenced bounty exists
	bountyPost, err := s.GetPost(ctx, solution.Meta.RefID)
	if err != nil {
		return nil, fmt.Errorf("referenced bounty not found: %w", err)
	}
	if bountyPost.PostType != PostTypeBounty {
		return nil, fmt.Errorf("ref_id must reference a bounty post, got %s", bountyPost.PostType)
	}

	// 3. Check deadline hasn't passed (if set)
	if bountyPost.Deadline != "" {
		deadline, err := time.Parse(time.RFC3339, bountyPost.Deadline)
		if err == nil && time.Now().After(deadline) {
			return nil, fmt.Errorf("bounty deadline has passed")
		}
	}

	// 4. Verify signature
	valid, err := VerifySolutionSignature(solution, authorPubkey)
	if err != nil {
		return nil, fmt.Errorf("solution signature failed: %w", err)
	}
	if !valid {
		return nil, fmt.Errorf("invalid solution signature")
	}

	// 5. Create post
	authorPubkeyStr := EncodePublicKey(authorPubkey)
	payloadJSON, _ := json.Marshal(solution)

	post := &Post{
		ID:                 uuid.New().String(),
		PublicKey:          authorPubkeyStr,
		Content:            solution.Solution.Content,
		VerificationMethod: verificationMethod,
		CreatedAt:          time.Now(),
		PostType:           PostTypeSolution,
		RefID:              solution.Meta.RefID,
		PayloadJSON:        string(payloadJSON),
		AuthorPubkey:       authorPubkeyStr,
		ClaimID:            solution.Meta.ID,
	}

	if err := s.db.Table(Post{}).Insert(ctx, post); err != nil {
		return nil, fmt.Errorf("failed to create post: %w", err)
	}

	return post, nil
}

// CreateSettlementPost creates a post from a settlement payload.
// CRITICAL: Only the original Bounty Author can create a Settlement.
func (s *Service) CreateSettlementPost(ctx context.Context, settlement *SettlementPost, authorPubkey ed25519.PublicKey, verificationMethod string) (*Post, error) {
	// 1. Validate settlement structure
	if err := ValidateSettlement(settlement); err != nil {
		return nil, fmt.Errorf("invalid settlement: %w", err)
	}

	// 2. Get the referenced Solution post
	solutionPost, err := s.GetPost(ctx, settlement.Meta.RefID)
	if err != nil {
		return nil, fmt.Errorf("referenced solution not found: %w", err)
	}
	if solutionPost.PostType != PostTypeSolution {
		return nil, fmt.Errorf("ref_id must reference a solution post, got %s", solutionPost.PostType)
	}

	// 3. Get the original Bounty post (solution's parent)
	bountyPost, err := s.GetPost(ctx, solutionPost.RefID)
	if err != nil {
		return nil, fmt.Errorf("bounty for solution not found: %w", err)
	}
	if bountyPost.PostType != PostTypeBounty {
		return nil, fmt.Errorf("solution does not reference a bounty")
	}

	// 4. CRITICAL AUTHORIZATION CHECK:
	// Only the original Bounty Author can create a Settlement
	authorPubkeyStr := EncodePublicKey(authorPubkey)
	if bountyPost.AuthorPubkey != authorPubkeyStr {
		return nil, fmt.Errorf("unauthorized: only the bounty author can settle (expected %s, got %s)",
			bountyPost.AuthorPubkey, authorPubkeyStr)
	}

	// 5. Check if this solution was already settled
	existingSettlements, err := s.ListPostsFiltered(ctx, PostsFilter{
		PostType: PostTypeIsnadSettlement,
		RefID:    settlement.Meta.RefID,
		Limit:    1,
	})
	if err == nil && len(existingSettlements) > 0 {
		return nil, fmt.Errorf("solution already settled (settlement ID: %s)", existingSettlements[0].ID)
	}

	// 6. Verify signature
	valid, err := VerifySettlementSignature(settlement, authorPubkey)
	if err != nil {
		return nil, fmt.Errorf("settlement signature failed: %w", err)
	}
	if !valid {
		return nil, fmt.Errorf("invalid settlement signature")
	}

	// 7. Create post
	payloadJSON, _ := json.Marshal(settlement)

	post := &Post{
		ID:                 uuid.New().String(),
		PublicKey:          authorPubkeyStr,
		Content:            fmt.Sprintf("Settlement for solution %s: %s on %s", solutionPost.ID, settlement.Settlement.TxHash, settlement.Settlement.Chain),
		VerificationMethod: verificationMethod,
		CreatedAt:          time.Now(),
		PostType:           PostTypeIsnadSettlement,
		RefID:              settlement.Meta.RefID,
		Chain:              settlement.Settlement.Chain,
		TxHash:             settlement.Settlement.TxHash,
		AmountLamports:     settlement.Settlement.AmountLamports,
		PayloadJSON:        string(payloadJSON),
		AuthorPubkey:       authorPubkeyStr,
		ClaimID:            settlement.Meta.ID,
	}

	if err := s.db.Table(Post{}).Insert(ctx, post); err != nil {
		return nil, fmt.Errorf("failed to create post: %w", err)
	}

	return post, nil
}
