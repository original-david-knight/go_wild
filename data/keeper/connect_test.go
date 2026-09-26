package keeper

import (
	"context"
	"errors"
	"testing"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

// closeCounter wraps a real in-memory database and counts Close calls, so a
// test can see whether the keeper released a handle it gave up on.
type closeCounter struct {
	*gowild_data.SqliteDatabase
	closes int
}

func (c *closeCounter) Close() error {
	c.closes++
	return c.SqliteDatabase.Close()
}

func newCounted(t *testing.T) *closeCounter {
	t.Helper()
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	return &closeCounter{SqliteDatabase: db}
}

// newIdleKeeper builds a Keeper without starting its loop, so connect and
// drop can be driven step by step.
func newIdleKeeper(dsn string, opts Options) *Keeper {
	return &Keeper{dsn: dsn, newDB: opts.NewDB, onConnect: opts.OnConnect, emptyDSNErr: opts.EmptyDSNErr, stop: make(chan struct{})}
}

func TestConnectSurfacesTheOpenError(t *testing.T) {
	want := errors.New("connection refused")
	var gotDSN string
	k := newIdleKeeper("  postgres://db  ", Options{NewDB: func(dsn string) (gowild_data.Database, error) {
		gotDSN = dsn
		return nil, want
	}})
	if err := k.connect(context.Background()); !errors.Is(err, want) {
		t.Fatalf("connect = %v, want %v", err, want)
	}
	if gotDSN != "postgres://db" {
		t.Fatalf("NewDB got DSN %q, want it trimmed", gotDSN)
	}
	if k.Up() {
		t.Fatal("a failed open must leave the keeper down")
	}
}

func TestConnectClosesTheHandleWhenOnConnectFails(t *testing.T) {
	db := newCounted(t)
	want := errors.New("backfill failed")
	var hookSaw gowild_data.Database
	k := newIdleKeeper("dsn", Options{
		NewDB: func(string) (gowild_data.Database, error) { return db, nil },
		OnConnect: func(_ context.Context, got gowild_data.Database) error {
			hookSaw = got
			return want
		},
	})
	if err := k.connect(context.Background()); !errors.Is(err, want) {
		t.Fatalf("connect = %v, want %v", err, want)
	}
	if hookSaw != db {
		t.Fatalf("OnConnect received %v, want the opened handle", hookSaw)
	}
	if db.closes != 1 {
		t.Fatalf("the rejected handle was closed %d times, want 1", db.closes)
	}
	if k.DB() != nil {
		t.Fatal("a failed OnConnect must not publish the handle")
	}
}

func TestDropReleasesTheHandleAndRecordsTheError(t *testing.T) {
	db := newCounted(t)
	k := newIdleKeeper("dsn", Options{NewDB: func(string) (gowild_data.Database, error) { return db, nil }})
	if err := k.connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if k.DB() != db || k.Err() != nil {
		t.Fatalf("after connect DB=%v Err=%v, want the handle and no error", k.DB(), k.Err())
	}

	pingErr := errors.New("ping timeout")
	k.drop(pingErr)
	if k.Up() {
		t.Fatal("drop must leave the keeper down")
	}
	if !errors.Is(k.Err(), pingErr) {
		t.Fatalf("Err after drop = %v, want %v", k.Err(), pingErr)
	}
	if db.closes != 1 {
		t.Fatalf("dropped handle closed %d times, want 1", db.closes)
	}

	// A second drop with nothing held only updates the error.
	again := errors.New("still down")
	k.drop(again)
	if db.closes != 1 || !errors.Is(k.Err(), again) {
		t.Fatalf("second drop: closes=%d err=%v", db.closes, k.Err())
	}
}

func TestWaitStopsOnContextCancel(t *testing.T) {
	k := newIdleKeeper("dsn", Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if k.wait(ctx, maxBackoff) {
		t.Fatal("wait on a cancelled context must report false")
	}
}

func TestLoopExitsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	k := newIdleKeeper("", Options{EmptyDSNErr: errors.New("no dsn")})
	done := make(chan struct{})
	go func() { k.loop(ctx); close(done) }()
	<-done
	if k.Err() == nil || k.Err().Error() != "no dsn" {
		t.Fatalf("loop recorded %v, want the empty-DSN error before exiting", k.Err())
	}
}
