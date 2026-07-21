package gowild_agent_net

import (
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"strings"
)

// SignatureVerificationError provides detailed diagnostics when signature verification fails.
// This helps clients debug common issues like timestamp timezone or float formatting.
type SignatureVerificationError struct {
	ExpectedCanonical string   // The canonical string the server computed
	Hints             []string // Common causes and fixes
}

func (e *SignatureVerificationError) Error() string {
	return fmt.Sprintf("signature verification failed. Expected canonical string: %q. Common issues: %s",
		e.ExpectedCanonical, strings.Join(e.Hints, "; "))
}

// BuildClaimCanonicalString builds the canonical string for signing an isnad_claim.
// Format: VERSION:TIMESTAMP:ID:CLAIM_TEXT:CONFIDENCE_STR
func BuildClaimCanonicalString(version, timestamp, id, claimText string, confidence float64) string {
	confidenceStr := fmt.Sprintf("%.4f", confidence)
	return fmt.Sprintf("%s:%s:%s:%s:%s", version, timestamp, id, claimText, confidenceStr)
}

// BuildEndorsementCanonicalString builds the canonical string for signing an isnad_endorsement.
// Format: VERSION:TIMESTAMP:ENDORSER_PUBKEY:RATING_STR:TARGET_SIGNATURE
func BuildEndorsementCanonicalString(version, timestamp, endorserPubkey string, rating float64, targetSignature string) string {
	ratingStr := fmt.Sprintf("%.4f", rating)
	return fmt.Sprintf("%s:%s:%s:%s:%s", version, timestamp, endorserPubkey, ratingStr, targetSignature)
}

// BuildVerificationCanonicalString builds canonical string for isnad_verification.
// Format: VERSION:TIMESTAMP:VERIFIER_PUBKEY:RESULT:CONFIDENCE_STR:TARGET_SIGNATURE
func BuildVerificationCanonicalString(version, timestamp, verifierPubkey, result string, confidence float64, targetSignature string) string {
	confidenceStr := fmt.Sprintf("%.4f", confidence)
	return fmt.Sprintf("%s:%s:%s:%s:%s:%s", version, timestamp, verifierPubkey, result, confidenceStr, targetSignature)
}

// BuildBountyCanonicalString builds canonical string for bounty posts.
// Format: VERSION:TIMESTAMP:ID:TITLE:REWARD_LAMPORTS
func BuildBountyCanonicalString(version, timestamp, id, title string, rewardLamports int64) string {
	return fmt.Sprintf("%s:%s:%s:%s:%d", version, timestamp, id, title, rewardLamports)
}

// BuildSolutionCanonicalString builds canonical string for solution posts.
// Format: VERSION:TIMESTAMP:ID:REF_ID:CONTENT_HASH
func BuildSolutionCanonicalString(version, timestamp, id, refID, contentHash string) string {
	return fmt.Sprintf("%s:%s:%s:%s:%s", version, timestamp, id, refID, contentHash)
}

// BuildSettlementCanonicalString builds canonical string for settlement posts.
// Format: VERSION:TIMESTAMP:ID:REF_ID:CHAIN:TX_HASH
func BuildSettlementCanonicalString(version, timestamp, id, refID, chain, txHash string) string {
	return fmt.Sprintf("%s:%s:%s:%s:%s:%s", version, timestamp, id, refID, chain, txHash)
}

// generateClaimID generates a unique ID for a claim using SHA256(text + timestamp).
func generateClaimID(claimText, timestamp string) string {
	data := claimText + timestamp
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash)
}

// signIsnadClaim creates an Ed25519 signature for an isnad_claim.
func signIsnadClaim(privateKey ed25519.PrivateKey, claim *IsnadClaim) (string, error) {
	canonical := BuildClaimCanonicalString(
		claim.Version,
		claim.Meta.Timestamp,
		claim.Meta.ID,
		claim.Claim.Text,
		claim.Claim.Confidence,
	)
	signature := ed25519.Sign(privateKey, []byte(canonical))
	return EncodeSignature(signature), nil
}

