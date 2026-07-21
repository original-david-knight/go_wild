package gowild_data

import (
	"context"
	"fmt"
	"testing"
)

func TestSqliteDatabase_Transaction_Commit(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	db.AddTable(TestUser{})
	ctx := context.Background()

	// Insert within a committed transaction
	err = db.RunInTransaction(ctx, func(tx Database) error {
		dao := tx.Table(TestUser{})
		return dao.Insert(ctx, &TestUser{ID: "tx-1", Name: "TxUser"})
	})
	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	// Verify data persisted
	var user TestUser
	if err := db.Table(TestUser{}).Get(ctx, "tx-1", &user); err != nil {
		t.Fatalf("Get after commit failed: %v", err)
	}
	if user.Name != "TxUser" {
		t.Errorf("expected name TxUser, got %s", user.Name)
	}
}

func TestSqliteDatabase_Transaction_Rollback(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	db.AddTable(TestUser{})
	ctx := context.Background()

	// Insert that gets rolled back
	err = db.RunInTransaction(ctx, func(tx Database) error {
		dao := tx.Table(TestUser{})
		dao.Insert(ctx, &TestUser{ID: "tx-2", Name: "RollbackUser"})
		return fmt.Errorf("intentional rollback")
	})
	if err == nil {
		t.Fatal("expected error from failed transaction")
	}

	// Verify data was NOT persisted
	var user TestUser
	if err := db.Table(TestUser{}).Get(ctx, "tx-2", &user); err == nil {
		t.Error("expected error getting rolled-back data")
	}
}

func TestSqliteDatabase_Transaction_CRUD(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	db.AddTable(TestUser{})
	ctx := context.Background()

	// Pre-populate
	db.Table(TestUser{}).Insert(ctx, &TestUser{ID: "u1", Name: "Original"})

	// Update and delete in transaction
	err = db.RunInTransaction(ctx, func(tx Database) error {
		dao := tx.Table(TestUser{})

		// Update
		if err := dao.Update(ctx, &TestUser{ID: "u1", Name: "Updated"}); err != nil {
			return err
		}

		// Insert new
		if err := dao.Insert(ctx, &TestUser{ID: "u2", Name: "New"}); err != nil {
			return err
		}

		// Get within transaction
		var u TestUser
		if err := dao.Get(ctx, "u1", &u); err != nil {
			return err
		}
		if u.Name != "Updated" {
			return fmt.Errorf("expected Updated, got %s", u.Name)
		}

		// GetAll within transaction
		all, err := dao.GetAll(ctx)
		if err != nil {
			return err
		}
		if len(all) != 2 {
			return fmt.Errorf("expected 2, got %d", len(all))
		}

		// Delete within transaction
		return dao.Delete(ctx, "u2")
	})
	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	// Verify outside transaction
	var u TestUser
	db.Table(TestUser{}).Get(ctx, "u1", &u)
	if u.Name != "Updated" {
		t.Errorf("expected Updated, got %s", u.Name)
	}

	results, _ := db.Table(TestUser{}).GetAll(ctx)
	if len(results) != 1 {
		t.Errorf("expected 1 user (u2 deleted), got %d", len(results))
	}
}

