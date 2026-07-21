package objectives

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestWorkQueue_PushPop(t *testing.T) {
	q := newWorkQueue()

	now := time.Now()
	q.push(&workItem{Objective: &Objective{ID: "low", Title: "Low"}, Priority: 10, EnqueuedAt: now})
	q.push(&workItem{Objective: &Objective{ID: "high", Title: "High"}, Priority: 1, EnqueuedAt: now})
	q.push(&workItem{Objective: &Objective{ID: "mid", Title: "Mid"}, Priority: 5, EnqueuedAt: now})

	if q.count() != 3 {
		t.Fatalf("expected 3 items, got %d", q.count())
	}

	// Should pop in priority order: 1, 5, 10
	item := q.pop()
	if item == nil || item.Objective.ID != "high" {
		t.Fatalf("expected high priority item, got %v", item)
	}

	item = q.pop()
	if item == nil || item.Objective.ID != "mid" {
		t.Fatalf("expected mid priority item, got %v", item)
	}

	item = q.pop()
	if item == nil || item.Objective.ID != "low" {
		t.Fatalf("expected low priority item, got %v", item)
	}

	item = q.pop()
	if item != nil {
		t.Fatalf("expected nil from empty queue, got %v", item)
	}
}

func TestWorkQueue_PushDuplicate(t *testing.T) {
	q := newWorkQueue()
	now := time.Now()

	q.push(&workItem{Objective: &Objective{ID: "a"}, Priority: 1, EnqueuedAt: now})
	q.push(&workItem{Objective: &Objective{ID: "a"}, Priority: 1, EnqueuedAt: now})

	if q.count() != 1 {
		t.Fatalf("expected 1 item (dup skipped), got %d", q.count())
	}
}

func TestWorkQueue_SamePriorityFIFO(t *testing.T) {
	q := newWorkQueue()

	t1 := time.Now()
	t2 := t1.Add(time.Second)

	q.push(&workItem{Objective: &Objective{ID: "first"}, Priority: 1, EnqueuedAt: t1})
	q.push(&workItem{Objective: &Objective{ID: "second"}, Priority: 1, EnqueuedAt: t2})

	item := q.pop()
	if item.Objective.ID != "first" {
		t.Fatalf("expected FIFO for same priority, got %s", item.Objective.ID)
	}
}

func TestWorkQueue_Remove(t *testing.T) {
	q := newWorkQueue()
	now := time.Now()

	q.push(&workItem{Objective: &Objective{ID: "a"}, Priority: 1, EnqueuedAt: now})
	q.push(&workItem{Objective: &Objective{ID: "b"}, Priority: 2, EnqueuedAt: now})
	q.push(&workItem{Objective: &Objective{ID: "c"}, Priority: 3, EnqueuedAt: now})

	q.remove("b")

	if q.count() != 2 {
		t.Fatalf("expected 2 items after remove, got %d", q.count())
	}

	item := q.pop()
	if item.Objective.ID != "a" {
		t.Fatalf("expected a, got %s", item.Objective.ID)
	}

	item = q.pop()
	if item.Objective.ID != "c" {
		t.Fatalf("expected c, got %s", item.Objective.ID)
	}
}

func TestMatchesCron(t *testing.T) {
	tests := []struct {
		name  string
		expr  string
		time  time.Time
		match bool
	}{
		{
			name:  "all wildcards",
			expr:  "* * * * *",
			time:  time.Date(2026, 3, 1, 14, 30, 0, 0, time.UTC),
			match: true,
		},
		{
			name:  "exact minute and hour",
			expr:  "30 14 * * *",
			time:  time.Date(2026, 3, 1, 14, 30, 0, 0, time.UTC),
			match: true,
		},
		{
			name:  "wrong minute",
			expr:  "0 14 * * *",
			time:  time.Date(2026, 3, 1, 14, 30, 0, 0, time.UTC),
			match: false,
		},
		{
			name:  "specific day of month",
			expr:  "0 0 1 * *",
			time:  time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			match: true,
		},
		{
			name:  "wrong day of month",
			expr:  "0 0 15 * *",
			time:  time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			match: false,
		},
		{
			name:  "specific month",
			expr:  "0 0 * 3 *",
			time:  time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			match: true,
		},
		{
			name:  "wrong month",
			expr:  "0 0 * 6 *",
			time:  time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			match: false,
		},
		{
			name:  "day of week sunday=0",
			expr:  "* * * * 0",
			time:  time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), // 2026-03-01 is Sunday
			match: true,
		},
		{
			name:  "step value */15",
			expr:  "*/15 * * * *",
			time:  time.Date(2026, 3, 1, 0, 30, 0, 0, time.UTC),
			match: true,
		},
		{
			name:  "step value */15 no match",
			expr:  "*/15 * * * *",
			time:  time.Date(2026, 3, 1, 0, 7, 0, 0, time.UTC),
			match: false,
		},
		{
			name:  "comma-separated values",
			expr:  "0,15,30,45 * * * *",
			time:  time.Date(2026, 3, 1, 0, 15, 0, 0, time.UTC),
			match: true,
		},
		{
			name:  "comma-separated no match",
			expr:  "0,15,30,45 * * * *",
			time:  time.Date(2026, 3, 1, 0, 10, 0, 0, time.UTC),
			match: false,
		},
		{
			name:  "invalid expression",
			expr:  "invalid",
			time:  time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			match: false,
		},
		{
			name:  "too few fields",
			expr:  "0 0 *",
			time:  time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			match: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesCron(tt.expr, tt.time)
			if got != tt.match {
				t.Errorf("matchesCron(%q, %v) = %v, want %v", tt.expr, tt.time, got, tt.match)
			}
		})
	}
}

