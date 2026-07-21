package gowild_data

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// cacheEntry stores a cached value with TTL expiration.
type cacheEntry struct {
	ID        string    `json:"id"`         // Cache key
	Value     string    `json:"value"`      // JSON-encoded value
	ExpiresAt time.Time `json:"expires_at"` // When this entry expires
	CreatedAt time.Time `json:"created_at"`
}

func init() {
	RegisterFunc(func(db Database) error {
		return db.AddTable(cacheEntry{})
	})
}

// Cache provides a general-purpose key-value cache backed by a Database.
// Any service or tool can store JSON values by key with a TTL.
type Cache struct {
	db Database
}

// NewCache creates a cache backed by the given database.
// The cacheEntry table must have been registered (happens automatically via init()).
func NewCache(db Database) *Cache {
	return &Cache{db: db}
}

// Get retrieves a cached value by key. Returns the value and true if found
// and not expired, or empty string and false otherwise.
func (c *Cache) Get(ctx context.Context, key string) (string, bool) {
	var entry cacheEntry
	err := c.db.Table(cacheEntry{}).Get(ctx, key, &entry)
	if err != nil {
		return "", false
	}
	if time.Now().UTC().After(entry.ExpiresAt) {
		if err := c.db.Table(cacheEntry{}).Delete(ctx, key); err != nil {
			// Treat expired entries as cache misses even if cleanup fails.
			return "", false
		}
		return "", false
	}
	return entry.Value, true
}

// Set stores a value with the given TTL. Overwrites any existing entry for the key.
func (c *Cache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	now := time.Now().UTC()
	entry := &cacheEntry{
		ID:        key,
		Value:     value,
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}
	// Delete any existing entry, then insert the new one.
	if err := c.db.Table(cacheEntry{}).Delete(ctx, key); err != nil {
		return fmt.Errorf("delete existing cache entry %q: %w", key, err)
	}
	return c.db.Table(cacheEntry{}).Insert(ctx, entry)
}

// GetJSON retrieves a cached value and unmarshals it into dest.
// Returns true if found and not expired, false otherwise.
func (c *Cache) GetJSON(ctx context.Context, key string, dest any) bool {
	val, ok := c.Get(ctx, key)
	if !ok {
		return false
	}
	if err := json.Unmarshal([]byte(val), dest); err != nil {
		return false
	}
	return true
}

// SetJSON marshals value to JSON and stores it with the given TTL.
func (c *Cache) SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.Set(ctx, key, string(data), ttl)
}
