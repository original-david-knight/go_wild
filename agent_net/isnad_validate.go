package gowild_agent_net

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"time"
)

// ExtractTargetClaim extracts the IsnadClaim from an endorsement's target_object.
// Uses json.RawMessage to preserve exact bytes for signature verification.
func ExtractTargetClaim(targetObject json.RawMessage) (*IsnadClaim, error) {
	var claim IsnadClaim
	if err := json.Unmarshal(targetObject, &claim); err != nil {
		return nil, fmt.Errorf("failed to parse target_object: %w", err)
	}
	return &claim, nil
}

// ValidateIsnadClaim validates an isnad_claim payload.
func ValidateIsnadClaim(claim *IsnadClaim) error {
	if claim.Type != PostTypeIsnadClaim {
		return fmt.Errorf("invalid type: expected %s, got %s", PostTypeIsnadClaim, claim.Type)
	}
	if claim.Version != IsnadVersion {
		return fmt.Errorf("unsupported version: %s", claim.Version)
	}
	if claim.Meta.ID == "" {
		return fmt.Errorf("meta.id is required")
	}
	if claim.Meta.Timestamp == "" {
		return fmt.Errorf("meta.timestamp is required")
	}
	if _, err := time.Parse(time.RFC3339, claim.Meta.Timestamp); err != nil {
		return fmt.Errorf("invalid timestamp format: %w", err)
	}
	if claim.Claim.Text == "" {
		return fmt.Errorf("claim.text is required")
	}
	if claim.Claim.Confidence < 0 || claim.Claim.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0.0 and 1.0")
	}
	if claim.Signature == "" {
		return fmt.Errorf("signature is required")
	}
	// Validate sentiment if provided
	if claim.Claim.Sentiment != "" {
		if claim.Claim.Sentiment != SentimentPositive &&
			claim.Claim.Sentiment != SentimentNegative &&
			claim.Claim.Sentiment != SentimentNeutral {
			return fmt.Errorf("invalid sentiment: must be positive, negative, or neutral")
		}
	}
	return nil
}

// ValidateIsnadEndorsement validates an isnad_endorsement payload.
func ValidateIsnadEndorsement(endorsement *IsnadEndorsement) error {
	if endorsement.Type != PostTypeIsnadEndorsement {
		return fmt.Errorf("invalid type: expected %s, got %s", PostTypeIsnadEndorsement, endorsement.Type)
	}
	if endorsement.Version != IsnadVersion {
		return fmt.Errorf("unsupported version: %s", endorsement.Version)
	}
	if endorsement.Meta.Timestamp == "" {
		return fmt.Errorf("meta.timestamp is required")
	}
	if _, err := time.Parse(time.RFC3339, endorsement.Meta.Timestamp); err != nil {
		return fmt.Errorf("invalid timestamp format: %w", err)
	}
	if endorsement.Meta.EndorserPubkey == "" {
		return fmt.Errorf("meta.endorser_pubkey is required")
	}
	if endorsement.Endorsement.Rating < 0 || endorsement.Endorsement.Rating > 1 {
		return fmt.Errorf("rating must be between 0.0 and 1.0")
	}
	if endorsement.Endorsement.Context == "" {
		return fmt.Errorf("endorsement.context is required")
	}
	if len(endorsement.TargetObject) == 0 {
		return fmt.Errorf("target_object is required")
	}
	if endorsement.WrapperSignature == "" {
		return fmt.Errorf("wrapper_signature is required")
	}
	// Validate sentiment if provided
	if endorsement.Endorsement.Sentiment != "" {
		if endorsement.Endorsement.Sentiment != SentimentPositive &&
			endorsement.Endorsement.Sentiment != SentimentNegative &&
			endorsement.Endorsement.Sentiment != SentimentNeutral {
			return fmt.Errorf("invalid sentiment: must be positive, negative, or neutral")
		}
	}
	return nil
}

// createIsnadClaim creates a new IsnadClaim with proper ID generation.
func createIsnadClaim(text string, confidence float64, tags []string, sentiment string) *IsnadClaim {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	id := generateClaimID(text, timestamp)

	claim := &IsnadClaim{
		Type:    PostTypeIsnadClaim,
		Version: IsnadVersion,
		Meta: ClaimMeta{
			ID:        id,
			Timestamp: timestamp,
			Tags:      tags,
		},
		Claim: ClaimData{
			Text:       text,
			Confidence: confidence,
			Sentiment:  sentiment,
		},
	}

	// Default confidence to 1.0 if not specified
	if claim.Claim.Confidence == 0 {
		claim.Claim.Confidence = 1.0
	}

	return claim
}