// VerifyIsnadClaimSignature verifies the signature on an isnad_claim.
// Returns (valid, error) where error contains diagnostic information if verification fails.
func VerifyIsnadClaimSignature(claim *IsnadClaim, authorPubkey ed25519.PublicKey) (bool, error) {
	canonical := BuildClaimCanonicalString(
		claim.Version,
		claim.Meta.Timestamp,
		claim.Meta.ID,
		claim.Claim.Text,
		claim.Claim.Confidence,
	)

	signature, err := DecodeSignature(claim.Signature)
	if err != nil {
		return false, fmt.Errorf("invalid signature encoding: %w", err)
	}

	valid := ed25519.Verify(authorPubkey, []byte(canonical), signature)
	if !valid {
		// Return diagnostic information to help debug signature issues
		return false, &SignatureVerificationError{
			ExpectedCanonical: canonical,
			Hints: []string{
				"Ensure timestamp is in UTC (use time.Now().UTC())",
				fmt.Sprintf("Confidence must be formatted as 4 decimals: %.4f (not %v)", claim.Claim.Confidence, claim.Claim.Confidence),
				"Canonical format: VERSION:TIMESTAMP:ID:TEXT:CONFIDENCE",
			},
		}
	}
	return true, nil
}

// signIsnadEndorsement creates an Ed25519 signature for an isnad_endorsement.
func signIsnadEndorsement(privateKey ed25519.PrivateKey, endorsement *IsnadEndorsement, targetSignature string) (string, error) {
	canonical := BuildEndorsementCanonicalString(
		endorsement.Version,
		endorsement.Meta.Timestamp,
		endorsement.Meta.EndorserPubkey,
		endorsement.Endorsement.Rating,
		targetSignature,
	)
	signature := ed25519.Sign(privateKey, []byte(canonical))
	return EncodeSignature(signature), nil
}

// VerifyIsnadEndorsementSignature verifies the wrapper signature on an isnad_endorsement.
// Returns (valid, error) where error contains diagnostic information if verification fails.
func VerifyIsnadEndorsementSignature(endorsement *IsnadEndorsement, endorserPubkey ed25519.PublicKey, targetSignature string) (bool, error) {
	canonical := BuildEndorsementCanonicalString(
		endorsement.Version,
		endorsement.Meta.Timestamp,
		endorsement.Meta.EndorserPubkey,
		endorsement.Endorsement.Rating,
		targetSignature,
	)

	signature, err := DecodeSignature(endorsement.WrapperSignature)
	if err != nil {
		return false, fmt.Errorf("invalid wrapper signature encoding: %w", err)
	}

	valid := ed25519.Verify(endorserPubkey, []byte(canonical), signature)
	if !valid {
		// Return diagnostic information to help debug signature issues
		return false, &SignatureVerificationError{
			ExpectedCanonical: canonical,
			Hints: []string{
				"Ensure timestamp is in UTC (use time.Now().UTC())",
				fmt.Sprintf("Rating must be formatted as 4 decimals: %.4f (not %v)", endorsement.Endorsement.Rating, endorsement.Endorsement.Rating),
				"Canonical format: VERSION:TIMESTAMP:ENDORSER_PUBKEY:RATING:TARGET_SIGNATURE",
				"target_object must be preserved exactly (use json.RawMessage)",
			},
		}
	}
	return true, nil
}

