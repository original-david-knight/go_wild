package gowild_agent_net

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/data"
)

// TestBuildClaimCanonicalString verifies the canonical string format for claims.
func TestBuildClaimCanonicalString(t *testing.T) {
	canonical := BuildClaimCanonicalString("1.0", "2026-02-02T12:00:00Z", "abc123", "Test claim", 0.95)
	expected := "1.0:2026-02-02T12:00:00Z:abc123:Test claim:0.9500"
	if canonical != expected {
		t.Errorf("Expected %s, got %s", expected, canonical)
	}
}

// TestBuildEndorsementCanonicalString verifies the canonical string format for endorsements.
func TestBuildEndorsementCanonicalString(t *testing.T) {
	canonical := BuildEndorsementCanonicalString("1.0", "2026-02-02T15:30:00Z", "endorser_pubkey", 1.0, "target_sig")
	expected := "1.0:2026-02-02T15:30:00Z:endorser_pubkey:1.0000:target_sig"
	if canonical != expected {
		t.Errorf("Expected %s, got %s", expected, canonical)
	}
}

// TestGenerateClaimID verifies claim ID generation.
func TestGenerateClaimID(t *testing.T) {
	id := generateClaimID("Test claim", "2026-02-02T12:00:00Z")

	// Should be a hex-encoded SHA256 hash (64 chars)
	if len(id) != 64 {
		t.Errorf("Expected 64 char hex string, got %d chars", len(id))
	}

	// Same input should produce same ID
	id2 := generateClaimID("Test claim", "2026-02-02T12:00:00Z")
	if id != id2 {
		t.Error("Same input should produce same ID")
	}

	// Different input should produce different ID
	id3 := generateClaimID("Different claim", "2026-02-02T12:00:00Z")
	if id == id3 {
		t.Error("Different input should produce different ID")
	}
}

// TestCreateIsnadClaim verifies claim creation helper.
func TestCreateIsnadClaim(t *testing.T) {
	claim := createIsnadClaim("Test claim text", 0.9, []string{"test", "tag"}, SentimentPositive)

	if claim.Type != PostTypeIsnadClaim {
		t.Errorf("Expected type %s, got %s", PostTypeIsnadClaim, claim.Type)
	}
	if claim.Version != IsnadVersion {
		t.Errorf("Expected version %s, got %s", IsnadVersion, claim.Version)
	}
	if claim.Claim.Text != "Test claim text" {
		t.Errorf("Expected text 'Test claim text', got %s", claim.Claim.Text)
	}
	if claim.Claim.Confidence != 0.9 {
		t.Errorf("Expected confidence 0.9, got %f", claim.Claim.Confidence)
	}
	if claim.Claim.Sentiment != SentimentPositive {
		t.Errorf("Expected sentiment %s, got %s", SentimentPositive, claim.Claim.Sentiment)
	}
	if len(claim.Meta.Tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(claim.Meta.Tags))
	}
	if claim.Meta.ID == "" {
		t.Error("Expected non-empty ID")
	}
	if claim.Meta.Timestamp == "" {
		t.Error("Expected non-empty timestamp")
	}
}

// TestCreateIsnadClaimDefaultConfidence verifies default confidence is 1.0.
func TestCreateIsnadClaimDefaultConfidence(t *testing.T) {
	claim := createIsnadClaim("Test", 0, nil, "")
	if claim.Claim.Confidence != 1.0 {
		t.Errorf("Expected default confidence 1.0, got %f", claim.Claim.Confidence)
	}
}

// TestSignAndVerifyIsnadClaim tests the full signing and verification flow for claims.
func TestSignAndVerifyIsnadClaim(t *testing.T) {
	// Generate keypair
	pubkey, privkey, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("Failed to generate keypair: %v", err)
	}

	// Create claim
	claim := createIsnadClaim("The speed of light is 299,792,458 m/s", 1.0, []string{"physics"}, SentimentNeutral)

	// Sign the claim
	signature, err := signIsnadClaim(privkey, claim)
	if err != nil {
		t.Fatalf("Failed to sign claim: %v", err)
	}
	claim.Signature = signature

	// Verify the claim
	valid, err := VerifyIsnadClaimSignature(claim, pubkey)
	if err != nil {
		t.Fatalf("Failed to verify claim: %v", err)
	}
	if !valid {
		t.Error("Claim signature should be valid")
	}

	// Verify with wrong key fails
	otherPubkey, _, _ := GenerateKeyPair()
	valid, _ = VerifyIsnadClaimSignature(claim, otherPubkey)
	if valid {
		t.Error("Claim should not verify with wrong key")
	}

	// Verify tampered claim fails
	claim.Claim.Text = "Tampered text"
	valid, _ = VerifyIsnadClaimSignature(claim, pubkey)
	if valid {
		t.Error("Tampered claim should not verify")
	}
}