func TestSqliteDatabase_Transaction_Query(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	db.AddTable(TestUser{})
	ctx := context.Background()

	err = db.RunInTransaction(ctx, func(tx Database) error {
		dao := tx.Table(TestUser{})
		dao.Insert(ctx, &TestUser{ID: "q1", Name: "Alice", Age: 30, Active: true})
		dao.Insert(ctx, &TestUser{ID: "q2", Name: "Bob", Age: 25, Active: false})
		dao.Insert(ctx, &TestUser{ID: "q3", Name: "Charlie", Age: 35, Active: true})

		// Query with where
		results, err := dao.Query(ctx, QueryOpts{
			Where: map[string]any{"active": 1},
		})
		if err != nil {
			return err
		}
		if len(results) != 2 {
			return fmt.Errorf("expected 2 active users, got %d", len(results))
		}

		// Query with order and limit
		results, err = dao.Query(ctx, QueryOpts{
			OrderBy:   "age",
			OrderDesc: true,
			Limit:     2,
		})
		if err != nil {
			return err
		}
		if len(results) != 2 {
			return fmt.Errorf("expected 2 results, got %d", len(results))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("transaction query test failed: %v", err)
	}
}

func TestSqliteDatabase_Transaction_UserScoped(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	db.AddTable(TestUser{})
	ctx := context.Background()

	err = db.RunInTransaction(ctx, func(tx Database) error {
		// Insert as user1
		dao1 := tx.ForUser("user1").Table(TestUser{})
		if err := dao1.Insert(ctx, &TestUser{ID: "item-1", Name: "User1Item"}); err != nil {
			return err
		}

		// Insert as user2
		dao2 := tx.ForUser("user2").Table(TestUser{})
		if err := dao2.Insert(ctx, &TestUser{ID: "item-2", Name: "User2Item"}); err != nil {
			return err
		}

		// User1 should only see their items
		results, err := dao1.GetAll(ctx)
		if err != nil {
			return err
		}
		if len(results) != 1 {
			return fmt.Errorf("expected 1 for user1, got %d", len(results))
		}

		// Get specific item as user1
		var u TestUser
		if err := dao1.Get(ctx, "item-1", &u); err != nil {
			return err
		}
		if u.Name != "User1Item" {
			return fmt.Errorf("expected User1Item, got %s", u.Name)
		}

		// Delete as user2
		return dao2.Delete(ctx, "item-2")
	})
	if err != nil {
		t.Fatalf("user-scoped transaction test failed: %v", err)
	}
}

func TestSqliteDatabase_Transaction_AddTableFails(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	db.AddTable(TestUser{})
	ctx := context.Background()

	err = db.RunInTransaction(ctx, func(tx Database) error {
		return tx.AddTable(TestUser{})
	})
	if err == nil {
		t.Error("expected error adding table within transaction")
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ID", "i_d"},
		{"UserName", "user_name"},
		{"CreatedAt", "created_at"},
		{"HTTPClient", "h_t_t_p_client"},
		{"simple", "simple"},
		{"A", "a"},
		{"", ""},
	}
	for _, tc := range tests {
		got := toSnakeCase(tc.input)
		if got != tc.expected {
			t.Errorf("toSnakeCase(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestGetModelMeta_Pointer(t *testing.T) {
	meta, err := getModelMeta(&TestUser{})
	if err != nil {
		t.Fatalf("getModelMeta with pointer failed: %v", err)
	}
	if meta.TableName != "test_users" {
		t.Errorf("expected test_users, got %s", meta.TableName)
	}
}

func TestGetModelMeta_NonStruct(t *testing.T) {
	_, err := getModelMeta("not a struct")
	if err == nil {
		t.Error("expected error for non-struct")
	}
}

func TestSqliteDatabase_QueryAllOptions(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	db.AddTable(TestUser{})
	ctx := context.Background()
	dao := db.Table(TestUser{})

	for i := 0; i < 10; i++ {
		dao.Insert(ctx, &TestUser{
			ID:     fmt.Sprintf("u%d", i),
			Name:   fmt.Sprintf("User%d", i),
			Age:    20 + i,
			Active: i%2 == 0,
		})
	}

	// Test OrderBy + Desc + Limit + Offset
	results, err := dao.Query(ctx, QueryOpts{
		OrderBy:   "age",
		OrderDesc: true,
		Limit:     3,
		Offset:    2,
	})
	if err != nil {
		t.Fatalf("complex query failed: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	// Check ordering (descending, so ages should be 27, 26, 25 after skipping 29, 28)
	first := results[0].(*TestUser)
	if first.Age != 27 {
		t.Errorf("expected age 27 (3rd highest), got %d", first.Age)
	}
}

func TestSqliteDatabase_TableNilForUnregistered(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	dao := db.Table(TestUser{})
	if dao != nil {
		t.Error("expected nil DAO for unregistered table")
	}
}

func TestCreateTableSQLPostgres_TypeMapping(t *testing.T) {
	meta, _ := getModelMeta(TestUser{})
	sql := meta.createTableSQLPostgres()

	// Should use PostgreSQL types
	if !containsStr(sql, "BOOLEAN") {
		t.Error("expected BOOLEAN for bool type in PostgreSQL")
	}
	if !containsStr(sql, "JSONB") {
		t.Error("expected JSONB for slice type in PostgreSQL")
	}
	if !containsStr(sql, "TIMESTAMPTZ") {
		t.Error("expected TIMESTAMPTZ for time.Time in PostgreSQL")
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
