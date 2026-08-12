package gowild_my

import (
	"testing"
	"time"
)

// The env names below exist only for these tests; t.Setenv scopes each value
// to the test that sets it.
const (
	testNowEnv      = "GOWILD_MY_TEST_NOW"
	testOverrideEnv = "GOWILD_MY_TEST_ALLOW_CLOCK_OVERRIDE"
)

func testClock() Clock {
	return Clock{TestNowEnv: testNowEnv, OverrideEnv: testOverrideEnv}
}

func TestDayKeyFormatsWallClockNotUTC(t *testing.T) {
	if got := DayKey(time.Date(2026, 8, 2, 12, 0, 0, 0, time.Local)); got != "2026-08-02" {
		t.Fatalf("DayKey = %q, want 2026-08-02", got)
	}

	// A day key is the wall-clock date where the machine stands. This instant is
	// still 2026-08-02 in UTC, so a stray .UTC() in DayKey would show up here.
	ahead := time.FixedZone("ahead", 5*60*60)
	if got := DayKey(time.Date(2026, 8, 3, 1, 0, 0, 0, ahead)); got != "2026-08-03" {
		t.Fatalf("DayKey = %q, want 2026-08-03 — day keys must not be converted to UTC", got)
	}
}

func TestTodayTracksTheRealClock(t *testing.T) {
	// An empty override is the normal-operation state, ambient env or not.
	t.Setenv(testNowEnv, "")
	c := testClock()

	before := time.Now()
	got := c.Today()
	after := time.Now()

	// Either bound is acceptable: the test may straddle local midnight.
	if got != before.Format(DayFormat) && got != after.Format(DayFormat) {
		t.Fatalf("Today = %q, want %q", got, before.Format(DayFormat))
	}
}

func TestTestNowOverrideIsHonoredAndReReadEveryCall(t *testing.T) {
	c := testClock()
	t.Setenv(testNowEnv, time.Date(2026, 8, 2, 8, 30, 0, 0, time.Local).Format(time.RFC3339))
	if got := c.Today(); got != "2026-08-02" {
		t.Fatalf("Today = %q, want the overridden 2026-08-02", got)
	}

	// A test harness advances time between requests, so the override cannot be
	// cached at process start.
	t.Setenv(testNowEnv, time.Date(2026, 8, 9, 8, 30, 0, 0, time.Local).Format(time.RFC3339))
	if got := c.Today(); got != "2026-08-09" {
		t.Fatalf("Today = %q, want the advanced 2026-08-09", got)
	}
}

func TestTestNowOverrideLandsInTheLocalZone(t *testing.T) {
	const raw = "2026-08-02T12:00:00Z"
	t.Setenv(testNowEnv, raw)
	c := testClock()

	want, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatal(err)
	}
	got := c.Now()
	if !got.Equal(want) {
		t.Fatalf("Now = %s, want the override instant %s", got, want)
	}
	if got.Location() != time.Local {
		t.Fatalf("Now location = %s, want Local — day keys are derived from it", got.Location())
	}
}

func TestUnparseableOverrideFallsBackToRealTime(t *testing.T) {
	t.Setenv(testNowEnv, "yesterday-ish")
	c := testClock()

	got := c.Now()
	if delta := time.Since(got); delta < -time.Second || delta > time.Second {
		t.Fatalf("Now is %s away from real time; a junk override must not stop the clock", delta)
	}
	before := time.Now().Format(DayFormat)
	day := c.Today()
	if day != before && day != time.Now().Format(DayFormat) {
		t.Fatalf("Today = %q, want the real %q", day, before)
	}
}