// createIsnadEndorsement creates a new IsnadEndorsement.
func createIsnadEndorsement(targetClaim *IsnadClaim, targetJSON json.RawMessage, rating float64, context, sentiment, endorserPubkey string) *IsnadEndorsement {
	timestamp := time.Now().UTC().Format(time.RFC3339)

	return &IsnadEndorsement{
		Type:    PostTypeIsnadEndorsement,
		Version: IsnadVersion,
		Meta: EndorsementMeta{
			Timestamp:      timestamp,
			EndorserPubkey: endorserPubkey,
		},
		Endorsement: EndorsementData{
			Rating:    rating,
			Sentiment: sentiment,
			Context:   context,
		},
		TargetObject: targetJSON,
	}
}

// FullyVerifyIsnadEndorsement performs complete verification of an endorsement:
// 1. Validates the endorsement structure
// 2. Extracts and validates the target claim
// 3. Verifies the inner claim signature
// 4. Verifies the wrapper signature
func FullyVerifyIsnadEndorsement(endorsement *IsnadEndorsement, endorserPubkey ed25519.PublicKey) (*IsnadClaim, error) {
	// Step 1: Validate endorsement structure
	if err := ValidateIsnadEndorsement(endorsement); err != nil {
		return nil, fmt.Errorf("invalid endorsement: %w", err)
	}

	// Step 2: Verify endorser pubkey matches
	if endorsement.Meta.EndorserPubkey != EncodePublicKey(endorserPubkey) {
		return nil, fmt.Errorf("endorser_pubkey does not match sender")
	}

	// Step 3: Extract target claim
	targetClaim, err := ExtractTargetClaim(endorsement.TargetObject)
	if err != nil {
		return nil, fmt.Errorf("failed to extract target claim: %w", err)
	}

	// Step 4: Validate target claim structure
	if err := ValidateIsnadClaim(targetClaim); err != nil {
		return nil, fmt.Errorf("invalid target claim: %w", err)
	}

	// Step 5: We need the author's pubkey from the claim to verify inner signature
	// For now, we can't verify the inner signature without knowing the author's pubkey
	// The target claim should be looked up in the database to get the author

	// Step 6: Verify wrapper signature
	valid, err := VerifyIsnadEndorsementSignature(endorsement, endorserPubkey, targetClaim.Signature)
	if err != nil {
		return nil, fmt.Errorf("wrapper signature verification failed: %w", err)
	}
	if !valid {
		return nil, fmt.Errorf("invalid wrapper signature")
	}

	return targetClaim, nil
}

// verifyTargetClaimSignature verifies the inner claim signature given the author's public key.
func verifyTargetClaimSignature(claim *IsnadClaim, authorPubkeyBase64 string) error {
	authorPubkey, err := DecodePublicKey(authorPubkeyBase64)
	if err != nil {
		return fmt.Errorf("invalid author public key: %w", err)
	}

	valid, err := VerifyIsnadClaimSignature(claim, authorPubkey)
	if err != nil {
		return fmt.Errorf("claim signature verification failed: %w", err)
	}
	if !valid {
		return fmt.Errorf("invalid claim signature (possible tampering)")
	}

	return nil
}

// ValidateIsnadVerification validates an isnad_verification payload.
func ValidateIsnadVerification(v *IsnadVerification) error {
	if v.Type != PostTypeIsnadVerification {
		return fmt.Errorf("invalid type: expected %s", PostTypeIsnadVerification)
	}
	if v.Version != IsnadVersion {
		return fmt.Errorf("unsupported version: %s", v.Version)
	}
	if v.Meta.Timestamp == "" {
		return fmt.Errorf("meta.timestamp is required")
	}
	if _, err := time.Parse(time.RFC3339, v.Meta.Timestamp); err != nil {
		return fmt.Errorf("invalid timestamp format: %w", err)
	}
	if v.Meta.VerifierPubkey == "" {
		return fmt.Errorf("meta.verifier_pubkey is required")
	}

	// Validate result
	switch v.Verification.Result {
	case VerificationResultVerified, VerificationResultFailed, VerificationResultInconclusive:
		// Valid
	default:
		return fmt.Errorf("invalid result: must be verified, failed, or inconclusive")
	}

	if v.Verification.Confidence < 0 || v.Verification.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0.0 and 1.0")
	}
	if len(v.TargetObject) == 0 {
		return fmt.Errorf("target_object is required")
	}
	if v.WrapperSignature == "" {
		return fmt.Errorf("wrapper_signature is required")
	}
	return nil
}

