package gowild_data

import (
	"context"
	"testing"
	"time"
)

func setupCacheDB(t *testing.T) (*SqliteDatabase, *Cache) {
	t.Helper()
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	if err := db.AddTable(cacheEntry{}); err != nil {
		t.Fatalf("failed to add table: %v", err)
	}
	return db, NewCache(db)
}

func TestCache_SetAndGet(t *testing.T) {
	db, cache := setupCacheDB(t)
	defer db.Close()
	ctx := context.Background()

	err := cache.Set(ctx, "key1", `{"hello":"world"}`, 1*time.Hour)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, ok := cache.Get(ctx, "key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if val != `{"hello":"world"}` {
		t.Fatalf("got %q, want %q", val, `{"hello":"world"}`)
	}
}

func TestCache_Miss(t *testing.T) {
	db, cache := setupCacheDB(t)
	defer db.Close()
	ctx := context.Background()

	_, ok := cache.Get(ctx, "nonexistent")
	if ok {
		t.Fatal("expected cache miss for nonexistent key")
	}
}

func TestCache_Expiry(t *testing.T) {
	db, cache := setupCacheDB(t)
	defer db.Close()
	ctx := context.Background()

	// Set with a very short TTL (already expired)
	err := cache.Set(ctx, "expired", "value", -1*time.Second)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	_, ok := cache.Get(ctx, "expired")
	if ok {
		t.Fatal("expected cache miss for expired key")
	}
}

func TestCache_Overwrite(t *testing.T) {
	db, cache := setupCacheDB(t)
	defer db.Close()
	ctx := context.Background()

	cache.Set(ctx, "key", "first", 1*time.Hour)
	cache.Set(ctx, "key", "second", 1*time.Hour)

	val, ok := cache.Get(ctx, "key")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if val != "second" {
		t.Fatalf("got %q, want %q", val, "second")
	}
}

func TestCache_JSON(t *testing.T) {
	db, cache := setupCacheDB(t)
	defer db.Close()
	ctx := context.Background()

	type article struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	}

	input := article{Title: "Test Article", URL: "https://example.com"}
	err := cache.SetJSON(ctx, "article:1", input, 1*time.Hour)
	if err != nil {
		t.Fatalf("SetJSON failed: %v", err)
	}

	var output article
	ok := cache.GetJSON(ctx, "article:1", &output)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if output.Title != "Test Article" || output.URL != "https://example.com" {
		t.Fatalf("got %+v, want %+v", output, input)
	}
}
