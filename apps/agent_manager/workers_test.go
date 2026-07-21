package main

import (
	"context"
	"testing"
)

func TestWorkerManager_StartAgent_NoToken(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	ctx := context.Background()
	svc.CreateAgent(ctx, "agent-1")

	wm := NewWorkerManager(nil, svc, nil, db)
	if err := wm.StartAgent("agent-1"); err != nil {
		t.Fatalf("StartAgent failed: %v", err)
	}

	// No workers should be registered
	wm.mu.RLock()
	workers := wm.workers["agent-1"]
	wm.mu.RUnlock()
	if len(workers) != 0 {
		t.Errorf("expected 0 workers, got %d", len(workers))
	}
}

func TestWorkerManager_StopAgent_NoWorkers(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	wm := NewWorkerManager(nil, svc, nil, db)

	// Should not panic
	wm.StopAgent("nonexistent-agent")
}

func TestWorkerManager_StopAll_Empty(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	wm := NewWorkerManager(nil, svc, nil, db)

	// Should not panic
	wm.StopAll()
}

func TestWorkerManager_GetTelegramTools_NoWorker(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	wm := NewWorkerManager(nil, svc, nil, db)

	tools := wm.GetTelegramTools("nonexistent-agent")
	if tools != nil {
		t.Errorf("expected nil tools, got %v", tools)
	}
}
