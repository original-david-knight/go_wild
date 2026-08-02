package objectives_planner

import (
	"testing"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

// setupTestDB mirrors the helper the planner tests used before the module
// split. AddAllTables picks up both this module's memory tables and the
// objectives tables, since importing either package registers them.
func setupTestDB(t *testing.T) gowild_data.Database {
	t.Helper()
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	if err := gowild_data.AddAllTables(db); err != nil {
		t.Fatalf("add tables: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