// TestSignAndVerifyIsnadEndorsement tests the full signing and verification flow for endorsements.
func TestSignAndVerifyIsnadEndorsement(t *testing.T) {
	// Generate keypairs for author and endorser
	authorPubkey, authorPrivkey, _ := GenerateKeyPair()
	endorserPubkey, endorserPrivkey, _ := GenerateKeyPair()

	// Create and sign original claim
	claim := createIsnadClaim("Test claim for endorsement", 0.95, []string{"test"}, "")
	claimSig, _ := signIsnadClaim(authorPrivkey, claim)
	claim.Signature = claimSig

	// Marshal claim to JSON
	claimJSON, _ := json.Marshal(claim)

	// Create endorsement
	endorsement := createIsnadEndorsement(claim, claimJSON, 1.0, "verified_locally", SentimentPositive, EncodePublicKey(endorserPubkey))

	// Sign endorsement
	wrapperSig, err := signIsnadEndorsement(endorserPrivkey, endorsement, claim.Signature)
	if err != nil {
		t.Fatalf("Failed to sign endorsement: %v", err)
	}
	endorsement.WrapperSignature = wrapperSig

	// Verify endorsement wrapper signature
	valid, err := VerifyIsnadEndorsementSignature(endorsement, endorserPubkey, claim.Signature)
	if err != nil {
		t.Fatalf("Failed to verify endorsement: %v", err)
	}
	if !valid {
		t.Error("Endorsement wrapper signature should be valid")
	}

	// Verify with wrong key fails
	valid, _ = VerifyIsnadEndorsementSignature(endorsement, authorPubkey, claim.Signature)
	if valid {
		t.Error("Endorsement should not verify with wrong key")
	}

	// Verify inner claim signature
	err = verifyTargetClaimSignature(claim, EncodePublicKey(authorPubkey))
	if err != nil {
		t.Errorf("Inner claim signature should verify: %v", err)
	}
}

// TestFullyVerifyIsnadEndorsement tests the complete endorsement verification flow.
func TestFullyVerifyIsnadEndorsement(t *testing.T) {
	// Generate keypairs
	_, authorPrivkey, _ := GenerateKeyPair()
	endorserPubkey, endorserPrivkey, _ := GenerateKeyPair()

	// Create and sign original claim
	claim := createIsnadClaim("Test claim", 0.8, nil, "")
	claimSig, _ := signIsnadClaim(authorPrivkey, claim)
	claim.Signature = claimSig

	// Marshal claim to JSON
	claimJSON, _ := json.Marshal(claim)

	// Create and sign endorsement
	endorsement := createIsnadEndorsement(claim, claimJSON, 0.9, "trusted_peer", "", EncodePublicKey(endorserPubkey))
	wrapperSig, _ := signIsnadEndorsement(endorserPrivkey, endorsement, claim.Signature)
	endorsement.WrapperSignature = wrapperSig

	// Full verification
	targetClaim, err := FullyVerifyIsnadEndorsement(endorsement, endorserPubkey)
	if err != nil {
		t.Fatalf("Full verification failed: %v", err)
	}

	if targetClaim.Claim.Text != claim.Claim.Text {
		t.Error("Extracted claim should match original")
	}
}

