package gowild_projects

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
	gowild_dbx "github.com/original-david-knight/go_wild/data/dbx"
)

// Run outcomes beyond the job outcomes the agent itself reports (submitted,
// blocked, approve, request_changes, merged, opened, answered, failed).
const (
	RunOutcomeTimeout = "timeout"
	RunOutcomeCrashed = "crashed"
	RunOutcomeSkipped = "skipped"
)

// OutputTailMax caps what a run keeps of the agent's output, in runes.
const OutputTailMax = 4000

// MaxItemFailures is how many consecutive failed runs an item takes before
// the tracker holds it for the owner. Without the cap a failing job is
// re-offered the moment it is released — priority-1 merge jobs retried at
// full speed forever in the crash-loops of 2026-09-01 and 2026-09-02 —
// and the worker never reaches the rest of its queue.
const MaxItemFailures = 3

// Run is one execution of an agent by the runner: what it was asked to do,
// how long it took, how it ended, and the tail of what it printed. It is the
// answer to "what did claude do at three in the morning".
type Run struct {
	ID    string `json:"id"`
	Agent string `json:"agent"`
	// Kind is the job kind: respond, implement, review, merge, pull_request.
	Kind string `json:"kind"`
	// ItemID or MentionID names the job's subject; ProjectID is "" for the
	// general room.
	ItemID    string    `json:"item_id"`
	MentionID string    `json:"mention_id"`
	ProjectID string    `json:"project_id"`
	Branch    string    `json:"branch"`
	Worktree  string    `json:"worktree"`
	StartedAt time.Time `json:"started_at"`
	// FinishedAt is zero while the run is still going.
	FinishedAt time.Time `json:"finished_at"`
	// Outcome is the parsed outcome block, or timeout / crashed / skipped.
	Outcome string `json:"outcome"`
	// ExitStatus is the process's fate in words: "ok", or the error.
	ExitStatus      string `json:"exit_status"`
	DurationSeconds int    `json:"duration_seconds"`
	// Summary is the outcome block's summary.
	Summary string `json:"summary"`
	// OutputTail is the last OutputTailMax runes of the agent's output.
	OutputTail string `json:"output_tail"`
	// Model and Effort are what the runner launched, copied from the agent
	// row at check-in, so a worker can be retuned without rewriting history.
	Model  string `json:"model"`
	Effort string `json:"effort"`
	// Token and cost columns are filled once the runners report them; zero
	// until then.
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// TableName pins the table name against a later model rename.
func (Run) TableName() string { return "project_runs" }

// RunInput starts a run. Model and Effort are what the runner is about to
// launch, as the agent row said at check-in.
type RunInput struct {
	Agent     string
	Kind      string
	ItemID    string
	MentionID string
	ProjectID string
	Branch    string
	Worktree  string
	Model     string
	Effort    string
}

// RunResult finishes a run.
type RunResult struct {
	Outcome      string
	ExitStatus   string
	Summary      string
	OutputTail   string
	InputTokens  int
	OutputTokens int
	CostUSD      float64
}

// RunFilter narrows a listing; zero fields match everything.
type RunFilter struct {
	Agent     string
	ItemID    string
	ProjectID string
	// Limit caps the result, newest first; 0 means 50.
	Limit int
}

// StartRun records that an agent has begun a job.
func (s *Service) StartRun(ctx context.Context, in RunInput) (*Run, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	if !ValidAgentName(in.Agent) {
		return nil, validationf("run agent %q is not an agent name", in.Agent)
	}
	switch in.Kind {
	case JobRespond, JobImplement, JobReview, JobMerge, JobPullRequest, JobGroom:
	default:
		return nil, validationf("run kind %q is unknown", in.Kind)
	}
	if in.ItemID == "" && in.MentionID == "" {
		return nil, validationf("a run needs an item or a mention")
	}
	run := &Run{
		ID: newID(), Agent: in.Agent, Kind: in.Kind, ItemID: in.ItemID, MentionID: in.MentionID,
		ProjectID: in.ProjectID, Branch: in.Branch, Worktree: in.Worktree,
		Model: strings.TrimSpace(in.Model), Effort: strings.TrimSpace(in.Effort), StartedAt: s.Now(),
	}
	if err := db.Table(Run{}).Insert(ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}

// FinishRun closes a run. A run already finished is left as it was.
func (s *Service) FinishRun(ctx context.Context, id string, res RunResult) (*Run, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	run, err := gowild_dbx.Get[Run](ctx, db, id)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, fmt.Errorf("%w: run %s", ErrNotFound, id)
	}
	if !run.FinishedAt.IsZero() {
		return run, nil
	}
	now := s.Now()
	run.FinishedAt = now
	run.DurationSeconds = int(now.Sub(run.StartedAt).Round(time.Second) / time.Second)
	run.Outcome = strings.TrimSpace(res.Outcome)
	run.ExitStatus = strings.TrimSpace(res.ExitStatus)
	run.Summary = strings.TrimSpace(res.Summary)
	run.OutputTail = tail(res.OutputTail, OutputTailMax)
	run.InputTokens, run.OutputTokens, run.CostUSD = res.InputTokens, res.OutputTokens, res.CostUSD
	if err := db.Table(Run{}).Update(ctx, run); err != nil {
		return nil, err
	}
	if err := s.noteRunOutcome(ctx, db, run, now); err != nil {
		return nil, err
	}
	return run, nil
}

// noteRunOutcome maintains the item's consecutive-failure count from the
// run that just finished: a failed run raises it and, at MaxItemFailures,
// holds the item with a comment saying why; a settled run resets it. An
// item that vanished has nothing to maintain.
func (s *Service) noteRunOutcome(ctx context.Context, db gowild_data.Database, run *Run, now time.Time) error {
	if run.ItemID == "" {
		return nil
	}
	it, err := gowild_dbx.Get[Item](ctx, db, run.ItemID)
	if err != nil || it == nil {
		return err
	}
	failed := false
	switch run.Outcome {
	case RunOutcomeCrashed, RunOutcomeTimeout, RunOutcomeSkipped, "failed":
		failed = true
	}
	if !failed {
		if it.Failures == 0 {
			return nil
		}
		it.Failures = 0
		it.UpdatedAt = now
		return db.Table(Item{}).Update(ctx, it)
	}
	it.Failures++
	holdNow := it.Failures >= MaxItemFailures && !it.Held
	if holdNow {
		it.Held = true
	}
	it.UpdatedAt = now
	if err := db.Table(Item{}).Update(ctx, it); err != nil {
		return err
	}
	if !holdNow {
		return nil
	}
	p, err := gowild_dbx.Get[Project](ctx, db, it.ProjectID)
	if err != nil {
		return err
	}
	key := it.ID
	if p != nil {
		key = ItemKey(p, it)
	}
	body := fmt.Sprintf("Runner (%s): %d consecutive runs on this item failed; the tracker holds it so the queue moves on. When the cause is dealt with, pm unhold %s resumes it.", run.Agent, it.Failures, key)
	c := &Comment{
		ID: newID(), TargetKind: TargetItem, TargetID: it.ID, Author: run.Agent,
		Kind: CommentKindComment, Body: body, CreatedAt: now,
	}
	return db.Table(Comment{}).Insert(ctx, c)
}

// GetRun reads one run.
func (s *Service) GetRun(ctx context.Context, id string) (*Run, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	run, err := gowild_dbx.Get[Run](ctx, db, id)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, fmt.Errorf("%w: run %s", ErrNotFound, id)
	}
	return run, nil
}

// ListRuns returns runs newest first.
func (s *Service) ListRuns(ctx context.Context, f RunFilter) ([]*Run, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	where := map[string]any{}
	if f.Agent != "" {
		where["agent"] = f.Agent
	}
	if f.ItemID != "" {
		where["item_id"] = f.ItemID
	}
	if f.ProjectID != "" {
		where["project_id"] = f.ProjectID
	}
	rows, err := gowild_dbx.All[Run](ctx, db, gowild_data.QueryOpts{Where: where})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].StartedAt.After(rows[j].StartedAt) })
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

// tail keeps the last max runes of s.
func tail(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[len(r)-max:])
}
