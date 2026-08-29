package gowild_projects

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

type fixture struct {
	t   *testing.T
	ctx context.Context
	s   *Service
	now time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := gowild_data.AddAllTables(db); err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, ctx: context.Background(), now: time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)}
	f.s = NewService(func() gowild_data.Database { return db }, WithClock(func() time.Time { return f.now }))
	return f
}

func (f *fixture) tick(d time.Duration) { f.now = f.now.Add(d) }

func (f *fixture) project(key string) *Project {
	f.t.Helper()
	p, err := f.s.CreateProject(f.ctx, ProjectInput{Key: key, Name: key + " project", RepoPath: "/tmp/" + key})
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *fixture) item(project, title string) *Item {
	f.t.Helper()
	it, err := f.s.CreateItem(f.ctx, project, ItemInput{Title: title, Type: TypeBug})
	if err != nil {
		f.t.Fatal(err)
	}
	f.tick(time.Second)
	return it
}

func (f *fixture) move(key string, in TransitionInput) *Item {
	f.t.Helper()
	it, err := f.s.Transition(f.ctx, key, in)
	if err != nil {
		f.t.Fatalf("%s %s by %s: %v", key, in.Action, in.Actor, err)
	}
	f.tick(time.Second)
	return it
}

func (f *fixture) refuse(key string, in TransitionInput, want error) {
	f.t.Helper()
	if _, err := f.s.Transition(f.ctx, key, in); !errors.Is(err, want) {
		f.t.Fatalf("%s %s by %s: got %v, want %v", key, in.Action, in.Actor, err, want)
	}
}

