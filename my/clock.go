package gowild_my

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// DayFormat is the day-key layout. Day keys are machine-local `YYYY-MM-DD`
// strings, and every producer and consumer must format them this way and no
// other so they agree on what a day is.
const DayFormat = "2006-01-02"

// DayKey formats t as a day key.
func DayKey(t time.Time) string { return t.Format(DayFormat) }

// ParseDay parses a day key into local midnight of that day.
func ParseDay(day string) (time.Time, error) {
	t, err := time.ParseInLocation(DayFormat, day, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("day must be YYYY-MM-DD, got %q", day)
	}
	return t, nil
}

// ValidDay reports whether day is a well-formed day key.
func ValidDay(day string) bool {
	_, err := ParseDay(day)
	return err == nil
}

// Clock is a source of "now" whose environment overrides are named by the
// caller. The zero value never consults the environment: an empty TestNowEnv
// disables the time override, and an empty OverrideEnv keeps the per-request
// door shut.
type Clock struct {
	// TestNowEnv names the environment variable that overrides the clock for
	// deterministic verification. It is read on every call so a test harness
	// can advance time between requests. Leave it empty to disable the
	// override entirely.
	TestNowEnv string

	// OverrideEnv names the environment variable that gates per-request clock
	// overrides. Verification needs to walk a scenario matrix without
	// restarting the process for each row, but a running service must never
	// let a query string decide what day it is — so the door only exists when
	// the named variable is set to "1".
	OverrideEnv string
}

// Now returns the current local time, or the override when TestNowEnv names a
// variable set to a parseable RFC3339 timestamp.
func (c Clock) Now() time.Time {
	if c.TestNowEnv != "" {
		if raw := GetEnvOrDefault(c.TestNowEnv, ""); raw != "" {
			if t, err := time.Parse(time.RFC3339, raw); err == nil {
				return t.Local()
			}
		}
	}
	return time.Now()
}

// OverrideAllowed reports whether per-request clock overrides are enabled. It
// is always false when OverrideEnv is empty.
func (c Clock) OverrideAllowed() bool {
	return c.OverrideEnv != "" && GetEnvOrDefault(c.OverrideEnv, "") == "1"
}

// nowKey is the type of the request-scoped clock override. It is unexported
// so WithNow is the only way to plant an override in a context.
type nowKey struct{}

// WithNow returns a context carrying an overridden "now". The context
// override is independent of any environment variable names, which is why it
// is a package function rather than a Clock method.
func WithNow(ctx context.Context, t time.Time) context.Context {
	return context.WithValue(ctx, nowKey{}, t)
}

// RequestContext applies a `?now=` override to a request's context. It is the
// one door verification uses to walk a time-dependent surface without waiting
// for the wall clock, and it stays shut unless OverrideEnv says otherwise — a
// running service must never let a query string decide what time it is.
//
// The returned error is already phrased for the 400 the handler answers with.
func (c Clock) RequestContext(r *http.Request) (context.Context, error) {
	ctx := r.Context()
	raw := r.URL.Query().Get("now")
	if raw == "" {
		return ctx, nil
	}
	if !c.OverrideAllowed() {
		return ctx, errors.New("clock override is not enabled")
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return ctx, errors.New("now must be an RFC3339 timestamp")
	}
	return WithNow(ctx, t.Local()), nil
}

// NowFrom returns the context's overridden time, falling back to Now().
func (c Clock) NowFrom(ctx context.Context) time.Time {
	if t, ok := ctx.Value(nowKey{}).(time.Time); ok {
		return t
	}
	return c.Now()
}

// TodayFrom returns the day key for the context's time.
func (c Clock) TodayFrom(ctx context.Context) string { return DayKey(c.NowFrom(ctx)) }

// Today returns today's day key in the machine's local timezone.
func (c Clock) Today() string { return DayKey(c.Now()) }

// LastNDays returns the day keys for the n-day window ending today, oldest
// first — the order a chronological consumer renders them in.
func (c Clock) LastNDays(n int) []string {
	today := c.Now()
	days := make([]string, 0, n)
	for i := n - 1; i >= 0; i-- {
		days = append(days, DayKey(today.AddDate(0, 0, -i)))
	}
	return days
}
