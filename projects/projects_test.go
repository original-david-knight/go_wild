package gowild_projects

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

// fixture is one in-memory tracker with a clock the test drives. The clock
// is guarded: a parked Wait re-reads it on another goroutine (availability
// ends on the clock), while the test advances it from its own. Production
// passes time.Now, which is safe already.
type fixture struct {
	t   *testing.T
	ctx context.Context
	s   *Service

	clockMu sync.Mutex
	clock   time.Time
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
	f := &fixture{t: t, ctx: context.Background(), clock: time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)}
	f.s = NewService(func() gowild_data.Database { return db }, WithClock(f.at))
	return f
}

// at is the fixture's clock, as the service reads it.
func (f *fixture) at() time.Time {
	f.clockMu.Lock()
	defer f.clockMu.Unlock()
	return f.clock
}

func (f *fixture) tick(d time.Duration) {
	f.clockMu.Lock()
	defer f.clockMu.Unlock()
	f.clock = f.clock.Add(d)
}

func (f *fixture) project(key string) *Project {
	f.t.Helper()
	p, err := f.s.CreateProject(f.ctx, ProjectInput{Key: key, Name: key + " project", RepoPath: "/tmp/" + key})
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

// item files a specced bug: ready for its implement job, no grooming. The
// groom path has its own tests over raw items.
func (f *fixture) item(project, title string) *Item {
	f.t.Helper()
	it, err := f.s.CreateItem(f.ctx, project, ItemInput{Title: title, Type: TypeBug, Specced: true})
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

// agent creates a worker; the first one a test creates is the default.
func (f *fixture) agent(name string) *Agent {
	f.t.Helper()
	a, err := f.s.CreateAgent(f.ctx, AgentInput{Name: name, CLI: CLIClaude, Model: name, Effort: EffortXHigh})
	if err != nil {
		f.t.Fatal(err)
	}
	return a
}

// workers creates claude and codex, claude the default.
func (f *fixture) workers() {
	f.agent("claude")
	f.agent("codex")
}

// holding lists the item IDs the worker has a live lease on.
func (f *fixture) holding(agent string) []string {
	f.t.Helper()
	items, err := f.s.Holding(f.ctx, agent)
	if err != nil {
		f.t.Fatal(err)
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return ids
}

func (f *fixture) holds(agent, itemID string) bool {
	for _, id := range f.holding(agent) {
		if id == itemID {
			return true
		}
	}
	return false
}

// slots caps a worker's concurrent jobs.
func (f *fixture) slots(agent string, n int) {
	f.t.Helper()
	if _, err := f.s.UpdateAgent(f.ctx, agent, AgentPatch{Slots: &n}); err != nil {
		f.t.Fatal(err)
	}
}

func TestProjectKeysAndNumbering(t *testing.T) {
	f := newFixture(t)
	f.workers()
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
	f.workers()
	f.project("EA")
	f.item("EA", "Fix the poller")

	it := f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	if it.Status != StatusInProgress || it.Assignee != "claude" || it.LeaseExpiresAt.IsZero() {
		t.Fatalf("claim: %+v", it)
	}
	// The other agent cannot take a live claim.
	f.refuse("EA-1", TransitionInput{Actor: "codex", Action: ActionClaim}, ErrInvalidTransition)
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim}, ErrInvalidTransition)
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
	f.refuse("EA-1", TransitionInput{Actor: "codex", Action: ActionClaim}, ErrInvalidTransition)
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
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim}, ErrInvalidTransition)
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
		if held := f.holding(a.ID); len(held) != 0 {
			t.Fatalf("%s still holds %v", a.ID, held)
		}
	}
}

