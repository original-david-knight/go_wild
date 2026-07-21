package data

import (
	"context"
	"testing"
	"time"
)

func TestSetAndGetMarketProperty(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	prop, err := SetMarketProperty(ctx, db, "co-1", "cond-1", "last_researched_at", "2026-03-17T12:00:00Z", MarketPropertyTypeDatetime)
	if err != nil {
		t.Fatalf("SetMarketProperty failed: %v", err)
	}
	if prop.Key != "last_researched_at" {
		t.Fatalf("expected key last_researched_at, got %s", prop.Key)
	}

	got, err := getMarketProperty(ctx, db, "co-1", "cond-1", "last_researched_at")
	if err != nil {
		t.Fatalf("GetMarketProperty failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected property, got nil")
	}
	if got.Value != "2026-03-17T12:00:00Z" {
		t.Fatalf("expected value 2026-03-17T12:00:00Z, got %s", got.Value)
	}
	if got.ValueType != MarketPropertyTypeDatetime {
		t.Fatalf("expected type datetime, got %s", got.ValueType)
	}

	ts, err := parseMarketPropertyDatetime(got)
	if err != nil {
		t.Fatalf("ParseMarketPropertyDatetime failed: %v", err)
	}
	if ts.Year() != 2026 || ts.Month() != 3 || ts.Day() != 17 {
		t.Fatalf("unexpected parsed time: %v", ts)
	}
}

func TestSetMarketPropertyUpserts(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := SetMarketProperty(ctx, db, "co-1", "cond-1", "flagged", "true", MarketPropertyTypeBool)
	if err != nil {
		t.Fatalf("first set failed: %v", err)
	}

	updated, err := SetMarketProperty(ctx, db, "co-1", "cond-1", "flagged", "false", MarketPropertyTypeBool)
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if updated.Value != "false" {
		t.Fatalf("expected value false, got %s", updated.Value)
	}

	props, err := listMarketProperties(ctx, db, "co-1", "cond-1")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("expected 1 property after upsert, got %d", len(props))
	}
}

func TestMarketPropertyTypeValidation(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	if _, err := SetMarketProperty(ctx, db, "co-1", "cond-1", "k", "not-a-date", MarketPropertyTypeDatetime); err == nil {
		t.Fatal("expected error for invalid datetime")
	}
	if _, err := SetMarketProperty(ctx, db, "co-1", "cond-1", "k", "maybe", MarketPropertyTypeBool); err == nil {
		t.Fatal("expected error for invalid bool")
	}
	if _, err := SetMarketProperty(ctx, db, "co-1", "cond-1", "k", "abc", MarketPropertyTypeCurrency); err == nil {
		t.Fatal("expected error for invalid currency")
	}
	if _, err := SetMarketProperty(ctx, db, "co-1", "cond-1", "k", "v", "unknown"); err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestMarketPropertyCurrency(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := SetMarketProperty(ctx, db, "co-1", "cond-1", "target_exposure", "150.50", MarketPropertyTypeCurrency)
	if err != nil {
		t.Fatalf("set failed: %v", err)
	}
	got, _ := getMarketProperty(ctx, db, "co-1", "cond-1", "target_exposure")
	val, err := parseMarketPropertyCurrency(got)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if val != 150.50 {
		t.Fatalf("expected 150.50, got %f", val)
	}
}

func TestMarketPropertyBool(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, err := SetMarketProperty(ctx, db, "co-1", "cond-1", "skip_research", "true", MarketPropertyTypeBool)
	if err != nil {
		t.Fatalf("set failed: %v", err)
	}
	got, _ := getMarketProperty(ctx, db, "co-1", "cond-1", "skip_research")
	val, err := parseMarketPropertyBool(got)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if !val {
		t.Fatal("expected true")
	}
}

func TestDeleteMarketProperty(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, _ = SetMarketProperty(ctx, db, "co-1", "cond-1", "k1", "v1", MarketPropertyTypeString)

	deleted, err := deleteMarketProperty(ctx, db, "co-1", "cond-1", "k1")
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if !deleted {
		t.Fatal("expected deleted=true")
	}

	got, _ := getMarketProperty(ctx, db, "co-1", "cond-1", "k1")
	if got != nil {
		t.Fatal("expected nil after delete")
	}

	deleted, _ = deleteMarketProperty(ctx, db, "co-1", "cond-1", "k1")
	if deleted {
		t.Fatal("expected deleted=false for missing key")
	}
}

func TestListMarketProperties(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, _ = SetMarketProperty(ctx, db, "co-1", "cond-1", "beta", "v2", MarketPropertyTypeString)
	_, _ = SetMarketProperty(ctx, db, "co-1", "cond-1", "alpha", "v1", MarketPropertyTypeString)
	_, _ = SetMarketProperty(ctx, db, "co-1", "cond-2", "gamma", "v3", MarketPropertyTypeString)

	props, err := listMarketProperties(ctx, db, "co-1", "cond-1")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(props) != 2 {
		t.Fatalf("expected 2 properties, got %d", len(props))
	}
	if props[0].Key != "alpha" {
		t.Fatalf("expected alpha first (sorted), got %s", props[0].Key)
	}
}

func TestListMarketPropertiesBulk(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	_, _ = SetMarketProperty(ctx, db, "co-1", "cond-1", "k1", "v1", MarketPropertyTypeString)
	_, _ = SetMarketProperty(ctx, db, "co-1", "cond-2", "k2", "v2", MarketPropertyTypeString)
	_, _ = SetMarketProperty(ctx, db, "co-1", "cond-3", "k3", "v3", MarketPropertyTypeString)

	bulk, err := ListMarketPropertiesBulk(ctx, db, "co-1", []string{"cond-1", "cond-2"})
	if err != nil {
		t.Fatalf("bulk list failed: %v", err)
	}
	if len(bulk["cond-1"]) != 1 {
		t.Fatalf("expected 1 property for cond-1, got %d", len(bulk["cond-1"]))
	}
	if len(bulk["cond-2"]) != 1 {
		t.Fatalf("expected 1 property for cond-2, got %d", len(bulk["cond-2"]))
	}
	if bulk["cond-3"] != nil {
		t.Fatal("did not request cond-3, should not be in results")
	}
}

func TestSetMarketPropertyRequiredFields(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	if _, err := SetMarketProperty(ctx, db, "", "cond", "k", "v", MarketPropertyTypeString); err == nil {
		t.Fatal("expected error for empty company_id")
	}
	if _, err := SetMarketProperty(ctx, db, "co", "", "k", "v", MarketPropertyTypeString); err == nil {
		t.Fatal("expected error for empty condition_id")
	}
	if _, err := SetMarketProperty(ctx, db, "co", "cond", "", "v", MarketPropertyTypeString); err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestGetMarketPropertyNotFound(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	got, err := getMarketProperty(ctx, db, "co-1", "cond-1", "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for missing property")
	}
}

func TestMarketPropertyUpdatedAtChangesOnUpsert(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	first, _ := SetMarketProperty(ctx, db, "co-1", "cond-1", "k", "v1", MarketPropertyTypeString)
	firstUpdated := first.UpdatedAt

	time.Sleep(2 * time.Millisecond)

	second, _ := SetMarketProperty(ctx, db, "co-1", "cond-1", "k", "v2", MarketPropertyTypeString)
	if !second.UpdatedAt.After(firstUpdated) {
		t.Fatal("expected updated_at to advance on upsert")
	}
}
