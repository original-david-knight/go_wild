package gowild_projects

import (
	"errors"
	"strings"
	"testing"
)

// A raw feature is groomed before it is implemented: the groomer claims it,
// replaces the description with the spec, may hand it to another worker, and
// the item comes back open for its implement job.
func TestGroomLifecycle(t *testing.T) {
	f := newFixture(t)
	f.agent("claude")
	f.agent("codex")
	f.project("EA")
	it, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "podcast player loses position", Type: TypeFeature, Description: "it just does"})
	if err != nil {
		t.Fatal(err)
	}
	if !it.NeedsGroom {
		t.Fatal("a raw feature has to need grooming")
	}
	job, err := f.s.NextFor(f.ctx, "claude")
	if err != nil || job == nil || job.Kind != JobGroom {
		t.Fatalf("raw ticket not offered as a groom job: %+v %v", job, err)
	}
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	got := f.move("EA-1", TransitionInput{
		Actor: "claude", Action: ActionGroom, Body: "split nothing; handing to codex",
		Description: "Problem: position resets on route switch. Where: player_controller.dart:120. Fix: persist on pause.",
		Assignee:    "codex",
	})
	if got.Status != StatusOpen || got.NeedsGroom || got.Assignee != "codex" {
		t.Fatalf("groomed item: %+v", got)
	}
	if !strings.Contains(got.Description, "player_controller.dart:120") {
		t.Fatalf("description not replaced by the spec: %q", got.Description)
	}
	if job, err := f.s.NextFor(f.ctx, "codex"); err != nil || job == nil || job.Kind != JobImplement {
		t.Fatalf("groomed item not offered to the new worker as implement: %+v %v", job, err)
	}
	if job, err := f.s.NextFor(f.ctx, "claude"); err != nil || job != nil {
		t.Fatalf("the groomer still sees the handed-off item: %+v %v", job, err)
	}
}

// What gets groomed and what does not, and who may groom.
func TestGroomGates(t *testing.T) {
	f := newFixture(t)
	f.agent("claude")
	f.agent("codex")
	f.project("EA")
	chore, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "delete dead code", Type: TypeChore})
	if err != nil || chore.NeedsGroom {
		t.Fatalf("a chore was marked for grooming: %+v %v", chore, err)
	}
	specced, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "specced bug", Type: TypeBug, Specced: true})
	if err != nil || specced.NeedsGroom {
		t.Fatalf("a specced bug was marked for grooming: %+v %v", specced, err)
	}
	raw, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "raw bug", Type: TypeBug})
	if err != nil || !raw.NeedsGroom {
		t.Fatalf("a raw bug was not marked for grooming: %+v %v", raw, err)
	}
	f.move("EA-3", TransitionInput{Actor: "claude", Action: ActionClaim})
	// No spec, wrong actor, the owner: all refused.
	f.refuse("EA-3", TransitionInput{Actor: "claude", Action: ActionGroom}, ErrValidation)
	f.refuse("EA-3", TransitionInput{Actor: "codex", Action: ActionGroom, Description: "spec"}, ErrForbidden)
	f.refuse("EA-3", TransitionInput{Actor: ActorOwner, Action: ActionGroom, Description: "spec"}, ErrForbidden)
	// An unknown worker in the hand-off is refused.
	f.refuse("EA-3", TransitionInput{Actor: "claude", Action: ActionGroom, Description: "spec", Assignee: "ghost"}, ErrValidation)
	// A groom on an already-groomed item is refused.
	f.move("EA-3", TransitionInput{Actor: "claude", Action: ActionGroom, Description: "the spec"})
	f.move("EA-3", TransitionInput{Actor: "claude", Action: ActionClaim})
	if _, err := f.s.Transition(f.ctx, "EA-3", TransitionInput{Actor: "claude", Action: ActionGroom, Description: "again"}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("second groom: %v", err)
	}
}

// A crashed groom run resumes as a groom job, and a groomer's question goes
// through the normal blocked path with the raw ticket still ungroomed.
func TestGroomCrashAndBlock(t *testing.T) {
	f := newFixture(t)
	f.agent("claude")
	f.project("EA")
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "vague idea", Type: TypeFeature}); err != nil {
		t.Fatal(err)
	}
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionRelease, Body: "crashed"})
	if job, err := f.s.NextFor(f.ctx, "claude"); err != nil || job == nil || job.Kind != JobGroom {
		t.Fatalf("released raw ticket not re-offered as groom: %+v %v", job, err)
	}
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionBlock, Body: "did you mean the queue or the player?"})
	it := f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionReopen})
	if !it.NeedsGroom {
		t.Fatal("reopened raw ticket lost its grooming")
	}
}
