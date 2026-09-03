package gowild_projects

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	gowild_data "github.com/original-david-knight/go_wild/data"
	gowild_dbx "github.com/original-david-knight/go_wild/data/dbx"
)

// DBFunc yields the live database handle, or nil while it is down.
type DBFunc = gowild_dbx.DBFunc

// ErrUnavailable reports the database being down.
var ErrUnavailable = gowild_dbx.ErrUnavailable

// The library's error kinds. Consumers map them to responses; every error
// the service returns wraps exactly one of them or ErrUnavailable.
var (
	ErrNotFound          = errors.New("not found")
	ErrInvalidTransition = errors.New("invalid transition")
	ErrForbidden         = errors.New("forbidden")
	ErrStaleRevision     = errors.New("stale revision")
	ErrValidation        = errors.New("invalid input")
	ErrConflict          = errors.New("conflict")
)

// DefaultLease is how long a claim holds before the holder is presumed dead.
const DefaultLease = 2 * time.Hour

// Service is the tracker. One instance per process: its mutex is what makes
// a claim atomic, because the data layer offers equality queries and
// read-modify-write only. That is honest for a single-instance service and
// is documented as such.
type Service struct {
	db    DBFunc
	now   func() time.Time
	lease time.Duration
	mu    sync.Mutex
	// wakeMu guards gen. gen is closed and replaced on every wake, so a
	// parked Wait holds the channel it read and returns when it closes.
	wakeMu sync.Mutex
	gen    chan struct{}
}

// Option customises a Service.
type Option func(*Service)

// WithClock pins the clock, for tests.
func WithClock(now func() time.Time) Option {
	return func(s *Service) { s.now = now }
}

// WithLease sets the claim lease.
func WithLease(d time.Duration) Option {
	return func(s *Service) { s.lease = d }
}

