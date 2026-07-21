package data

import (
	"testing"

	"github.com/original-david-knight/go_wild/data"
	kg "github.com/original-david-knight/go_wild/knowledge_graph"
)

// setupTestDB creates an in-memory database with all agent data tables registered.
func setupTestDB(t *testing.T) gowild_data.Database {
	t.Helper()
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	if err := RegisterTables(db); err != nil {
		t.Fatalf("failed to register tables: %v", err)
	}
	// Register knowledge graph tables (needed for DeleteAgent)
	db.AddTable(kg.Node{})
	db.AddTable(kg.Edge{})
	t.Cleanup(func() { db.Close() })
	return db
}