func TestProjectKeysAndNumbering(t *testing.T) {
	f := newFixture(t)
	p := f.project("EA")
	if p.NextNumber != 1 || p.MergePolicy != MergePolicyMerge || p.DefaultBranch != "main" {
		t.Fatalf("defaults: %+v", p)
	}
	if _, err := f.s.CreateProject(f.ctx, ProjectInput{Key: "ea", Name: "dup"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate key: %v", err)
	}
	if _, err := f.s.CreateProject(f.ctx, ProjectInput{Key: "toolongkey", Name: "x"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad key: %v", err)
	}
	a := f.item("EA", "first")
	b := f.item("EA", "second")
	if a.Number != 1 || b.Number != 2 {
		t.Fatalf("numbers %d %d", a.Number, b.Number)
	}
	got, proj, err := f.s.GetItem(f.ctx, "ea-2")
	if err != nil || got.ID != b.ID || ItemKey(proj, got) != "EA-2" {
		t.Fatalf("get by key: %v %+v", err, got)
	}
	if _, _, err := f.s.GetItem(f.ctx, "EA-9"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing item: %v", err)
	}
	counts, err := f.s.Counts(f.ctx, p.ID)
	if err != nil || counts[StatusOpen] != 2 || counts[StatusDone] != 0 {
		t.Fatalf("counts: %v %v", err, counts)
	}
}

func TestHappyPathMergePolicy(t *testing.T) {
	f := newFixture(t)
	f.project("EA")
	f.item("EA", "Fix the poller")

	it := f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	if it.Status != StatusInProgress || it.Assignee != "claude" || it.LeaseExpiresAt.IsZero() {
		t.Fatalf("claim: %+v", it)
	}
	// The other agent cannot take a live claim.
	f.refuse("EA-1", TransitionInput{Actor: "codex", Action: ActionClaim}, ErrInvalidTransition)
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit}, ErrValidation) // no branch

	it = f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Branch: "pm/ea-1-fix", Body: "done"})
	if it.Status != StatusInReview || it.Implementer != "claude" || it.Assignee != "" || it.Branch != "pm/ea-1-fix" {
		t.Fatalf("submit: %+v", it)
	}
	// The implementer cannot review its own work.
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionReview, Verdict: VerdictApprove}, ErrForbidden)
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim}, ErrForbidden)

	it = f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionClaim})
	if it.Reviewer != "codex" || it.Status != StatusInReview {
		t.Fatalf("review claim: %+v", it)
	}
	f.refuse("EA-1", TransitionInput{Actor: "codex", Action: ActionReview, Verdict: VerdictRequestChanges}, ErrValidation) // no body
	it = f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionReview, Verdict: VerdictRequestChanges, Body: "tests missing"})
	if it.Status != StatusInProgress || it.Assignee != "claude" || it.LastVerdict != VerdictRequestChanges || it.LastVerdictBy != "codex" {
		t.Fatalf("request changes: %+v", it)
	}
	// Owner cannot approve from in_progress; agent resumes and resubmits.
	f.refuse("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionApprove}, ErrInvalidTransition)
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	it = f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Body: "added tests"})
	if it.Branch != "pm/ea-1-fix" || it.Reviewer != "" {
		t.Fatalf("resubmit keeps branch, clears reviewer: %+v", it)
	}
	it = f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionReview, Verdict: VerdictApprove, Body: "lgtm"})
	if it.Status != StatusPendingApproval || it.LastVerdict != VerdictApprove {
		t.Fatalf("approve review: %+v", it)
	}
	f.refuse("EA-1", TransitionInput{Actor: "codex", Action: ActionApprove}, ErrForbidden)
	it = f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionApprove})
	if it.Status != StatusApproved {
		t.Fatalf("owner approve: %+v", it)
	}
	f.refuse("EA-1", TransitionInput{Actor: "codex", Action: ActionComplete}, ErrForbidden)
	f.refuse("EA-1", TransitionInput{Actor: "codex", Action: ActionClaim}, ErrForbidden)
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	it = f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionComplete, Body: "merged"})
	if it.Status != StatusDone || it.ClosedAt.IsZero() {
		t.Fatalf("complete: %+v", it)
	}
	f.refuse("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionClose}, ErrInvalidTransition)

	feed, err := f.s.ItemComments(f.ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, c := range feed {
		if c.Kind != CommentKindTransition {
			t.Fatalf("unexpected kind %s", c.Kind)
		}
		actions = append(actions, c.Action)
	}
	want := []string{"claim", "submit", "claim", "review", "claim", "submit", "review", "approve", "claim", "complete"}
	if len(actions) != len(want) {
		t.Fatalf("feed %v", actions)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Fatalf("feed %v, want %v", actions, want)
		}
	}
	agents, err := f.s.ListAgents(f.ctx)
	if err != nil || len(agents) != 2 {
		t.Fatalf("agents: %v %v", err, agents)
	}
	for _, a := range agents {
		if a.CurrentItemID != "" {
			t.Fatalf("%s still holds %s", a.ID, a.CurrentItemID)
		}
	}
}

func TestOwnerPaths(t *testing.T) {
	f := newFixture(t)
	f.project("EA")
	f.item("EA", "Question")
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionBlock}, ErrValidation)
	it := f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionBlock, Body: "which account?"})
	if it.Status != StatusBlocked || it.Assignee != "" {
		t.Fatalf("block: %+v", it)
	}
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionReopen}, ErrForbidden)
	it = f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionReopen, Body: "personal"})
	if it.Status != StatusOpen {
		t.Fatalf("reopen: %+v", it)
	}
	it = f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionClose})
	if it.Status != StatusClosed || it.ClosedAt.IsZero() {
		t.Fatalf("close: %+v", it)
	}
	it = f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionReopen})
	if it.Status != StatusOpen || !it.ClosedAt.IsZero() {
		t.Fatalf("reopen from closed: %+v", it)
	}
	// Owner request_changes from pending approval goes back to the implementer.
	f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionClaim})
	f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionSubmit, Branch: "b"})
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionReview, Verdict: VerdictApprove})
	it = f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionRequestChanges, Body: "not like that"})
	if it.Status != StatusInProgress || it.Assignee != "codex" || it.LastVerdictBy != ActorOwner {
		t.Fatalf("owner request changes: %+v", it)
	}
	// Owner may complete an approved item by hand.
	f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionClaim})
	f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionSubmit})
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionReview, Verdict: VerdictApprove})
	f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionApprove})
	it = f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionComplete, PRURL: "https://x/pr/1"})
	if it.Status != StatusDone || it.PRURL != "https://x/pr/1" {
		t.Fatalf("owner complete: %+v", it)
	}
}

