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
	s := &Service{db: db, now: time.Now, lease: DefaultLease}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Now is the service's clock, UTC.
func (s *Service) Now() time.Time { return s.now().UTC() }

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

// EnsureAgent returns the agent's row, creating it enabled when absent.
func (s *Service) EnsureAgent(ctx context.Context, name string) (*Agent, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureAgent(ctx, db, name)
}

func (s *Service) ensureAgent(ctx context.Context, db gowild_data.Database, name string) (*Agent, error) {
	if !ValidAgentName(name) {
		return nil, validationf("agent name %q is not lowercase letters, digits, - or _", name)
	}
	a, err := gowild_dbx.Get[Agent](ctx, db, name)
	if err != nil {
		return nil, err
	}
	if a != nil {
		return a, nil
	}
	a = &Agent{ID: name, Enabled: true, CreatedAt: s.Now()}
	if err := db.Table(Agent{}).Insert(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

// GetAgent reads one agent.
func (s *Service) GetAgent(ctx context.Context, name string) (*Agent, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	a, err := gowild_dbx.Get[Agent](ctx, db, name)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("%w: agent %s", ErrNotFound, name)
	}
	return a, nil
}

// ListAgents returns every agent by name.
func (s *Service) ListAgents(ctx context.Context) ([]*Agent, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	rows, err := gowild_dbx.All[Agent](ctx, db, gowild_data.QueryOpts{})
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows, nil
}

// SetAgentEnabled flips the pause switch, creating the row if needed.
func (s *Service) SetAgentEnabled(ctx context.Context, name string, enabled bool) (*Agent, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.ensureAgent(ctx, db, name)
	if err != nil {
		return nil, err
	}
	a.Enabled = enabled
	if err := db.Table(Agent{}).Update(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

// setAgentCurrent records what an agent holds; "" clears it. Missing rows
// are created so a transition by a never-seen agent still leaves a trace.
func (s *Service) setAgentCurrent(ctx context.Context, db gowild_data.Database, name, itemID string) error {
	a, err := s.ensureAgent(ctx, db, name)
	if err != nil {
		return err
	}
	a.CurrentItemID = itemID
	return db.Table(Agent{}).Update(ctx, a)
}
