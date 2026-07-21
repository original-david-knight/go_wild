package main

import (
	"context"
	"strings"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
)

// TestLoadPipelineMethodDefinition pins the behavior contract of the shared
// method-definition loader used by every pipeline runner (Claude Code, Codex,
// etc.) after the rename from loadClaudeCodeMethodDefinition. Each branch of
// the svc -> fallback -> nil chain is exercised so a future refactor (e.g.
// dropping the system-scope fallback, or changing empty-string handling)
// cannot silently change behavior for any runner.
func TestLoadPipelineMethodDefinition(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	engine := &PipelineEngine{db: db, service: NewAgentService(db)}

	systemSvc := data.NewAgentService(db, "system")
	if _, err := systemSvc.CreateA2AMethod(ctx, "system_only_method", "system-registered", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod(system_only_method): %v", err)
	}

	agentSvc := data.NewAgentService(db, "agent-1")
	if _, err := agentSvc.CreateA2AMethod(ctx, "agent_only_method", "agent-registered", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod(agent_only_method): %v", err)
	}

	t.Run("empty method returns nil", func(t *testing.T) {
		if got := engine.loadPipelineMethodDefinition(ctx, agentSvc, ""); got != nil {
			t.Fatalf("empty method: want nil, got %+v", got)
		}
	})

	t.Run("whitespace-only method returns nil", func(t *testing.T) {
		if got := engine.loadPipelineMethodDefinition(ctx, agentSvc, "   \t\n "); got != nil {
			t.Fatalf("whitespace method: want nil, got %+v", got)
		}
	})

	t.Run("agent-scope svc hit returns method", func(t *testing.T) {
		got := engine.loadPipelineMethodDefinition(ctx, agentSvc, "agent_only_method")
		if got == nil {
			t.Fatal("want method, got nil")
		}
		if got.Method != "agent_only_method" {
			t.Fatalf("method name = %q, want agent_only_method", got.Method)
		}
	})

	t.Run("agent-scope miss falls back to system scope", func(t *testing.T) {
		// agentSvc cannot see system_only_method — the loader must fall
		// through to the engine's system-scope service to find it. This
		// fallback is load-bearing for pipelines where steps target methods
		// that live only in the system registry.
		got := engine.loadPipelineMethodDefinition(ctx, agentSvc, "system_only_method")
		if got == nil {
			t.Fatal("want system-scope method via fallback, got nil")
		}
		if got.Method != "system_only_method" {
			t.Fatalf("method name = %q, want system_only_method", got.Method)
		}
	})

	t.Run("nil svc uses system scope directly", func(t *testing.T) {
		got := engine.loadPipelineMethodDefinition(ctx, nil, "system_only_method")
		if got == nil {
			t.Fatal("want method via system fallback (svc=nil), got nil")
		}
		if got.Method != "system_only_method" {
			t.Fatalf("method name = %q, want system_only_method", got.Method)
		}
	})

	t.Run("method absent from both scopes returns nil", func(t *testing.T) {
		if got := engine.loadPipelineMethodDefinition(ctx, agentSvc, "nonexistent_method"); got != nil {
			t.Fatalf("want nil for unknown method, got %+v", got)
		}
	})

	t.Run("trims whitespace before lookup", func(t *testing.T) {
		got := engine.loadPipelineMethodDefinition(ctx, agentSvc, "  agent_only_method  ")
		if got == nil {
			t.Fatal("want method despite surrounding whitespace, got nil")
		}
		if got.Method != "agent_only_method" {
			t.Fatalf("method name = %q, want agent_only_method", got.Method)
		}
	})

	t.Run("engine with nil db returns nil on system-scope fallback", func(t *testing.T) {
		// Pins the doc comment's "returns nil when the engine has no DB
		// handle" clause. Realistic: a PipelineEngine constructed without
		// db (e.g. during early startup or a test that stubs svc) must
		// not panic on the NewAgentService(pe.db, "system") path.
		noDBEngine := &PipelineEngine{}
		if got := noDBEngine.loadPipelineMethodDefinition(ctx, nil, "system_only_method"); got != nil {
			t.Fatalf("want nil when engine has no db, got %+v", got)
		}
	})
}