func TestOwnerPaths(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	f.item("EA", "Question")
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionBlock}, ErrValidation)
	it := f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionBlock, Body: "which account?"})
	// Blocked keeps its worker: reopen hands it straight back.
	if it.Status != StatusBlocked || it.Assignee != "claude" || !it.LeaseExpiresAt.IsZero() {
		t.Fatalf("block: %+v", it)
	}
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionReopen}, ErrForbidden)
	it = f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionReopen, Body: "personal"})
	if it.Status != StatusOpen || it.Assignee != "claude" {
		t.Fatalf("reopen: %+v", it)
	}
	it = f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionClose})
	if it.Status != StatusClosed || it.ClosedAt.IsZero() || it.Assignee != "" {
		t.Fatalf("close: %+v", it)
	}
	// Closed before anyone implemented it: reopen puts it back in its
	// tier's pool.
	it = f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionReopen})
	if it.Status != StatusOpen || !it.ClosedAt.IsZero() || it.Assignee != "" || it.Tier != DefaultTier {
		t.Fatalf("reopen from closed: %+v", it)
	}
	// Owner request_changes from pending approval goes back to the implementer.
	toCodex := "codex"
	if _, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Assignee: &toCodex}, 0); err != nil {
		t.Fatalf("hand the reopened item to codex: %v", err)
	}
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
	f.workers()
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
	f.workers()
	f.project("EA")
	f.item("EA", "low thing")
	urgent, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "urgent thing", Priority: PriorityUrgent, Specced: true})
	if err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	forCodex, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "for codex", Assignee: "codex", Specced: true})
	if err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)

	// A disabled agent gets nothing, and is still recorded as seen.
	if _, err := f.s.SetAgentEnabled(f.ctx, "claude", false); err != nil {
		t.Fatal(err)
	}
	job, err := f.s.CheckIn(f.ctx, "claude", true, nil)
	if err != nil || job != nil {
		t.Fatalf("disabled agent: %v %+v", err, job)
	}
	a, _ := f.s.GetAgent(f.ctx, "claude")
	if a.LastSeenAt.IsZero() {
		t.Fatal("check-in not recorded")
	}
	f.s.SetAgentEnabled(f.ctx, "claude", true)

	// Urgent first out of the tier's pool — claude leads it — and the
	// claim is made.
	job, err = f.s.CheckIn(f.ctx, "claude", true, nil)
	if err != nil || job == nil || job.Kind != JobImplement || job.Item.ID != urgent.ID || job.Item.Assignee != "claude" {
		t.Fatalf("first job: %v %+v", err, job)
	}
	if job.Project == nil || job.Project.Key != "EA" || job.Pinned == nil || job.Chat == nil || job.Comments == nil {
		t.Fatalf("job context: %+v", job)
	}
	// A second slot takes the pool's next item; a live item is never
	// offered again.
	job, err = f.s.CheckIn(f.ctx, "claude", true, nil)
	if err != nil || job == nil || job.Item.Number != 1 {
		t.Fatalf("second slot: %v %+v", err, job)
	}
	if job, err = f.s.CheckIn(f.ctx, "claude", true, nil); err != nil || job != nil {
		t.Fatalf("pool empty for claude, got %v %+v", err, job)
	}
	// Codex gets its own pinned item; the pool was claude's while claude
	// was in.
	job, err = f.s.CheckIn(f.ctx, "codex", true, nil)
	if err != nil || job == nil || job.Item.ID != forCodex.ID || job.Item.Assignee != "codex" {
		t.Fatalf("codex job: %v %+v", err, job)
	}
	// A name with no row is not a worker; a check-in does not create one.
	if _, err := f.s.CheckIn(f.ctx, "gemini", true, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("check-in without a row: %v", err)
	}
	if _, err := f.s.GetAgent(f.ctx, "gemini"); !errors.Is(err, ErrNotFound) {
		t.Fatal("check-in created a row")
	}

	// Claude submits; codex, its tier-mate, takes the review in a free
	// slot, and with its slots all taken it could not.
	f.move("EA-2", TransitionInput{Actor: "claude", Action: ActionSubmit, Branch: "pm/ea-2"})
	f.slots("codex", 1)
	job, err = f.s.CheckIn(f.ctx, "codex", true, nil)
	if err != nil || job != nil {
		t.Fatalf("codex past its slots: %v %+v", err, job)
	}
	f.move("EA-3", TransitionInput{Actor: "codex", Action: ActionRelease})
	job, err = f.s.CheckIn(f.ctx, "codex", true, nil)
	if err != nil || job.Kind != JobReview || job.Item.ID != urgent.ID || job.Item.Reviewer != "codex" {
		t.Fatalf("review job: %v %+v", err, job)
	}
	f.move("EA-2", TransitionInput{Actor: "codex", Action: ActionReview, Verdict: VerdictApprove, Body: "fine"})
	f.move("EA-2", TransitionInput{Actor: ActorOwner, Action: ActionApprove})
	// Approved work of mine comes first, as a merge job under the merge policy.
	job, err = f.s.CheckIn(f.ctx, "claude", true, nil)
	if err != nil || job.Kind != JobMerge || job.Item.ID != urgent.ID || job.Item.LeaseExpiresAt.IsZero() {
		t.Fatalf("merge job: %v %+v", err, job)
	}
	policy := MergePolicyPullRequest
	if _, err := f.s.UpdateProject(f.ctx, "EA", ProjectPatch{MergePolicy: &policy}); err != nil {
		t.Fatal(err)
	}
	job, err = f.s.NextFor(f.ctx, "claude")
	if err != nil || job != nil {
		t.Fatalf("live merge job offered again: %v %+v", err, job)
	}
	f.tick(DefaultLease + time.Minute)
	job, err = f.s.NextFor(f.ctx, "claude")
	if err != nil || job.Kind != JobPullRequest {
		t.Fatalf("pr job: %v %+v", err, job)
	}
}

// A dead runner never moves work to a different worker: an expired lease is
// resumed by its assignee, and by nobody else until the owner reassigns it.
func TestExpiredLeaseStaysWithItsWorker(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	f.item("EA", "stuck")
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	if job, _ := f.s.NextFor(f.ctx, "claude"); job != nil {
		t.Fatalf("live lease offered again to holder: %+v", job)
	}
	if job, _ := f.s.NextFor(f.ctx, "codex"); job != nil {
		t.Fatalf("live lease offered to codex: %+v", job)
	}
	f.tick(DefaultLease + time.Minute)
	if job, _ := f.s.NextFor(f.ctx, "codex"); job != nil {
		t.Fatalf("expired lease offered to codex: %+v", job)
	}
	f.refuse("EA-1", TransitionInput{Actor: "codex", Action: ActionClaim}, ErrForbidden)
	job, err := f.s.NextFor(f.ctx, "claude")
	if err != nil || job == nil || job.Kind != JobImplement || job.Item.Number != 1 {
		t.Fatalf("expired lease not offered to its worker: %v %+v", err, job)
	}
	it := f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	if it.Status != StatusInProgress || it.Assignee != "claude" || !it.LeaseExpiresAt.After(f.at()) {
		t.Fatalf("reclaim: %+v", it)
	}
	if !f.holds("claude", it.ID) {
		t.Fatalf("claude not holding %s: %v", it.ID, f.holding("claude"))
	}
}

