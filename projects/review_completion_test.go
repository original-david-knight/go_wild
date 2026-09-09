package gowild_projects

import (
	"errors"
	"testing"
)

func TestExternalReviewCompletionPreservesConcurrentWork(t *testing.T) {
	f := newFixture(t)
	f.project("EA")
	f.workers()
	it, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Type: TypeCodeReview, Title: "Review", PRURL: reviewedPR})
	if err != nil {
		t.Fatal(err)
	}
	active := f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	if _, err := f.s.CompleteReview(f.ctx, it.ID, active.Revision, "Submitted on GitHub"); !errors.Is(err, ErrConflict) {
		t.Fatalf("cleared running review: %v", err)
	}
	ready := f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Body: "Review ready"})
	if _, err := f.s.CompleteReview(f.ctx, it.ID, active.Revision, "Submitted on GitHub"); !errors.Is(err, ErrConflict) {
		t.Fatalf("accepted stale observation: %v", err)
	}
	done, err := f.s.CompleteReview(f.ctx, it.ID, ready.Revision, "Submitted on GitHub")
	if err != nil || !done.Done || done.Assignee != "" || done.CompletedAt.IsZero() {
		t.Fatalf("completion: %+v %v", done, err)
	}
	queued, err := f.s.QueueReview(f.ctx, it.ID, reviewedPR, done.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CompleteReview(f.ctx, it.ID, done.Revision, "Old observation"); !errors.Is(err, ErrConflict) {
		t.Fatalf("cleared new manual review: %v", err)
	}
	if _, err := f.s.QueueReview(f.ctx, it.ID, reviewedPR, done.Revision); !errors.Is(err, ErrConflict) {
		t.Fatalf("queued stale observation: %v", err)
	}
	done, err = f.s.CompleteReview(f.ctx, it.ID, queued.Revision, "Owner reviewed before worker claimed")
	if err != nil || !done.Done {
		t.Fatalf("queued completion: %+v %v", done, err)
	}
	comments, err := f.s.ItemComments(f.ctx, it.ID)
	if err != nil || comments[len(comments)-1].Body != "Owner reviewed before worker claimed" {
		t.Fatal("completion was not recorded")
	}
}
