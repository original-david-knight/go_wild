//go:build docker_refresh
// +build docker_refresh

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRefreshAgentImage_Docker(t *testing.T) {
	dm := dockerManagerOrSkip(t)
	t.Cleanup(func() {
		_ = dm.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	hub := NewSessionHub(dm, svc)
	h := NewHandlers(svc, dm, hub, nil, nil)

	agentID := newTestAgentID(t)
	agent, err := svc.CreateAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	if err := dm.BuildImage(ctx); err != nil {
		t.Fatalf("BuildImage failed: %v", err)
	}

	if err := dm.EnsureVolume(ctx, agent.ID); err != nil {
		t.Fatalf("EnsureVolume failed: %v", err)
	}

	if err := dm.CreateContainer(ctx, agent); err != nil {
		t.Fatalf("CreateContainer failed: %v", err)
	}
	t.Cleanup(func() {
		_ = dm.RemoveContainer(context.Background(), agent.ID)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/agents/"+agentID+"/refresh-image", nil)
	rec := httptest.NewRecorder()
	h.refreshAgentImage(rec, req, agentID)

	if rec.Code != http.StatusOK {
		t.Fatalf("refreshAgentImage expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "refreshed" {
		t.Fatalf("unexpected status: %q", resp["status"])
	}

	if status := dm.ContainerStatus(ctx, agentID); status == "" {
		t.Fatalf("expected container to exist after refresh")
	}
}