// The owner moves a dead runner's item through PATCH assignee: the branch
// and implementer stay, the lease goes, and the new worker's queue resumes it.
func TestReassignResumesExpiredWorkUnderNewWorker(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	f.item("EA", "half done")
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Branch: "pm/ea-1-half"})
	f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionReview, Verdict: VerdictRequestChanges, Body: "more"})
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	codex := "codex"
	if _, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Assignee: &codex}, 0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("reassign under a live lease: %v", err)
	}
	f.tick(DefaultLease + time.Minute)
	got, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Assignee: &codex}, 0)
	if err != nil || got.Status != StatusInProgress || got.Assignee != "codex" || got.Branch != "pm/ea-1-half" || got.Implementer != "claude" {
		t.Fatalf("reassign after expiry: %v %+v", err, got)
	}
	if !got.ClaimedAt.IsZero() || !got.LeaseExpiresAt.IsZero() {
		t.Fatalf("dead lease kept: %+v", got)
	}
	if held := f.holding("claude"); len(held) != 0 {
		t.Fatalf("claude still holding %v", held)
	}
	feed, _ := f.s.ItemComments(f.ctx, got.ID)
	last := feed[len(feed)-1]
	if last.Action != ActionAssign || last.Body != "assigned to codex" || last.Assignee != "codex" || last.FromStatus != StatusInProgress || last.ToStatus != StatusInProgress {
		t.Fatalf("assign feed line: %+v", last)
	}
	if job, _ := f.s.NextFor(f.ctx, "claude"); job != nil {
		t.Fatalf("reassigned item still offered to claude: %+v", job)
	}
	f.refuse("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim}, ErrForbidden)
	job, err := f.s.CheckIn(f.ctx, "codex", true, nil)
	if err != nil || job == nil || job.Kind != JobImplement || job.Item.ID != got.ID || job.Item.Assignee != "codex" || job.Item.Branch != "pm/ea-1-half" {
		t.Fatalf("resumed under codex: %v %+v", err, job)
	}
	if !f.holds("codex", got.ID) {
		t.Fatalf("codex not holding %s: %v", got.ID, f.holding("codex"))
	}
	it := f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionSubmit})
	if it.Status != StatusInReview || it.Implementer != "codex" || it.Branch != "pm/ea-1-half" {
		t.Fatalf("resubmit by the new worker: %+v", it)
	}
}

func TestAgentClaimsUpToItsSlots(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	f.item("EA", "first")
	f.item("EA", "second")
	f.slots("claude", 1)
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	f.refuse("EA-2", TransitionInput{Actor: "claude", Action: ActionClaim}, ErrInvalidTransition)
	if job, _ := f.s.NextFor(f.ctx, "claude"); job != nil {
		t.Fatalf("offered past its slots: %+v", job)
	}
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionRelease})
	if it := f.move("EA-2", TransitionInput{Actor: "claude", Action: ActionClaim}); it.Assignee != "claude" {
		t.Fatalf("claim after release: %+v", it)
	}
}

func TestUnworkableProjectsAreSkipped(t *testing.T) {
	f := newFixture(t)
	f.agent("claude")
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
	f.agent("claude")
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

func TestChatCursorWithUnchangedClock(t *testing.T) {
	f := newFixture(t)
	f.project("EA")
	first, err := f.s.PostChat(f.ctx, "EA", ActorOwner, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.s.PostChat(f.ctx, "EA", ActorOwner, "second")
	if err != nil {
		t.Fatal(err)
	}
	if !second.CreatedAt.After(first.CreatedAt) {
		t.Fatalf("chat timestamps did not advance: %s then %s", first.CreatedAt, second.CreatedAt)
	}
	after, err := f.s.ListChat(f.ctx, "EA", first.ID, 50)
	if err != nil || len(after) != 1 || after[0].ID != second.ID {
		t.Fatalf("after cursor: %v %+v", err, after)
	}
}

func TestEditedSourcesSynchronizeMentions(t *testing.T) {
	t.Run("item description", func(t *testing.T) {
		f := newFixture(t)
		f.workers()
		f.project("EA")
		it, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "q", Description: "ask @claude"})
		if err != nil {
			t.Fatal(err)
		}
		body := "ask @codex instead"
		if _, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Description: &body}, it.Revision); err != nil {
			t.Fatal(err)
		}
		if old, _ := f.s.UnansweredMentions(f.ctx, "claude"); len(old) != 0 {
			t.Fatalf("removed mention remains: %+v", old)
		}
		added, err := f.s.UnansweredMentions(f.ctx, "codex")
		if err != nil || len(added) != 1 || added[0].SourceKind != "item" || added[0].SourceID != it.ID {
			t.Fatalf("added mention: %v %+v", err, added)
		}
	})

	t.Run("post body", func(t *testing.T) {
		f := newFixture(t)
		f.project("EA")
		post, err := f.s.CreatePost(f.ctx, "EA", PostInput{Author: ActorOwner, Title: "q", Body: "ask @claude"})
		if err != nil {
			t.Fatal(err)
		}
		body := "ask @codex instead"
		if _, err := f.s.UpdatePost(f.ctx, post.ID, ActorOwner, PostPatch{Body: &body}); err != nil {
			t.Fatal(err)
		}
		if old, _ := f.s.UnansweredMentions(f.ctx, "claude"); len(old) != 0 {
			t.Fatalf("removed mention remains: %+v", old)
		}
		added, err := f.s.UnansweredMentions(f.ctx, "codex")
		if err != nil || len(added) != 1 || added[0].SourceKind != "post" || added[0].SourceID != post.ID {
			t.Fatalf("added mention: %v %+v", err, added)
		}
	})
}

