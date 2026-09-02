package gowild_projects

import (
	"errors"
	"testing"
	"time"
)

// The claim gates: a held item and an item waiting on another are open on
// the board but never offered by the queue, and a claim is refused with the
// reason. The transitions that settle the dependency or lift the hold put
// the item back in front of its worker.

func TestAfterGateBlocksUntilSettled(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	first, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "first", Assignee: "codex", Specced: true})
	if err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	second, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "second", Assignee: "claude", After: "ea-1", Specced: true})
	if err != nil {
		t.Fatal(err)
	}
	if second.After != "EA-1" {
		t.Fatalf("after not canonicalised: %q", second.After)
	}
	if job, _ := f.s.NextFor(f.ctx, "claude"); job != nil {
		t.Fatalf("gated item offered: %+v", job)
	}
	f.refuse("EA-2", TransitionInput{Actor: "claude", Action: ActionClaim}, ErrInvalidTransition)
	// Closing the dependency settles it as well as completing it would.
	f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionClose})
	job, err := f.s.NextFor(f.ctx, "claude")
	if err != nil || job == nil || job.Kind != JobImplement || job.Item.ID != second.ID {
		t.Fatalf("settled dependency not offered: %v %+v", err, job)
	}
	it := f.move("EA-2", TransitionInput{Actor: "claude", Action: ActionClaim})
	if it.Status != StatusInProgress {
		t.Fatalf("claim after settle: %+v", it)
	}
	_ = first
}

func TestAfterGateLiftsWhenTheDependencyIsDone(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "dep", Assignee: "claude", Specced: true}); err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "waiter", Assignee: "claude", After: "EA-1", Specced: true}); err != nil {
		t.Fatal(err)
	}
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	// The waiter is not what a free check-in picks up while the dependency
	// is live work.
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Branch: "pm/ea-1"})
	if job, _ := f.s.NextFor(f.ctx, "claude"); job != nil {
		t.Fatalf("waiter offered while dependency in review: %+v", job)
	}
	f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionReview, Verdict: VerdictApprove, Body: "fine"})
	f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionApprove})
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionComplete})
	job, err := f.s.NextFor(f.ctx, "claude")
	if err != nil || job == nil || job.Kind != JobImplement || job.Item.Number != 2 {
		t.Fatalf("waiter not offered once dependency done: %v %+v", err, job)
	}
}

func TestAfterValidation(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	f.project("OM")
	f.item("EA", "one")
	f.item("OM", "elsewhere")
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "x", After: "EA-9"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown after: %v", err)
	}
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "x", After: "OM-1"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("cross-project after: %v", err)
	}
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "x", After: "nonsense"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("unparseable after: %v", err)
	}
	// EA-2 waits on EA-1; pointing EA-1 at EA-2 would close the loop.
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "two", After: "EA-1"}); err != nil {
		t.Fatal(err)
	}
	loop := "EA-2"
	if _, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{After: &loop}, 0); !errors.Is(err, ErrValidation) {
		t.Fatalf("cycle: %v", err)
	}
	self := "EA-1"
	if _, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{After: &self}, 0); !errors.Is(err, ErrValidation) {
		t.Fatalf("self: %v", err)
	}
	clear := ""
	it, err := f.s.UpdateItem(f.ctx, "EA-2", ItemPatch{After: &clear}, 0)
	if err != nil || it.After != "" {
		t.Fatalf("clear after: %v %+v", err, it)
	}
}

func TestHold(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	parked, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "parked", Assignee: "claude", Held: true})
	if err != nil || !parked.Held {
		t.Fatalf("create held: %v %+v", err, parked)
	}
	f.tick(time.Second)
	if job, _ := f.s.NextFor(f.ctx, "claude"); job != nil {
		t.Fatalf("held item offered: %+v", job)
	}
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim}, ErrInvalidTransition)
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionUnhold}, ErrForbidden)
	f.refuse("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionHold}, ErrInvalidTransition) // already held
	it := f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionUnhold})
	if it.Held || it.Status != StatusOpen {
		t.Fatalf("unhold: %+v", it)
	}
	f.refuse("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionUnhold}, ErrInvalidTransition) // not held
	job, err := f.s.NextFor(f.ctx, "claude")
	if err != nil || job == nil || job.Item.ID != parked.ID {
		t.Fatalf("unheld item not offered: %v %+v", err, job)
	}
	// The hold and the lift are in the feed.
	comments, err := f.s.ItemComments(f.ctx, parked.ID)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]bool{}
	for _, c := range comments {
		if c.Kind == CommentKindTransition {
			actions[c.Action] = true
		}
	}
	if !actions[ActionUnhold] {
		t.Fatalf("unhold not in the feed: %+v", actions)
	}
	// Live work is not holdable.
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	f.refuse("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionHold}, ErrInvalidTransition)
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionHold}, ErrForbidden)
}

func TestHoldSurvivesCloseAndReopen(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "parked", Held: true}); err != nil {
		t.Fatal(err)
	}
	f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionClose})
	f.refuse("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionUnhold}, ErrInvalidTransition)
	it := f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionReopen})
	if !it.Held || it.Status != StatusOpen {
		t.Fatalf("reopen kept the park: %+v", it)
	}
	if job, _ := f.s.NextFor(f.ctx, "claude"); job != nil {
		t.Fatalf("reopened held item offered: %+v", job)
	}
}

func TestLabels(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "tagged", Label: " debt-audit "}); err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	f.item("EA", "plain")
	rows, err := f.s.ListItems(f.ctx, ItemFilter{ProjectKey: "EA", Label: "debt-audit"})
	if err != nil || len(rows) != 1 || rows[0].Label != "debt-audit" {
		t.Fatalf("label filter: %v %+v", err, rows)
	}
	long := make([]rune, 65)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "x", Label: string(long)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("long label: %v", err)
	}
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "x", Label: "two\nlines"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("multiline label: %v", err)
	}
	tag, clear := "later", ""
	it, err := f.s.UpdateItem(f.ctx, "EA-2", ItemPatch{Label: &tag}, 0)
	if err != nil || it.Label != "later" {
		t.Fatalf("set label: %v %+v", err, it)
	}
	it, err = f.s.UpdateItem(f.ctx, "EA-2", ItemPatch{Label: &clear}, 0)
	if err != nil || it.Label != "" {
		t.Fatalf("clear label: %v %+v", err, it)
	}
}

func TestWaitWakesWhenAGateLifts(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "parked", Assignee: "claude", Held: true}); err != nil {
		t.Fatal(err)
	}
	ch := f.wait(f.ctx, "claude", time.Minute)
	time.Sleep(parked)
	f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionUnhold})
	if r := f.settle(ch); r.err != nil || !r.ready {
		t.Fatalf("wait after unhold: %+v", r)
	}
}

func TestWaitWakesWhenADependencySettles(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	f.item("EA", "dep")
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "waiter", Assignee: "codex", After: "EA-1"}); err != nil {
		t.Fatal(err)
	}
	ch := f.wait(f.ctx, "codex", time.Minute)
	time.Sleep(parked)
	f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionClose})
	if r := f.settle(ch); r.err != nil || !r.ready {
		t.Fatalf("wait after settle: %+v", r)
	}
}
