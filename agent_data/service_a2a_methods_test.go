package data

import (
	"context"
	"strings"
	"testing"
)

func TestDeleteA2AMethodReturnsNotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := NewAgentService(db, "system")

	err := svc.DeleteA2AMethod(context.Background(), "missing_method")
	if err == nil || !strings.Contains(err.Error(), "method not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestDeleteA2AMethodDeletesExisting(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	svc := NewAgentService(db, "system")

	if _, err := svc.CreateA2AMethod(ctx, "method_to_delete", "temp", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}
	if err := svc.DeleteA2AMethod(ctx, "method_to_delete"); err != nil {
		t.Fatalf("DeleteA2AMethod failed: %v", err)
	}
	if _, err := svc.GetA2AMethod(ctx, "method_to_delete"); err == nil {
		t.Fatalf("expected deleted method lookup to fail")
	}
}

func TestCreateAndUpdateA2AMethodInstructions(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	svc := NewAgentService(db, "system")

	created, err := svc.CreateA2AMethodWithInstructions(
		ctx,
		"method_with_instructions",
		"initial description",
		"step by step instructions",
		`{"type":"object"}`,
		`{"type":"object"}`,
	)
	if err != nil {
		t.Fatalf("CreateA2AMethodWithInstructions failed: %v", err)
	}
	if got := strings.TrimSpace(created.Instructions); got != "step by step instructions" {
		t.Fatalf("expected instructions to be saved, got %q", got)
	}

	updated, err := svc.UpdateA2AMethodWithInstructions(
		ctx,
		"method_with_instructions",
		"updated description",
		"updated execution guidance",
		`{"type":"object","additionalProperties":false}`,
		`{"type":"object","additionalProperties":false}`,
	)
	if err != nil {
		t.Fatalf("UpdateA2AMethodWithInstructions failed: %v", err)
	}
	if got := strings.TrimSpace(updated.Instructions); got != "updated execution guidance" {
		t.Fatalf("expected updated instructions, got %q", got)
	}

	fetched, err := svc.GetA2AMethod(ctx, "method_with_instructions")
	if err != nil {
		t.Fatalf("GetA2AMethod failed: %v", err)
	}
	if got := strings.TrimSpace(fetched.Instructions); got != "updated execution guidance" {
		t.Fatalf("expected fetched instructions to match update, got %q", got)
	}
}

func TestCreateAndUpdateA2AMethodConfig(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	svc := NewAgentService(db, "system")

	created, err := svc.CreateA2AMethodWithConfig(
		ctx,
		"market_review",
		"review a market",
		"look at current position",
		`{"type":"object"}`,
		`{"type":"object"}`,
		true,
		true,
		true,
		true,
		true,
	)
	if err != nil {
		t.Fatalf("CreateA2AMethodWithConfig failed: %v", err)
	}
	if !created.AutoMarketNote {
		t.Fatalf("expected auto market note to be enabled on create")
	}
	if !created.FreshContext {
		t.Fatalf("expected fresh context to be enabled on create")
	}
	if !created.RedactMarketPrices {
		t.Fatalf("expected redact market prices to be enabled on create")
	}
	if !created.DisableMarketNotes {
		t.Fatalf("expected market notes to be disabled on create")
	}
	if !created.DisablePolymarketNoteAugmentation {
		t.Fatalf("expected polymarket note augmentation to be disabled on create")
	}

	updated, err := svc.UpdateA2AMethodWithConfig(
		ctx,
		"market_review",
		"review a market",
		"look at current position",
		`{"type":"object","additionalProperties":false}`,
		`{"type":"object","additionalProperties":false}`,
		false,
		false,
		false,
		false,
		false,
	)
	if err != nil {
		t.Fatalf("UpdateA2AMethodWithConfig failed: %v", err)
	}
	if updated.AutoMarketNote {
		t.Fatalf("expected auto market note to be disabled on update")
	}
	if updated.FreshContext {
		t.Fatalf("expected fresh context to be disabled on update")
	}
	if updated.RedactMarketPrices {
		t.Fatalf("expected redact market prices to be disabled on update")
	}
	if updated.DisableMarketNotes {
		t.Fatalf("expected market notes to be enabled on update")
	}
	if updated.DisablePolymarketNoteAugmentation {
		t.Fatalf("expected polymarket note augmentation to be enabled on update")
	}

	fetched, err := svc.GetA2AMethod(ctx, "market_review")
	if err != nil {
		t.Fatalf("GetA2AMethod failed: %v", err)
	}
	if fetched.AutoMarketNote {
		t.Fatalf("expected fetched auto market note to match updated value")
	}
	if fetched.FreshContext {
		t.Fatalf("expected fetched fresh context to match updated value")
	}
	if fetched.RedactMarketPrices {
		t.Fatalf("expected fetched redact market prices to match updated value")
	}
	if fetched.DisableMarketNotes {
		t.Fatalf("expected fetched disable market notes to match updated value")
	}
	if fetched.DisablePolymarketNoteAugmentation {
		t.Fatalf("expected fetched disable polymarket note augmentation to match updated value")
	}
}