func TestMentionsBecomeRespondJobs(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	f.item("EA", "open work")

	// A chat mention outranks open work, and answering in the room closes it.
	msg, err := f.s.PostChat(f.ctx, "EA", ActorOwner, "@claude what do you think about the queue? cc @codex")
	if err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	job, err := f.s.CheckIn(f.ctx, "claude", true, nil)
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
	job, _ = f.s.CheckIn(f.ctx, "codex", false, nil)
	if job == nil || job.Kind != JobRespond {
		t.Fatalf("codex respond: %+v", job)
	}
	// Now claude's next job is the open item.
	job, _ = f.s.CheckIn(f.ctx, "claude", false, nil)
	if job == nil || job.Kind != JobImplement {
		t.Fatalf("after answering: %+v", job)
	}

	// An item comment mention is answered by a transition comment on the item.
	if _, err := f.s.CommentOnItem(f.ctx, "EA-1", ActorOwner, "@claude take this one first"); err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	job, _ = f.s.CheckIn(f.ctx, "claude", false, nil)
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
		job, _ = f.s.CheckIn(f.ctx, "codex", true, nil)
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
	if !ValidAgentName("sol") || !ValidAgentName("gpt56sol") || ValidAgentName("a") || ValidAgentName("opus-2") ||
		ValidAgentName("claude_2") || ValidAgentName(strings.Repeat("a", 17)) {
		t.Fatal("agent name rule: 2–16 lowercase letters and digits")
	}
	if !ValidCLI(CLIClaude) || !ValidCLI(CLICodex) || ValidCLI("gemini") || ValidCLI("") {
		t.Fatal("clis")
	}
	for _, e := range Efforts {
		if !ValidEffort(CLIClaude, e) || !ValidEffort(CLICodex, e) {
			t.Fatalf("effort %s", e)
		}
	}
	if !ValidEffort(CLIClaude, "") || !ValidEffort(CLICodex, "") || !ValidEffort(CLIClaude, EffortMax) ||
		ValidEffort(CLICodex, EffortMax) || ValidEffort(CLIClaude, "ultra") {
		t.Fatal("efforts")
	}
	if StatusOrder(StatusPendingApproval) >= StatusOrder(StatusOpen) || StatusOrder(StatusDone) <= StatusOrder(StatusOpen) {
		t.Fatal("status order")
	}
}

func TestRunsRecordWhatAnAgentDid(t *testing.T) {
	f := newFixture(t)
	f.workers()
	p := f.project("EA")
	it := f.item("EA", "thing")
	if _, err := f.s.StartRun(f.ctx, RunInput{Agent: "claude", Kind: "dance", ItemID: it.ID}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad kind: %v", err)
	}
	if _, err := f.s.StartRun(f.ctx, RunInput{Agent: "claude", Kind: JobImplement}); !errors.Is(err, ErrValidation) {
		t.Fatalf("no subject: %v", err)
	}
	run, err := f.s.StartRun(f.ctx, RunInput{
		Agent: "claude", Kind: JobImplement, ItemID: it.ID, ProjectID: p.ID, Branch: "pm/ea-1-thing",
		Model: " fable ", Effort: EffortXHigh,
	})
	if err != nil || run.StartedAt.IsZero() || !run.FinishedAt.IsZero() || run.Model != "fable" || run.Effort != EffortXHigh {
		t.Fatalf("start: %v %+v", err, run)
	}
	f.tick(90 * time.Second)
	long := strings.Repeat("x", OutputTailMax+10)
	got, err := f.s.FinishRun(f.ctx, run.ID, RunResult{Outcome: "submitted", ExitStatus: "ok", Summary: "did it", OutputTail: long, CostUSD: 0.12})
	if err != nil || got.DurationSeconds != 90 || got.Outcome != "submitted" || len([]rune(got.OutputTail)) != OutputTailMax || got.CostUSD != 0.12 {
		t.Fatalf("finish: %v %+v", err, got)
	}
	if stored, _ := f.s.GetRun(f.ctx, run.ID); stored.Model != "fable" || stored.Effort != EffortXHigh {
		t.Fatalf("model and effort not on the row: %+v", stored)
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
	// With no worker there is nothing to assign an item to.
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "x"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("no workers: %v", err)
	}
	f.workers()
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "x", Assignee: "Owner"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad assignee: %v", err)
	}
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "x", Assignee: "gemini"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("assignee without a row: %v", err)
	}
	// Filed without an assignee, the item sits unpinned in the top tier's
	// pool; nothing is a hand-off in the feed.
	def := f.item("EA", "for the pool")
	if def.Assignee != "" || def.Tier != DefaultTier {
		t.Fatalf("pool filing: %+v", def)
	}
	if feed, _ := f.s.ItemComments(f.ctx, def.ID); len(feed) != 0 {
		t.Fatalf("pool filing in the feed: %+v", feed[0])
	}
	it, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "for codex", Assignee: "codex", Specced: true})
	if err != nil || it.Assignee != "codex" {
		t.Fatalf("assigned create: %v %+v", err, it)
	}
	f.tick(time.Second)
	// The pool is the lead's while it is in; a pinned item is nobody
	// else's.
	if job, _ := f.s.NextFor(f.ctx, "claude"); job == nil || job.Item.ID != def.ID {
		t.Fatalf("claude offered the wrong item: %+v", job)
	}
	f.refuse("EA-2", TransitionInput{Actor: "claude", Action: ActionClaim}, ErrInvalidTransition)
	job, _ := f.s.NextFor(f.ctx, "codex")
	if job == nil || job.Item.ID != it.ID {
		t.Fatalf("codex not offered its item: %+v", job)
	}
	// Re-pinning an open item records the hand-off; unpinning returns it
	// to the pool; a name without a row is refused.
	claude := "claude"
	got, err := f.s.UpdateItem(f.ctx, "EA-2", ItemPatch{Assignee: &claude}, 0)
	if err != nil || got.Assignee != "claude" {
		t.Fatalf("reassign: %v %+v", err, got)
	}
	gemini := "gemini"
	if _, err := f.s.UpdateItem(f.ctx, "EA-2", ItemPatch{Assignee: &gemini}, 0); !errors.Is(err, ErrValidation) {
		t.Fatalf("reassign to no row: %v", err)
	}
	none := ""
	if got, err = f.s.UpdateItem(f.ctx, "EA-2", ItemPatch{Assignee: &none}, 0); err != nil || got.Assignee != "" {
		t.Fatalf("unpin: %v %+v", err, got)
	}
	feed, _ := f.s.ItemComments(f.ctx, it.ID)
	var assigns []string
	var named []string
	for _, c := range feed {
		if c.Action == ActionAssign {
			assigns = append(assigns, c.Body)
			named = append(named, c.Assignee)
		}
	}
	if len(assigns) != 3 || assigns[0] != "assigned to codex" || assigns[1] != "assigned to claude" || assigns[2] != "returned to the tier 10 pool" {
		t.Fatalf("assign feed: %v", assigns)
	}
	// The name is a field of its own, not something read back out of the
	// prose; an unpin leaves it empty.
	if len(named) != 3 || named[0] != "codex" || named[1] != "claude" || named[2] != "" {
		t.Fatalf("assign feed assignees: %q", named)
	}
	// Once claimed and live, the owner cannot swap the holder or unpin.
	f.move("EA-2", TransitionInput{Actor: "claude", Action: ActionClaim})
	if _, err := f.s.UpdateItem(f.ctx, "EA-2", ItemPatch{Assignee: &none}, 0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("unpin in progress: %v", err)
	}
	codex := "codex"
	if _, err := f.s.UpdateItem(f.ctx, "EA-2", ItemPatch{Assignee: &codex}, 0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("reassign in progress: %v", err)
	}
	// An unchanged assignee is not a reassignment and passes in any status.
	same := "claude"
	if _, err := f.s.UpdateItem(f.ctx, "EA-2", ItemPatch{Assignee: &same}, 0); err != nil {
		t.Fatalf("same assignee: %v", err)
	}
}

