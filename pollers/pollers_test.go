package pollers

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
)

// The tests need source names but the backbone does not care what they are;
// these two stand in for any pair of registered sources.
const (
	sourceWeather = "weather"
	sourceRSS     = "rss"
)

func TestMain(m *testing.M) {
	// A poller on a millisecond cadence logs a line per cycle; these tests
	// assert on behaviour, not on the log, and the noise would bury a real
	// failure.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// memDB backs the pollers with the data module's in-memory SQLite driver. The
// sync row a failure must not overwrite and the record a poll stores only
// exist with real storage behind them.
func memDB(t *testing.T) gowild_data.Database {
	t.Helper()
	db, err := gowild_data.NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatalf("open in-memory database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := gowild_data.AddAllTables(db); err != nil {
		t.Fatalf("create tables: %v", err)
	}
	return db
}

// upDB is the always-there handle; the down case gets its own DBFunc.
func upDB(db gowild_data.Database) DBFunc {
	return func() gowild_data.Database { return db }
}

// waitFor polls cond until it holds. The framework is asynchronous by nature,
// so a fixed sleep would be either flaky or slow.
func waitFor(t *testing.T, limit time.Duration, cond func() bool, complaint string) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%s (waited %s)", complaint, limit)
}

// TestARegisteredPollerFiresOnItsInterval is the framework's whole promise: a
// source registered with a cadence gets polled on it, starting immediately so
// a fresh process does not wait a whole interval before it has anything to
// show.
func TestARegisteredPollerFiresOnItsInterval(t *testing.T) {
	const interval = 40 * time.Millisecond
	runs := make(chan struct{}, 32)

	m := NewManager(upDB(memDB(t)))
	if err := m.Register(Poller{Source: sourceWeather, Interval: interval, Run: func(context.Context, gowild_data.Database) error {
		select {
		case runs <- struct{}{}:
		default:
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	m.Start(context.Background())
	defer m.Stop()

	for i := range 4 {
		select {
		case <-runs:
		case <-time.After(2 * time.Second):
			t.Fatalf("%d runs in 2s, want the startup poll plus three ticks at %s", i, interval)
		}
	}
	// The startup poll lands at once, so three intervals must have elapsed by
	// the fourth run — anything faster means the loop is free-running.
	if elapsed := time.Since(start); elapsed < 3*interval {
		t.Fatalf("four runs took %s, want at least %s at a %s cadence", elapsed, 3*interval, interval)
	}
}

// TestAPollThatOverrunsItsIntervalIsNeverOverlapped is the dedupe guarantee: a
// slow source falls behind its cadence rather than stacking concurrent fetches
// on the same API.
func TestAPollThatOverrunsItsIntervalIsNeverOverlapped(t *testing.T) {
	var inFlight, overlaps, runs atomic.Int32
	done := make(chan struct{}, 8)

	m := NewManager(upDB(memDB(t)))
	if err := m.Register(Poller{Source: sourceRSS, Interval: 5 * time.Millisecond, Run: func(context.Context, gowild_data.Database) error {
		if inFlight.Add(1) > 1 {
			overlaps.Add(1)
		}
		time.Sleep(40 * time.Millisecond) // eight ticks' worth of work
		runs.Add(1)
		inFlight.Add(-1)
		select {
		case done <- struct{}{}:
		default:
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	m.Start(context.Background())

	for i := range 3 {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("%d cycles finished in 2s, want 3", i)
		}
	}
	m.Stop()

	if n := overlaps.Load(); n != 0 {
		t.Fatalf("%d of %d cycles started while another was still running", n, runs.Load())
	}
}

// TestStopHaltsEveryPollerAndWaitsForTheCycleInFlight covers shutdown: the
// owning process stops the manager on its way down, and a half-written poll
// must not be abandoned mid-cycle.
func TestStopHaltsEveryPollerAndWaitsForTheCycleInFlight(t *testing.T) {
	var runs, finished atomic.Int32
	started := map[string]chan struct{}{
		sourceWeather: make(chan struct{}, 1),
		sourceRSS:     make(chan struct{}, 1),
	}

	m := NewManager(upDB(memDB(t)))
	for source, ready := range started {
		if err := m.Register(Poller{Source: source, Interval: 10 * time.Millisecond, Run: func(context.Context, gowild_data.Database) error {
			runs.Add(1)
			select {
			case ready <- struct{}{}:
			default:
			}
			// Deliberately deaf to cancellation: Stop has to wait for a cycle
			// that is mid-flight, not merely ask it to end.
			time.Sleep(60 * time.Millisecond)
			finished.Add(1)
			return nil
		}}); err != nil {
			t.Fatal(err)
		}
	}

	m.Start(context.Background())
	for source, ready := range started {
		select {
		case <-ready:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s never polled", source)
		}
	}

	m.Stop()

	if r, f := runs.Load(), finished.Load(); r != f {
		t.Errorf("Stop returned with %d of %d cycles still in flight", r-f, r)
	}
	if sources := m.Sources(); len(sources) != 0 {
		t.Errorf("Sources = %v after Stop, want nothing running", sources)
	}
	settled := runs.Load()
	time.Sleep(50 * time.Millisecond) // five intervals
	if again := runs.Load(); again != settled {
		t.Errorf("%d further poll(s) after Stop", again-settled)
	}
}

// TestDeregisterStopsTheSourceAndWaitsForTheCycleInFlight covers removing one
// source while the rest keep running: the removed poller's in-flight cycle
// finishes before Deregister returns, no further cycle starts, and the other
// source is untouched.
func TestDeregisterStopsTheSourceAndWaitsForTheCycleInFlight(t *testing.T) {
	var runs, finished atomic.Int32
	ready := make(chan struct{}, 1)

	m := NewManager(upDB(memDB(t)))
	if err := m.Register(Poller{Source: sourceWeather, Interval: 10 * time.Millisecond, Run: func(context.Context, gowild_data.Database) error {
		runs.Add(1)
		select {
		case ready <- struct{}{}:
		default:
		}
		// Deliberately deaf to cancellation: Deregister has to wait for a
		// cycle that is mid-flight, not merely ask it to end.
		time.Sleep(60 * time.Millisecond)
		finished.Add(1)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Register(Poller{Source: sourceRSS, Interval: 10 * time.Millisecond, Run: func(context.Context, gowild_data.Database) error {
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	m.Start(context.Background())
	defer m.Stop()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s never polled", sourceWeather)
	}

	m.Deregister(sourceWeather)

	if r, f := runs.Load(), finished.Load(); r != f {
		t.Errorf("Deregister returned with %d of %d cycles still in flight", r-f, r)
	}
	settled := runs.Load()
	time.Sleep(50 * time.Millisecond) // five intervals
	if again := runs.Load(); again != settled {
		t.Errorf("%d further poll(s) after Deregister", again-settled)
	}
	if sources := m.Sources(); len(sources) != 1 || sources[0] != sourceRSS {
		t.Errorf("Sources = %v after deregistering %s, want just %s", sources, sourceWeather, sourceRSS)
	}
	if _, ok := m.Interval(sourceWeather); ok {
		t.Errorf("Interval still reports a cadence for the deregistered %s", sourceWeather)
	}
}

// TestRescheduleChangesTheCadenceWithoutLeavingTwoLoops is the cadence-change
// path: a new interval takes effect on the running poller, and the loop it
// replaces is gone rather than doubling the poll rate.
func TestRescheduleChangesTheCadenceWithoutLeavingTwoLoops(t *testing.T) {
	const (
		fast = 5 * time.Millisecond
		slow = 60 * time.Millisecond
	)
	var runs atomic.Int32

	m := NewManager(upDB(memDB(t)))
	if err := m.Register(Poller{Source: sourceWeather, Interval: fast, Run: func(context.Context, gowild_data.Database) error {
		runs.Add(1)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	m.Start(context.Background())
	waitFor(t, 2*time.Second, func() bool { return runs.Load() >= 5 }, "the fast poller never got going")

	if err := m.Reschedule(sourceWeather, slow); err != nil {
		t.Fatal(err)
	}
	if got, ok := m.Interval(sourceWeather); !ok || got != slow {
		t.Fatalf("Interval = %s/%v after rescheduling, want %s", got, ok, slow)
	}

	const window = 300 * time.Millisecond
	runs.Store(0)
	time.Sleep(window)
	got := runs.Load()
	if got == 0 {
		t.Errorf("no runs in %s at a %s cadence — the reschedule dropped the poller", window, slow)
	}
	// One loop on the slow cadence runs about five times in the window; the
	// abandoned fast loop would have run sixty.
	if got > 15 {
		t.Errorf("%d runs in %s after rescheduling to %s — the %s loop is still ticking", got, window, slow, fast)
	}

	m.Stop()
	settled := runs.Load()
	time.Sleep(3 * slow)
	if again := runs.Load(); again != settled {
		t.Errorf("%d poll(s) after Stop, want the rescheduled loop stopped too", again-settled)
	}
}

// TestACycleSkippedForADownDatabaseRetriesOnTheShortTimer pins the startup
// race: the manager starts before the database is reachable, and a startup
// poll that waited out its whole cadence would leave the source empty for
// hours over a fault that cleared in seconds.
func TestACycleSkippedForADownDatabaseRetriesOnTheShortTimer(t *testing.T) {
	db := memDB(t)
	var handles atomic.Int32
	down := func() gowild_data.Database {
		if handles.Add(1) <= 2 {
			return nil
		}
		return db
	}

	polled := make(chan struct{}, 1)
	m := NewManager(down)
	// An interval far longer than the retry timer: only the short retry can
	// deliver a successful cycle inside the deadline below.
	if err := m.Register(Poller{Source: sourceWeather, Interval: time.Hour, Run: func(context.Context, gowild_data.Database) error {
		select {
		case polled <- struct{}{}:
		default:
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	m.Start(context.Background())
	defer m.Stop()

	select {
	case <-polled:
	case <-time.After(4 * dbRetry):
		t.Fatalf("no cycle within %s: the skipped ticks are waiting out the 1h interval instead of retrying every %s", 4*dbRetry, dbRetry)
	}
	if elapsed := time.Since(start); elapsed < dbRetry {
		t.Errorf("polled after %s, want the two skipped cycles to have cost at least %s", elapsed, dbRetry)
	}

	// The cycle that finally ran was a whole one, sync row and all.
	waitFor(t, 2*time.Second, func() bool {
		syncs, err := LastSyncs(context.Background(), db)
		return err == nil && syncs[sourceWeather] != nil
	}, "the retried cycle recorded no successful sync")

	if got, ok := m.Interval(sourceWeather); !ok || got != time.Hour {
		t.Errorf("Interval = %s/%v, want the registered 1h unchanged by the skips", got, ok)
	}
}

// TestAFailedPollRecordsTheErrorAndKeepsTheLastSuccess is what "last sync"
// means: the last time the data was actually fresh, not the last time the
// service tried.
func TestAFailedPollRecordsTheErrorAndKeepsTheLastSuccess(t *testing.T) {
	db := memDB(t)
	ctx := context.Background()
	success := time.Date(2026, 8, 2, 6, 41, 0, 0, time.UTC)
	RecordSuccess(ctx, db, sourceWeather, success)

	m := NewManager(upDB(db))
	// The startup poll fires at once and the next tick is an hour away, so
	// exactly one failing cycle runs.
	if err := m.Register(Poller{Source: sourceWeather, Interval: time.Hour, Run: func(context.Context, gowild_data.Database) error {
		return fmt.Errorf("upstream returned 500 Internal Server Error")
	}}); err != nil {
		t.Fatal(err)
	}
	m.Start(ctx)
	defer m.Stop()

	var row *Sync
	waitFor(t, 2*time.Second, func() bool {
		syncs, err := LastSyncs(ctx, db)
		if err != nil {
			return false
		}
		row = syncs[sourceWeather]
		return row != nil && row.LastError != ""
	}, "the failure was never recorded on the sync row")

	if !row.At.Equal(success) {
		t.Errorf("last success = %s, want %s untouched by the failure", row.At, success)
	}
	if !strings.Contains(row.LastError, "500") {
		t.Errorf("last_error = %q, want the run's own message", row.LastError)
	}
	if row.ErrorAt.IsZero() {
		t.Error("error_at is zero, want the time of the failure")
	}
}

// TestACycleThatHangsIsCutOffAndTheSourceKeepsPolling is what stops one stuck
// outbound call from taking a source down for the life of the process. Cycles
// never overlap, so a cycle that never returns is a poller that never runs
// again — which is exactly what a hung outbound call with no deadline of its
// own would cause.
func TestACycleThatHangsIsCutOffAndTheSourceKeepsPolling(t *testing.T) {
	db := memDB(t)
	ctx := context.Background()

	var mu sync.Mutex
	cycles := 0
	m := NewManager(upDB(db))
	if err := m.Register(Poller{
		Source: sourceWeather, Interval: 10 * time.Millisecond,
		Timeout: 20 * time.Millisecond,
		Run: func(ctx context.Context, _ gowild_data.Database) error {
			mu.Lock()
			n := cycles
			cycles++
			mu.Unlock()
			if n == 0 {
				// The first cycle hangs until its deadline, as an outbound
				// call with no timeout of its own would.
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	m.Start(ctx)
	defer m.Stop()

	// The hang is recorded as a failed poll, naming the budget it exceeded.
	var row *Sync
	waitFor(t, 3*time.Second, func() bool {
		syncs, err := LastSyncs(ctx, db)
		if err != nil {
			return false
		}
		row = syncs[sourceWeather]
		return row != nil && row.LastError != ""
	}, "the hung cycle was never recorded as a failure")
	if !strings.Contains(row.LastError, "exceeded") {
		t.Errorf("last_error = %q, want it to name the exceeded budget", row.LastError)
	}

	// And the source is still polling: the next cycle succeeds.
	waitFor(t, 3*time.Second, func() bool {
		syncs, err := LastSyncs(ctx, db)
		if err != nil {
			return false
		}
		return syncs[sourceWeather] != nil && !syncs[sourceWeather].At.IsZero()
	}, "the source never polled again after a hung cycle")
}

func TestAPollerTakesTheDefaultCycleTimeout(t *testing.T) {
	if got := (Poller{}).timeout(); got != DefaultCycleTimeout {
		t.Errorf("zero Timeout = %s, want the default %s", got, DefaultCycleTimeout)
	}
	if got := (Poller{Timeout: time.Second}).timeout(); got != time.Second {
		t.Errorf("explicit Timeout = %s, want it honoured", got)
	}
}

// TestRegisterRefusesAPollerItCouldNotRun keeps a half-built registration out
// of the running set, where it would be a loop that ticks forever doing
// nothing.
func TestRegisterRefusesAPollerItCouldNotRun(t *testing.T) {
	m := NewManager(upDB(memDB(t)))
	ok := func(context.Context, gowild_data.Database) error { return nil }

	for name, p := range map[string]Poller{
		"no source":         {Interval: time.Minute, Run: ok},
		"no run function":   {Source: sourceWeather, Interval: time.Minute},
		"zero interval":     {Source: sourceWeather, Run: ok},
		"negative interval": {Source: sourceWeather, Interval: -time.Minute, Run: ok},
	} {
		if err := m.Register(p); err == nil {
			t.Errorf("Register(%s) = nil, want an error", name)
		}
	}
	if sources := m.Sources(); len(sources) != 0 {
		t.Errorf("Sources = %v after four refused registrations, want none", sources)
	}
	if _, ok := m.Interval(sourceWeather); ok {
		t.Error("Interval reports a cadence for a source that was never registered")
	}
	if err := m.Reschedule(sourceWeather, time.Minute); err == nil {
		t.Error("Reschedule of an unregistered source = nil, want an error")
	}
}

// TestSuccessIsStampedWithTheManagersClock pins the injectable clock: a
// consumer with its own notion of now sets Now before Start, and the sync row
// carries that time rather than the wall clock's.
func TestSuccessIsStampedWithTheManagersClock(t *testing.T) {
	db := memDB(t)
	ctx := context.Background()
	fixed := time.Date(2026, 8, 2, 6, 41, 0, 0, time.UTC)

	m := NewManager(upDB(db))
	m.Now = func() time.Time { return fixed }
	if err := m.Register(Poller{Source: sourceWeather, Interval: time.Hour, Run: func(context.Context, gowild_data.Database) error {
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	m.Start(ctx)
	defer m.Stop()

	waitFor(t, 2*time.Second, func() bool {
		syncs, err := LastSyncs(ctx, db)
		return err == nil && syncs[sourceWeather] != nil && syncs[sourceWeather].At.Equal(fixed)
	}, "the sync row never carried the injected clock's time")
}