func TestRevisionPrecondition(t *testing.T) {
	f := newFixture(t)
	f.project("EA")
	it := f.item("EA", "title")
	title := "better title"
	if _, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Title: &title}, it.Revision+5); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale: %v", err)
	}
	got, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Title: &title}, it.Revision)
	if err != nil || got.Title != title || got.Revision != it.Revision+1 {
		t.Fatalf("update: %v %+v", err, got)
	}
}

func TestCheckInQueueOrder(t *testing.T) {
	f := newFixture(t)
	f.project("EA")
	f.item("EA", "low thing")
	urgent, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "urgent thing", Priority: PriorityUrgent})
	if err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)

	// A disabled agent gets nothing, and is still recorded as seen.
	if _, err := f.s.SetAgentEnabled(f.ctx, "claude", false); err != nil {
		t.Fatal(err)
	}
	job, err := f.s.CheckIn(f.ctx, "claude", true)
	if err != nil || job != nil {
		t.Fatalf("disabled agent: %v %+v", err, job)
	}
	a, _ := f.s.GetAgent(f.ctx, "claude")
	if a.LastSeenAt.IsZero() {
		t.Fatal("check-in not recorded")
	}
	f.s.SetAgentEnabled(f.ctx, "claude", true)

	// Urgent first, and the claim is made.
	job, err = f.s.CheckIn(f.ctx, "claude", true)
	if err != nil || job == nil || job.Kind != JobImplement || job.Item.ID != urgent.ID || job.Item.Assignee != "claude" {
		t.Fatalf("first job: %v %+v", err, job)
	}
	if job.Project == nil || job.Project.Key != "EA" || job.Pinned == nil || job.Chat == nil || job.Comments == nil {
		t.Fatalf("job context: %+v", job)
	}
	// Checking in again resumes my own in-progress work rather than taking more.
	job, err = f.s.CheckIn(f.ctx, "claude", true)
	if err != nil || job.Kind != JobImplement || job.Item.ID != urgent.ID {
		t.Fatalf("resume: %v %+v", err, job)
	}
	// Codex gets the other open item, not claude's.
	job, err = f.s.CheckIn(f.ctx, "codex", true)
	if err != nil || job.Item.Number != 1 || job.Item.Assignee != "codex" {
		t.Fatalf("codex job: %v %+v", err, job)
	}
	// Nothing left for a third agent.
	job, err = f.s.CheckIn(f.ctx, "gemini", true)
	if err != nil || job != nil {
		t.Fatalf("empty queue: %v %+v", err, job)
	}

	// Claude submits; codex's next job is the review, ahead of its own work.
	f.move("EA-2", TransitionInput{Actor: "claude", Action: ActionSubmit, Branch: "pm/ea-2"})
	job, err = f.s.CheckIn(f.ctx, "codex", true)
	if err != nil || job.Kind != JobReview || job.Item.ID != urgent.ID || job.Item.Reviewer != "codex" {
		t.Fatalf("review job: %v %+v", err, job)
	}
	// Claude cannot be handed its own review; it gets nothing (its item is under review, codex holds EA-1).
	job, err = f.s.CheckIn(f.ctx, "claude", true)
	if err != nil || job != nil {
		t.Fatalf("claude idle: %v %+v", err, job)
	}
	f.move("EA-2", TransitionInput{Actor: "codex", Action: ActionReview, Verdict: VerdictApprove, Body: "fine"})
	f.move("EA-2", TransitionInput{Actor: ActorOwner, Action: ActionApprove})
	// Approved work of mine comes first, as a merge job under the merge policy.
	job, err = f.s.CheckIn(f.ctx, "claude", true)
	if err != nil || job.Kind != JobMerge || job.Item.ID != urgent.ID || job.Item.LeaseExpiresAt.IsZero() {
		t.Fatalf("merge job: %v %+v", err, job)
	}
	policy := MergePolicyPullRequest
	if _, err := f.s.UpdateProject(f.ctx, "EA", ProjectPatch{MergePolicy: &policy}); err != nil {
		t.Fatal(err)
	}
	job, err = f.s.NextFor(f.ctx, "claude")
	if err != nil || job.Kind != JobPullRequest {
		t.Fatalf("pr job: %v %+v", err, job)
	}
}