// NewService builds a tracker over db.
func NewService(db DBFunc, opts ...Option) *Service {
	s := &Service{db: db, now: time.Now, lease: DefaultLease, gen: make(chan struct{})}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Now is the service's clock, UTC.
func (s *Service) Now() time.Time { return s.now().UTC() }

// wake tells every parked Wait to re-evaluate its queue. Every write that
// can create work calls it once the write is in: an item created,
// reassigned or transitioned, a mention recorded or synchronized, an agent
// enabled, a project un-archived or given its repository path. It touches
// no database and takes only wakeMu, so a caller may hold s.mu.
func (s *Service) wake() {
	s.wakeMu.Lock()
	close(s.gen)
	s.gen = make(chan struct{})
	s.wakeMu.Unlock()
}

// generation is the channel the next wake closes.
func (s *Service) generation() <-chan struct{} {
	s.wakeMu.Lock()
	defer s.wakeMu.Unlock()
	return s.gen
}

func (s *Service) database() (gowild_data.Database, error) {
	if s.db == nil {
		return nil, ErrUnavailable
	}
	db := s.db()
	if db == nil {
		return nil, ErrUnavailable
	}
	return db, nil
}

func newID() string { return uuid.New().String() }

func validationf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

// ---------------------------------------------------------------- projects

// ProjectInput is what creating a project takes.
type ProjectInput struct {
	Key           string
	Name          string
	Description   string
	RepoPath      string
	DefaultBranch string
	MergePolicy   string
	Instructions  string
}

// ProjectPatch is a partial update; nil fields are left alone.
type ProjectPatch struct {
	Name          *string
	Description   *string
	RepoPath      *string
	DefaultBranch *string
	MergePolicy   *string
	Instructions  *string
	Status        *string
}

func validMergePolicy(p string) bool {
	return p == MergePolicyMerge || p == MergePolicyPullRequest
}

// CreateProject inserts a project. The key must be unique.
func (s *Service) CreateProject(ctx context.Context, in ProjectInput) (*Project, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	in.Key = strings.ToUpper(strings.TrimSpace(in.Key))
	if !ValidProjectKey(in.Key) {
		return nil, validationf("project key %q must be 2–6 uppercase letters", in.Key)
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, validationf("project name is required")
	}
	if in.MergePolicy == "" {
		in.MergePolicy = MergePolicyMerge
	}
	if !validMergePolicy(in.MergePolicy) {
		return nil, validationf("merge policy %q is not merge or pull_request", in.MergePolicy)
	}
	if in.DefaultBranch == "" {
		in.DefaultBranch = "main"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, err := gowild_dbx.All[Project](ctx, db, gowild_data.QueryOpts{Where: map[string]any{"key": in.Key}, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, fmt.Errorf("%w: project key %s exists", ErrConflict, in.Key)
	}
	now := s.Now()
	p := &Project{
		ID: newID(), Key: in.Key, Name: strings.TrimSpace(in.Name), Description: in.Description,
		RepoPath: strings.TrimSpace(in.RepoPath), DefaultBranch: in.DefaultBranch,
		MergePolicy: in.MergePolicy, Instructions: in.Instructions,
		Status: ProjectActive, NextNumber: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Table(Project{}).Insert(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// ListProjects returns projects by key, archived ones only on request.
func (s *Service) ListProjects(ctx context.Context, includeArchived bool) ([]*Project, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	opts := gowild_data.QueryOpts{}
	if !includeArchived {
		opts.Where = map[string]any{"status": ProjectActive}
	}
	rows, err := gowild_dbx.All[Project](ctx, db, opts)
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	return rows, nil
}

// GetProject reads one project by key.
func (s *Service) GetProject(ctx context.Context, key string) (*Project, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	return s.projectByKey(ctx, db, key)
}

func (s *Service) projectByKey(ctx context.Context, db gowild_data.Database, key string) (*Project, error) {
	key = strings.ToUpper(strings.TrimSpace(key))
	rows, err := gowild_dbx.All[Project](ctx, db, gowild_data.QueryOpts{Where: map[string]any{"key": key}, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, key)
	}
	return rows[0], nil
}

func (s *Service) projectByID(ctx context.Context, db gowild_data.Database, id string) (*Project, error) {
	p, err := gowild_dbx.Get[Project](ctx, db, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("%w: project id %s", ErrNotFound, id)
	}
	return p, nil
}

// UpdateProject applies a patch.
func (s *Service) UpdateProject(ctx context.Context, key string, patch ProjectPatch) (*Project, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.projectByKey(ctx, db, key)
	if err != nil {
		return nil, err
	}
	wasActive, hadRepo := p.Status == ProjectActive, p.RepoPath != ""
	if patch.Name != nil {
		if strings.TrimSpace(*patch.Name) == "" {
			return nil, validationf("project name is required")
		}
		p.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.Description != nil {
		p.Description = *patch.Description
	}
	if patch.RepoPath != nil {
		p.RepoPath = strings.TrimSpace(*patch.RepoPath)
	}
	if patch.DefaultBranch != nil {
		if strings.TrimSpace(*patch.DefaultBranch) == "" {
			return nil, validationf("default branch is required")
		}
		p.DefaultBranch = strings.TrimSpace(*patch.DefaultBranch)
	}
	if patch.MergePolicy != nil {
		if !validMergePolicy(*patch.MergePolicy) {
			return nil, validationf("merge policy %q is not merge or pull_request", *patch.MergePolicy)
		}
		p.MergePolicy = *patch.MergePolicy
	}
	if patch.Instructions != nil {
		p.Instructions = *patch.Instructions
	}
	if patch.Status != nil {
		if *patch.Status != ProjectActive && *patch.Status != ProjectArchived {
			return nil, validationf("project status %q is not active or archived", *patch.Status)
		}
		p.Status = *patch.Status
	}
	p.UpdatedAt = s.Now()
	if err := db.Table(Project{}).Update(ctx, p); err != nil {
		return nil, err
	}
	// Un-archiving puts the project's mentions on the queue, and a first
	// repository path puts its items there: the parked waits re-check.
	if p.Status == ProjectActive && (!wasActive || (p.RepoPath != "" && !hadRepo)) {
		s.wake()
	}
	return p, nil
}

// Counts is the number of items per status in a project, every status
// present with at least a zero.
func (s *Service) Counts(ctx context.Context, projectID string) (map[string]int, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	rows, err := gowild_dbx.All[Item](ctx, db, gowild_data.QueryOpts{Where: map[string]any{"project_id": projectID}})
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(Statuses))
	for _, st := range Statuses {
		counts[st] = 0
	}
	for _, it := range rows {
		counts[it.Status]++
	}
	return counts, nil
}

// ------------------------------------------------------------------ agents

// AgentInput is what creating a worker takes. Label defaults to Name; Model
// is required; Effort may be ""; Tier and Slots default to DefaultTier and
// DefaultSlots. The row leads its tier when the tier has no lead yet.
type AgentInput struct {
	Name   string
	Label  string
	CLI    string
	Model  string
	Effort string
	Tier   int
	Slots  int
}

// AgentPatch is a partial update; nil fields are left alone. Lead true
// makes the row its tier's lead and the tier's old lead a backup; false is
// refused, since a lead is moved, not cleared. A Tier move takes the flag
// along only when the new tier has no lead, and the tier left behind
// promotes its first worker by name.
type AgentPatch struct {
	Enabled *bool
	Label   *string
	CLI     *string
	Model   *string
	Effort  *string
	Tier    *int
	Slots   *int
	Lead    *bool
}

// CreateAgent inserts a worker. The name must be unused. The row leads its
// tier when no row there is the lead yet, so the first worker created in a
// tier leads it.
func (s *Service) CreateAgent(ctx context.Context, in AgentInput) (*Agent, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if !ValidAgentName(in.Name) {
		return nil, validationf("agent name %q is not 2–16 lowercase letters and digits", in.Name)
	}
	in.Label = strings.TrimSpace(in.Label)
	if in.Label == "" {
		in.Label = in.Name
	}
	if !ValidCLI(in.CLI) {
		return nil, validationf("cli %q is not claude or codex", in.CLI)
	}
	in.Model = strings.TrimSpace(in.Model)
	if in.Model == "" {
		return nil, validationf("model is required")
	}
	in.Effort = strings.TrimSpace(in.Effort)
	if !ValidEffort(in.CLI, in.Effort) {
		return nil, validationf("effort %q is not low, medium, high, xhigh, max (claude only) or empty", in.Effort)
	}
	if in.Tier < 0 {
		return nil, validationf("tier %d is not a positive number", in.Tier)
	}
	if in.Slots < 0 {
		return nil, validationf("slots %d is not a positive number", in.Slots)
	}
	if in.Tier == 0 {
		in.Tier = DefaultTier
	}
	if in.Slots == 0 {
		in.Slots = DefaultSlots
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, err := gowild_dbx.Get[Agent](ctx, db, in.Name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("%w: agent %s exists", ErrConflict, in.Name)
	}
	rows, err := gowild_dbx.All[Agent](ctx, db, gowild_data.QueryOpts{})
	if err != nil {
		return nil, err
	}
	a := &Agent{
		ID: in.Name, Label: in.Label, CLI: in.CLI, Model: in.Model, Effort: in.Effort,
		Tier: in.Tier, Slots: in.Slots, Lead: !tierHasLead(rows, in.Tier),
		Enabled: true, CreatedAt: s.Now(),
	}
	if err := db.Table(Agent{}).Insert(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

func tierHasLead(rows []*Agent, tier int) bool {
	for _, a := range rows {
		if a.Lead && a.TierOrDefault() == tier {
			return true
		}
	}
	return false
}

// GetAgent reads one agent.
func (s *Service) GetAgent(ctx context.Context, name string) (*Agent, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	return s.agentByName(ctx, db, name)
}

func (s *Service) agentByName(ctx context.Context, db gowild_data.Database, name string) (*Agent, error) {
	a, err := gowild_dbx.Get[Agent](ctx, db, name)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("%w: no worker named %s", ErrNotFound, name)
	}
	return a, nil
}

// ListAgents returns every agent: strongest tier first, the lead first
// within a tier, then by name.
func (s *Service) ListAgents(ctx context.Context) ([]*Agent, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	rows, err := gowild_dbx.All[Agent](ctx, db, gowild_data.QueryOpts{})
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool {
		if ti, tj := rows[i].TierOrDefault(), rows[j].TierOrDefault(); ti != tj {
			return ti > tj
		}
		if rows[i].Lead != rows[j].Lead {
			return rows[i].Lead
		}
		return rows[i].ID < rows[j].ID
	})
	return rows, nil
}

// UpdateAgent applies a patch. Making a row the lead demotes the tier's
// old lead in the same transaction; a tier move re-settles both tiers'
// leads. A change that can put work in front of a worker — enabling,
// pausing (its backups step in), a lead or tier or slot change — wakes the
// parked waits.
func (s *Service) UpdateAgent(ctx context.Context, name string, patch AgentPatch) (*Agent, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.agentByName(ctx, db, name)
	if err != nil {
		return nil, err
	}
	rotation := false
	if patch.Enabled != nil {
		rotation = rotation || a.Enabled != *patch.Enabled
		a.Enabled = *patch.Enabled
	}
	if patch.Label != nil {
		a.Label = strings.TrimSpace(*patch.Label)
		if a.Label == "" {
			a.Label = a.ID
		}
	}
	if patch.CLI != nil {
		if !ValidCLI(*patch.CLI) {
			return nil, validationf("cli %q is not claude or codex", *patch.CLI)
		}
		a.CLI = *patch.CLI
	}
	if patch.Model != nil {
		a.Model = strings.TrimSpace(*patch.Model)
		if a.Model == "" {
			return nil, validationf("model is required")
		}
	}
	if patch.Effort != nil {
		a.Effort = strings.TrimSpace(*patch.Effort)
	}
	if (patch.CLI != nil || patch.Effort != nil) && !ValidEffort(a.CLI, a.Effort) {
		return nil, validationf("effort %q is not low, medium, high, xhigh, max (claude only) or empty", a.Effort)
	}
	if patch.Slots != nil {
		if *patch.Slots < 1 {
			return nil, validationf("slots %d is not a positive number", *patch.Slots)
		}
		rotation = rotation || a.Slots != *patch.Slots
		a.Slots = *patch.Slots
	}
	if patch.Lead != nil && !*patch.Lead {
		return nil, fmt.Errorf("%w: a tier's lead is moved, not cleared; make another worker of the tier the lead", ErrConflict)
	}
	moveTier := patch.Tier != nil && *patch.Tier != a.TierOrDefault()
	if patch.Tier != nil && *patch.Tier < 1 {
		return nil, validationf("tier %d is not a positive number", *patch.Tier)
	}
	makeLead := patch.Lead != nil && (!a.Lead || moveTier)
	if !moveTier && !makeLead {
		if err := db.Table(Agent{}).Update(ctx, a); err != nil {
			return nil, err
		}
	} else {
		rotation = true
		err = db.RunInTransaction(ctx, func(tx gowild_data.Database) error {
			rows, err := gowild_dbx.All[Agent](ctx, tx, gowild_data.QueryOpts{})
			if err != nil {
				return err
			}
			for i, other := range rows {
				if other.ID == a.ID {
					rows[i] = a
				}
			}
			if moveTier {
				a.Tier = *patch.Tier
				// The flag follows only into a tier that has no lead; the
				// tier left behind is re-settled below.
				a.Lead = a.Lead && !tierHasLead(otherRows(rows, a.ID), a.Tier)
			}
			if makeLead {
				for _, other := range rows {
					if other.ID != a.ID && other.Lead && other.TierOrDefault() == a.TierOrDefault() {
						other.Lead = false
						if err := tx.Table(Agent{}).Update(ctx, other); err != nil {
							return err
						}
					}
				}
				a.Lead = true
			}
			if err := tx.Table(Agent{}).Update(ctx, a); err != nil {
				return err
			}
			return ensureLeads(ctx, tx, rows)
		})
		if err != nil {
			return nil, err
		}
	}
	if rotation {
		s.wake()
	}
	return a, nil
}

func otherRows(rows []*Agent, except string) []*Agent {
	out := make([]*Agent, 0, len(rows))
	for _, a := range rows {
		if a.ID != except {
			out = append(out, a)
		}
	}
	return out
}

// SetLead makes the worker its tier's lead.
func (s *Service) SetLead(ctx context.Context, name string) (*Agent, error) {
	yes := true
	return s.UpdateAgent(ctx, name, AgentPatch{Lead: &yes})
}

// DeleteAgent removes a worker. It refuses while the row holds an item or
// is the assignee of any item that is not done or closed. The tier it
// leaves re-settles its lead.
func (s *Service) DeleteAgent(ctx context.Context, name string) error {
	db, err := s.database()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.agentByName(ctx, db, name)
	if err != nil {
		return err
	}
	live, err := s.liveLeasesForAgent(ctx, db, name, s.Now())
	if err != nil {
		return err
	}
	if len(live) > 0 {
		return fmt.Errorf("%w: %s holds an item", ErrConflict, name)
	}
	assigned, err := gowild_dbx.All[Item](ctx, db, gowild_data.QueryOpts{Where: map[string]any{"assignee": name}})
	if err != nil {
		return err
	}
	for _, it := range assigned {
		if it.Status != StatusDone && it.Status != StatusClosed {
			return fmt.Errorf("%w: %s is the assignee of an item that is not done or closed; reassign it first", ErrConflict, name)
		}
	}
	// In-flight items past submit have no assignee, but their merge job
	// matches only the implementer: deleting that worker strands them in
	// approved until the owner completes by hand.
	implementing, err := gowild_dbx.All[Item](ctx, db, gowild_data.QueryOpts{
		Where:   map[string]any{"implementer": name},
		WhereIn: map[string][]any{"status": {StatusInReview, StatusPendingApproval, StatusApproved}},
	})
	if err != nil {
		return err
	}
	if len(implementing) > 0 {
		return fmt.Errorf("%w: %s is the implementer of an item that is still in flight; let it finish or close it first", ErrConflict, name)
	}
	err = db.RunInTransaction(ctx, func(tx gowild_data.Database) error {
		if err := tx.Table(Agent{}).Delete(ctx, name); err != nil {
			return err
		}
		rows, err := gowild_dbx.All[Agent](ctx, tx, gowild_data.QueryOpts{})
		if err != nil {
			return err
		}
		return ensureLeads(ctx, tx, otherRows(rows, a.ID))
	})
	if err != nil {
		return err
	}
	s.wake()
	return nil
}

// SetAgentEnabled flips the pause switch.
func (s *Service) SetAgentEnabled(ctx context.Context, name string, enabled bool) (*Agent, error) {
	return s.UpdateAgent(ctx, name, AgentPatch{Enabled: &enabled})
}
