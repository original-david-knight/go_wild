package gowild_projects

import (
	"errors"
	"testing"
)

const reviewedPR = "https://github.com/acme/widgets/pull/12"

// A code review needs its pull request from the start, the URL has one shape,
// and the item is never groomed.
func TestCodeReviewCreation(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "review the widget PR", Type: TypeCodeReview}); !errors.Is(err, ErrValidation) {
		t.Fatalf("code review without a pull request: %v", err)
	}
	for _, bad := range []string{"https://gitlab.com/acme/widgets/-/merge_requests/12", "https://github.com/acme/widgets", "github.com/acme/widgets/pull/12", "https://github.com/acme/widgets/pull/twelve"} {
		if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "review", Type: TypeCodeReview, PRURL: bad}); !errors.Is(err, ErrValidation) {
			t.Fatalf("pr_url %q accepted: %v", bad, err)
		}
	}
	it, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "review the widget PR", Type: TypeCodeReview, PRURL: " " + reviewedPR + "/ "})
	if err != nil {
		t.Fatal(err)
	}
	if it.PRURL != reviewedPR || it.NeedsGroom || it.Type != TypeCodeReview {
		t.Fatalf("code review filed as %+v", it)
	}
	// The URL is validated on every type; a bug may carry one too.
	bug, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "a bug with a PR", Type: TypeBug, PRURL: reviewedPR})
	if err != nil || bug.PRURL != reviewedPR || !bug.NeedsGroom {
		t.Fatalf("bug with a pull request: %+v %v", bug, err)
	}
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "a bug with a bad PR", Type: TypeBug, PRURL: "https://example.com/pr/1"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad pr_url on a bug accepted: %v", err)
	}
	// A patch cannot strip the URL from a code review, nor retype an item
	// into one without giving it a URL; a valid re-point goes through.
	empty, other := "", "https://github.com/acme/widgets/pull/13/"
	if _, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{PRURL: &empty}, 0); !errors.Is(err, ErrValidation) {
		t.Fatalf("stripping the URL from a code review: %v", err)
	}
	typ := TypeCodeReview
	if _, err := f.s.UpdateItem(f.ctx, "EA-2", ItemPatch{Type: &typ, PRURL: &empty}, 0); !errors.Is(err, ErrValidation) {
		t.Fatalf("retyping to code_review with no URL: %v", err)
	}
	chore, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "tidy", Type: TypeChore})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.UpdateItem(f.ctx, ItemKey(f.mustProject("EA"), chore), ItemPatch{Type: &typ}, 0); !errors.Is(err, ErrValidation) {
		t.Fatalf("retyping a chore with no URL to code_review: %v", err)
	}
	got, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{PRURL: &other}, 0)
	if err != nil || got.PRURL != "https://github.com/acme/widgets/pull/13" {
		t.Fatalf("re-pointing the pull request: %+v %v", got, err)
	}
	bugTyped, err := f.s.UpdateItem(f.ctx, "EA-2", ItemPatch{Type: &typ}, 0)
	if err != nil || bugTyped.Type != TypeCodeReview || bugTyped.PRURL != reviewedPR {
		t.Fatalf("retyping a bug that has a URL to code_review: %+v %v", bugTyped, err)
	}
}