func TestLeaseExpiryReassigns(t *testing.T) {
	f := newFixture(t)
	f.project("EA")
	f.item("EA", "stuck")
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	if job, _ := f.s.NextFor(f.ctx, "codex"); job != nil {
		t.Fatalf("live lease offered to codex: %+v", job)
	}
	f.tick(DefaultLease + time.Minute)
	job, err := f.s.CheckIn(f.ctx, "codex", true)
	if err != nil || job == nil || job.Item.Assignee != "codex" || job.Item.Status != StatusInProgress {
		t.Fatalf("expired lease: %v %+v", err, job)
	}
	a, _ := f.s.GetAgent(f.ctx, "claude")
	if a.CurrentItemID != "" {
		t.Fatalf("claude still marked holding %s", a.CurrentItemID)
	}
}

func TestUnworkableProjectsAreSkipped(t *testing.T) {
	f := newFixture(t)
	if _, err := f.s.CreateProject(f.ctx, ProjectInput{Key: "NP", Name: "no path"}); err != nil {
		t.Fatal(err)
	}
	f.item("NP", "cannot be worked")
	if job, err := f.s.NextFor(f.ctx, "claude"); err != nil || job != nil {
		t.Fatalf("no repo path: %v %+v", err, job)
	}
	f.project("AR")
	f.item("AR", "archived soon")
	archived := ProjectArchived
	f.s.UpdateProject(f.ctx, "AR", ProjectPatch{Status: &archived})
	if job, err := f.s.NextFor(f.ctx, "claude"); err != nil || job != nil {
		t.Fatalf("archived: %v %+v", err, job)
	}
}

