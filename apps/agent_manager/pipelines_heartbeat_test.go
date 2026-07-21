package main

import (
	"context"
	"strings"
	"testing"

	"github.com/original-david-knight/go_wild/data"
)

type heartbeatCall struct {
	agentID string
	message string
}

type stubHeartbeatSender struct {
	calls []heartbeatCall
}

func (s *stubHeartbeatSender) SendHeartbeat(agentID, message string) error {
	s.calls = append(s.calls, heartbeatCall{
		agentID: agentID,
		message: message,
	})
	return nil
}

func TestDeliverPipelineStepJobSendsStructuredHeartbeat(t *testing.T) {
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	gowild_data.AddAllTables(db)

	sender := &stubHeartbeatSender{}
	engine := &PipelineEngine{
		db:              db,
		localQueue:      newLocalA2AQueue(db),
		heartbeatSender: sender,
	}

	ctx := context.Background()
	dataSvc := NewAgentService(db)
	if _, err := dataSvc.CreateA2AMethodWithConfig(ctx, "test_method", "test method", "", "", "", false, true, false, false, false); err != nil {
		t.Fatal(err)
	}

	// Enqueue a job so ClaimJob can find it.
	queue := engine.localQueueOrDefault()
	jobResult, _, err := queue.Submit(ctx, "pipeline:test-run", "target-agent", "", localA2ARequest{
		Method: "test_method",
		Params: map[string]any{"foo": "bar"},
	})
	if err != nil {
		t.Fatal(err)
	}
	jobID, _ := jobResult["job_id"].(string)
	if jobID == "" {
		t.Fatal("expected job_id from submit")
	}

	engine.deliverPipelineStepJob(ctx, "target-agent", jobID, "test_method")

	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 heartbeat call, got %d", len(sender.calls))
	}
	call := sender.calls[0]
	if call.agentID != "target-agent" {
		t.Fatalf("agentID = %q, want %q", call.agentID, "target-agent")
	}
	if !strings.Contains(call.message, jobID) {
		t.Fatalf("expected heartbeat message to include job id %q, got %q", jobID, call.message)
	}
	if !strings.Contains(call.message, "test_method") {
		t.Fatalf("expected heartbeat message to include method name, got %q", call.message)
	}
	if !strings.Contains(call.message, "Execute this method call now") {
		t.Fatalf("expected structured heartbeat with method call instructions, got %q", call.message)
	}
	if !strings.Contains(call.message, "Completion Rules") {
		t.Fatalf("expected structured heartbeat with completion rules, got %q", call.message)
	}
	if !strings.Contains(call.message, "foo") {
		t.Fatalf("expected heartbeat to include input parameters, got %q", call.message)
	}
	if !strings.Contains(call.message, "Fresh Context: true") {
		t.Fatalf("expected heartbeat to include fresh-context marker, got %q", call.message)
	}
}