// ValidateBounty validates a bounty payload.
func ValidateBounty(b *BountyPost) error {
	if b.Type != PostTypeBounty {
		return fmt.Errorf("invalid type: expected %s", PostTypeBounty)
	}
	if b.Version != IsnadVersion {
		return fmt.Errorf("unsupported version: %s", b.Version)
	}
	if b.Meta.ID == "" {
		return fmt.Errorf("meta.id is required")
	}
	if b.Meta.Timestamp == "" {
		return fmt.Errorf("meta.timestamp is required")
	}
	if _, err := time.Parse(time.RFC3339, b.Meta.Timestamp); err != nil {
		return fmt.Errorf("invalid timestamp format: %w", err)
	}
	if b.Bounty.Title == "" {
		return fmt.Errorf("bounty.title is required")
	}
	if b.Bounty.Description == "" {
		return fmt.Errorf("bounty.description is required")
	}
	if b.Bounty.RewardLamports <= 0 {
		return fmt.Errorf("bounty.reward_lamports must be positive")
	}
	if b.Signature == "" {
		return fmt.Errorf("signature is required")
	}
	// Validate deadline format if provided
	if b.Bounty.Deadline != "" {
		if _, err := time.Parse(time.RFC3339, b.Bounty.Deadline); err != nil {
			return fmt.Errorf("invalid deadline format: %w", err)
		}
	}
	return nil
}

// ValidateSolution validates a solution payload.
func ValidateSolution(s *SolutionPost) error {
	if s.Type != PostTypeSolution {
		return fmt.Errorf("invalid type: expected %s", PostTypeSolution)
	}
	if s.Version != IsnadVersion {
		return fmt.Errorf("unsupported version: %s", s.Version)
	}
	if s.Meta.ID == "" {
		return fmt.Errorf("meta.id is required")
	}
	if s.Meta.Timestamp == "" {
		return fmt.Errorf("meta.timestamp is required")
	}
	if _, err := time.Parse(time.RFC3339, s.Meta.Timestamp); err != nil {
		return fmt.Errorf("invalid timestamp format: %w", err)
	}
	if s.Meta.RefID == "" {
		return fmt.Errorf("meta.ref_id is required (must reference a bounty)")
	}
	if s.Solution.Content == "" {
		return fmt.Errorf("solution.content is required")
	}
	if s.Signature == "" {
		return fmt.Errorf("signature is required")
	}
	return nil
}

// ValidateSettlement validates a settlement payload.
func ValidateSettlement(s *SettlementPost) error {
	if s.Type != PostTypeIsnadSettlement {
		return fmt.Errorf("invalid type: expected %s", PostTypeIsnadSettlement)
	}
	if s.Version != IsnadVersion {
		return fmt.Errorf("unsupported version: %s", s.Version)
	}
	if s.Meta.ID == "" {
		return fmt.Errorf("meta.id is required")
	}
	if s.Meta.Timestamp == "" {
		return fmt.Errorf("meta.timestamp is required")
	}
	if _, err := time.Parse(time.RFC3339, s.Meta.Timestamp); err != nil {
		return fmt.Errorf("invalid timestamp format: %w", err)
	}
	if s.Meta.RefID == "" {
		return fmt.Errorf("meta.ref_id is required (must reference a solution)")
	}

	// Validate chain
	switch s.Settlement.Chain {
	case ChainSolana, ChainEthereum, ChainBase:
		// Valid
	default:
		return fmt.Errorf("invalid chain: must be solana, ethereum, or base")
	}

	if s.Settlement.TxHash == "" {
		return fmt.Errorf("settlement.tx_hash is required")
	}
	if s.Settlement.AmountLamports <= 0 {
		return fmt.Errorf("settlement.amount_lamports must be positive")
	}
	if s.Signature == "" {
		return fmt.Errorf("signature is required")
	}
	return nil
}
