package gowild_data

import (
	"context"
	"testing"
	"time"
)

type TestUser struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Age       int       `json:"age"`
	Active    bool      `json:"active"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
}

func TestSqliteDatabase_AddTable(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	err = db.AddTable(TestUser{})
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
}

func TestSqliteDatabase_CRUD(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

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
	err = dao.Get(ctx, "user-1", &retrieved)
	if err == nil {
		t.Error("expected error getting deleted user")
	}
}

func TestSqliteDatabase_Query(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

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

	// Query with where
	results, err = dao.Query(ctx, QueryOpts{
		Where: map[string]any{"active": 1},
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
}

func TestSqliteDatabase_UserScoped(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

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
}

func TestGetModelMeta(t *testing.T) {
	meta, err := getModelMeta(TestUser{})
	if err != nil {
		t.Fatalf("failed to get model meta: %v", err)
	}

	if meta.TableName != "test_users" {
		t.Errorf("expected table name test_users, got %s", meta.TableName)
	}

	if meta.IDField != "ID" {
		t.Errorf("expected ID field ID, got %s", meta.IDField)
	}

	if len(meta.Fields) != 8 {
		t.Errorf("expected 8 fields, got %d", len(meta.Fields))
	}
}

func TestCreateTableSQL(t *testing.T) {
	meta, _ := getModelMeta(TestUser{})
	sql := meta.createTableSQL()

	expected := `CREATE TABLE IF NOT EXISTS test_users (
    id TEXT PRIMARY KEY,
    user_id TEXT,
    name TEXT,
    email TEXT,
    age INTEGER,
    active INTEGER,
    tags TEXT,
    created_at TEXT
)`
	if sql != expected {
		t.Errorf("unexpected SQL:\ngot:\n%s\nexpected:\n%s", sql, expected)
	}
}

func TestRegistry_Integration(t *testing.T) {
	// Clear default registry
	defaultRegistry.clear()

	// Simulate package init registering tables
	RegisterFunc(func(db Database) error {
		return db.AddTable(TestUser{})
	})

	// App creates database and adds all tables
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	if err := AddAllTables(db); err != nil {
		t.Fatalf("failed to add all tables: %v", err)
	}

	// Verify table was created
	dao := db.Table(TestUser{})
	if dao == nil {
		t.Error("expected table to be registered")
	}

	// Clean up
	defaultRegistry.clear()
}

// TestUserV1 is the original model without new columns
type TestUserV1 struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TestUserV2 is the evolved model with new columns
type TestUserV2 struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Email    string  `json:"email,omitempty"`    // New column
	Score    float64 `json:"score,omitempty"`    // New column
	Verified *bool   `json:"verified,omitempty"` // New nullable column
}

// Override table name to use the same table
func (TestUserV1) TableName() string { return "evolving_users" }
func (TestUserV2) TableName() string { return "evolving_users" }

func TestSqliteDatabase_SchemaMigration(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

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
}

// TestSqliteDatabase_InMemoryVisibleAcrossGoroutines pins the
// single-connection pool pin that NewSqliteDatabase applies when the DSN
// is ":memory:". Without it, Go's sql pool will open a second connection
// on concurrent access and each in-memory connection is a separate
// private database, so tables (and rows) created on one connection
// would be invisible on another. This manifests in tests as flaky
// "no such table" errors under concurrency. Regression guard for a fix
// to the pipeline-engine test flake that surfaced this bug.
func TestSqliteDatabase_InMemoryVisibleAcrossGoroutines(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("NewSqliteDatabase failed: %v", err)
	}
	defer db.Close()

	if err := db.AddTable(TestUser{}); err != nil {
		t.Fatalf("AddTable failed: %v", err)
	}

	ctx := context.Background()
	dao := db.Table(TestUser{})

	// Run several concurrent inserts. If the pool were multi-connection,
	// some goroutines would hit a fresh in-memory DB with no table, and
	// Insert would fail with "no such table". Tight loop + goroutine count
	// is enough to force the pool to expand without SetMaxOpenConns(1).
	const workers = 8
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		id := i
		go func() {
			errs <- dao.Insert(ctx, &TestUser{
				ID:        "concurrent-user-" + string(rune('A'+id)),
				Name:      "concurrent",
				CreatedAt: time.Now(),
			})
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent Insert failed: %v (regression: :memory: DB must share state across connections)", err)
		}
	}

	rows, err := dao.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if len(rows) != workers {
		t.Fatalf("expected %d rows after concurrent inserts, got %d", workers, len(rows))
	}
}