// Workers are rows the owner creates: the first in a tier leads it, the
// lead moves and is never cleared, and a row leaves only when nothing
// depends on it.
func TestWorkersAreOwnerData(t *testing.T) {
	f := newFixture(t)
	if top, err := f.s.TopTier(f.ctx); err != nil || top != 0 {
		t.Fatalf("top tier with no rows: %v %d", err, top)
	}
	for _, in := range []AgentInput{
		{Name: "a", CLI: CLIClaude, Model: "opus"},
		{Name: "owner", CLI: CLIClaude, Model: "opus"},
		{Name: "opus-2", CLI: CLIClaude, Model: "opus"},
		{Name: "opus", CLI: "gemini", Model: "opus"},
		{Name: "opus", CLI: CLIClaude},
		{Name: "sol", CLI: CLICodex, Model: "gpt-5.6-sol", Effort: EffortMax},
		{Name: "opus", CLI: CLIClaude, Model: "opus", Effort: "ultra"},
	} {
		if _, err := f.s.CreateAgent(f.ctx, in); !errors.Is(err, ErrValidation) {
			t.Fatalf("%+v: %v", in, err)
		}
	}
	opus, err := f.s.CreateAgent(f.ctx, AgentInput{Name: "opus", CLI: CLIClaude, Model: "opus", Effort: EffortMax})
	if err != nil || !opus.Lead || opus.Tier != DefaultTier || opus.Slots != DefaultSlots || opus.Label != "opus" || !opus.Enabled || opus.CLI != CLIClaude || opus.Model != "opus" || opus.Effort != EffortMax {
		t.Fatalf("first worker: %v %+v", err, opus)
	}
	if _, err := f.s.CreateAgent(f.ctx, AgentInput{Name: "opus", CLI: CLICodex, Model: "x"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name: %v", err)
	}
	fable, err := f.s.CreateAgent(f.ctx, AgentInput{Name: "fable", Label: " Fable XHigh (Claude) ", CLI: CLIClaude, Model: "fable", Effort: EffortXHigh})
	if err != nil || fable.Lead || fable.Label != "Fable XHigh (Claude)" {
		t.Fatalf("second worker: %v %+v", err, fable)
	}
	if _, err := f.s.CreateAgent(f.ctx, AgentInput{Name: "sol", CLI: CLICodex, Model: "gpt-5.6-sol", Effort: EffortXHigh}); err != nil {
		t.Fatal(err)
	}
	for _, in := range []AgentInput{
		{Name: "neg", CLI: CLIClaude, Model: "x", Tier: -1},
		{Name: "neg", CLI: CLIClaude, Model: "x", Slots: -1},
	} {
		if _, err := f.s.CreateAgent(f.ctx, in); !errors.Is(err, ErrValidation) {
			t.Fatalf("%+v: %v", in, err)
		}
	}
	agents, err := f.s.ListAgents(f.ctx)
	if err != nil || len(agents) != 3 || agents[0].ID != "opus" || agents[1].ID != "fable" || agents[2].ID != "sol" {
		t.Fatalf("lead first, then by name: %v %+v", err, agents)
	}

	// The lead moves; it is never cleared.
	yes, no := true, false
	if _, err := f.s.UpdateAgent(f.ctx, "opus", AgentPatch{Lead: &no}); !errors.Is(err, ErrConflict) {
		t.Fatalf("clearing the lead: %v", err)
	}
	got, err := f.s.UpdateAgent(f.ctx, "fable", AgentPatch{Lead: &yes})
	if err != nil || !got.Lead {
		t.Fatalf("move lead: %v %+v", err, got)
	}
	if opus, _ = f.s.GetAgent(f.ctx, "opus"); opus.Lead {
		t.Fatal("two leads")
	}
	agents, _ = f.s.ListAgents(f.ctx)
	if agents[0].ID != "fable" || agents[1].ID != "opus" {
		t.Fatalf("list after move: %+v", agents)
	}
	if got, err = f.s.SetLead(f.ctx, "fable"); err != nil || !got.Lead {
		t.Fatalf("lead made lead again: %v %+v", err, got)
	}
	// A tier move: the mover leads an empty tier, the tier it left keeps
	// its lead, and the list runs strongest tier first.
	eleven := 11
	if got, err = f.s.UpdateAgent(f.ctx, "sol", AgentPatch{Tier: &eleven}); err != nil || got.Tier != 11 || !got.Lead {
		t.Fatalf("sol to tier 11: %v %+v", err, got)
	}
	if top, _ := f.s.TopTier(f.ctx); top != 11 {
		t.Fatalf("top tier: %d", top)
	}
	agents, _ = f.s.ListAgents(f.ctx)
	if agents[0].ID != "sol" || agents[1].ID != "fable" || !agents[1].Lead || agents[2].ID != "opus" {
		t.Fatalf("list by tier: %+v", agents)
	}
	// The lead moving into a tier that has one becomes a backup there, and
	// the tier it left promotes its first worker by name.
	if got, err = f.s.UpdateAgent(f.ctx, "fable", AgentPatch{Tier: &eleven}); err != nil || got.Lead {
		t.Fatalf("fable to tier 11: %v %+v", err, got)
	}
	if opus, _ = f.s.GetAgent(f.ctx, "opus"); !opus.Lead {
		t.Fatal("opus not promoted in the tier fable left")
	}
	ten, zero := 10, 0
	if _, err := f.s.UpdateAgent(f.ctx, "fable", AgentPatch{Tier: &zero}); !errors.Is(err, ErrValidation) {
		t.Fatalf("tier zero: %v", err)
	}
	if _, err := f.s.UpdateAgent(f.ctx, "fable", AgentPatch{Slots: &zero}); !errors.Is(err, ErrValidation) {
		t.Fatalf("slots zero: %v", err)
	}
	if got, err = f.s.UpdateAgent(f.ctx, "fable", AgentPatch{Tier: &ten, Lead: &yes}); err != nil || got.Tier != 10 || !got.Lead {
		t.Fatalf("fable back as lead of 10: %v %+v", err, got)
	}
	if opus, _ = f.s.GetAgent(f.ctx, "opus"); opus.Lead {
		t.Fatal("opus still lead after fable took it")
	}
	if _, err := f.s.UpdateAgent(f.ctx, "sol", AgentPatch{Tier: &ten}); err != nil {
		t.Fatal(err)
	}

	// Patches: the effort must suit the CLI after the patch; the model
	// stays required; an empty label falls back to the name.
	codex := CLICodex
	if _, err := f.s.UpdateAgent(f.ctx, "opus", AgentPatch{CLI: &codex}); !errors.Is(err, ErrValidation) {
		t.Fatalf("max effort on codex: %v", err)
	}
	high := EffortHigh
	if got, err = f.s.UpdateAgent(f.ctx, "opus", AgentPatch{CLI: &codex, Effort: &high}); err != nil || got.CLI != CLICodex || got.Effort != EffortHigh {
		t.Fatalf("retune: %v %+v", err, got)
	}
	empty := ""
	if _, err := f.s.UpdateAgent(f.ctx, "opus", AgentPatch{Model: &empty}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty model: %v", err)
	}
	if got, _ = f.s.UpdateAgent(f.ctx, "opus", AgentPatch{Label: &empty}); got.Label != "opus" {
		t.Fatalf("empty label: %+v", got)
	}
	if _, err := f.s.UpdateAgent(f.ctx, "nobody", AgentPatch{Enabled: &no}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("patch without a row: %v", err)
	}
	if got, err = f.s.SetAgentEnabled(f.ctx, "sol", false); err != nil || got.Enabled {
		t.Fatalf("pause: %v %+v", err, got)
	}
	if _, err := f.s.SetAgentEnabled(f.ctx, "nobody", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pause without a row: %v", err)
	}

	// Deleting: a holder and the assignee of unfinished work are refused;
	// a missing row is not found.
	if err := f.s.DeleteAgent(f.ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete without a row: %v", err)
	}
	f.project("EA")
	if it := f.item("EA", "for the pool"); it.Assignee != "" || it.Tier != 10 {
		t.Fatalf("pool filing: %+v", it)
	}
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "for sol", Assignee: "sol"}); err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "for opus", Assignee: "opus"}); err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	if err := f.s.DeleteAgent(f.ctx, "sol"); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete the assignee of an open item: %v", err)
	}
	f.refuse("EA-3", TransitionInput{Actor: "nobody", Action: ActionClaim}, ErrNotFound)
	f.move("EA-3", TransitionInput{Actor: "opus", Action: ActionClaim})
	if err := f.s.DeleteAgent(f.ctx, "opus"); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete a holder: %v", err)
	}
	f.move("EA-2", TransitionInput{Actor: ActorOwner, Action: ActionClose})
	if err := f.s.DeleteAgent(f.ctx, "sol"); err != nil {
		t.Fatalf("delete once its item is closed: %v", err)
	}
	if _, err := f.s.GetAgent(f.ctx, "sol"); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted row still read")
	}
	// Release keeps the item with its worker, so opus is still its assignee.
	if it := f.move("EA-3", TransitionInput{Actor: "opus", Action: ActionRelease}); it.Assignee != "opus" || it.Status != StatusOpen {
		t.Fatalf("release: %+v", it)
	}
	if err := f.s.DeleteAgent(f.ctx, "opus"); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete the assignee of a released item: %v", err)
	}
	toFable := "fable"
	if _, err := f.s.UpdateItem(f.ctx, "EA-3", ItemPatch{Assignee: &toFable}, 0); err != nil {
		t.Fatalf("reassign the released item: %v", err)
	}
	if err := f.s.DeleteAgent(f.ctx, "opus"); err != nil {
		t.Fatalf("delete once reassigned: %v", err)
	}
	if agents, _ = f.s.ListAgents(f.ctx); len(agents) != 1 || agents[0].ID != "fable" {
		t.Fatalf("after deletes: %+v", agents)
	}
}

