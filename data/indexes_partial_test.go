package gowild_data

import (
	"context"
	"strings"
	"testing"
)

// auditRow is a toy model used to exercise the two index shapes geet
// needs that the base CreateTableSQL path does not emit:
//
//   - composite UNIQUE, which also serves as the surrogate for the design
//     doc's "composite PRIMARY KEY" on org_members / challenges — the
//     reflection schema generator only emits single-column PK, so we
//     enforce multi-column uniqueness via a unique index instead.
//   - a partial UNIQUE index, load-bearing for geet's one-open-PR-per-
//     branch invariant.
type auditRow struct {
	ID     string `json:"id"`
	OrgID  int64  `json:"org_id"`
	Pubkey string `json:"pubkey"`
	Branch string `json:"branch"`
	Status string `json:"status"`
}

func (auditRow) TableName() string { return "audit_rows" }
func (r auditRow) GetID() string   { return r.ID }

func TestAuditSchemaFeaturesForGeet(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("NewSqliteDatabase: %v", err)
	}
	defer db.Close()

	if err := db.AddTable(auditRow{}); err != nil {
		t.Fatalf("AddTable: %v", err)
	}

	// 1. Composite UNIQUE: simulates org_members (org_id, pubkey).
	if err := EnsureUniqueIndex(db, auditRow{}, "idx_audit_org_pubkey", "org_id", "pubkey"); err != nil {
		t.Fatalf("EnsureUniqueIndex composite: %v", err)
	}

	// 2. Partial UNIQUE: simulates pull_requests (repo_id, head_branch)
	//    WHERE status='open'. This is the load-bearing gap.
	if err := ensureUniqueIndexWhere(
		db, auditRow{},
		"idx_audit_open_branch",
		[]string{"org_id", "branch"},
		"status = 'open'",
	); err != nil {
		t.Fatalf("EnsureUniqueIndexWhere partial: %v", err)
	}

	ctx := context.Background()
	dao := db.Table(auditRow{})

	// Insert two open rows on distinct branches: both should succeed.
	if err := dao.Insert(ctx, auditRow{ID: "1", OrgID: 1, Pubkey: "k1", Branch: "feat/a", Status: "open"}); err != nil {
		t.Fatalf("insert row 1: %v", err)
	}
	if err := dao.Insert(ctx, auditRow{ID: "2", OrgID: 1, Pubkey: "k2", Branch: "feat/b", Status: "open"}); err != nil {
		t.Fatalf("insert row 2: %v", err)
	}

	// Composite UNIQUE rejects duplicate (org_id, pubkey).
	err = dao.Insert(ctx, auditRow{ID: "3", OrgID: 1, Pubkey: "k1", Branch: "feat/c", Status: "closed"})
	if err == nil {
		t.Fatal("composite UNIQUE(org_id,pubkey) should have rejected duplicate")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("expected UNIQUE constraint error, got: %v", err)
	}

	// Partial UNIQUE rejects a second open row on the same branch ...
	err = dao.Insert(ctx, auditRow{ID: "4", OrgID: 1, Pubkey: "k3", Branch: "feat/a", Status: "open"})
	if err == nil {
		t.Fatal("partial UNIQUE should have rejected second open row on feat/a")
	}

	// ... but allows a closed row on the same branch (status != 'open'
	// excludes it from the partial index).
	if err := dao.Insert(ctx, auditRow{ID: "5", OrgID: 1, Pubkey: "k4", Branch: "feat/a", Status: "closed"}); err != nil {
		t.Fatalf("closed row on feat/a should be allowed by partial UNIQUE: %v", err)
	}
	if err := dao.Insert(ctx, auditRow{ID: "6", OrgID: 1, Pubkey: "k5", Branch: "feat/a", Status: "merged"}); err != nil {
		t.Fatalf("merged row on feat/a should be allowed by partial UNIQUE: %v", err)
	}
}

func TestEnsureUniqueIndexWhereDetectsStaleDefinition(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("NewSqliteDatabase: %v", err)
	}
	defer db.Close()
	if err := db.AddTable(auditRow{}); err != nil {
		t.Fatalf("AddTable: %v", err)
	}

	// Create an index that does NOT match the definition we'll ask for.
	// This simulates a stale migration where someone ran a prior version
	// of the schema that created the index without the WHERE clause.
	if err := EnsureUniqueIndex(db, auditRow{}, "idx_audit_stale", "org_id", "branch"); err != nil {
		t.Fatalf("seed index: %v", err)
	}

	// Ask for a partial unique on the same name — must be rejected, not
	// silently skipped by CREATE IF NOT EXISTS.
	err = ensureUniqueIndexWhere(
		db, auditRow{},
		"idx_audit_stale",
		[]string{"org_id", "branch"},
		"status = 'open'",
	)
	if err == nil {
		t.Fatal("expected stale-definition rejection, got nil")
	}
	if !strings.Contains(err.Error(), "different definition") {
		t.Fatalf("expected 'different definition' error, got: %v", err)
	}

	// Re-requesting the SAME definition that already exists must be a no-op.
	if err := EnsureUniqueIndex(db, auditRow{}, "idx_audit_stale", "org_id", "branch"); err != nil {
		t.Fatalf("idempotent re-apply should succeed: %v", err)
	}
}

func TestEnsureUniqueIndexWhereCaseInStringLiteral(t *testing.T) {
	// Regression guard: normalization must NOT lowercase inside single
	// quotes. 'open' and 'OPEN' are semantically distinct SQL values and
	// two indices that differ only in literal case must be detected as
	// different definitions.
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("NewSqliteDatabase: %v", err)
	}
	defer db.Close()
	if err := db.AddTable(auditRow{}); err != nil {
		t.Fatalf("AddTable: %v", err)
	}
	if err := ensureUniqueIndexWhere(
		db, auditRow{}, "idx_audit_literal",
		[]string{"org_id", "branch"}, "status = 'open'",
	); err != nil {
		t.Fatalf("seed: %v", err)
	}
	err = ensureUniqueIndexWhere(
		db, auditRow{}, "idx_audit_literal",
		[]string{"org_id", "branch"}, "status = 'OPEN'",
	)
	if err == nil {
		t.Fatal("expected mismatch between 'open' and 'OPEN' string literals")
	}
	if !strings.Contains(err.Error(), "different definition") {
		t.Fatalf("expected 'different definition' error, got: %v", err)
	}
}

func TestEnsureUniqueIndexWhereRejectsBadInput(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("NewSqliteDatabase: %v", err)
	}
	defer db.Close()
	if err := db.AddTable(auditRow{}); err != nil {
		t.Fatalf("AddTable: %v", err)
	}

	if err := ensureUniqueIndexWhere(db, auditRow{}, "", []string{"org_id"}, ""); err == nil {
		t.Fatal("empty index name should be rejected")
	}
	if err := ensureUniqueIndexWhere(db, auditRow{}, "idx_empty", nil, ""); err == nil {
		t.Fatal("empty columns should be rejected")
	}
}
