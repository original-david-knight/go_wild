// Package pollers is a background-refresh backbone: one registered poller per
// source, each on its own interval, all started and stopped with the process
// that owns them.
//
// The guarantees: cycles of one source never overlap; every cycle is bounded
// by a timeout so a hung outbound call cannot wedge its source; a cycle
// skipped for a down database retries on a short timer instead of waiting out
// its whole cadence; and each source's last successful sync is recorded, with
// the most recent failure kept alongside it.
package pollers

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/data/dbx"
)

// RunFunc is one poll cycle. It should return promptly when ctx is cancelled.
type RunFunc func(ctx context.Context, db gowild_data.Database) error

// Poller is a registered source.
type Poller struct {
	// Source is the key the source is registered and its sync row stored
	// under, e.g. "weather".
	Source string
	// Interval is the resolved cadence for this source.
	Interval time.Duration
	// Timeout bounds one cycle. Zero takes DefaultCycleTimeout.
	Timeout time.Duration
	Run     RunFunc
}

// DefaultCycleTimeout bounds a poll cycle.
//
// Cycles of one source never overlap, which is what keeps a slow poll from
// stacking up — but it also means a cycle that never returns stops that source
// for the life of the process. A single hung outbound call with no deadline of
// its own is enough to cause it. A cycle that runs long is a failed poll, and
// a failed poll is a thing this backbone already knows how to record and
// retry.
const DefaultCycleTimeout = 4 * time.Minute

// Sync records a source's last successful poll. Failures deliberately do not
// touch it: "last sync" means the last time the data was actually fresh.
type Sync struct {
	ID     string    `json:"id"`
	Source string    `json:"source"`
	At     time.Time `json:"at"`
	// LastError is the most recent failure, kept alongside the last success so
	// a degraded source can say so without losing when it last worked.
	LastError string    `json:"last_error"`
	ErrorAt   time.Time `json:"error_at"`
}

// TableName pins the table name against a later model rename. The name is
// load-bearing: changing it would orphan the poller_syncs table existing
// consumers already hold their history in.
func (Sync) TableName() string { return "poller_syncs" }

func init() {
	gowild_data.RegisterFunc(func(db gowild_data.Database) error {
		return db.AddTable(Sync{})
	})
}

func (p Poller) timeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return DefaultCycleTimeout
}

// DBFunc yields the live database handle, or nil while it is down.
type DBFunc = dbx.DBFunc

// Manager owns the running pollers.
type Manager struct {
	db DBFunc

	// Now supplies the timestamps stamped onto sync rows. Nil falls back to
	// time.Now. A consumer with its own clock sets it before Start; running
	// loops read the field without a lock after that.
	Now func() time.Time

	mu      sync.Mutex
	running map[string]*runner
	ctx     context.Context
	cancel  context.CancelFunc
	started bool
}

type runner struct {
	poller   Poller
	cancel   context.CancelFunc
	done     chan struct{}
	interval time.Duration
}

// NewManager builds a manager over db.
func NewManager(db DBFunc) *Manager {
	return &Manager{db: db, running: map[string]*runner{}}
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Start begins the manager's lifetime. Pollers registered before or after
// Start all run; registering after Start begins immediately.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.started = true
	for _, r := range m.running {
		m.launch(r)
	}
}

// Register adds or replaces a poller. Replacing one — which is what a cadence
// change does — stops the old loop before starting the new, so a reschedule
// never leaves two loops on one source.
func (m *Manager) Register(p Poller) error {
	if p.Source == "" || p.Run == nil {
		return fmt.Errorf("a poller needs a source and a run function")
	}
	if p.Interval <= 0 {
		return fmt.Errorf("poller %s: interval must be positive, got %s", p.Source, p.Interval)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.running[p.Source]; ok {
		m.stop(old)
	}
	r := &runner{poller: p, interval: p.Interval, done: make(chan struct{})}
	m.running[p.Source] = r
	if m.started {
		m.launch(r)
	}
	return nil
}

// Deregister stops and removes one source's poller, waiting for an in-flight
// cycle. Disconnecting a source is what calls it: a poller left running
// against a revoked grant can only fail, and its failures would land on a row
// whose true state is "not connected".
func (m *Manager) Deregister(source string) {
	m.mu.Lock()
	r, ok := m.running[source]
	if ok {
		delete(m.running, source)
	}
	m.mu.Unlock()
	if ok {
		m.stop(r)
	}
}

// Reschedule changes a source's cadence without a process restart.
func (m *Manager) Reschedule(source string, interval time.Duration) error {
	m.mu.Lock()
	r, ok := m.running[source]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no poller registered for %q", source)
	}
	p := r.poller
	p.Interval = interval
	return m.Register(p)
}

// Interval reports a source's current cadence.
func (m *Manager) Interval(source string) (time.Duration, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.running[source]
	if !ok {
		return 0, false
	}
	return r.interval, true
}

// Sources lists the registered source names.
func (m *Manager) Sources() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.running))
	for source := range m.running {
		out = append(out, source)
	}
	return out
}

