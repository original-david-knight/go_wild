package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/original-david-knight/go_wild/apps/agent_manager/dockermgr"
)

func requireDockerImageOrSkip(t *testing.T, dm *dockermgr.DockerManager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := dm.Ping(ctx); err != nil {
		t.Skipf("docker not available: %v", err)
	}
	if !dm.ImageExists(ctx) {
		t.Skipf("docker image %s missing", dockermgr.AgentImageName)
	}
	stale, _, _, err := dm.ImageStale(ctx, dockermgr.AgentImageName)
	if err != nil {
		t.Skipf("failed to check image staleness: %v", err)
	}
	if stale {
		t.Skipf("docker image %s is stale; would rebuild", dockermgr.AgentImageName)
	}
}

func newTestAgentID(t *testing.T) string {
	t.Helper()
	base := strings.ToLower(t.Name())
	buf := make([]byte, 0, len(base))
	for i := 0; i < len(base); i++ {
		c := base[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			buf = append(buf, c)
			continue
		}
		if c == '-' {
			buf = append(buf, c)
			continue
		}
		buf = append(buf, '-')
	}
	return fmt.Sprintf("test-%s-%d", string(buf), time.Now().UTC().UnixNano())
}

func TestLifecycleStartStopRestart(t *testing.T) {
	dm := dockerManagerOrSkip(t)
	requireDockerImageOrSkip(t, dm)
	t.Cleanup(func() {
		_ = dm.Close()
	})

	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	hub := NewSessionHub(dm, svc)
	h := NewHandlers(svc, dm, hub, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	agentID := newTestAgentID(t)
	agent, err := svc.CreateAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	if err := dm.EnsureVolume(ctx, agent.ID); err != nil {
		t.Fatalf("EnsureVolume failed: %v", err)
	}

	t.Cleanup(func() {
		_ = dm.RemoveContainer(context.Background(), agent.ID)
	})

	// start
	startReq := httptest.NewRequest(http.MethodPost, "/api/agents/"+agentID+"/start", nil)
	startRec := httptest.NewRecorder()
	h.startAgent(startRec, startReq, agentID)
	if startRec.Code != http.StatusOK {
		t.Fatalf("startAgent expected 200, got %d: %s", startRec.Code, startRec.Body.String())
	}
	var startResp map[string]string
	_ = json.NewDecoder(startRec.Body).Decode(&startResp)
	if startResp["status"] != "started" && startResp["status"] != "already running" {
		t.Fatalf("unexpected start status: %q", startResp["status"])
	}

	// stop
	stopReq := httptest.NewRequest(http.MethodPost, "/api/agents/"+agentID+"/stop", nil)
	stopRec := httptest.NewRecorder()
	h.stopAgent(stopRec, stopReq, agentID)
	if stopRec.Code != http.StatusOK {
		t.Fatalf("stopAgent expected 200, got %d: %s", stopRec.Code, stopRec.Body.String())
	}
	var stopResp map[string]string
	_ = json.NewDecoder(stopRec.Body).Decode(&stopResp)
	if stopResp["status"] != "stopped" {
		t.Fatalf("unexpected stop status: %q", stopResp["status"])
	}

	// restart
	restartReq := httptest.NewRequest(http.MethodPost, "/api/agents/"+agentID+"/restart", nil)
	restartRec := httptest.NewRecorder()
	h.restartAgent(restartRec, restartReq, agentID)
	if restartRec.Code != http.StatusOK {
		t.Fatalf("restartAgent expected 200, got %d: %s", restartRec.Code, restartRec.Body.String())
	}
	var restartResp map[string]string
	_ = json.NewDecoder(restartRec.Body).Decode(&restartResp)
	if restartResp["status"] != "restarted" {
		t.Fatalf("unexpected restart status: %q", restartResp["status"])
	}
}