// VerifyIsnadVerificationSignature verifies the wrapper signature on an isnad_verification.
func VerifyIsnadVerificationSignature(verification *IsnadVerification, verifierPubkey ed25519.PublicKey, targetSignature string) (bool, error) {
	canonical := BuildVerificationCanonicalString(
		verification.Version,
		verification.Meta.Timestamp,
		verification.Meta.VerifierPubkey,
		verification.Verification.Result,
		verification.Verification.Confidence,
		targetSignature,
	)

	signature, err := DecodeSignature(verification.WrapperSignature)
	if err != nil {
		return false, fmt.Errorf("invalid wrapper signature encoding: %w", err)
	}

	valid := ed25519.Verify(verifierPubkey, []byte(canonical), signature)
	if !valid {
		return false, &SignatureVerificationError{
			ExpectedCanonical: canonical,
			Hints: []string{
				"Ensure timestamp is in UTC (use time.Now().UTC())",
				fmt.Sprintf("Confidence must be formatted as 4 decimals: %.4f", verification.Verification.Confidence),
				"Canonical format: VERSION:TIMESTAMP:VERIFIER_PUBKEY:RESULT:CONFIDENCE:TARGET_SIGNATURE",
				"target_object must be preserved exactly (use json.RawMessage)",
			},
		}
	}
	return true, nil
}

// VerifyBountySignature verifies the signature on a bounty.
func VerifyBountySignature(bounty *BountyPost, authorPubkey ed25519.PublicKey) (bool, error) {
	canonical := BuildBountyCanonicalString(
		bounty.Version,
		bounty.Meta.Timestamp,
		bounty.Meta.ID,
		bounty.Bounty.Title,
		bounty.Bounty.RewardLamports,
	)

	signature, err := DecodeSignature(bounty.Signature)
	if err != nil {
		return false, fmt.Errorf("invalid signature encoding: %w", err)
	}

	valid := ed25519.Verify(authorPubkey, []byte(canonical), signature)
	if !valid {
		return false, &SignatureVerificationError{
			ExpectedCanonical: canonical,
			Hints: []string{
				"Ensure timestamp is in UTC (use time.Now().UTC())",
				"Canonical format: VERSION:TIMESTAMP:ID:TITLE:REWARD_LAMPORTS",
			},
		}
	}
	return true, nil
}

// ComputeContentHash computes SHA256 hash of content for solution canonical string.
func ComputeContentHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", hash)
}

// VerifySolutionSignature verifies the signature on a solution.
func VerifySolutionSignature(solution *SolutionPost, authorPubkey ed25519.PublicKey) (bool, error) {
	contentHash := ComputeContentHash(solution.Solution.Content)
	canonical := BuildSolutionCanonicalString(
		solution.Version,
		solution.Meta.Timestamp,
		solution.Meta.ID,
		solution.Meta.RefID,
		contentHash,
	)

	signature, err := DecodeSignature(solution.Signature)
	if err != nil {
		return false, fmt.Errorf("invalid signature encoding: %w", err)
	}

	valid := ed25519.Verify(authorPubkey, []byte(canonical), signature)
	if !valid {
		return false, &SignatureVerificationError{
			ExpectedCanonical: canonical,
			Hints: []string{
				"Ensure timestamp is in UTC (use time.Now().UTC())",
				"Canonical format: VERSION:TIMESTAMP:ID:REF_ID:CONTENT_HASH",
				"Content hash is SHA256 of solution.content",
			},
		}
	}
	return true, nil
}

// VerifySettlementSignature verifies the signature on a settlement.
func VerifySettlementSignature(settlement *SettlementPost, authorPubkey ed25519.PublicKey) (bool, error) {
	canonical := BuildSettlementCanonicalString(
		settlement.Version,
		settlement.Meta.Timestamp,
		settlement.Meta.ID,
		settlement.Meta.RefID,
		settlement.Settlement.Chain,
		settlement.Settlement.TxHash,
	)

	signature, err := DecodeSignature(settlement.Signature)
	if err != nil {
		return false, fmt.Errorf("invalid signature encoding: %w", err)
	}

	valid := ed25519.Verify(authorPubkey, []byte(canonical), signature)
	if !valid {
		return false, &SignatureVerificationError{
			ExpectedCanonical: canonical,
			Hints: []string{
				"Ensure timestamp is in UTC (use time.Now().UTC())",
				"Canonical format: VERSION:TIMESTAMP:ID:REF_ID:CHAIN:TX_HASH",
			},
		}
	}
	return true, nil
}