func TestListItemsFilters(t *testing.T) {
	f := newFixture(t)
	f.project("EA")
	f.item("EA", "a")
	f.item("EA", "b")
	f.move("EA-2", TransitionInput{Actor: ActorOwner, Action: ActionClose})
	rows, err := f.s.ListItems(f.ctx, ItemFilter{ProjectKey: "EA"})
	if err != nil || len(rows) != 1 || rows[0].Number != 1 {
		t.Fatalf("default hides closed: %v %d", err, len(rows))
	}
	rows, _ = f.s.ListItems(f.ctx, ItemFilter{ProjectKey: "EA", IncludeClosed: true})
	if len(rows) != 2 {
		t.Fatalf("include closed: %d", len(rows))
	}
	rows, _ = f.s.ListItems(f.ctx, ItemFilter{Statuses: []string{StatusClosed}})
	if len(rows) != 1 || rows[0].Number != 2 {
		t.Fatalf("by status: %d", len(rows))
	}
	if _, err := f.s.ListItems(f.ctx, ItemFilter{Statuses: []string{"nope"}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad status: %v", err)
	}
}

func TestBoardAndChat(t *testing.T) {
	f := newFixture(t)
	p := f.project("EA")
	if _, err := f.s.CreatePost(f.ctx, "EA", PostInput{Author: "claude", Title: "x", Pinned: true}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent pin: %v", err)
	}
	pinned, err := f.s.CreatePost(f.ctx, "EA", PostInput{Author: ActorOwner, Title: "Conventions", Body: "Run selfcheck.", Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	post, err := f.s.CreatePost(f.ctx, "EA", PostInput{Author: "codex", Title: "Design note", Body: "Thoughts, @claude?"})
	if err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	reply, err := f.s.ReplyToPost(f.ctx, post.ID, ActorOwner, "agree")
	if err != nil || reply.TargetKind != TargetPost {
		t.Fatalf("reply: %v", err)
	}
	f.tick(time.Second)
	got, _, _ := f.s.GetPost(f.ctx, post.ID)
	if got.ReplyCount != 1 || got.LastReplyAt.IsZero() {
		t.Fatalf("reply count: %+v", got)
	}
	posts, _ := f.s.ListPosts(f.ctx, "EA")
	if len(posts) != 2 || posts[0].ID != pinned.ID {
		t.Fatalf("pinned first: %+v", posts)
	}
	if _, err := f.s.UpdatePost(f.ctx, post.ID, "claude", PostPatch{Title: &post.Title}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("other agent edit: %v", err)
	}
	yes := true
	if _, err := f.s.UpdatePost(f.ctx, post.ID, "codex", PostPatch{Pinned: &yes}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("author pin: %v", err)
	}
	standing, _ := f.s.PinnedPosts(f.ctx, p.ID)
	if len(standing) != 1 {
		t.Fatalf("pinned: %d", len(standing))
	}

	// Chat: project room and general room are distinct; paging by after.
	m1, err := f.s.PostChat(f.ctx, "EA", ActorOwner, "morning")
	if err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	f.s.PostChat(f.ctx, "", ActorOwner, "general only")
	f.tick(time.Second)
	m3, _ := f.s.PostChat(f.ctx, "EA", "codex", "on it")
	room, _ := f.s.ListChat(f.ctx, "EA", "", 0)
	if len(room) != 2 || room[0].ID != m1.ID {
		t.Fatalf("room: %+v", room)
	}
	general, _ := f.s.ListChat(f.ctx, "", "", 0)
	if len(general) != 1 {
		t.Fatalf("general: %+v", general)
	}
	after, _ := f.s.ListChat(f.ctx, "EA", m1.ID, 0)
	if len(after) != 1 || after[0].ID != m3.ID {
		t.Fatalf("after: %+v", after)
	}
	if _, err := f.s.ListChat(f.ctx, "EA", "missing", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bad anchor: %v", err)
	}
	last1, _ := f.s.ListChat(f.ctx, "EA", "", 1)
	if len(last1) != 1 || last1[0].ID != m3.ID {
		t.Fatalf("limit: %+v", last1)
	}
}

func TestMentionsBecomeRespondJobs(t *testing.T) {
	f := newFixture(t)
	f.project("EA")
	f.item("EA", "open work")

	// A chat mention outranks open work, and answering in the room closes it.
	msg, err := f.s.PostChat(f.ctx, "EA", ActorOwner, "@claude what do you think about the queue? cc @codex")
	if err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	job, err := f.s.CheckIn(f.ctx, "claude", true)
	if err != nil || job == nil || job.Kind != JobRespond || job.Mention == nil || job.Mention.ChatMessage == nil || job.Mention.ChatMessage.ID != msg.ID {
		t.Fatalf("respond job: %v %+v", err, job)
	}
	if job.Mention.Mention.Attempts != 1 || len(job.Chat) != 1 || job.Project.Key != "EA" {
		t.Fatalf("respond context: %+v", job.Mention.Mention)
	}
	if m, err := f.s.GetMention(f.ctx, job.Mention.Mention.ID); err != nil || m.ProjectID != job.Project.ID || m.Agent != "claude" {
		t.Fatalf("GetMention: %v %+v", err, m)
	}
	if _, err := f.s.GetMention(f.ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetMention on nothing: %v", err)
	}
	if _, err := f.s.PostChat(f.ctx, "EA", "claude", "I think it is fine"); err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	open, _ := f.s.UnansweredMentions(f.ctx, "claude")
	if len(open) != 0 {
		t.Fatalf("still open: %+v", open[0])
	}
	// Codex was mentioned too and has not spoken: it gets the respond job first.
	job, _ = f.s.CheckIn(f.ctx, "codex", false)
	if job == nil || job.Kind != JobRespond {
		t.Fatalf("codex respond: %+v", job)
	}
	// Now claude's next job is the open item.
	job, _ = f.s.CheckIn(f.ctx, "claude", false)
	if job == nil || job.Kind != JobImplement {
		t.Fatalf("after answering: %+v", job)
	}

	// An item comment mention is answered by a transition comment on the item.
	if _, err := f.s.CommentOnItem(f.ctx, "EA-1", ActorOwner, "@claude take this one first"); err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	job, _ = f.s.CheckIn(f.ctx, "claude", false)
	if job == nil || job.Kind != JobRespond || job.Mention.Comment == nil || job.Mention.Item == nil || len(job.Mention.Thread) != 1 {
		t.Fatalf("item mention: %+v", job)
	}
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim, Body: "on it"})
	open, _ = f.s.UnansweredMentions(f.ctx, "claude")
	if len(open) != 0 {
		t.Fatalf("item mention still open")
	}

	// Attempts cap: a mention handed out three times stops being offered;
	// the explicit mark closes it too. Codex first answers the chat mention
	// it still owes from above, so the post mention is its only open one.
	if _, err := f.s.PostChat(f.ctx, "EA", "codex", "noted"); err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	post, _ := f.s.CreatePost(f.ctx, "EA", PostInput{Author: ActorOwner, Title: "q", Body: "@codex?"})
	f.tick(time.Second)
	for i := 0; i < MaxMentionAttempts; i++ {
		job, _ = f.s.CheckIn(f.ctx, "codex", true)
		if job == nil || job.Kind != JobRespond {
			t.Fatalf("attempt %d: %+v", i, job)
		}
	}
	if job, _ = f.s.NextFor(f.ctx, "codex"); job != nil && job.Kind == JobRespond {
		t.Fatalf("capped mention still offered: %+v", job.Mention.Mention)
	}
	m, _ := f.s.PostChat(f.ctx, "", ActorOwner, "@codex ping")
	_ = post
	f.tick(time.Second)
	job, _ = f.s.NextFor(f.ctx, "codex")
	if job == nil || job.Kind != JobRespond || job.Project != nil || job.Mention.ChatMessage.ID != m.ID {
		t.Fatalf("general room mention: %+v", job)
	}
	if _, err := f.s.MarkMentionAnswered(f.ctx, job.Mention.Mention.ID); err != nil {
		t.Fatal(err)
	}
	if job, _ = f.s.NextFor(f.ctx, "codex"); job != nil && job.Kind == JobRespond {
		t.Fatalf("marked mention still offered")
	}
}

// A notice is the runner speaking for an agent, not the agent speaking: it
// records no mention, whatever the body quotes, and closes none.
func TestNoticesNeitherNameNorAnswer(t *testing.T) {
	f := newFixture(t)
	f.project("EA")
	if _, err := f.s.PostChat(f.ctx, "EA", ActorOwner, "@claude where is the cursor kept?"); err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	m, err := f.s.PostNotice(f.ctx, "EA", "claude", "claude started EA-1 — Ask @codex to review the poller")
	if err != nil {
		t.Fatal(err)
	}
	if m.Author != "claude" || m.Mentions != "" {
		t.Fatalf("notice row: %+v", m)
	}
	f.tick(time.Second)
	open, _ := f.s.UnansweredMentions(f.ctx, "claude")
	if len(open) != 1 {
		t.Fatalf("the notice closed the owner's question: %d open", len(open))
	}
	if named, _ := f.s.UnansweredMentions(f.ctx, "codex"); len(named) != 0 {
		t.Fatalf("the notice recorded a mention nobody wrote: %+v", named[0])
	}
	if job, _ := f.s.NextFor(f.ctx, "codex"); job != nil && job.Kind == JobRespond {
		t.Fatalf("codex owes a respond job for a quoted @name: %+v", job.Mention.Mention)
	}
	// The line is in the room like any other.
	room, _ := f.s.ListChat(f.ctx, "EA", "", 0)
	if len(room) != 2 || room[1].ID != m.ID {
		t.Fatalf("room: %+v", room)
	}
	// Speaking still answers.
	if _, err := f.s.PostChat(f.ctx, "EA", "claude", "in the poller"); err != nil {
		t.Fatal(err)
	}
	if open, _ = f.s.UnansweredMentions(f.ctx, "claude"); len(open) != 0 {
		t.Fatalf("still open after the answer: %+v", open[0])
	}
	if _, err := f.s.PostNotice(f.ctx, "EA", "claude", "  "); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty notice: %v", err)
	}
}

