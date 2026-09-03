package gowild_projects

import (
	"strings"
	"time"
)

// A worker's availability, derived from its last quota report and the
// clock. The runner for a Claude worker reads the account's usage before
// each check-in and sends it along; the tracker keeps the windows and
// decides, so the state flips on its own when a window resets, even with
// the desk off, and a stale report can never pin a worker out past the
// window that produced it.
const (
	// AvailabilityAvailable: the worker pulls work and may be pinned.
	AvailabilityAvailable = "available"
	// AvailabilityBreak: the five-hour window is nearly spent and resets
	// within QuotaBreakWindow. The worker pulls nothing but may be pinned,
	// and the tier's other workers do not take over: the pool waits.
	AvailabilityBreak = "break"
	// AvailabilityUnavailable: the weekly window is nearly spent, or the
	// five-hour one is and its reset is further off than QuotaBreakWindow.
	// The worker pulls nothing, cannot be pinned, and its tier-mates take
	// the tier's unpinned work.
	AvailabilityUnavailable = "unavailable"
)

// The quota floors, in percent of the window still unused, and how close a
// five-hour reset has to be for a spent window to count as a break rather
// than an outage.
const (
	QuotaWeeklyFloor  = 15.0
	QuotaSessionFloor = 10.0
	QuotaBreakWindow  = time.Hour
)

// QuotaScopeAll names the account-wide weekly window in QuotaWeeklyScope.
const QuotaScopeAll = "all"

// The windows a report carries, named in Availability.Window.
const (
	QuotaWindowSession = "session"
	QuotaWindowWeekly  = "weekly"
)

// QuotaWindow is one rate-limit window: how much of it is used, in
// percent, and when it resets. A zero ResetsAt is a window the account does
// not have.
type QuotaWindow struct {
	Used     float64   `json:"used"`
	ResetsAt time.Time `json:"resets_at"`
}

// ScopedQuota is a weekly window the account scopes to one model, named by
// the display name the account uses ("Fable").
type ScopedQuota struct {
	Model string `json:"model"`
	QuotaWindow
}

// QuotaReport is what a runner sends at check-in: the account's five-hour
// window, its weekly window across models, and every model-scoped weekly
// window it has. The tracker keeps the session window and the tightest
// weekly one that applies to the worker's model.
type QuotaReport struct {
	Session QuotaWindow   `json:"session"`
	Weekly  QuotaWindow   `json:"weekly"`
	Scoped  []ScopedQuota `json:"scoped"`
}

// Availability is a worker's state at one moment and the window behind
// it. Window, Scope, Used and Until are empty for an available worker.
type Availability struct {
	State string `json:"state"`
	// Window is the window that decides the state: session or weekly.
	Window string `json:"window"`
	// Scope is the weekly window's scope (QuotaScopeAll or a model name);
	// "" for the session window.
	Scope string `json:"scope"`
	// Used is that window's usage in percent.
	Used float64 `json:"used"`
	// Until is when that window resets and the state ends on its own.
	Until time.Time `json:"until"`
}

// Available reports whether the state is AvailabilityAvailable.
func (av Availability) Available() bool { return av.State == AvailabilityAvailable }

// Availability derives the worker's state at now from its last report.
// The weekly window is checked first: a spent week is an outage whatever
// the five-hour window says. A window whose reset has passed counts as
// fresh, so the state ends at the reset without a new report.
func (a *Agent) Availability(now time.Time) Availability {
	if live(a.QuotaWeeklyResetsAt, now) && 100-a.QuotaWeeklyUsed < QuotaWeeklyFloor {
		return Availability{
			State: AvailabilityUnavailable, Window: QuotaWindowWeekly, Scope: a.QuotaWeeklyScope,
			Used: a.QuotaWeeklyUsed, Until: a.QuotaWeeklyResetsAt,
		}
	}
	if live(a.QuotaSessionResetsAt, now) && 100-a.QuotaSessionUsed < QuotaSessionFloor {
		state := AvailabilityBreak
		if a.QuotaSessionResetsAt.Sub(now) > QuotaBreakWindow {
			state = AvailabilityUnavailable
		}
		return Availability{
			State: state, Window: QuotaWindowSession,
			Used: a.QuotaSessionUsed, Until: a.QuotaSessionResetsAt,
		}
	}
	return Availability{State: AvailabilityAvailable}
}

// Out reports whether the worker is out of its tier's rotation at now:
// disabled, or unavailable on quota. A worker on break is not out — its
// pool waits for it.
func (a *Agent) Out(now time.Time) bool {
	return !a.Enabled || a.Availability(now).State == AvailabilityUnavailable
}

func live(resetsAt, now time.Time) bool {
	return !resetsAt.IsZero() && now.Before(resetsAt)
}

// applyQuota records a report on the row: the session window as sent, and
// as the weekly window the account-wide one or a scoped one that matches
// the worker's model, whichever is fuller. A scoped window matches when its
// name appears in the model the runner launches ("Fable" in
// "claude-fable-5-1", "opus" in "opus").
func applyQuota(a *Agent, q *QuotaReport, now time.Time) {
	a.QuotaSessionUsed, a.QuotaSessionResetsAt = q.Session.Used, q.Session.ResetsAt
	weekly, scope := q.Weekly, QuotaScopeAll
	for _, sc := range q.Scoped {
		if !scopeMatches(a.Model, sc.Model) || sc.ResetsAt.IsZero() {
			continue
		}
		if weekly.ResetsAt.IsZero() || sc.Used > weekly.Used {
			weekly, scope = sc.QuotaWindow, sc.Model
		}
	}
	a.QuotaWeeklyUsed, a.QuotaWeeklyResetsAt, a.QuotaWeeklyScope = weekly.Used, weekly.ResetsAt, scope
	a.QuotaReportedAt = now
}

func scopeMatches(model, scope string) bool {
	scope = strings.TrimSpace(scope)
	return scope != "" && strings.Contains(strings.ToLower(model), strings.ToLower(scope))
}
