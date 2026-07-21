package objectives

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/original-david-knight/go_wild/data"
)

func setupAPIServer(t *testing.T) (*APIServer, *httptest.Server) {
	t.Helper()
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	if err := gowild_data.AddAllTables(db); err != nil {
		t.Fatalf("add tables: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	store := NewObjectiveStore(db, "")
	activity := NewActivityStore(db, "")
	api := NewAPIServer(store, activity)
	ts := httptest.NewServer(api)
	t.Cleanup(ts.Close)

	return api, ts
}

func TestHealth(t *testing.T) {
	_, ts := setupAPIServer(t)

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}
}

func TestCreateAndListObjectives(t *testing.T) {
	_, ts := setupAPIServer(t)

	// Create an objective
	payload := `{"title":"Test Mission","description":"A test mission"}`
	resp, err := http.Post(ts.URL+"/api/objectives", "application/json", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("create objective: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var created Objective
	json.NewDecoder(resp.Body).Decode(&created)
	if created.Title != "Test Mission" {
		t.Fatalf("expected title 'Test Mission', got %q", created.Title)
	}
	if created.ID == "" {
		t.Fatal("expected ID to be set")
	}

	// List objectives
	resp2, err := http.Get(ts.URL + "/api/objectives")
	if err != nil {
		t.Fatalf("list objectives: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}

	var rollups []StatusRollup
	json.NewDecoder(resp2.Body).Decode(&rollups)
	if len(rollups) != 1 {
		t.Fatalf("expected 1 rollup, got %d", len(rollups))
	}
	if rollups[0].Objective.Title != "Test Mission" {
		t.Fatalf("expected title 'Test Mission', got %q", rollups[0].Objective.Title)
	}
}

func TestGetObjectiveWithChildren(t *testing.T) {
	api, ts := setupAPIServer(t)
	ctx := context.Background()

	// Create parent
	parent := &Objective{Title: "Parent", Priority: 1}
	api.store.Create(ctx, parent)

	// Create children
	child := &Objective{Title: "Child", ParentID: parent.ID, Priority: 1, Depth: 1}
	api.store.Create(ctx, child)

	// GET single objective
	resp, err := http.Get(ts.URL + "/api/objectives/" + parent.ID)
	if err != nil {
		t.Fatalf("get objective: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Objective      Objective       `json:"objective"`
		Children       []Objective     `json:"children"`
		RecentActivity []ActivityEvent `json:"recent_activity"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Objective.Title != "Parent" {
		t.Fatalf("expected 'Parent', got %q", result.Objective.Title)
	}
	if len(result.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(result.Children))
	}
	if result.Children[0].Title != "Child" {
		t.Fatalf("expected child 'Child', got %q", result.Children[0].Title)
	}
}

func TestPauseAndResume(t *testing.T) {
	api, ts := setupAPIServer(t)
	ctx := context.Background()

	obj := &Objective{Title: "Pausable", Priority: 1, Status: StatusActive}
	api.store.Create(ctx, obj)
	// Force active status after create (which defaults to pending)
	obj.Status = StatusActive
	api.store.Update(ctx, obj)

	// Pause
	resp, err := http.Post(ts.URL+"/api/objectives/"+obj.ID+"/pause", "application/json", nil)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var paused Objective
	json.NewDecoder(resp.Body).Decode(&paused)
	if paused.Status != StatusPaused {
		t.Fatalf("expected paused status, got %s", paused.Status)
	}

	// Resume
	resp2, err := http.Post(ts.URL+"/api/objectives/"+obj.ID+"/resume", "application/json", nil)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}

	var resumed Objective
	json.NewDecoder(resp2.Body).Decode(&resumed)
	if resumed.Status != StatusActive {
		t.Fatalf("expected active status, got %s", resumed.Status)
	}
}

func TestGetActivity(t *testing.T) {
	api, ts := setupAPIServer(t)
	ctx := context.Background()

	// Log some events
	api.activity.logTaskStarted(ctx, "obj-1", "Starting task")
	api.activity.logTaskCompleted(ctx, "obj-1", "Task done", nil)

	// GET all activity
	resp, err := http.Get(ts.URL + "/api/activity")
	if err != nil {
		t.Fatalf("get activity: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var events []ActivityEvent
	json.NewDecoder(resp.Body).Decode(&events)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	// GET filtered activity
	resp2, err := http.Get(ts.URL + "/api/activity?objective_id=obj-1&limit=1")
	if err != nil {
		t.Fatalf("get filtered activity: %v", err)
	}
	defer resp2.Body.Close()

	var filtered []ActivityEvent
	json.NewDecoder(resp2.Body).Decode(&filtered)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 event with limit=1, got %d", len(filtered))
	}
}

func TestGetStatus(t *testing.T) {
	api, ts := setupAPIServer(t)
	ctx := context.Background()

	// Create objectives with different statuses
	obj1 := &Objective{Title: "Active One", Priority: 1}
	api.store.Create(ctx, obj1)
	obj1.Status = StatusActive
	api.store.Update(ctx, obj1)

	api.store.Create(ctx, &Objective{Title: "Pending One", Priority: 2})

	resp, err := http.Get(ts.URL + "/api/status")
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var status map[string]any
	json.NewDecoder(resp.Body).Decode(&status)

	counts := status["objective_counts"].(map[string]any)
	if counts["active"].(float64) != 1 {
		t.Fatalf("expected 1 active, got %v", counts["active"])
	}
	if counts["pending"].(float64) != 1 {
		t.Fatalf("expected 1 pending, got %v", counts["pending"])
	}
	if status["total_objectives"].(float64) != 2 {
		t.Fatalf("expected 2 total, got %v", status["total_objectives"])
	}
}

func TestUpdateObjective(t *testing.T) {
	api, ts := setupAPIServer(t)
	ctx := context.Background()

	obj := &Objective{Title: "Original", Description: "Original desc", Priority: 1}
	api.store.Create(ctx, obj)

	payload := `{"title":"Updated","description":"Updated desc"}`
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/objectives/"+obj.ID, bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var updated Objective
	json.NewDecoder(resp.Body).Decode(&updated)
	if updated.Title != "Updated" {
		t.Fatalf("expected 'Updated', got %q", updated.Title)
	}
	if updated.Description != "Updated desc" {
		t.Fatalf("expected 'Updated desc', got %q", updated.Description)
	}
}

func TestEscalations(t *testing.T) {
	api, ts := setupAPIServer(t)
	ctx := context.Background()

	// Create an escalation directly
	esc := &Escalation{
		ID:          "esc-1",
		ObjectiveID: "obj-1",
		Question:    "What should I do?",
		Context:     "I'm stuck",
		Severity:    SeverityWarning,
		Status:      EscalationPending,
	}
	api.store.db.Table(Escalation{}).Insert(ctx, esc)

	// List escalations
	resp, err := http.Get(ts.URL + "/api/escalations")
	if err != nil {
		t.Fatalf("get escalations: %v", err)
	}
	defer resp.Body.Close()

	var escalations []Escalation
	json.NewDecoder(resp.Body).Decode(&escalations)
	if len(escalations) != 1 {
		t.Fatalf("expected 1 escalation, got %d", len(escalations))
	}

	// Resolve the escalation
	payload := `{"resolution":"Just do it"}`
	resp2, err := http.Post(ts.URL+"/api/escalations/esc-1/resolve", "application/json", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}

	var resolved Escalation
	json.NewDecoder(resp2.Body).Decode(&resolved)
	if resolved.Status != EscalationResolved {
		t.Fatalf("expected resolved, got %s", resolved.Status)
	}
	if resolved.Resolution != "Just do it" {
		t.Fatalf("expected resolution 'Just do it', got %q", resolved.Resolution)
	}
}
