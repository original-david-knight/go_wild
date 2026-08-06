package keeper

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

// sqliteOptions points the connect step at an in-memory database, so the
// loop's real connect path runs without a Postgres.
func sqliteOptions() Options {
	return Options{
		NewDB: func(string) (gowild_data.Database, error) {
			return gowild_data.NewSqliteDatabase(":memory:")
		},
	}
}

func TestOpenDoesNotBlockOnAnUnreachableDatabase(t *testing.T) {
	start := time.Now()
	k := Open(context.Background(), "postgres://nobody@127.0.0.1:59999/none?sslmode=disable", Options{})
	defer k.Close()

	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Open blocked for %s; startup must not wait on the database", elapsed)
	}
	if k.Up() {
		t.Fatal("an unreachable DSN must not report up")
	}
}

func TestEmptyDSNIsReportedNotPanicked(t *testing.T) {
	k := Open(context.Background(), "  ", Options{})
	defer k.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && k.Err() == nil {
		time.Sleep(20 * time.Millisecond)
	}
	err := k.Err()
	if err == nil {
		t.Fatal("an empty DSN should surface an error")
	}
	if !strings.Contains(err.Error(), "DSN is empty") {
		t.Fatalf("the error should name the empty DSN, got: %v", err)
	}
}

// TestEmptyDSNSurfacesTheConfiguredError pins the EmptyDSNErr seam: a caller
// that supplies its own error sees that error, so it can point at its own
// configuration fix.
func TestEmptyDSNSurfacesTheConfiguredError(t *testing.T) {
	want := errors.New("MY_DATABASE_URL is empty: run the setup script")
	k := Open(context.Background(), "", Options{EmptyDSNErr: want})
	defer k.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && k.Err() == nil {
		time.Sleep(20 * time.Millisecond)
	}
	if err := k.Err(); !errors.Is(err, want) {
		t.Fatalf("the configured empty-DSN error should surface, got: %v", err)
	}
}

// TestOnUpRunsWhenTheDatabaseConnects pins the hook reconnect-sensitive
// callers hang on: a callback registered while the keeper is still connecting
// runs once the connect lands, whichever side of it the registration falls.
func TestOnUpRunsWhenTheDatabaseConnects(t *testing.T) {
	k := Open(context.Background(), "in-memory-for-test", sqliteOptions())
	defer k.Close()

	ran := make(chan struct{})
	var once sync.Once
	k.OnUp(func() { once.Do(func() { close(ran) }) })

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("the OnUp callback did not run after the database connected")
	}
}

// TestOnUpOnAConnectedKeeperRunsImmediately pins the late-wiring case: a
// caller is built after Open returns, and a connect that beats that wiring
// must not be missed.
func TestOnUpOnAConnectedKeeperRunsImmediately(t *testing.T) {
	k := Open(context.Background(), "in-memory-for-test", sqliteOptions())
	defer k.Close()
	if !k.WaitReady(2 * time.Second) {
		t.Fatal("the keeper never connected")
	}

	var ran atomic.Bool
	k.OnUp(func() { ran.Store(true) })
	if !ran.Load() {
		t.Fatal("OnUp on an already-connected keeper must run the callback before returning")
	}
}

// TestOnConnectRunsAfterTablesOnConnect pins the OnConnect seam: the hook runs
// on a successful connect, after the schema is in place, with the same handle
// the keeper is about to publish.
func TestOnConnectRunsAfterTablesOnConnect(t *testing.T) {
	ran := make(chan struct{})
	var once sync.Once
	opts := sqliteOptions()
	opts.OnConnect = func(ctx context.Context, db gowild_data.Database) error {
		if db == nil {
			t.Error("OnConnect must receive the freshly opened database")
		}
		once.Do(func() { close(ran) })
		return nil
	}
	k := Open(context.Background(), "in-memory-for-test", opts)
	defer k.Close()

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("the OnConnect hook did not run after the database connected")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	k := Open(context.Background(), "postgres://nobody@127.0.0.1:59999/none?sslmode=disable", Options{})
	k.Close()
	k.Close()
}