func TestActivityMergesEverythingNewestFirst(t *testing.T) {
	f := newFixture(t)
	f.workers()
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

func TestReopenRestoresAWorker(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	f.item("EA", "Poller")
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Branch: "pm/ea-1", Body: "done"})
	f.move("EA-1", TransitionInput{Actor: "codex", Action: ActionReview, Verdict: VerdictApprove, Body: "fine"})
	f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionApprove})
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	it := f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionComplete, Body: "merged"})
	if it.Status != StatusDone || it.Assignee != "" {
		t.Fatalf("complete: %+v", it)
	}
	// A done item reopens under the worker that implemented it, even when
	// the tier's lead is someone else.
	if _, err := f.s.SetLead(f.ctx, "codex"); err != nil {
		t.Fatal(err)
	}
	it = f.move("EA-1", TransitionInput{Actor: ActorOwner, Action: ActionReopen})
	if it.Status != StatusOpen || it.Assignee != "claude" {
		t.Fatalf("reopen from done: %+v", it)
	}
	// Released work stays with its worker and is offered to it again.
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	it = f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionRelease})
	if it.Status != StatusOpen || it.Assignee != "claude" {
		t.Fatalf("release: %+v", it)
	}
	job, err := f.s.NextFor(f.ctx, "codex")
	if err != nil || job != nil {
		t.Fatalf("codex offered claude's released item: %+v %v", job, err)
	}
	if job, err = f.s.NextFor(f.ctx, "claude"); err != nil || job == nil || job.Kind != JobImplement {
		t.Fatalf("claude not offered its released item: %+v %v", job, err)
	}
}

