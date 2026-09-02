package gowild_projects

import (
	"strings"
	"testing"
	"time"
)

// finishRun opens and closes one run for the item with the given outcome,
// the way the runner books a job.
func (f *fixture) finishRun(agent, kind, itemID, outcome string) {
	f.t.Helper()
	run, err := f.s.StartRun(f.ctx, RunInput{Agent: agent, Kind: kind, ItemID: itemID})
	if err != nil {
		f.t.Fatal(err)
	}
	f.tick(time.Second)
	if _, err := f.s.FinishRun(f.ctx, run.ID, RunResult{Outcome: outcome, ExitStatus: "x"}); err != nil {
		f.t.Fatal(err)
	}
	f.tick(time.Second)
}

// Three consecutive failed runs hold the item: the queue moves on instead of
// retrying forever, the feed says why, and an unhold starts a clean slate.
func TestConsecutiveFailedRunsHoldTheItem(t *testing.T) {
	f := newFixture(t)
	f.agent("claude")
	f.agent("codex")
	f.project("EA")
	it := f.item("EA", "Fix the poller")

	for i := 1; i <= MaxItemFailures; i++ {
		f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
		f.finishRun("claude", JobImplement, it.ID, RunOutcomeCrashed)
		f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionRelease, Body: "crashed"})
	}
	got, _, err := f.s.GetItem(f.ctx, "EA-1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Held || got.Failures != MaxItemFailures {
		t.Fatalf("after %d crashes: held=%v failures=%d", MaxItemFailures, got.Held, got.Failures)
	}
	comments, err := f.s.ItemComments(f.ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range comments {
		if strings.Contains(c.Body, "the tracker holds it") && strings.Contains(c.Body, "pm unhold EA-1") {
			found = true
		}
	}
	if !found {
		t.Fatal("no comment says why the item was held")
	}
	// The queue skips it and a claim is refused.
	if job, err := f.s.NextFor(f.ctx, "claude"); err != nil || job != nil {
		t.Fatalf("held item still offered: %v %v", job, err)
	}
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim}, ErrInvalidTransition)
	// The owner lifts the hold: clean slate, work resumes.
	f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionUnhold})
	got, _, err = f.s.GetItem(f.ctx, "EA-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Held || got.Failures != 0 {
		t.Fatalf("after unhold: held=%v failures=%d", got.Held, got.Failures)
	}
	if job, err := f.s.NextFor(f.ctx, "claude"); err != nil || job == nil || job.Kind != JobImplement {
		t.Fatalf("unheld item not offered: %+v %v", job, err)
	}
}

// A run that settles resets the failure count.
func TestSettledRunResetsFailures(t *testing.T) {
	f := newFixture(t)
	f.agent("claude")
	f.project("EA")
	it := f.item("EA", "Fix the poller")
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	f.finishRun("claude", JobImplement, it.ID, RunOutcomeTimeout)
	got, _, _ := f.s.GetItem(f.ctx, "EA-1")
	if got.Failures != 1 {
		t.Fatalf("failures %d, want 1", got.Failures)
	}
	f.finishRun("claude", JobImplement, it.ID, "submitted")
	got, _, _ = f.s.GetItem(f.ctx, "EA-1")
	if got.Failures != 0 || got.Held {
		t.Fatalf("after a settled run: failures=%d held=%v", got.Failures, got.Held)
	}
}

// An approved item can be held — the escape hatch for a merge that cannot
// land — and the merge job is not offered while it is.
func TestHoldFromApproved(t *testing.T) {
	f := newFixture(t)
	f.agent("claude")
	f.agent("codex")
	f.project("EA")
	f.item("EA", "Fix the poller")
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Branch: "pm/ea-1-fix"})
	f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionClaim})
	f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionReview, Verdict: VerdictApprove, Body: "checked"})
	f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionApprove})

	if job, err := f.s.NextFor(f.ctx, "claude"); err != nil || job == nil || job.Kind != JobMerge {
		t.Fatalf("approved item not offered as a merge: %+v %v", job, err)
	}
	f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionHold})
	if job, err := f.s.NextFor(f.ctx, "claude"); err != nil || job != nil {
		t.Fatalf("held approved item still offered: %+v %v", job, err)
	}
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim}, ErrInvalidTransition)
	got, _, _ := f.s.GetItem(f.ctx, "EA-1")
	if got.Status != StatusApproved || !got.Held {
		t.Fatalf("held approved item: status=%s held=%v", got.Status, got.Held)
	}
	f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionUnhold})
	if job, err := f.s.NextFor(f.ctx, "claude"); err != nil || job == nil || job.Kind != JobMerge {
		t.Fatalf("unheld approved item not offered: %+v %v", job, err)
	}
}

// After request_changes the item is in progress with a zero lease; the owner
// can hand it to another worker without close-and-reopen.
func TestReassignAfterRequestChanges(t *testing.T) {
	f := newFixture(t)
	f.agent("claude")
	f.agent("codex")
	f.agent("third")
	f.project("EA")
	f.item("EA", "Fix the poller")
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Branch: "pm/ea-1-fix"})
	f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionClaim})
	f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionReview, Verdict: VerdictRequestChanges, Body: "the cursor is off by one"})

	to := "third"
	it, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Assignee: &to}, 0)
	if err != nil {
		t.Fatalf("reassign after request_changes: %v", err)
	}
	if it.Assignee != "third" || it.Status != StatusInProgress {
		t.Fatalf("reassigned item: %+v", it)
	}
	if job, err := f.s.NextFor(f.ctx, "third"); err != nil || job == nil || job.Kind != JobImplement {
		t.Fatalf("reassigned item not offered to the new worker: %+v %v", job, err)
	}
}

// The implementer of an in-flight item cannot be deleted: its merge job
// would match nobody and the item would sit in approved forever.
func TestDeleteAgentRefusedForInFlightImplementer(t *testing.T) {
	f := newFixture(t)
	f.agent("codex") // default
	f.agent("claude")
	f.project("EA")
	f.item("EA", "Fix the poller")
	to := "claude"
	if _, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Assignee: &to}, 0); err != nil {
		t.Fatal(err)
	}
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Branch: "pm/ea-1-fix"})
	if err := f.s.DeleteAgent(f.ctx, "claude"); err == nil {
		t.Fatal("deleting the implementer of an in-review item was allowed")
	}
}

// Reopening an item whose implementer no longer exists falls back to the
// default worker instead of assigning a ghost.
func TestReopenSkipsGhostImplementer(t *testing.T) {
	f := newFixture(t)
	f.agent("codex") // default
	f.agent("claude")
	f.project("EA")
	f.item("EA", "Fix the poller")
	to := "claude"
	if _, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Assignee: &to}, 0); err != nil {
		t.Fatal(err)
	}
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Branch: "pm/ea-1-fix"})
	f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionClaim})
	f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionReview, Verdict: VerdictApprove, Body: "checked"})
	f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionApprove})
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionComplete})
	if err := f.s.DeleteAgent(f.ctx, "claude"); err != nil {
		t.Fatalf("deleting the implementer of a done item: %v", err)
	}
	it := f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionReopen})
	if it.Assignee != "codex" {
		t.Fatalf("reopened item assigned to %q, want the default worker", it.Assignee)
	}
}
