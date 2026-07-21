package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	obj "github.com/original-david-knight/go_wild/objectives"
)

func TestSendMissionMessageReactivatesCompletedMission(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	company, err := svc.CreateCompany(ctx, "Mission Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	store := obj.NewObjectiveStore(db, company.ID)
	activity := obj.NewActivityStore(db, company.ID)

	mission := &obj.Objective{
		Title:         "Create a profitable shopify site",
		Status:        obj.StatusCompleted,
		ScheduleType:  obj.ScheduleContinuous,
		CompletedAt:   time.Now().UTC(),
		CooldownUntil: time.Now().UTC().Add(10 * time.Minute),
	}
	if err := store.Create(ctx, mission); err != nil {
		t.Fatalf("store.Create mission failed: %v", err)
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(map[string]any{
		"content": "  Continue by improving catalog depth, checking sales daily, and running marketing.  ",
	}); err != nil {
		t.Fatalf("encode request body failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/companies/"+company.ID+"/missions/"+mission.ID+"/message", &body)
	rec := httptest.NewRecorder()
	h.handleCompany(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var directive obj.ActivityEvent
	if err := json.NewDecoder(rec.Body).Decode(&directive); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if directive.EventType != "user_directive" {
		t.Fatalf("expected event_type user_directive, got %q", directive.EventType)
	}
	if directive.Summary != "Continue by improving catalog depth, checking sales daily, and running marketing." {
		t.Fatalf("expected trimmed guidance summary, got %q", directive.Summary)
	}

	updated, err := store.Get(ctx, mission.ID)
	if err != nil {
		t.Fatalf("store.Get updated mission failed: %v", err)
	}
	if updated.Status != obj.StatusActive {
		t.Fatalf("expected mission status %q after guidance, got %q", obj.StatusActive, updated.Status)
	}
	if !updated.CompletedAt.IsZero() {
		t.Fatalf("expected completed_at to be cleared, got %s", updated.CompletedAt)
	}
	if !updated.CooldownUntil.IsZero() {
		t.Fatalf("expected cooldown_until to be cleared, got %s", updated.CooldownUntil)
	}

	events, err := activity.GetEvents(ctx, mission.ID, 20)
	if err != nil {
		t.Fatalf("activity.GetEvents failed: %v", err)
	}
	foundDirective := false
	foundResume := false
	for _, ev := range events {
		if ev.EventType == "user_directive" {
			foundDirective = true
		}
		if ev.EventType == "objective_resumed" {
			foundResume = true
		}
	}
	if !foundDirective {
		t.Fatalf("expected user_directive event in activity log")
	}
	if !foundResume {
		t.Fatalf("expected objective_resumed event in activity log")
	}
}

func TestSendMissionMessageKeepsPausedMissionPaused(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	company, err := svc.CreateCompany(ctx, "Paused Mission Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	store := obj.NewObjectiveStore(db, company.ID)

	cooldown := time.Now().UTC().Add(30 * time.Minute)
	mission := &obj.Objective{
		Title:         "Paused mission",
		Status:        obj.StatusPaused,
		ScheduleType:  obj.ScheduleContinuous,
		CooldownUntil: cooldown,
	}
	if err := store.Create(ctx, mission); err != nil {
		t.Fatalf("store.Create mission failed: %v", err)
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(map[string]any{
		"content": "Please add a marketing campaign when resumed",
	}); err != nil {
		t.Fatalf("encode request body failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/companies/"+company.ID+"/missions/"+mission.ID+"/message", &body)
	rec := httptest.NewRecorder()
	h.handleCompany(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
	}

	updated, err := store.Get(ctx, mission.ID)
	if err != nil {
		t.Fatalf("store.Get updated mission failed: %v", err)
	}
	if updated.Status != obj.StatusPaused {
		t.Fatalf("expected paused mission to stay %q, got %q", obj.StatusPaused, updated.Status)
	}
	if updated.CooldownUntil.IsZero() {
		t.Fatalf("expected cooldown_until to remain set for paused mission")
	}
}

func TestSendMissionMessageRejectsBlankContent(t *testing.T) {
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	h := NewHandlers(svc, nil, nil, nil, nil)
	ctx := context.Background()

	company, err := svc.CreateCompany(ctx, "Blank Guidance Co", "", "")
	if err != nil {
		t.Fatalf("CreateCompany failed: %v", err)
	}

	store := obj.NewObjectiveStore(db, company.ID)
	mission := &obj.Objective{
		Title:        "Mission",
		Status:       obj.StatusActive,
		ScheduleType: obj.ScheduleContinuous,
	}
	if err := store.Create(ctx, mission); err != nil {
		t.Fatalf("store.Create mission failed: %v", err)
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(map[string]any{
		"content": "   ",
	}); err != nil {
		t.Fatalf("encode request body failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/companies/"+company.ID+"/missions/"+mission.ID+"/message", &body)
	rec := httptest.NewRecorder()
	h.handleCompany(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}