func TestHelpers(t *testing.T) {
	if got := ExtractMentions("hi @Claude and @codex, mail me@example.com, @claude again"); len(got) != 2 || got[0] != "claude" || got[1] != "codex" {
		t.Fatalf("mentions: %v", got)
	}
	if BranchName("EA-12", "Fix the Poller: retry!") != "pm/ea-12-fix-the-poller-retry" {
		t.Fatalf("branch: %s", BranchName("EA-12", "Fix the Poller: retry!"))
	}
	if BranchName("EA-3", "") != "pm/ea-3" {
		t.Fatal("empty slug")
	}
	if _, _, err := ParseItemKey("EA12"); !errors.Is(err, ErrValidation) {
		t.Fatal("bad key parsed")
	}
	if !ValidAgentName("claude") || ValidAgentName("owner") || ValidAgentName("Claude") {
		t.Fatal("agent names")
	}
	if StatusOrder(StatusPendingApproval) >= StatusOrder(StatusOpen) || StatusOrder(StatusDone) <= StatusOrder(StatusOpen) {
		t.Fatal("status order")
	}
}

func TestRunsRecordWhatAnAgentDid(t *testing.T) {
	f := newFixture(t)
	p := f.project("EA")
	it := f.item("EA", "thing")
	if _, err := f.s.StartRun(f.ctx, RunInput{Agent: "claude", Kind: "dance", ItemID: it.ID}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad kind: %v", err)
	}
	if _, err := f.s.StartRun(f.ctx, RunInput{Agent: "claude", Kind: JobImplement}); !errors.Is(err, ErrValidation) {
		t.Fatalf("no subject: %v", err)
	}
	run, err := f.s.StartRun(f.ctx, RunInput{Agent: "claude", Kind: JobImplement, ItemID: it.ID, ProjectID: p.ID, Branch: "pm/ea-1-thing"})
	if err != nil || run.StartedAt.IsZero() || !run.FinishedAt.IsZero() {
		t.Fatalf("start: %v %+v", err, run)
	}
	f.tick(90 * time.Second)
	long := strings.Repeat("x", OutputTailMax+10)
	got, err := f.s.FinishRun(f.ctx, run.ID, RunResult{Outcome: "submitted", ExitStatus: "ok", Summary: "did it", OutputTail: long, CostUSD: 0.12})
	if err != nil || got.DurationSeconds != 90 || got.Outcome != "submitted" || len([]rune(got.OutputTail)) != OutputTailMax || got.CostUSD != 0.12 {
		t.Fatalf("finish: %v %+v", err, got)
	}
	// Finishing twice keeps the first result.
	again, _ := f.s.FinishRun(f.ctx, run.ID, RunResult{Outcome: "crashed"})
	if again.Outcome != "submitted" {
		t.Fatalf("refinished: %+v", again)
	}
	f.tick(time.Second)
	second, _ := f.s.StartRun(f.ctx, RunInput{Agent: "codex", Kind: JobReview, ItemID: it.ID, ProjectID: p.ID})
	runs, err := f.s.ListRuns(f.ctx, RunFilter{ItemID: it.ID})
	if err != nil || len(runs) != 2 || runs[0].ID != second.ID {
		t.Fatalf("list newest first: %v %d", err, len(runs))
	}
	runs, _ = f.s.ListRuns(f.ctx, RunFilter{Agent: "claude"})
	if len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("by agent: %d", len(runs))
	}
	runs, _ = f.s.ListRuns(f.ctx, RunFilter{ProjectID: p.ID, Limit: 1})
	if len(runs) != 1 {
		t.Fatalf("limit: %d", len(runs))
	}
	if _, err := f.s.GetRun(f.ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing run: %v", err)
	}
}

