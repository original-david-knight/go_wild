package gowild_data

import (
	"context"
	"os"
	"testing"
	"time"
)

// getPostgresTestURL returns the PostgreSQL connection string for testing.
// Set POSTGRES_TEST_URL environment variable to run PostgreSQL tests.
// Example: POSTGRES_TEST_URL="postgres://user:pass@localhost:5432/testdb?sslmode=disable"
func getPostgresTestURL() string {
	return os.Getenv("POSTGRES_TEST_URL")
}

func skipIfNoPostgres(t *testing.T) *PostgresDatabase {
	url := getPostgresTestURL()
	if url == "" {
		t.Skip("POSTGRES_TEST_URL not set, skipping PostgreSQL tests")
	}

	db, err := NewPostgresDatabase(url)
	if err != nil {
		t.Fatalf("failed to connect to PostgreSQL: %v", err)
	}

	return db
}

// cleanupTable drops a table if it exists for test isolation.
func cleanupTable(db *PostgresDatabase, tableName string) {
	db.db.Exec("DROP TABLE IF EXISTS " + tableName)
}

func TestPostgresDatabase_AddTable(t *testing.T) {
	db := skipIfNoPostgres(t)
	defer db.Close()
	cleanupTable(db, "test_users")

	err := db.AddTable(TestUser{})
	if err != nil {
		t.Fatalf("failed to add table: %v", err)
	}

	// Verify table exists by inserting
	user := &TestUser{
		ID:        "user-1",
		Name:      "Alice",
		Email:     "alice@example.com",
		Age:       30,
		Active:    true,
		Tags:      []string{"admin", "user"},
		CreatedAt: time.Now(),
	}

	dao := db.Table(TestUser{})
	if dao == nil {
		t.Fatal("expected non-nil DAO")
	}

	err = dao.Insert(context.Background(), user)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	// Cleanup
	cleanupTable(db, "test_users")
}