func TestScheduler_CooldownEnforcement(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	// Create an objective with a cooldown in the future
	obj := &Objective{
		Title:         "Cooled Down",
		ScheduleType:  ScheduleCron,
		ScheduleCron:  "* * * * *",
		CooldownUntil: time.Now().UTC().Add(1 * time.Hour),
	}
	if err := store.Create(ctx, obj); err != nil {
		t.Fatalf("create: %v", err)
	}

	activity := NewActivityStore(db, "")
	scheduler := NewScheduler(store, activity, nil, nil, Config{MaxConcurrency: 1})

	// Run a cron scan — the objective should NOT be enqueued due to cooldown
	scheduler.scanCronObjectives(ctx)

	if scheduler.queue.count() != 0 {
		t.Fatalf("expected 0 items (cooldown active), got %d", scheduler.queue.count())
	}

	// Now set cooldown to past and scan again
	obj.CooldownUntil = time.Now().UTC().Add(-1 * time.Hour)
	if err := store.Update(ctx, obj); err != nil {
		t.Fatalf("update: %v", err)
	}

	scheduler.scanCronObjectives(ctx)

	if scheduler.queue.count() != 1 {
		t.Fatalf("expected 1 item (cooldown expired), got %d", scheduler.queue.count())
	}
}

func TestScheduler_FailedLeavesBlockMissionAndEscalate(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	activity := NewActivityStore(db, "")
	ctx := context.Background()

	root := &Objective{
		Title:        "Root Mission",
		Status:       StatusActive,
		ScheduleType: ScheduleContinuous,
	}
	if err := store.Create(ctx, root); err != nil {
		t.Fatalf("create root: %v", err)
	}

	failedLeaf := &Objective{
		Title:      "Failed leaf",
		ParentID:   root.ID,
		Status:     StatusFailed,
		LastResult: "permanent error",
	}
	if err := store.Create(ctx, failedLeaf); err != nil {
		t.Fatalf("create failed leaf: %v", err)
	}

	scheduler := NewScheduler(store, activity, nil, nil, Config{MaxConcurrency: 1})
	scheduler.executeWorkItem(ctx, &workItem{
		Objective:  root,
		Priority:   1,
		EnqueuedAt: time.Now().UTC(),
	})

	updatedRoot, err := store.Get(ctx, root.ID)
	if err != nil {
		t.Fatalf("get root: %v", err)
	}
	if updatedRoot.Status != StatusBlocked {
		t.Fatalf("expected root status blocked, got %s", updatedRoot.Status)
	}
	if !strings.Contains(strings.ToLower(updatedRoot.LastResult), "failed") {
		t.Fatalf("expected root last_result to mention failures, got %q", updatedRoot.LastResult)
	}

	escs := store.GetEscalations(ctx, root.ID)
	if len(escs) == 0 {
		t.Fatal("expected escalation to be created")
	}
}

func TestScheduler_GracefulShutdown(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	activity := NewActivityStore(db, "")

	scheduler := NewScheduler(store, activity, nil, nil, Config{MaxConcurrency: 1})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- scheduler.Run(ctx)
	}()

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Signal stop
	scheduler.Stop()

	// Wait for done
	select {
	case <-scheduler.done:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not stop within timeout")
	}

	err := <-errCh
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestGetByScheduleType(t *testing.T) {
	db := setupTestDB(t)
	store := NewObjectiveStore(db, "")
	ctx := context.Background()

	store.Create(ctx, &Objective{Title: "One-shot", ScheduleType: ScheduleOneShot})
	store.Create(ctx, &Objective{Title: "Cron 1", ScheduleType: ScheduleCron, ScheduleCron: "0 * * * *"})
	store.Create(ctx, &Objective{Title: "Cron 2", ScheduleType: ScheduleCron, ScheduleCron: "*/5 * * * *"})
	store.Create(ctx, &Objective{Title: "Continuous", ScheduleType: ScheduleContinuous})

	crons, err := store.getByScheduleType(ctx, ScheduleCron)
	if err != nil {
		t.Fatalf("get by schedule type: %v", err)
	}
	if len(crons) != 2 {
		t.Fatalf("expected 2 cron objectives, got %d", len(crons))
	}
}