func TestAssignmentIsExclusiveWhenSet(t *testing.T) {
	f := newFixture(t)
	f.project("EA")
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "x", Assignee: "Owner"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad assignee: %v", err)
	}
	it, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "for codex", Assignee: "codex"})
	if err != nil || it.Assignee != "codex" {
		t.Fatalf("assigned create: %v %+v", err, it)
	}
	f.tick(time.Second)
	// The other agent is not offered it and cannot claim it.
	if job, _ := f.s.NextFor(f.ctx, "claude"); job != nil {
		t.Fatalf("claude offered codex's item: %+v", job.Item)
	}
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim}, ErrInvalidTransition)
	job, _ := f.s.NextFor(f.ctx, "codex")
	if job == nil || job.Item.ID != it.ID {
		t.Fatalf("codex not offered its item: %+v", job)
	}
	// Reassigning an open item records the hand-off; "" returns it to the queue.
	claude := "claude"
	got, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Assignee: &claude}, 0)
	if err != nil || got.Assignee != "claude" {
		t.Fatalf("reassign: %v %+v", err, got)
	}
	none := ""
	if got, err = f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Assignee: &none}, 0); err != nil || got.Assignee != "" {
		t.Fatalf("unassign: %v %+v", err, got)
	}
	feed, _ := f.s.ItemComments(f.ctx, it.ID)
	var assigns []string
	for _, c := range feed {
		if c.Action == ActionAssign {
			assigns = append(assigns, c.Body)
		}
	}
	if len(assigns) != 3 || assigns[0] != "assigned to codex" || assigns[1] != "assigned to claude" || assigns[2] != "returned to the queue" {
		t.Fatalf("assign feed: %v", assigns)
	}
	// Once claimed, the owner cannot swap the holder.
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	if _, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Assignee: &none}, 0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("reassign in progress: %v", err)
	}
	// An unchanged assignee is not a reassignment and passes in any status.
	same := "claude"
	if _, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Assignee: &same}, 0); err != nil {
		t.Fatalf("same assignee: %v", err)
	}
}