// TestValidateIsnadClaim tests claim validation.
func TestValidateIsnadClaim(t *testing.T) {
	tests := []struct {
		name    string
		claim   *IsnadClaim
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid claim",
			claim: &IsnadClaim{
				Type:    PostTypeIsnadClaim,
				Version: IsnadVersion,
				Meta: ClaimMeta{
					ID:        "test_id",
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				},
				Claim: ClaimData{
					Text:       "Test claim",
					Confidence: 0.9,
				},
				Signature: "test_signature",
			},
			wantErr: false,
		},
		{
			name: "invalid type",
			claim: &IsnadClaim{
				Type:    "wrong_type",
				Version: IsnadVersion,
				Meta:    ClaimMeta{ID: "id", Timestamp: time.Now().UTC().Format(time.RFC3339)},
				Claim:   ClaimData{Text: "test", Confidence: 0.9},
			},
			wantErr: true,
			errMsg:  "invalid type",
		},
		{
			name: "invalid version",
			claim: &IsnadClaim{
				Type:    PostTypeIsnadClaim,
				Version: "2.0",
				Meta:    ClaimMeta{ID: "id", Timestamp: time.Now().UTC().Format(time.RFC3339)},
				Claim:   ClaimData{Text: "test", Confidence: 0.9},
			},
			wantErr: true,
			errMsg:  "unsupported version",
		},
		{
			name: "missing ID",
			claim: &IsnadClaim{
				Type:    PostTypeIsnadClaim,
				Version: IsnadVersion,
				Meta:    ClaimMeta{Timestamp: time.Now().UTC().Format(time.RFC3339)},
				Claim:   ClaimData{Text: "test", Confidence: 0.9},
			},
			wantErr: true,
			errMsg:  "meta.id is required",
		},
		{
			name: "missing timestamp",
			claim: &IsnadClaim{
				Type:    PostTypeIsnadClaim,
				Version: IsnadVersion,
				Meta:    ClaimMeta{ID: "id"},
				Claim:   ClaimData{Text: "test", Confidence: 0.9},
			},
			wantErr: true,
			errMsg:  "meta.timestamp is required",
		},
		{
			name: "invalid timestamp format",
			claim: &IsnadClaim{
				Type:    PostTypeIsnadClaim,
				Version: IsnadVersion,
				Meta:    ClaimMeta{ID: "id", Timestamp: "not-a-date"},
				Claim:   ClaimData{Text: "test", Confidence: 0.9},
			},
			wantErr: true,
			errMsg:  "invalid timestamp format",
		},
		{
			name: "missing text",
			claim: &IsnadClaim{
				Type:    PostTypeIsnadClaim,
				Version: IsnadVersion,
				Meta:    ClaimMeta{ID: "id", Timestamp: time.Now().UTC().Format(time.RFC3339)},
				Claim:   ClaimData{Confidence: 0.9},
			},
			wantErr: true,
			errMsg:  "claim.text is required",
		},
		{
			name: "confidence too low",
			claim: &IsnadClaim{
				Type:    PostTypeIsnadClaim,
				Version: IsnadVersion,
				Meta:    ClaimMeta{ID: "id", Timestamp: time.Now().UTC().Format(time.RFC3339)},
				Claim:   ClaimData{Text: "test", Confidence: -0.1},
			},
			wantErr: true,
			errMsg:  "confidence must be between",
		},
		{
			name: "confidence too high",
			claim: &IsnadClaim{
				Type:    PostTypeIsnadClaim,
				Version: IsnadVersion,
				Meta:    ClaimMeta{ID: "id", Timestamp: time.Now().UTC().Format(time.RFC3339)},
				Claim:   ClaimData{Text: "test", Confidence: 1.1},
			},
			wantErr: true,
			errMsg:  "confidence must be between",
		},
		{
			name: "missing signature",
			claim: &IsnadClaim{
				Type:    PostTypeIsnadClaim,
				Version: IsnadVersion,
				Meta:    ClaimMeta{ID: "id", Timestamp: time.Now().UTC().Format(time.RFC3339)},
				Claim:   ClaimData{Text: "test", Confidence: 0.9},
			},
			wantErr: true,
			errMsg:  "signature is required",
		},
		{
			name: "invalid sentiment",
			claim: &IsnadClaim{
				Type:    PostTypeIsnadClaim,
				Version: IsnadVersion,
				Meta:    ClaimMeta{ID: "id", Timestamp: time.Now().UTC().Format(time.RFC3339)},
				Claim:   ClaimData{Text: "test", Confidence: 0.9, Sentiment: "angry"},
			},
			wantErr: true,
			errMsg:  "invalid sentiment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.claim.Signature == "" && tt.errMsg != "signature is required" {
				tt.claim.Signature = "test_sig"
			}
			err := ValidateIsnadClaim(tt.claim)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errMsg)
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestValidateIsnadEndorsement tests endorsement validation.
func TestValidateIsnadEndorsement(t *testing.T) {
	validTargetJSON, _ := json.Marshal(map[string]any{
		"type":      "isnad_claim",
		"signature": "test_sig",
	})

	tests := []struct {
		name        string
		endorsement *IsnadEndorsement
		wantErr     bool
		errMsg      string
	}{
		{
			name: "valid endorsement",
			endorsement: &IsnadEndorsement{
				Type:    PostTypeIsnadEndorsement,
				Version: IsnadVersion,
				Meta: EndorsementMeta{
					Timestamp:      time.Now().UTC().Format(time.RFC3339),
					EndorserPubkey: "test_pubkey",
				},
				Endorsement: EndorsementData{
					Rating:  0.9,
					Context: "verified",
				},
				TargetObject:     validTargetJSON,
				WrapperSignature: "test_sig",
			},
			wantErr: false,
		},
		{
			name: "invalid type",
			endorsement: &IsnadEndorsement{
				Type:    "wrong_type",
				Version: IsnadVersion,
				Meta: EndorsementMeta{
					Timestamp:      time.Now().UTC().Format(time.RFC3339),
					EndorserPubkey: "test_pubkey",
				},
				Endorsement:      EndorsementData{Rating: 0.9, Context: "test"},
				TargetObject:     validTargetJSON,
				WrapperSignature: "test_sig",
			},
			wantErr: true,
			errMsg:  "invalid type",
		},
		{
			name: "rating too high",
			endorsement: &IsnadEndorsement{
				Type:    PostTypeIsnadEndorsement,
				Version: IsnadVersion,
				Meta: EndorsementMeta{
					Timestamp:      time.Now().UTC().Format(time.RFC3339),
					EndorserPubkey: "test_pubkey",
				},
				Endorsement:      EndorsementData{Rating: 1.5, Context: "test"},
				TargetObject:     validTargetJSON,
				WrapperSignature: "test_sig",
			},
			wantErr: true,
			errMsg:  "rating must be between",
		},
		{
			name: "missing context",
			endorsement: &IsnadEndorsement{
				Type:    PostTypeIsnadEndorsement,
				Version: IsnadVersion,
				Meta: EndorsementMeta{
					Timestamp:      time.Now().UTC().Format(time.RFC3339),
					EndorserPubkey: "test_pubkey",
				},
				Endorsement:      EndorsementData{Rating: 0.9},
				TargetObject:     validTargetJSON,
				WrapperSignature: "test_sig",
			},
			wantErr: true,
			errMsg:  "endorsement.context is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIsnadEndorsement(tt.endorsement)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errMsg)
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestExtractTargetClaim tests extracting claims from endorsement target_object.
func TestExtractTargetClaim(t *testing.T) {
	claim := &IsnadClaim{
		Type:    PostTypeIsnadClaim,
		Version: IsnadVersion,
		Meta: ClaimMeta{
			ID:        "test_id",
			Timestamp: "2026-02-02T12:00:00Z",
		},
		Claim: ClaimData{
			Text:       "Test claim",
			Confidence: 0.9,
		},
		Signature: "test_sig",
	}

	claimJSON, _ := json.Marshal(claim)

	extracted, err := ExtractTargetClaim(claimJSON)
	if err != nil {
		t.Fatalf("Failed to extract claim: %v", err)
	}

	if extracted.Meta.ID != claim.Meta.ID {
		t.Errorf("Expected ID %s, got %s", claim.Meta.ID, extracted.Meta.ID)
	}
	if extracted.Claim.Text != claim.Claim.Text {
		t.Errorf("Expected text %s, got %s", claim.Claim.Text, extracted.Claim.Text)
	}
}

// TestServiceCreateIsnadClaimPost tests creating claim posts via service.
func TestServiceCreateIsnadClaimPost(t *testing.T) {
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	if err := AddTables(db); err != nil {
		t.Fatalf("Failed to register tables: %v", err)
	}

	service := NewService(db, nil)
	ctx := context.Background()

	// Generate keypair
	pubkey, privkey, _ := GenerateKeyPair()

	// Create and sign claim
	claim := createIsnadClaim("Test claim via service", 0.95, []string{"test"}, SentimentPositive)
	sig, _ := signIsnadClaim(privkey, claim)
	claim.Signature = sig

	// Create post via service
	post, err := service.CreateIsnadClaimPost(ctx, claim, pubkey, VerificationMethodPoW)
	if err != nil {
		t.Fatalf("Failed to create claim post: %v", err)
	}

	if post.PostType != PostTypeIsnadClaim {
		t.Errorf("Expected post type %s, got %s", PostTypeIsnadClaim, post.PostType)
	}
	if post.Content != claim.Claim.Text {
		t.Errorf("Expected content %s, got %s", claim.Claim.Text, post.Content)
	}
	if post.Confidence == nil || *post.Confidence != 0.95 {
		t.Error("Expected confidence 0.95")
	}
	if post.ClaimID != claim.Meta.ID {
		t.Error("Claim ID should be stored")
	}
}

// TestServiceCreateIsnadEndorsementPost tests creating endorsement posts via service.
func TestServiceCreateIsnadEndorsementPost(t *testing.T) {
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	if err := AddTables(db); err != nil {
		t.Fatalf("Failed to register tables: %v", err)
	}

	service := NewService(db, nil)
	ctx := context.Background()

	// Generate keypairs
	_, authorPrivkey, _ := GenerateKeyPair()
	endorserPubkey, endorserPrivkey, _ := GenerateKeyPair()

	// Create and sign original claim
	claim := createIsnadClaim("Claim to endorse", 0.9, nil, "")
	claimSig, _ := signIsnadClaim(authorPrivkey, claim)
	claim.Signature = claimSig

	// Create and sign endorsement
	claimJSON, _ := json.Marshal(claim)
	endorsement := createIsnadEndorsement(claim, claimJSON, 1.0, "verified", SentimentPositive, EncodePublicKey(endorserPubkey))
	wrapperSig, _ := signIsnadEndorsement(endorserPrivkey, endorsement, claim.Signature)
	endorsement.WrapperSignature = wrapperSig

	// Create post via service
	post, err := service.CreateIsnadEndorsementPost(ctx, endorsement, endorserPubkey, VerificationMethodPremium)
	if err != nil {
		t.Fatalf("Failed to create endorsement post: %v", err)
	}

	if post.PostType != PostTypeIsnadEndorsement {
		t.Errorf("Expected post type %s, got %s", PostTypeIsnadEndorsement, post.PostType)
	}
	if post.Rating == nil || *post.Rating != 1.0 {
		t.Error("Expected rating 1.0")
	}
	if post.TargetPostID != claim.Meta.ID {
		t.Error("Target post ID should reference the claim")
	}
}

// TestServiceListPostsFiltered tests filtered post listing.
func TestServiceListPostsFiltered(t *testing.T) {
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()
	if err := AddTables(db); err != nil {
		t.Fatalf("Failed to register tables: %v", err)
	}

	service := NewService(db, nil)
	ctx := context.Background()

	// Generate keypair
	pubkey, privkey, _ := GenerateKeyPair()

	// Create some claims with different confidence levels
	for i, conf := range []float64{0.5, 0.7, 0.9, 0.95} {
		claim := createIsnadClaim("Claim "+string(rune('A'+i)), conf, []string{"test"}, "")
		sig, _ := signIsnadClaim(privkey, claim)
		claim.Signature = sig
		service.CreateIsnadClaimPost(ctx, claim, pubkey, VerificationMethodPoW)
	}

	// Create a text post too
	service.CreatePost(ctx, EncodePublicKey(pubkey), "Text post", VerificationMethodPoW, nil)

	// Test filtering by type
	posts, err := service.ListPostsFiltered(ctx, PostsFilter{
		PostType: PostTypeIsnadClaim,
		Limit:    100,
	})
	if err != nil {
		t.Fatalf("Failed to list posts: %v", err)
	}
	if len(posts) != 4 {
		t.Errorf("Expected 4 claims, got %d", len(posts))
	}

	// Test filtering by minimum confidence
	minConf := 0.8
	posts, err = service.ListPostsFiltered(ctx, PostsFilter{
		PostType:      PostTypeIsnadClaim,
		MinConfidence: &minConf,
		Limit:         100,
	})
	if err != nil {
		t.Fatalf("Failed to list posts: %v", err)
	}
	if len(posts) != 2 {
		t.Errorf("Expected 2 claims with confidence >= 0.8, got %d", len(posts))
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