// ------------------------------------------------------------------- wait

type waitResult struct {
	ready bool
	err   error
}

// wait runs Wait in the background; the result lands on the channel. The
// fixture's clock must not tick while the wait is live.
func (f *fixture) wait(ctx context.Context, agent string, timeout time.Duration) <-chan waitResult {
	ch := make(chan waitResult, 1)
	go func() {
		ready, err := f.s.Wait(ctx, agent, timeout)
		ch <- waitResult{ready: ready, err: err}
	}()
	return ch
}

func (f *fixture) settle(ch <-chan waitResult) waitResult {
	f.t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		f.t.Fatal("Wait did not return")
		return waitResult{}
	}
}

// parked gives a background Wait time to reach its park before the write
// that should wake it. The assertions hold either way; the pause is what
// makes the test exercise the park rather than the first check.
const parked = 20 * time.Millisecond

func TestWakeClosesTheGeneration(t *testing.T) {
	f := newFixture(t)
	before := f.s.generation()
	select {
	case <-before:
		t.Fatal("generation closed before any wake")
	default:
	}
	f.s.wake()
	select {
	case <-before:
	default:
		t.Fatal("wake left the generation open")
	}
	after := f.s.generation()
	if after == before {
		t.Fatal("wake did not replace the generation")
	}
	select {
	case <-after:
		t.Fatal("new generation already closed")
	default:
	}
}

func TestWaitReturnsAtOnceWhenAJobExists(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	it := f.item("EA", "ready work")
	ready, err := f.s.Wait(f.ctx, "claude", 10*time.Millisecond)
	if err != nil || !ready {
		t.Fatalf("Wait with work: %v %v", ready, err)
	}
	// Nothing was claimed and no check-in was recorded.
	got, _, err := f.s.GetItem(f.ctx, "EA-1")
	if err != nil || got.Status != StatusOpen || got.Revision != it.Revision {
		t.Fatalf("Wait touched the item: %v %+v", err, got)
	}
	if a, _ := f.s.GetAgent(f.ctx, "claude"); !a.LastSeenAt.IsZero() {
		t.Fatal("Wait recorded a check-in")
	}
	// A wait for the other worker, whose queue is empty, times out.
	ready, err = f.s.Wait(f.ctx, "codex", 10*time.Millisecond)
	if err != nil || ready {
		t.Fatalf("Wait without work: %v %v", ready, err)
	}
}

func TestWaitWakesOnCreateItem(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	ch := f.wait(f.ctx, "claude", 2*time.Second)
	time.Sleep(parked)
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "new work"}); err != nil {
		t.Fatal(err)
	}
	if r := f.settle(ch); r.err != nil || !r.ready {
		t.Fatalf("woken by CreateItem: %+v", r)
	}
}

func TestWaitWakesOnChatMention(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	ch := f.wait(f.ctx, "codex", 2*time.Second)
	time.Sleep(parked)
	if _, err := f.s.PostChat(f.ctx, "EA", ActorOwner, "@codex is the cursor kept in the poller?"); err != nil {
		t.Fatal(err)
	}
	if r := f.settle(ch); r.err != nil || !r.ready {
		t.Fatalf("woken by a chat mention: %+v", r)
	}
	// The general room counts too.
	if _, err := f.s.PostChat(f.ctx, "EA", "codex", "yes"); err != nil {
		t.Fatal(err)
	}
	ch = f.wait(f.ctx, "codex", 2*time.Second)
	time.Sleep(parked)
	if _, err := f.s.PostChat(f.ctx, "", ActorOwner, "@codex ping"); err != nil {
		t.Fatal(err)
	}
	if r := f.settle(ch); r.err != nil || !r.ready {
		t.Fatalf("woken by a general-room mention: %+v", r)
	}
}