// Stop halts every poller and waits for in-flight cycles to return.
func (m *Manager) Stop() {
	m.mu.Lock()
	runners := make([]*runner, 0, len(m.running))
	for _, r := range m.running {
		runners = append(runners, r)
	}
	m.running = map[string]*runner{}
	if m.cancel != nil {
		m.cancel()
	}
	m.started = false
	m.mu.Unlock()

	for _, r := range runners {
		m.stop(r)
	}
}

// launch starts a runner's loop. The caller holds the lock.
func (m *Manager) launch(r *runner) {
	ctx, cancel := context.WithCancel(m.ctx)
	r.cancel = cancel
	r.done = make(chan struct{})
	go m.loop(ctx, r)
}

func (m *Manager) stop(r *runner) {
	if r.cancel == nil {
		// Registered but never launched — the manager had not started yet, so
		// there is no loop to wait for and `done` will never close.
		return
	}
	r.cancel()
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		slog.Warn("poller did not stop in time", "source", r.poller.Source)
	}
}

// dbRetry is how long a poller waits before retrying a cycle it had to skip
// because the database was not there. Without it a startup poll that lands in
// the few milliseconds before the database connects would wait a whole
// interval — hours of an empty source for a fault that cleared instantly.
const dbRetry = 5 * time.Second

// loop runs a source on its interval. A cycle that overruns its interval is
// never overlapped by the next tick — the dedupe is expressed as a
// single-goroutine loop rather than a shared flag.
func (m *Manager) loop(ctx context.Context, r *runner) {
	defer close(r.done)
	slog.Info("poller started", "source", r.poller.Source, "interval", r.interval)

	// Poll once at startup so a fresh process does not wait a whole interval
	// before it has anything to show, retrying until the database is
	// reachable.
	for !m.cycle(ctx, r) {
		if !sleep(ctx, dbRetry) {
			return
		}
	}

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// A tick skipped for a down database retries on the short timer
			// rather than waiting out the full cadence.
			for !m.cycle(ctx, r) {
				if !sleep(ctx, dbRetry) {
					return
				}
			}
		}
	}
}

// sleep waits for d, reporting false if the context ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// cycle runs one poll. It reports whether the cycle was attempted at all: a
// false return means the database was down and the caller should retry, not
// that the poll failed (a failed poll is recorded and returns true).
func (m *Manager) cycle(ctx context.Context, r *runner) bool {
	if ctx.Err() != nil {
		return true
	}
	db := m.db()
	if db == nil {
		// The database being down degrades to a skipped tick that says why.
		slog.Debug("poll skipped, database unavailable", "source", r.poller.Source)
		return false
	}

	start := m.now()
	runCtx, cancel := context.WithTimeout(ctx, r.poller.timeout())
	defer cancel()
	if err := r.poller.Run(runCtx, db); err != nil {
		// The manager shutting down is not a poll failure to record.
		if ctx.Err() != nil {
			return true
		}
		if runCtx.Err() != nil {
			err = fmt.Errorf("poll exceeded %s: %w", r.poller.timeout(), err)
		}
		slog.Warn("poll failed", "source", r.poller.Source, "err", err)
		// recordFailure needs a context the timeout has not already cancelled.
		recordFailure(ctx, db, r.poller.Source, err, m.now())
		return true
	}
	slog.Info("poll ok", "source", r.poller.Source, "dur", time.Since(start))
	RecordSuccess(ctx, db, r.poller.Source, m.now())
	return true
}

// RecordSuccess stamps a source's last successful sync.
func RecordSuccess(ctx context.Context, db gowild_data.Database, source string, at time.Time) {
	upsertSync(ctx, db, source, func(s *Sync) {
		s.At = at.UTC()
		s.LastError, s.ErrorAt = "", time.Time{}
	})
}

func recordFailure(ctx context.Context, db gowild_data.Database, source string, err error, at time.Time) {
	upsertSync(ctx, db, source, func(s *Sync) {
		s.LastError = err.Error()
		s.ErrorAt = at.UTC()
	})
}

func upsertSync(ctx context.Context, db gowild_data.Database, source string, apply func(*Sync)) {
	row, err := dbx.Get[Sync](ctx, db, source)
	if err != nil {
		slog.Warn("read sync row", "source", source, "err", err)
		return
	}
	if row == nil {
		row = &Sync{ID: source, Source: source}
	}
	apply(row)
	if err := dbx.Upsert(ctx, db, source, row); err != nil {
		slog.Warn("write sync row", "source", source, "err", err)
	}
}

// LastSyncs returns every source's sync record, keyed by source.
func LastSyncs(ctx context.Context, db gowild_data.Database) (map[string]*Sync, error) {
	if db == nil {
		return nil, dbx.ErrUnavailable
	}
	dao := db.Table(Sync{})
	if dao == nil {
		return nil, dbx.ErrUnavailable
	}
	rows, err := dao.Query(ctx, gowild_data.QueryOpts{})
	if err != nil {
		return nil, err
	}
	out := make(map[string]*Sync, len(rows))
	for _, raw := range rows {
		if s, ok := raw.(*Sync); ok {
			out[s.Source] = s
		}
	}
	return out, nil
}
