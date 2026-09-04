// Package gowild_projects is the core of a small work tracker for one owner
// and a few coding agents: projects, work items with a fixed state machine,
// an activity feed, a message board, chat rooms, and the queue rules that
// decide what an agent should do when it checks in. It holds no HTTP and no
// product policy — consumers put an API and a runner over it.
//
// Tables register at init() through the data module's registry; the
// consumer calls gowild_data.AddAllTables. The additive-only migration rule
// applies: never rename or retype a column.
package gowild_projects

import (
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

// Item statuses. The transitions between them are fixed in items.go.
const (
	StatusOpen            = "open"
	StatusInProgress      = "in_progress"
	StatusInReview        = "in_review"
	StatusPendingApproval = "pending_approval"
	StatusApproved        = "approved"
	StatusDone            = "done"
	StatusBlocked         = "blocked"
	StatusClosed          = "closed"
)

// Statuses lists every item status in board order.
var Statuses = []string{
	StatusOpen, StatusInProgress, StatusInReview, StatusPendingApproval,
	StatusApproved, StatusBlocked, StatusDone, StatusClosed,
}

// Item types.
const (
	TypeFeature = "feature"
	TypeBug     = "bug"
	TypeChore   = "chore"
	// TypeCodeReview is a review of somebody else's pull request: the item
	// carries the PR's URL, a worker reviews the PR head and hands the
	// review back as its submit, and the owner's approve closes it. It is
	// never groomed and never merged.
	TypeCodeReview = "code_review"
)

// Types lists every item type.
var Types = []string{TypeFeature, TypeBug, TypeChore, TypeCodeReview}

// Item priorities, lowest first.
const (
	PriorityLow    = "low"
	PriorityNormal = "normal"
	PriorityHigh   = "high"
	PriorityUrgent = "urgent"
)

// Priorities lists every priority, lowest first.
var Priorities = []string{PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent}

// Project merge policies: what the implementer does once the owner approves.
const (
	// MergePolicyMerge fast-forwards the default branch.
	MergePolicyMerge = "merge"
	// MergePolicyPullRequest pushes the branch and opens a pull request; the
	// item is done there and someone else's process takes over.
	MergePolicyPullRequest = "pull_request"
)

// Project statuses.
const (
	ProjectActive   = "active"
	ProjectArchived = "archived"
)

// ActorOwner is the one human. Every other actor string is an agent name.
const ActorOwner = "owner"

// Transition actions.
const (
	ActionClaim          = "claim"
	ActionRelease        = "release"
	ActionSubmit         = "submit"
	ActionReview         = "review"
	ActionBlock          = "block"
	ActionApprove        = "approve"
	ActionRequestChanges = "request_changes"
	ActionComplete       = "complete"
	ActionReopen         = "reopen"
	ActionClose          = "close"
	// ActionAssign is the owner handing an item to another worker; the
	// status does not move.
	ActionAssign = "assign"
	// ActionHold parks an open item and ActionUnhold releases it; the
	// status does not move. The owner only.
	ActionHold   = "hold"
	ActionUnhold = "unhold"
	// ActionGroom is a groom job settling: the groomer replaces the raw
	// description with the spec it wrote, may hand the item to another
	// worker, and the item returns to open ready for its implement job.
	ActionGroom = "groom"
)

// Worker CLIs — which command the runner spawns for an agent.
const (
	CLIClaude = "claude"
	CLICodex  = "codex"
)

// CLIs lists every CLI.
var CLIs = []string{CLIClaude, CLICodex}

// Effort levels. The first four are valid for either CLI; EffortMax is
// claude only. An empty effort is the CLI's own default.
const (
	EffortLow    = "low"
	EffortMedium = "medium"
	EffortHigh   = "high"
	EffortXHigh  = "xhigh"
	EffortMax    = "max"
)

// Efforts lists the effort levels either CLI accepts, lowest first.
var Efforts = []string{EffortLow, EffortMedium, EffortHigh, EffortXHigh}

// Review verdicts. The review action takes the first two; a code_review
// item's submit may carry any of the three as the suggested verdict.
const (
	VerdictApprove        = "approve"
	VerdictRequestChanges = "request_changes"
	VerdictComment        = "comment"
)

// Comment kinds and targets.
const (
	CommentKindComment    = "comment"
	CommentKindTransition = "transition"

	TargetItem = "item"
	TargetPost = "post"
)

// Job kinds — what a check-in hands an agent.
const (
	JobRespond     = "respond"
	JobImplement   = "implement"
	JobReview      = "review"
	JobMerge       = "merge"
	JobPullRequest = "pull_request"
	// JobGroom turns a raw ticket into a spec before any implement job: the
	// assignee reads the code, rewrites the description, splits the work
	// into after-chained items when it is really several, and assigns.
	JobGroom = "groom"
	// JobCodeReview is a code_review item's job: the assignee checks out the
	// pull request the item names, reviews it and submits the review.
	JobCodeReview = "code_review"
)

// Mention rooms — where an @mention was made and where its answer belongs.
const (
	RoomChat = "chat"
	RoomPost = "post"
	RoomItem = "item"
)

// Project is one repository's tracker.
type Project struct {
	ID string `json:"id"`
	// Key is the item prefix: 2–6 uppercase letters, unique.
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// RepoPath is the absolute path of the repository on the machine that
	// runs the agents. Empty means no agent work happens here yet.
	RepoPath      string `json:"repo_path"`
	DefaultBranch string `json:"default_branch"`
	MergePolicy   string `json:"merge_policy"`
	// Instructions is free text appended to every agent prompt for this
	// project.
	Instructions string `json:"instructions"`
	Status       string `json:"status"`
	// NextNumber is the number the next item gets.
	NextNumber int       `json:"next_number"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// TableName pins the table name against a later model rename.
func (Project) TableName() string { return "project_projects" }

// Item is one unit of work: a feature, a bug, a chore or a code review.
type Item struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Number    int    `json:"number"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	// Description is markdown, served verbatim.
	Description string `json:"description"`
	Priority    string `json:"priority"`
	Status      string `json:"status"`
	// Assignee is the worker holding the item or, while open, the worker it
	// is pinned to. An open item with no assignee sits in its tier's pool
	// and goes to whichever worker of that tier pulls it (tiers.go); once
	// claimed the item stays with its worker. The transitions that hand an
	// item back to the owner clear it.
	Assignee string `json:"assignee"`
	// Tier is the pool the item is pulled from while unpinned, and the tier
	// its review is offered to first. Filed without one, an item takes the
	// top tier that has an enabled worker; zero on an older row reads as
	// its worker's tier, else the baseline.
	Tier int `json:"tier"`
	// Implementer is the agent that submitted the branch; it sticks after
	// submit so the merge job and the reviewer-must-differ rule can find it.
	Implementer string `json:"implementer"`
	// Reviewer is the agent reviewing or that reviewed, "" until then.
	Reviewer string `json:"reviewer"`
	// ClaimedAt and LeaseExpiresAt make a claim a lease: past the expiry the
	// holder is presumed dead and the item is claimable again.
	ClaimedAt      time.Time `json:"claimed_at"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
	Branch         string    `json:"branch"`
	// PRURL is the pull request: the one a complete reports, or, on a
	// code_review item, the one under review — required there, and
	// validated as a GitHub pull request URL when an item is filed or
	// edited. A submit or complete writes the URL it carries as given.
	PRURL string `json:"pr_url"`
	// LastVerdict* record the latest review so a list can show "changes
	// requested" without reading the feed. On a code_review item the
	// submit records the reviewer's suggested verdict here.
	LastVerdict   string    `json:"last_verdict"`
	LastVerdictBy string    `json:"last_verdict_by"`
	LastVerdictAt time.Time `json:"last_verdict_at"`
	// Label is a free-text tag for grouping and filtering; "" is no label.
	Label string `json:"label"`
	// After is the key of an item in the same project ("EA-1") this one
	// waits on: until that item is done or closed the queue never offers
	// this one and a claim is refused. "" is no dependency.
	After string `json:"after"`
	// Held parks an item: the queue never offers it and a claim is refused
	// until the owner lifts the hold. The owner holds an item that is open,
	// in review or approved (not one under a live lease); the tracker holds
	// one itself after MaxItemFailures consecutive failed runs.
	Held bool `json:"held"`
	// Failures counts consecutive failed runs (crashed, timeout, skipped, or
	// an agent-reported failure) since the last run that settled; FinishRun
	// maintains it, a settled run or an unhold resets it, and at
	// MaxItemFailures the item is held for the owner.
	Failures int `json:"failures"`
	// NeedsGroom marks a raw ticket that gets a groom job before any
	// implement job: a feature or bug filed without the specced flag. The
	// groom transition clears it.
	NeedsGroom bool   `json:"needs_groom"`
	CreatedBy  string `json:"created_by"`
	// Revision bumps on every write; the API's X-Base-Revision precondition
	// stands on it.
	Revision  int       `json:"revision"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ClosedAt  time.Time `json:"closed_at"`
}

// TableName pins the table name against a later model rename.
func (Item) TableName() string { return "project_items" }

// Comment is one row of an item's activity feed or a post's replies.
type Comment struct {
	ID         string `json:"id"`
	TargetKind string `json:"target_kind"`
	TargetID   string `json:"target_id"`
	Author     string `json:"author"`
	// Kind is comment or transition; a transition carries the action, the
	// two statuses and, for a review or a code review's submit, the verdict.
	Kind       string `json:"kind"`
	Action     string `json:"action"`
	FromStatus string `json:"from_status"`
	ToStatus   string `json:"to_status"`
	Verdict    string `json:"verdict"`
	// Assignee is the worker an ActionAssign transition handed the item to,
	// "" when that transition returned it to its tier's pool and on every
	// other kind of row. It is the fact itself: Body phrases the same fact
	// for a client to print, and no client parses that phrasing back apart
	// to learn the name.
	Assignee string `json:"assignee"`
	// Body is markdown, served verbatim.
	Body string `json:"body"`
	// Mentions is the @names found in the body, comma-joined.
	Mentions  string    `json:"mentions"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName pins the table name against a later model rename.
func (Comment) TableName() string { return "project_comments" }

// Post is one message-board entry.
type Post struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Author    string `json:"author"`
	Title     string `json:"title"`
	// Body is markdown, served verbatim.
	Body string `json:"body"`
	// Pinned posts are standing context: every job prompt for the project
	// carries them in full.
	Pinned      bool      `json:"pinned"`
	Mentions    string    `json:"mentions"`
	ReplyCount  int       `json:"reply_count"`
	LastReplyAt time.Time `json:"last_reply_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName pins the table name against a later model rename.
func (Post) TableName() string { return "project_posts" }

// ChatMessage is one line in a project's room; ProjectID "" is the general
// room.
type ChatMessage struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	Mentions  string    `json:"mentions"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName pins the table name against a later model rename.
func (ChatMessage) TableName() string { return "project_chat" }

// Agent is one worker: a model behind a CLI, with a short handle. ID is the
// handle — the bearer identity, the watcher instance and the @mention. Rows
// are the owner's data: created through CreateAgent, never by a check-in.
// The is_default and current_item_id columns of earlier rungs stay in the
// table (additive-only) but nothing reads them: the lead of the top tier is
// what "default" used to mean, and what a worker holds is derived from the
// items' leases.
type Agent struct {
	ID string `json:"id"`
	// Label is what the UI shows; it defaults to the name.
	Label string `json:"label"`
	// CLI is which command the runner spawns: CLIClaude or CLICodex.
	CLI string `json:"cli"`
	// Model is handed to the CLI as-is: a Claude Code alias or a Codex model
	// name. No code spells a model ID; this column is where they live.
	Model string `json:"model"`
	// Effort is one of Efforts for either CLI, EffortMax for claude only, or
	// "" for the CLI's own default.
	Effort string `json:"effort"`
	// Tier is the pool the worker pulls from: unpinned work at a tier goes
	// to that tier's workers and to nobody else, so a weaker model never
	// stands in for a stronger one. Higher is stronger. DefaultTier is the
	// baseline; weaker models sit below it, stronger ones above. Zero reads
	// as the baseline.
	Tier int `json:"tier"`
	// Lead marks the tier's preferred worker: it takes the tier's unpinned
	// work while it can, and the tier's other workers pull only while it is
	// out (disabled or unavailable on quota). Every non-empty tier has
	// exactly one lead: the first worker created in a tier leads it, the
	// flag moves with SetLead, and a tier whose lead leaves promotes its
	// first worker by name.
	Lead bool `json:"lead"`
	// Slots is how many jobs the worker runs at once; zero reads as
	// DefaultSlots.
	Slots      int       `json:"slots"`
	Enabled    bool      `json:"enabled"`
	LastSeenAt time.Time `json:"last_seen_at"`
	CreatedAt  time.Time `json:"created_at"`
	// The worker's last account-usage report, sent by its runner at
	// check-in (quota.go). Session is the account's five-hour window;
	// Weekly is the tightest weekly window that applies to this worker's
	// model, with QuotaWeeklyScope naming it: QuotaScopeAll, or the model
	// the account scopes it to ("Fable"). Availability derives from these
	// and the clock; a worker whose runner reports nothing is available.
	QuotaSessionUsed     float64   `json:"quota_session_used"`
	QuotaSessionResetsAt time.Time `json:"quota_session_resets_at"`
	QuotaWeeklyUsed      float64   `json:"quota_weekly_used"`
	QuotaWeeklyResetsAt  time.Time `json:"quota_weekly_resets_at"`
	QuotaWeeklyScope     string    `json:"quota_weekly_scope"`
	QuotaReportedAt      time.Time `json:"quota_reported_at"`
}

// TableName pins the table name against a later model rename.
func (Agent) TableName() string { return "project_agents" }

// Mention is one @agent in chat, on a post or on an item, and whether it has
// been answered. It is a row rather than a scan because the equality-only
// query layer cannot search inside a mentions column; it cannot get stuck
// because the agent posting in the same room answers it, and the runner's
// attempts are capped.
type Mention struct {
	ID    string `json:"id"`
	Agent string `json:"agent"`
	// Room and RoomID say where the answer belongs: chat + project id ("" for
	// general), post + post id, item + item id.
	Room      string `json:"room"`
	RoomID    string `json:"room_id"`
	ProjectID string `json:"project_id"`
	// SourceKind and SourceID name the message that mentioned: chat, post or
	// comment, with its id.
	SourceKind string    `json:"source_kind"`
	SourceID   string    `json:"source_id"`
	Author     string    `json:"author"`
	Attempts   int       `json:"attempts"`
	CreatedAt  time.Time `json:"created_at"`
	AnsweredAt time.Time `json:"answered_at"`
}

// TableName pins the table name against a later model rename.
func (Mention) TableName() string { return "project_mentions" }

func init() {
	gowild_data.RegisterFunc(func(db gowild_data.Database) error {
		for _, model := range []any{Project{}, Item{}, Comment{}, Post{}, ChatMessage{}, Agent{}, Mention{}, Run{}} {
			if err := db.AddTable(model); err != nil {
				return err
			}
		}
		if err := gowild_data.EnsureUniqueIndex(db, Project{}, "project_projects_key", "key"); err != nil {
			return err
		}
		return gowild_data.EnsureUniqueIndex(db, Item{}, "project_items_project_number", "project_id", "number")
	})
}