func TestZeroValueClockIgnoresEnvOverrides(t *testing.T) {
	// The variables are set, but a zero-value Clock names no variables at all,
	// so it must not read them.
	t.Setenv(testNowEnv, time.Date(2001, 1, 1, 8, 30, 0, 0, time.Local).Format(time.RFC3339))
	t.Setenv(testOverrideEnv, "1")

	var c Clock
	got := c.Now()
	if delta := time.Since(got); delta < -time.Second || delta > time.Second {
		t.Fatalf("zero-value Now is %s away from real time; empty env names must disable the override", delta)
	}
	if c.OverrideAllowed() {
		t.Fatal("zero-value OverrideAllowed = true, want false — an empty OverrideEnv keeps the door shut")
	}
}

func TestParseDayReturnsLocalMidnight(t *testing.T) {
	got, err := ParseDay("2026-08-02")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 8, 2, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("ParseDay = %s, want local midnight %s", got, want)
	}
	if got.Location() != time.Local {
		t.Fatalf("ParseDay location = %s, want Local", got.Location())
	}
	if round := DayKey(got); round != "2026-08-02" {
		t.Fatalf("round trip = %q, want 2026-08-02", round)
	}
}

func TestValidDay(t *testing.T) {
	cases := map[string]bool{
		"2026-08-02": true,
		"2026-13-01": false,
		"not-a-date": false,
		"":           false,
	}
	for day, want := range cases {
		if got := ValidDay(day); got != want {
			t.Errorf("ValidDay(%q) = %v, want %v", day, got, want)
		}
		if _, err := ParseDay(day); (err == nil) != want {
			t.Errorf("ParseDay(%q) error = %v, want error: %v", day, err, !want)
		}
	}
}

func TestLastNDaysIsOldestFirstEndingToday(t *testing.T) {
	t.Setenv(testNowEnv, time.Date(2026, 8, 2, 8, 30, 0, 0, time.Local).Format(time.RFC3339))
	c := testClock()

	// The window crosses a month boundary, which is where naive string
	// arithmetic on day keys breaks.
	want := []string{"2026-07-27", "2026-07-28", "2026-07-29", "2026-07-30", "2026-07-31", "2026-08-01", "2026-08-02"}
	got := c.LastNDays(7)
	if len(got) != len(want) {
		t.Fatalf("LastNDays(7) returned %d keys: %v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("LastNDays(7) = %v, want %v", got, want)
		}
	}
	if got[len(got)-1] != c.Today() {
		t.Fatalf("window ends on %q, want today %q", got[len(got)-1], c.Today())
	}
}

func TestDaysFromStopsAtToday(t *testing.T) {
	t.Setenv(testNowEnv, time.Date(2026, 8, 2, 8, 30, 0, 0, time.Local).Format(time.RFC3339))
	c := testClock()

	cases := []struct {
		name  string
		start string
		n     int
		want  []string
	}{
		{
			// The window crosses a month boundary, which is where naive string
			// arithmetic on day keys breaks.
			name:  "a window still running is elapsed up to today",
			start: "2026-07-30",
			n:     7,
			want:  []string{"2026-07-30", "2026-07-31", "2026-08-01", "2026-08-02"},
		},
		{
			name:  "a window already over is its whole length",
			start: "2026-07-28",
			n:     3,
			want:  []string{"2026-07-28", "2026-07-29", "2026-07-30"},
		},
		{
			name:  "a window starting today holds today alone",
			start: "2026-08-02",
			n:     366,
			want:  []string{"2026-08-02"},
		},
		{name: "a window not yet begun has elapsed nothing", start: "2026-08-03", n: 7},
		{name: "an unparseable start is no window", start: "not-a-day", n: 7},
		{name: "a window of no days is no window", start: "2026-07-30", n: 0},
	}

	for _, c2 := range cases {
		t.Run(c2.name, func(t *testing.T) {
			got := c.DaysFrom(c2.start, c2.n)
			if len(got) != len(c2.want) {
				t.Fatalf("DaysFrom(%q, %d) = %v, want %v", c2.start, c2.n, got, c2.want)
			}
			for i := range c2.want {
				if got[i] != c2.want[i] {
					t.Fatalf("DaysFrom(%q, %d) = %v, want %v", c2.start, c2.n, got, c2.want)
				}
			}
		})
	}
}
