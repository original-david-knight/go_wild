package main

import (
	"context"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/agent_net"
	"github.com/original-david-knight/go_wild/data"
)

func newTestDB(t *testing.T) gowild_data.Database {
	t.Helper()
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("failed to create sqlite db: %v", err)
	}
	if err := gowild_data.AddAllTables(db); err != nil {
		t.Fatalf("failed to register tables: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func TestPromoteAccountCreatesPremiumRecord(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	pubkey := "acct-promote-new"

	if err := promoteAccount(ctx, db, pubkey, "", ""); err != nil {
		t.Fatalf("promoteAccount failed: %v", err)
	}

	rows, err := db.Table(gowild_agent_net.PremiumAgent{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"public_key": pubkey},
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 premium record, got %d", len(rows))
	}

	agent, ok := rows[0].(*gowild_agent_net.PremiumAgent)
	if !ok {
		t.Fatalf("unexpected row type: %T", rows[0])
	}
	if agent.Chain != gowild_agent_net.ChainSolana {
		t.Fatalf("expected default chain %q, got %q", gowild_agent_net.ChainSolana, agent.Chain)
	}
	if agent.Revoked {
		t.Fatalf("expected revoked=false")
	}
	if agent.TxHash == "" {
		t.Fatalf("expected generated tx hash")
	}
}

func TestPromoteAccountUpdatesRevokedPremium(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	pubkey := "acct-promote-existing"

	now := time.Now().Add(-time.Hour)
	err := db.Table(gowild_agent_net.PremiumAgent{}).Insert(ctx, &gowild_agent_net.PremiumAgent{
		ID:           pubkey,
		PublicKey:    pubkey,
		TxHash:       "old-tx",
		Chain:        gowild_agent_net.ChainSolana,
		UpgradedAt:   now,
		Revoked:      true,
		LastActiveAt: now,
	})
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	if err := promoteAccount(ctx, db, pubkey, gowild_agent_net.ChainBase, "new-admin-tx"); err != nil {
		t.Fatalf("promoteAccount failed: %v", err)
	}

	rows, err := db.Table(gowild_agent_net.PremiumAgent{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"public_key": pubkey},
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 premium record, got %d", len(rows))
	}

	agent := rows[0].(*gowild_agent_net.PremiumAgent)
	if agent.Revoked {
		t.Fatalf("expected revoked=false")
	}
	if agent.Chain != gowild_agent_net.ChainBase {
		t.Fatalf("expected chain %q, got %q", gowild_agent_net.ChainBase, agent.Chain)
	}
	if agent.TxHash != "new-admin-tx" {
		t.Fatalf("expected tx hash to update, got %q", agent.TxHash)
	}
}

func TestPromoteAccountRejectsDuplicateTxHash(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	now := time.Now()

	err := db.Table(gowild_agent_net.PremiumAgent{}).Insert(ctx, &gowild_agent_net.PremiumAgent{
		ID:           "acct-1",
		PublicKey:    "acct-1",
		TxHash:       "shared-tx",
		Chain:        gowild_agent_net.ChainSolana,
		UpgradedAt:   now,
		Revoked:      false,
		LastActiveAt: now,
	})
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	if err := promoteAccount(ctx, db, "acct-2", gowild_agent_net.ChainSolana, "shared-tx"); err == nil {
		t.Fatalf("expected duplicate tx hash error")
	}
}

func TestDeleteAccountCascadeDeletesPostsAndRelatedRows(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	target := "acct-delete-target"
	other := "acct-delete-other"

	err := db.Table(gowild_agent_net.AgentProfile{}).Insert(ctx, &gowild_agent_net.AgentProfile{
		ID:        target,
		PublicKey: target,
		Name:      "Target",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("insert profile failed: %v", err)
	}

	for _, post := range []gowild_agent_net.Post{
		{ID: "post-1", PublicKey: target, Content: "a", CreatedAt: time.Now()},
		{ID: "post-2", PublicKey: target, Content: "b", CreatedAt: time.Now()},
		{ID: "post-3", PublicKey: other, Content: "c", CreatedAt: time.Now()},
	} {
		p := post
		if err := db.Table(gowild_agent_net.Post{}).Insert(ctx, &p); err != nil {
			t.Fatalf("insert post failed: %v", err)
		}
	}

	err = db.Table(gowild_agent_net.DirectMessage{}).Insert(ctx, &gowild_agent_net.DirectMessage{
		ID:            "dm-1",
		FromPublicKey: target,
		ToPublicKey:   other,
		Ciphertext:    "x",
		Nonce:         "n",
		CreatedAt:     time.Now(),
	})
	if err != nil {
		t.Fatalf("insert message failed: %v", err)
	}

	summary, err := deleteAccountCascade(ctx, db, target)
	if err != nil {
		t.Fatalf("deleteAccountCascade failed: %v", err)
	}

	if summary.ByTable["posts"] != 2 {
		t.Fatalf("expected 2 posts deleted, got %d", summary.ByTable["posts"])
	}

	targetPosts, err := db.Table(gowild_agent_net.Post{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"public_key": target},
	})
	if err != nil {
		t.Fatalf("query target posts failed: %v", err)
	}
	if len(targetPosts) != 0 {
		t.Fatalf("expected target posts removed, got %d", len(targetPosts))
	}

	otherPosts, err := db.Table(gowild_agent_net.Post{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"public_key": other},
	})
	if err != nil {
		t.Fatalf("query other posts failed: %v", err)
	}
	if len(otherPosts) != 1 {
		t.Fatalf("expected other account posts to remain, got %d", len(otherPosts))
	}

	profiles, err := db.Table(gowild_agent_net.AgentProfile{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"public_key": target},
	})
	if err != nil {
		t.Fatalf("query profile failed: %v", err)
	}
	if len(profiles) != 0 {
		t.Fatalf("expected profile deleted, got %d", len(profiles))
	}
}