// TestRecordPipelineJob pins the upsert contract of the shared job-recording
// helper used by every pipeline runner (Claude Code, Codex, etc.) after the
// rename from recordClaudePipelineJob. Existing pipeline-engine integration
// tests cover the happy paths via real runner invocations, but a direct test
// makes the contract — guard branches, status default, insert vs update,
// timestamp UTC normalization — explicit so a future refactor can't silently
// change behavior shared across runners.
func TestRecordPipelineJob(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	engine := &PipelineEngine{db: db, service: NewAgentService(db)}

	getJob := func(t *testing.T, jobID string) *localA2AJob {
		t.Helper()
		var job localA2AJob
		if err := db.Table(localA2AJob{}).Get(ctx, jobID, &job); err != nil {
			t.Fatalf("Get(%q): %v", jobID, err)
		}
		return &job
	}

	t.Run("nil engine is a no-op", func(t *testing.T) {
		// Defensive: every other shared helper guards against (*PipelineEngine)(nil),
		// so the contract is consistent across the package.
		var nilEngine *PipelineEngine
		nilEngine.recordPipelineJob(ctx, "nil-engine-job", "agent", localA2ARequest{Method: "m"}, "running", nil, nil, time.Time{})
	})

	t.Run("engine with nil db is a no-op", func(t *testing.T) {
		noDBEngine := &PipelineEngine{}
		noDBEngine.recordPipelineJob(ctx, "no-db-job", "agent", localA2ARequest{Method: "m"}, "running", nil, nil, time.Time{})
	})

	t.Run("empty jobID is a no-op", func(t *testing.T) {
		// Empty / whitespace-only IDs would corrupt the table primary key.
		// The guard short-circuits before any DB access.
		engine.recordPipelineJob(ctx, "", "agent", localA2ARequest{Method: "m"}, "running", nil, nil, time.Time{})
		engine.recordPipelineJob(ctx, "   \t\n ", "agent", localA2ARequest{Method: "m"}, "running", nil, nil, time.Time{})
	})

	t.Run("insert with empty status defaults to running", func(t *testing.T) {
		// Callers in the shared executor always pass non-empty status today,
		// but the default behavior is part of the documented lifecycle and
		// removing it would let a buggy caller silently insert blank-status
		// rows that the queue's status filters skip.
		jobID := "empty-status-default-job"
		engine.recordPipelineJob(ctx, jobID, "agent-default", localA2ARequest{Method: "inspect"}, "", nil, nil, time.Time{})
		got := getJob(t, jobID)
		if got.Status != "running" {
			t.Fatalf("status = %q, want running", got.Status)
		}
		if got.FromAgentID != "pipeline" {
			t.Fatalf("from_agent_id = %q, want pipeline", got.FromAgentID)
		}
		if got.ToAgentID != "agent-default" {
			t.Fatalf("to_agent_id = %q, want agent-default", got.ToAgentID)
		}
		if got.CompletedAt != nil {
			t.Fatalf("completed_at = %v, want nil for non-terminal status", got.CompletedAt)
		}
	})

	t.Run("trims whitespace on jobID and targetAgentID", func(t *testing.T) {
		// The trim is what lets the upsert path find the existing row on
		// re-record (callers occasionally pass IDs with surrounding space
		// from db rows that haven't been canonicalized).
		jobID := "trimmed-job"
		engine.recordPipelineJob(ctx, "  "+jobID+"  ", "  agent-trim  ", localA2ARequest{Method: "m"}, "running", nil, nil, time.Time{})
		got := getJob(t, jobID)
		if got.ID != jobID {
			t.Fatalf("id = %q, want %q", got.ID, jobID)
		}
		if got.ToAgentID != "agent-trim" {
			t.Fatalf("to_agent_id = %q, want agent-trim", got.ToAgentID)
		}
	})

	t.Run("running insert then terminal update is an upsert", func(t *testing.T) {
		// Mirrors the real lifecycle: shared executor calls recordPipelineJob
		// once with "running" + nil result, then again with the terminal
		// status + result + completedAt. The same row must be updated, not a
		// duplicate inserted (primary key is jobID).
		jobID := "lifecycle-job"
		engine.recordPipelineJob(ctx, jobID, "agent-life", localA2ARequest{Method: "analyze"}, "running", nil, nil, time.Time{})
		first := getJob(t, jobID)
		if first.Status != "running" {
			t.Fatalf("first status = %q, want running", first.Status)
		}
		if first.ResultJSON != "" || first.ErrorJSON != "" {
			t.Fatalf("first result/error should be empty: result=%q error=%q", first.ResultJSON, first.ErrorJSON)
		}
		if first.CompletedAt != nil {
			t.Fatalf("first completed_at = %v, want nil", first.CompletedAt)
		}

		// Pacific time so we exercise the .UTC() conversion in the helper.
		pacific, err := time.LoadLocation("America/Los_Angeles")
		if err != nil {
			t.Fatalf("LoadLocation: %v", err)
		}
		completedAt := time.Date(2026, 4, 16, 9, 30, 0, 0, pacific)
		engine.recordPipelineJob(ctx, jobID, "agent-life",
			localA2ARequest{Method: "analyze"},
			"succeeded",
			map[string]any{"value": 42},
			nil,
			completedAt,
		)

		updated := getJob(t, jobID)
		if updated.Status != "succeeded" {
			t.Fatalf("updated status = %q, want succeeded", updated.Status)
		}
		if !strings.Contains(updated.ResultJSON, "\"value\":42") {
			t.Fatalf("result_json = %q, want value:42", updated.ResultJSON)
		}
		if updated.ErrorJSON != "" {
			t.Fatalf("error_json = %q, want empty (no error payload passed)", updated.ErrorJSON)
		}
		if updated.CompletedAt == nil {
			t.Fatal("completed_at = nil, want UTC timestamp")
		}
		if updated.CompletedAt.Location() != time.UTC {
			t.Fatalf("completed_at location = %v, want UTC", updated.CompletedAt.Location())
		}
		if !updated.CompletedAt.Equal(completedAt) {
			t.Fatalf("completed_at = %v, want equal to %v", updated.CompletedAt, completedAt)
		}
	})

	t.Run("failure update sets error payload", func(t *testing.T) {
		jobID := "failure-job"
		engine.recordPipelineJob(ctx, jobID, "agent-fail", localA2ARequest{Method: "boom"}, "running", nil, nil, time.Time{})
		engine.recordPipelineJob(ctx, jobID, "agent-fail",
			localA2ARequest{Method: "boom"},
			"failed",
			nil,
			map[string]any{"message": "exit 1"},
			time.Now(),
		)
		got := getJob(t, jobID)
		if got.Status != "failed" {
			t.Fatalf("status = %q, want failed", got.Status)
		}
		if !strings.Contains(got.ErrorJSON, "exit 1") {
			t.Fatalf("error_json = %q, want to contain exit 1", got.ErrorJSON)
		}
		if got.CompletedAt == nil {
			t.Fatal("completed_at = nil, want set on terminal status")
		}
	})
}
