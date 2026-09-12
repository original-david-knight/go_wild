package gowild_projects

import (
	"strings"
	"testing"
)

func TestMergeApprovalIsPerProjectAndDefaultsToOwner(t *testing.T) {
	for _, mode := range []string{"", MergeApprovalOwner, MergeApprovalAutomatic} {
		t.Run("mode_"+mode, func(t *testing.T) {
			f := newFixture(t)
			f.workers()
			p, err := f.s.CreateProject(f.ctx, ProjectInput{Key: "APP", Name: "App", RepoPath: "/tmp/app", MergeApproval: mode})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "" && p.MergeApproval != MergeApprovalOwner {
				t.Fatal("new project bypasses owner approval by default")
			}
			f.item("APP", "Implementation")
			f.move("APP-1", TransitionInput{Actor: "claude", Action: ActionClaim})
			f.move("APP-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Branch: "app-1"})
			f.refuse("APP-1", TransitionInput{Actor: "claude", Action: ActionReview, Verdict: VerdictApprove, ReviewCommit: strings.Repeat("a", 40)}, ErrForbidden)
			if mode == MergeApprovalAutomatic {
				f.refuse("APP-1", TransitionInput{Actor: "codex", Action: ActionReview, Verdict: VerdictApprove}, ErrValidation)
			}
			it := f.move("APP-1", TransitionInput{Actor: "codex", Action: ActionReview, Verdict: VerdictApprove, ReviewCommit: strings.Repeat("a", 40)})
			want := StatusPendingApproval
			if mode == MergeApprovalAutomatic {
				want = StatusApproved
			}
			if it.Status != want || it.ReviewCommit != strings.Repeat("a", 40) || it.LastVerdictBy != "codex" {
				t.Fatalf("review result = %+v", it)
			}
			comments, err := f.s.ItemComments(f.ctx, it.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range comments {
				if c.Action == ActionApprove {
					t.Fatal("automatic review forged owner approval")
				}
			}
			if mode == MergeApprovalAutomatic {
				f.refuse("APP-1", TransitionInput{Actor: ActorOwner, Action: ActionRework, Body: "conflict"}, ErrForbidden)
				it = f.move("APP-1", TransitionInput{Actor: "claude", Action: ActionRework, Body: "Default branch changed"})
				if it.Status != StatusInProgress || it.Assignee != "claude" || it.ReviewCommit != "" {
					t.Fatalf("rework = %+v", it)
				}
			}
		})
	}
	if (Project{}).MergeApprovalOrDefault() != MergeApprovalOwner {
		t.Fatal("legacy empty policy must require owner")
	}
}

func TestMergeApprovalPolicyValidation(t *testing.T) {
	f := newFixture(t)
	if _, err := f.s.CreateProject(f.ctx, ProjectInput{Key: "APP", Name: "App", MergeApproval: "account"}); err == nil {
		t.Fatal("accepted account-derived approval")
	}
	f.project("APP")
	mode := MergeApprovalAutomatic
	p, err := f.s.UpdateProject(f.ctx, "APP", ProjectPatch{MergeApproval: &mode})
	if err != nil || p.MergeApproval != mode {
		t.Fatalf("project policy: %+v %v", p, err)
	}
	mode = ""
	if _, err := f.s.UpdateProject(f.ctx, "APP", ProjectPatch{MergeApproval: &mode}); err == nil {
		t.Fatal("accepted ambiguous empty policy edit")
	}
}