func TestPostgresDatabase_CRUD(t *testing.T) {
	db := skipIfNoPostgres(t)
	defer db.Close()
	cleanupTable(db, "test_users")

	db.AddTable(TestUser{})
	dao := db.Table(TestUser{})
	ctx := context.Background()

	// Insert
	user := &TestUser{
		ID:     "user-1",
		Name:   "Alice",
		Email:  "alice@example.com",
		Age:    30,
		Active: true,
	}
	if err := dao.Insert(ctx, user); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	// Get
	var retrieved TestUser
	if err := dao.Get(ctx, "user-1", &retrieved); err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if retrieved.Name != "Alice" {
		t.Errorf("expected name Alice, got %s", retrieved.Name)
	}

	// Update
	user.Name = "Alice Updated"
	if err := dao.Update(ctx, user); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	if err := dao.Get(ctx, "user-1", &retrieved); err != nil {
		t.Fatalf("get after update failed: %v", err)
	}
	if retrieved.Name != "Alice Updated" {
		t.Errorf("expected name Alice Updated, got %s", retrieved.Name)
	}

	// Delete
	if err := dao.Delete(ctx, "user-1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	// Verify deleted
	err := dao.Get(ctx, "user-1", &retrieved)
	if err == nil {
		t.Error("expected error getting deleted user")
	}

	// Cleanup
	cleanupTable(db, "test_users")
}

func TestPostgresDatabase_Query(t *testing.T) {
	db := skipIfNoPostgres(t)
	defer db.Close()
	cleanupTable(db, "test_users")

	db.AddTable(TestUser{})
	dao := db.Table(TestUser{})
	ctx := context.Background()

	// Insert test data
	users := []TestUser{
		{ID: "1", Name: "Alice", Age: 30, Active: true},
		{ID: "2", Name: "Bob", Age: 25, Active: false},
		{ID: "3", Name: "Charlie", Age: 35, Active: true},
	}
	for _, u := range users {
		dao.Insert(ctx, &u)
	}

	// Query all
	results, err := dao.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	// Query with where - PostgreSQL uses native boolean
	results, err = dao.Query(ctx, QueryOpts{
		Where: map[string]any{"active": true},
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 active users, got %d", len(results))
	}

	// Query with limit
	results, err = dao.Query(ctx, QueryOpts{
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("Query with limit failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results with limit, got %d", len(results))
	}

	// Cleanup
	cleanupTable(db, "test_users")
}

func TestPostgresDatabase_UserScoped(t *testing.T) {
	db := skipIfNoPostgres(t)
	defer db.Close()
	cleanupTable(db, "test_users")

	db.AddTable(TestUser{})
	ctx := context.Background()

	// Insert as user1
	user1DB := db.ForUser("user1")
	dao1 := user1DB.Table(TestUser{})

	item1 := &TestUser{ID: "item-1", Name: "User1's Item"}
	dao1.Insert(ctx, item1)

	// Insert as user2
	user2DB := db.ForUser("user2")
	dao2 := user2DB.Table(TestUser{})

	item2 := &TestUser{ID: "item-2", Name: "User2's Item"}
	dao2.Insert(ctx, item2)

	// User1 should only see their items
	results, err := dao1.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for user1, got %d", len(results))
	}

	// User2 should only see their items
	results, err = dao2.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for user2, got %d", len(results))
	}

	// Cleanup
	cleanupTable(db, "test_users")
}

func TestPostgresDatabase_SchemaMigration(t *testing.T) {
	db := skipIfNoPostgres(t)
	defer db.Close()
	cleanupTable(db, "evolving_users")

	ctx := context.Background()

	// Step 1: Create table with V1 schema
	if err := db.AddTable(TestUserV1{}); err != nil {
		t.Fatalf("failed to add V1 table: %v", err)
	}

	// Insert a V1 record
	v1User := &TestUserV1{ID: "user-1", Name: "Alice"}
	if err := db.Table(TestUserV1{}).Insert(ctx, v1User); err != nil {
		t.Fatalf("failed to insert V1 user: %v", err)
	}

	// Step 2: "Upgrade" by adding table with V2 schema (should add new columns)
	if err := db.AddTable(TestUserV2{}); err != nil {
		t.Fatalf("failed to add V2 table: %v", err)
	}

	// Step 3: Insert a V2 record with new columns
	verified := true
	v2User := &TestUserV2{
		ID:       "user-2",
		Name:     "Bob",
		Email:    "bob@example.com",
		Score:    95.5,
		Verified: &verified,
	}
	if err := db.Table(TestUserV2{}).Insert(ctx, v2User); err != nil {
		t.Fatalf("failed to insert V2 user: %v", err)
	}

	// Step 4: Read back and verify both records work
	var retrieved TestUserV2
	if err := db.Table(TestUserV2{}).Get(ctx, "user-1", &retrieved); err != nil {
		t.Fatalf("failed to get V1 user as V2: %v", err)
	}
	if retrieved.Name != "Alice" {
		t.Errorf("expected name Alice, got %s", retrieved.Name)
	}
	// New columns should be zero/nil for old record
	if retrieved.Email != "" {
		t.Errorf("expected empty email for V1 user, got %s", retrieved.Email)
	}

	if err := db.Table(TestUserV2{}).Get(ctx, "user-2", &retrieved); err != nil {
		t.Fatalf("failed to get V2 user: %v", err)
	}
	if retrieved.Email != "bob@example.com" {
		t.Errorf("expected email bob@example.com, got %s", retrieved.Email)
	}
	if retrieved.Score != 95.5 {
		t.Errorf("expected score 95.5, got %f", retrieved.Score)
	}
	if retrieved.Verified == nil || !*retrieved.Verified {
		t.Error("expected verified to be true")
	}

	// Cleanup
	cleanupTable(db, "evolving_users")
}

func TestPostgresDatabase_Transaction(t *testing.T) {
	db := skipIfNoPostgres(t)
	defer db.Close()
	cleanupTable(db, "test_users")

	db.AddTable(TestUser{})
	ctx := context.Background()

	// Test successful transaction
	err := db.RunInTransaction(ctx, func(tx Database) error {
		dao := tx.Table(TestUser{})
		user := &TestUser{ID: "tx-user-1", Name: "Transaction User"}
		return dao.Insert(ctx, user)
	})
	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	// Verify inserted
	var retrieved TestUser
	if err := db.Table(TestUser{}).Get(ctx, "tx-user-1", &retrieved); err != nil {
		t.Fatalf("failed to get transaction user: %v", err)
	}
	if retrieved.Name != "Transaction User" {
		t.Errorf("expected name Transaction User, got %s", retrieved.Name)
	}

	// Test rollback on error
	err = db.RunInTransaction(ctx, func(tx Database) error {
		dao := tx.Table(TestUser{})
		user := &TestUser{ID: "tx-user-2", Name: "Rollback User"}
		dao.Insert(ctx, user)
		return context.Canceled // Simulate error
	})
	if err == nil {
		t.Error("expected transaction to fail")
	}

	// Verify not inserted due to rollback
	err = db.Table(TestUser{}).Get(ctx, "tx-user-2", &retrieved)
	if err == nil {
		t.Error("expected user to not exist due to rollback")
	}

	// Cleanup
	cleanupTable(db, "test_users")
}

func TestPostgresDatabase_NativeTypes(t *testing.T) {
	db := skipIfNoPostgres(t)
	defer db.Close()
	cleanupTable(db, "test_users")

	db.AddTable(TestUser{})
	dao := db.Table(TestUser{})
	ctx := context.Background()

	// Test native boolean and timestamp
	now := time.Now().Truncate(time.Microsecond) // PostgreSQL has microsecond precision
	user := &TestUser{
		ID:        "native-1",
		Name:      "Native Types",
		Active:    true,
		Tags:      []string{"tag1", "tag2"},
		CreatedAt: now,
	}

	if err := dao.Insert(ctx, user); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	var retrieved TestUser
	if err := dao.Get(ctx, "native-1", &retrieved); err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if !retrieved.Active {
		t.Error("expected Active to be true")
	}

	if len(retrieved.Tags) != 2 || retrieved.Tags[0] != "tag1" {
		t.Errorf("expected tags [tag1, tag2], got %v", retrieved.Tags)
	}

	// Compare timestamps with some tolerance for timezone handling
	if retrieved.CreatedAt.Sub(now).Abs() > time.Second {
		t.Errorf("expected CreatedAt %v, got %v", now, retrieved.CreatedAt)
	}

	// Cleanup
	cleanupTable(db, "test_users")
}

func TestCreateTableSQLPostgres(t *testing.T) {
	meta, _ := getModelMeta(TestUser{})
	sql := meta.createTableSQLPostgres()

	expected := `CREATE TABLE IF NOT EXISTS test_users (
    id TEXT PRIMARY KEY,
    user_id TEXT,
    name TEXT,
    email TEXT,
    age INTEGER,
    active BOOLEAN,
    tags JSONB,
    created_at TIMESTAMPTZ
)`
	if sql != expected {
		t.Errorf("unexpected SQL:\ngot:\n%s\nexpected:\n%s", sql, expected)
	}
}