func TestActivityMergesEverythingNewestFirst(t *testing.T) {
	f := newFixture(t)
	p := f.project("EA")
	f.project("OT")
	it := f.item("EA", "the item")
	f.s.CommentOnItem(f.ctx, "EA-1", ActorOwner, "a comment")
	f.tick(time.Second)
	post, _ := f.s.CreatePost(f.ctx, "EA", PostInput{Author: "claude", Title: "a post", Body: "opening"})
	f.tick(time.Second)
	f.s.ReplyToPost(f.ctx, post.ID, ActorOwner, "a reply")
	f.tick(time.Second)
	f.s.PostChat(f.ctx, "EA", "codex", "a chat line")
	f.tick(time.Second)
	f.s.PostChat(f.ctx, "", ActorOwner, "general line")
	f.tick(time.Second)
	f.s.PostChat(f.ctx, "OT", ActorOwner, "other project line")
	f.tick(time.Second)
	run, _ := f.s.StartRun(f.ctx, RunInput{Agent: "claude", Kind: JobImplement, ItemID: it.ID, ProjectID: p.ID})
	f.tick(time.Second)
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})

	entries, err := f.s.Activity(f.ctx, ActivityFilter{ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range entries {
		kinds = append(kinds, e.Kind)
		if e.ProjectID != p.ID {
			t.Fatalf("foreign entry in project feed: %+v", e)
		}
	}
	want := []string{ActivityTransition, ActivityRun, ActivityChat, ActivityReply, ActivityPost, ActivityComment}
	if len(kinds) != len(want) {
		t.Fatalf("kinds %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds %v, want %v", kinds, want)
		}
	}
	if entries[0].Action != ActionClaim || entries[0].Title != "the item" || entries[1].RunID != run.ID || entries[1].Outcome != "" {
		t.Fatalf("head entries: %+v %+v", entries[0], entries[1])
	}
	all, _ := f.s.Activity(f.ctx, ActivityFilter{})
	if len(all) != 8 {
		t.Fatalf("global feed %d, want 8", len(all))
	}
	two, _ := f.s.Activity(f.ctx, ActivityFilter{Limit: 2})
	if len(two) != 2 {
		t.Fatalf("limit: %d", len(two))
	}
}