func TestWaitWakesOnSubmitForTheReviewer(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	f.item("EA", "Fix the poller")
	f.move("EA-1", TransitionInput{Actor: "claude", Action: ActionClaim})
	ch := f.wait(f.ctx, "codex", 2*time.Second)
	time.Sleep(parked)
	if _, err := f.s.Transition(f.ctx, "EA-1", TransitionInput{Actor: "claude", Action: ActionSubmit, Branch: "pm/ea-1", Body: "done"}); err != nil {
		t.Fatal(err)
	}
	r := f.settle(ch)
	if r.err != nil || !r.ready {
		t.Fatalf("woken by submit: %+v", r)
	}
	if job, _ := f.s.NextFor(f.ctx, "codex"); job == nil || job.Kind != JobReview {
		t.Fatalf("codex's job after the wake: %+v", job)
	}
}

func TestWaitWakesOnReassignment(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "for codex", Assignee: "codex", Specced: true}); err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	ch := f.wait(f.ctx, "claude", 2*time.Second)
	time.Sleep(parked)
	claude := "claude"
	if _, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Assignee: &claude}, 0); err != nil {
		t.Fatal(err)
	}
	if r := f.settle(ch); r.err != nil || !r.ready {
		t.Fatalf("woken by reassignment: %+v", r)
	}
}

// A wake for someone else's work re-checks and parks again; the wait then
// times out with false. A notice wakes nobody.
func TestWaitTimesOutOnAnotherWorkersWork(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	ch := f.wait(f.ctx, "codex", 150*time.Millisecond)
	time.Sleep(parked)
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "for claude"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.PostNotice(f.ctx, "EA", "claude", "claude started EA-1 for @codex"); err != nil {
		t.Fatal(err)
	}
	if r := f.settle(ch); r.err != nil || r.ready {
		t.Fatalf("codex woken into claude's work: %+v", r)
	}
	if job, _ := f.s.NextFor(f.ctx, "claude"); job == nil {
		t.Fatal("claude's work is missing")
	}
}

func TestWaitIsNeverReadyForADisabledWorker(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	f.item("EA", "work")
	if _, err := f.s.SetAgentEnabled(f.ctx, "claude", false); err != nil {
		t.Fatal(err)
	}
	ready, err := f.s.Wait(f.ctx, "claude", 30*time.Millisecond)
	if err != nil || ready {
		t.Fatalf("disabled worker: %v %v", ready, err)
	}
	// Un-pausing wakes the wait, and now the work counts.
	ch := f.wait(f.ctx, "claude", 2*time.Second)
	time.Sleep(parked)
	if _, err := f.s.SetAgentEnabled(f.ctx, "claude", true); err != nil {
		t.Fatal(err)
	}
	if r := f.settle(ch); r.err != nil || !r.ready {
		t.Fatalf("woken by enable: %+v", r)
	}
}

func TestWaitWakesWhenAProjectBecomesWorkable(t *testing.T) {
	f := newFixture(t)
	f.workers()
	// No repository path: the item is filed but not on the queue.
	if _, err := f.s.CreateProject(f.ctx, ProjectInput{Key: "EA", Name: "EA project"}); err != nil {
		t.Fatal(err)
	}
	f.item("EA", "waiting for a repo")
	if ready, err := f.s.Wait(f.ctx, "claude", 30*time.Millisecond); err != nil || ready {
		t.Fatalf("project without a repo_path: %v %v", ready, err)
	}
	ch := f.wait(f.ctx, "claude", 2*time.Second)
	time.Sleep(parked)
	repo := "/tmp/EA"
	if _, err := f.s.UpdateProject(f.ctx, "EA", ProjectPatch{RepoPath: &repo}); err != nil {
		t.Fatal(err)
	}
	if r := f.settle(ch); r.err != nil || !r.ready {
		t.Fatalf("woken by repo_path: %+v", r)
	}
	// Archived: off the queue again; un-archiving wakes the wait.
	archived, active := ProjectArchived, ProjectActive
	if _, err := f.s.UpdateProject(f.ctx, "EA", ProjectPatch{Status: &archived}); err != nil {
		t.Fatal(err)
	}
	if ready, err := f.s.Wait(f.ctx, "claude", 30*time.Millisecond); err != nil || ready {
		t.Fatalf("archived project: %v %v", ready, err)
	}
	ch = f.wait(f.ctx, "claude", 2*time.Second)
	time.Sleep(parked)
	if _, err := f.s.UpdateProject(f.ctx, "EA", ProjectPatch{Status: &active}); err != nil {
		t.Fatal(err)
	}
	if r := f.settle(ch); r.err != nil || !r.ready {
		t.Fatalf("woken by un-archive: %+v", r)
	}
	// An edit that changes neither wakes nobody: the other worker's wait
	// times out as before.
	name := "renamed"
	if _, err := f.s.UpdateProject(f.ctx, "EA", ProjectPatch{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if ready, err := f.s.Wait(f.ctx, "codex", 30*time.Millisecond); err != nil || ready {
		t.Fatalf("other worker after a rename: %v %v", ready, err)
	}
}

func TestWaitStopsWhenTheContextEnds(t *testing.T) {
	f := newFixture(t)
	f.workers()
	f.project("EA")
	ctx, cancel := context.WithCancel(f.ctx)
	ch := f.wait(ctx, "claude", 2*time.Second)
	time.Sleep(parked)
	cancel()
	if r := f.settle(ch); !errors.Is(r.err, context.Canceled) || r.ready {
		t.Fatalf("cancelled wait: %+v", r)
	}
}

func TestWaitForAnUnknownWorkerIsNotFound(t *testing.T) {
	f := newFixture(t)
	f.workers()
	if _, err := f.s.Wait(f.ctx, "gemini", 10*time.Millisecond); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown worker: %v", err)
	}
	if _, err := f.s.GetAgent(f.ctx, "gemini"); !errors.Is(err, ErrNotFound) {
		t.Fatal("Wait created a row")
	}
}
