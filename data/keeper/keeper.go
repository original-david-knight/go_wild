// Package keeper supervises a database connection. It never blocks startup:
// when the database is unreachable the caller comes up degraded and a
// background loop reconnects with backoff.
package keeper

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

// Options configures how a Keeper opens and prepares its connection. The zero
// value is usable: it opens a Postgres database and surfaces a generic error
// for an empty DSN.
type Options struct {
	// NewDB opens the configured backend. Nil defaults to
	// gowild_data.NewPostgresDatabase.
	NewDB func(dsn string) (gowild_data.Database, error)
	// OnConnect runs after gowild_data.AddAllTables on every successful
	// connect — the first and any reconnect after a drop — so it must be
	// idempotent and cheap: a reconnect after a database restart runs it
	// again. Nil is a no-op. Use it for data moves the additive-only schema
	// rule cannot express as a column; table creation itself belongs in the
	// data package's registry.
	OnConnect func(context.Context, gowild_data.Database) error
	// EmptyDSNErr is the error surfaced when the DSN is empty after
	// trimming, so callers can point at their own configuration fix. Nil
	// defaults to a generic error.
	EmptyDSNErr error
}

// Keeper holds the current database handle, or nil while disconnected.
type Keeper struct {
	dsn string
	// newDB, onConnect and emptyDSNErr are captured from Options at Open, so
	// a Keeper keeps the configuration it was opened with for its whole life.
	newDB       func(string) (gowild_data.Database, error)
	onConnect   func(context.Context, gowild_data.Database) error
	emptyDSNErr error

	mu    sync.RWMutex
	db    gowild_data.Database
	err   error
	upFns []func()

	stop chan struct{}
	once sync.Once
}

// retry backoff bounds for the reconnect loop.
const (
	minBackoff = 500 * time.Millisecond
	maxBackoff = 5 * time.Second
	pingEvery  = 2 * time.Second
)

// Open returns a Keeper and starts its connect/health loop. It returns before
// the first connection attempt completes: callers must tolerate DB() == nil.
func Open(ctx context.Context, dsn string, opts Options) *Keeper {
	k := &Keeper{
		dsn:         dsn,
		newDB:       opts.NewDB,
		onConnect:   opts.OnConnect,
		emptyDSNErr: opts.EmptyDSNErr,
		stop:        make(chan struct{}),
	}
	if k.newDB == nil {
		k.newDB = func(dsn string) (gowild_data.Database, error) {
			return gowild_data.NewPostgresDatabase(dsn)
		}
	}
	if k.emptyDSNErr == nil {
		k.emptyDSNErr = errors.New("database DSN is empty")
	}
	go k.loop(ctx)
	return k
}

// DB returns the live database handle, or nil when disconnected.
func (k *Keeper) DB() gowild_data.Database {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.db
}

// Up reports whether the database is currently reachable.
func (k *Keeper) Up() bool { return k.DB() != nil }

// OnUp registers fn to run after every successful connect — the first and any
// reconnect after a drop. A keeper that is already up runs fn once
// immediately: callers wire their hooks after Open returns, and Open does not
// wait for the first connect, so the connect can land in that gap and the
// hook would otherwise be missed. fn may therefore run more than once, and on
// the keeper's own goroutine: it must be idempotent and quick.
func (k *Keeper) OnUp(fn func()) {
	k.mu.Lock()
	k.upFns = append(k.upFns, fn)
	up := k.db != nil
	k.mu.Unlock()
	if up {
		fn()
	}
}

// upCallbacks snapshots the registered OnUp hooks for a run outside the lock.
func (k *Keeper) upCallbacks() []func() {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return slices.Clone(k.upFns)
}

// Err returns the last connection error, or nil when connected.
func (k *Keeper) Err() error {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.err
}

// Close stops the health loop and releases the connection.
func (k *Keeper) Close() {
	k.once.Do(func() { close(k.stop) })
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.db != nil {
		_ = k.db.Close()
		k.db = nil
	}
}

// WaitReady blocks until the database is up or the timeout elapses. Used by
// tests and one-shot commands, never by a long-lived server.
func (k *Keeper) WaitReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if k.Up() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return k.Up()
}

func (k *Keeper) loop(ctx context.Context) {
	backoff := minBackoff
	for {
		if k.DB() == nil {
			if err := k.connect(ctx); err != nil {
				k.setErr(err)
				slog.Warn("database unavailable, serving degraded", "err", err, "retry_in", backoff)
				if !k.wait(ctx, backoff) {
					return
				}
				backoff = min(backoff*2, maxBackoff)
				continue
			}
			backoff = minBackoff
			slog.Info("database connected")
			for _, fn := range k.upCallbacks() {
				fn()
			}
		}

		if !k.wait(ctx, pingEvery) {
			return
		}

		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := func() error {
			db := k.DB()
			if db == nil {
				return nil
			}
			return db.Ping(pingCtx)
		}()
		cancel()
		if err != nil {
			slog.Warn("database ping failed, dropping connection", "err", err)
			k.drop(err)
		}
	}
}

func (k *Keeper) connect(ctx context.Context) error {
	dsn := strings.TrimSpace(k.dsn)
	if dsn == "" {
		return k.emptyDSNErr
	}
	db, err := k.newDB(dsn)
	if err != nil {
		return err
	}
	// Every table registered by an imported package materialises here; the
	// drivers create missing tables and add missing columns only, so a
	// reconnect can run this safely.
	if err := gowild_data.AddAllTables(db); err != nil {
		_ = db.Close()
		return err
	}
	if k.onConnect != nil {
		if err := k.onConnect(ctx, db); err != nil {
			_ = db.Close()
			return err
		}
	}
	k.mu.Lock()
	k.db, k.err = db, nil
	k.mu.Unlock()
	return nil
}

func (k *Keeper) drop(err error) {
	k.mu.Lock()
	old := k.db
	k.db, k.err = nil, err
	k.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
}

func (k *Keeper) setErr(err error) {
	k.mu.Lock()
	k.err = err
	k.mu.Unlock()
}

// wait sleeps for d, returning false if the keeper or context is done.
func (k *Keeper) wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-k.stop:
		return false
	case <-ctx.Done():
		return false
	}
}
