package gowild_projects

import "testing"

func TestQueueReviewReusesTicketAndReassignsUnavailableReviewer(t *testing.T) {
	f := newFixture(t)
	f.project("EA")
	f.workers()
	it, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Type: TypeCodeReview, Title: "Review", PRURL: reviewedPR})
	if err != nil {
		t.Fatal(err)
	}
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	active, err := f.s.QueueReview(f.ctx, it.ID, reviewedPR)
	if err != nil || active.Assignee != "claude" || active.Status != StatusInProgress {
		t.Fatalf("active review changed: %+v %v", active, err)
	}
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Body: "First pass"})
	queued, err := f.s.QueueReview(f.ctx, it.ID, reviewedPR)
	if err != nil || queued.ID != it.ID || queued.Status != StatusOpen || queued.Assignee != "claude" {
		t.Fatalf("pending review: %+v %v", queued, err)
	}
	before := queued.Revision
	queued, err = f.s.QueueReview(f.ctx, it.ID, reviewedPR)
	if err != nil || queued.Revision != before {
		t.Fatal("duplicate request changed queued review")
	}
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Body: "Second pass"})
	f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionApprove})
	no := false
	if _, err := f.s.UpdateAgent(f.ctx, "claude", AgentPatch{Enabled: &no}); err != nil {
		t.Fatal(err)
	}
	queued, err = f.s.QueueReview(f.ctx, it.ID, reviewedPR)
	if err != nil || queued.ID != it.ID || queued.Assignee != "" || queued.Done || !queued.ClosedAt.IsZero() {
		t.Fatalf("reassigned review: %+v %v", queued, err)
	}
	job, err := f.s.NextFor(f.ctx, "codex")
	if err != nil || job == nil || job.Item.ID != it.ID || job.Kind != JobCodeReview {
		t.Fatalf("new reviewer: %+v %v", job, err)
	}
	comments, err := f.s.ItemComments(f.ctx, it.ID)
	if err != nil || len(comments) < 5 {
		t.Fatal("review history was lost")
	}
}