// mustProject reads a project by key for the tests that need to build an
// item key.
func (f *fixture) mustProject(key string) *Project {
	f.t.Helper()
	p, err := f.s.GetProject(f.ctx, key)
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

// The whole lifecycle: offered as a code_review job, resumed as one, submitted
// with no branch straight to the owner with the suggested verdict, sent back
// for another pass, and closed by approve — never by complete.
func TestCodeReviewLifecycle(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	it, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "review the widget PR", Type: TypeCodeReview, PRURL: reviewedPR})
	if err != nil {
		t.Fatal(err)
	}
	if job, err := f.s.NextFor(f.ctx, "claude"); err != nil || job == nil || job.Kind != JobCodeReview || job.Item.ID != it.ID {
		t.Fatalf("open code review not offered as a code_review job: %+v %v", job, err)
	}
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionRelease, Body: "crashed"})
	if job, err := f.s.NextFor(f.ctx, "claude"); err != nil || job == nil || job.Kind != JobCodeReview {
		t.Fatalf("released code review not re-offered as a code_review job: %+v %v", job, err)
	}
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	// The review is the body; a verdict outside the three is refused; a
	// branch is not needed and not recorded.
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Verdict: VerdictApprove}, ErrValidation)
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Body: "looks wrong", Verdict: "reject"}, ErrValidation)
	f.refuse("EA-1", TransitionInput{Actor: "codex", Action: ActionSubmit, Body: "not mine"}, ErrForbidden)
	it = f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Body: "The migration drops the index.", Verdict: VerdictRequestChanges})
	if it.Status != StatusPendingApproval || it.Implementer != "claude" || it.Assignee != "" || it.Branch != "" || it.PRURL != reviewedPR {
		t.Fatalf("code review submit: %+v", it)
	}
	if it.LastVerdict != VerdictRequestChanges || it.LastVerdictBy != "claude" || it.LastVerdictAt.IsZero() || !it.LeaseExpiresAt.IsZero() {
		t.Fatalf("suggested verdict not recorded: %+v", it)
	}
	if f.holds("claude", it.ID) {
		t.Fatal("the reviewer still holds the submitted review")
	}
	// The owner sends it back for another pass: the reviewer gets it again.
	f.refuse("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionRequestChanges}, ErrValidation) // no body
	it = f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionRequestChanges, Body: "the index is rebuilt in the next migration; look again"})
	if it.Status != StatusInProgress || it.Assignee != "claude" || it.LastVerdict != VerdictRequestChanges || it.LastVerdictBy != ActorOwner {
		t.Fatalf("owner request_changes on a code review: %+v", it)
	}
	if job, err := f.s.NextFor(f.ctx, "claude"); err != nil || job == nil || job.Kind != JobCodeReview {
		t.Fatalf("returned code review not offered as a code_review job: %+v %v", job, err)
	}
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	it = f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Body: "Second pass: the index comes back in 0042; nothing blocks.", Verdict: VerdictComment})
	if it.Status != StatusPendingApproval || it.LastVerdict != VerdictComment || it.LastVerdictBy != "claude" {
		t.Fatalf("second submit: %+v", it)
	}
	// Approve is the end; complete never applies to a code review.
	f.refuse("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionComplete}, ErrInvalidTransition)
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionComplete}, ErrInvalidTransition)
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionApprove}, ErrForbidden)
	it = f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionApprove})
	if it.Status != StatusDone || it.ClosedAt.IsZero() || it.Assignee != "" {
		t.Fatalf("owner approve on a code review: %+v", it)
	}
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionComplete}, ErrInvalidTransition)
	if job, err := f.s.NextFor(f.ctx, "claude"); err != nil || job != nil {
		t.Fatalf("a done code review is still offered: %+v %v", job, err)
	}
	// Reopen hands it back to its reviewer, as any done item.
	it = f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionReopen})
	if it.Status != StatusOpen || it.Assignee != "claude" || !it.ClosedAt.IsZero() {
		t.Fatalf("reopen: %+v", it)
	}

	feed, err := f.s.ItemComments(f.ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	var actions, verdicts []string
	for _, c := range feed {
		actions = append(actions, c.Action)
		verdicts = append(verdicts, c.Verdict)
	}
	wantActions := []string{"claim", "release", "claim", "submit", "request_changes", "claim", "submit", "approve", "reopen"}
	wantVerdicts := []string{"", "", "", VerdictRequestChanges, "", "", VerdictComment, "", ""}
	if len(actions) != len(wantActions) {
		t.Fatalf("feed %v, want %v", actions, wantActions)
	}
	for i := range wantActions {
		if actions[i] != wantActions[i] || verdicts[i] != wantVerdicts[i] {
			t.Fatalf("feed %v %v, want %v %v", actions, verdicts, wantActions, wantVerdicts)
		}
	}
}

// A submit without a verdict is a plain review handed to the owner: nothing
// is recorded as the last verdict.
func TestCodeReviewSubmitWithoutAVerdict(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "review", Type: TypeCodeReview, PRURL: reviewedPR}); err != nil {
		t.Fatal(err)
	}
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	it := f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Body: "Read it; nothing to add.", Branch: "ignored"})
	if it.Status != StatusPendingApproval || it.LastVerdict != "" || it.LastVerdictBy != "" || !it.LastVerdictAt.IsZero() || it.Branch != "" {
		t.Fatalf("submit without a verdict: %+v", it)
	}
	feed, err := f.s.ItemComments(f.ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if last := feed[len(feed)-1]; last.Action != ActionSubmit || last.Verdict != "" {
		t.Fatalf("submit comment: %+v", last)
	}
}

// The other types are as they were: a feature's submit needs a branch and
// goes to review, its approve lands in approved, and the review action still
// takes only approve and request_changes.
func TestCodeReviewLeavesTheOtherTypesAlone(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	f.item("EA", "Fix the poller")
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Body: "done", Verdict: VerdictApprove}, ErrValidation) // no branch
	it := f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Branch: "pm/ea-1-fix", Body: "done"})
	if it.Status != StatusInReview || it.LastVerdict != "" {
		t.Fatalf("feature submit: %+v", it)
	}
	f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionClaim})
	f.refuse("EA-1", TransitionInput{Actor: "codex", Action: ActionReview, Verdict: VerdictComment, Body: "a note"}, ErrValidation)
	f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionReview, Verdict: VerdictApprove, Body: "fine"})
	it = f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionApprove})
	if it.Status != StatusApproved || !it.ClosedAt.IsZero() {
		t.Fatalf("feature approve: %+v", it)
	}
	if run, err := f.s.StartRun(f.ctx, RunInput{Agent: "claude", Kind: JobCodeReview, ItemID: it.ID}); err != nil || run.Kind != JobCodeReview {
		t.Fatalf("a code_review run: %+v %v", run, err)
	}
}
