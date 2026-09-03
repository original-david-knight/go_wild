package gowild_projects

import (
	"context"
	"errors"
	"testing"
	"time"
)

// tiered sets up the desk as of 2026-09-03: fable leads tier 11 with sol as
// its backup, opus alone at tier 10.
func (f *fixture) tiered() {
	f.t.Helper()
	for _, in := range []AgentInput{
		{Name: "fable", CLI: CLIClaude, Model: "claude-fable-5-1", Effort: EffortXHigh, Tier: 11},
		{Name: "sol", CLI: CLICodex, Model: "gpt-5.6-sol", Effort: EffortXHigh, Tier: 11},
		{Name: "opus", CLI: CLIClaude, Model: "opus", Effort: EffortXHigh, Tier: 10},
	} {
		if _, err := f.s.CreateAgent(f.ctx, in); err != nil {
			f.t.Fatal(err)
		}
	}
}

// report sends a quota report through a check-in without claiming.
func (f *fixture) report(agent string, q QuotaReport) {
	f.t.Helper()
	if _, err := f.s.CheckIn(f.ctx, agent, false, &q); err != nil {
		f.t.Fatal(err)
	}
}

// spentWeek is a report with the Fable-scoped week at 92% (the account's
// real reading on 2026-09-03) and a fresh five-hour window.
func (f *fixture) spentWeek() QuotaReport {
	return QuotaReport{
		Session: QuotaWindow{Used: 5, ResetsAt: f.now.Add(4 * time.Hour)},
		Weekly:  QuotaWindow{Used: 52, ResetsAt: f.now.Add(26 * time.Hour)},
		Scoped:  []ScopedQuota{{Model: "Fable", QuotaWindow: QuotaWindow{Used: 92, ResetsAt: f.now.Add(26 * time.Hour)}}},
	}
}

// offered is what the queue holds for the worker, whatever its own state:
// NextFor previews the queue; the availability gate sits on the check-in.
func (f *fixture) offered(agent string) *Item {
	f.t.Helper()
	job, err := f.s.NextFor(f.ctx, agent)
	if err != nil {
		f.t.Fatal(err)
	}
	if job == nil {
		return nil
	}
	return job.Item
}

// peek is what a check-in would hand the worker, without claiming: nothing
// while it is disabled, on break or unavailable.
func (f *fixture) peek(agent string) *Item {
	f.t.Helper()
	job, err := f.s.CheckIn(f.ctx, agent, false, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	if job == nil {
		return nil
	}
	return job.Item
}

func TestAvailabilityDerivesFromTheWindows(t *testing.T) {
	now := time.Date(2026, 9, 3, 15, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		agent Agent
		want  string
		until time.Duration
	}{
		{"no report", Agent{}, AvailabilityAvailable, 0},
		{"plenty", Agent{QuotaSessionUsed: 5, QuotaSessionResetsAt: now.Add(3 * time.Hour), QuotaWeeklyUsed: 52, QuotaWeeklyResetsAt: now.Add(20 * time.Hour)}, AvailabilityAvailable, 0},
		{"week spent", Agent{QuotaSessionUsed: 5, QuotaSessionResetsAt: now.Add(3 * time.Hour), QuotaWeeklyUsed: 92, QuotaWeeklyResetsAt: now.Add(20 * time.Hour)}, AvailabilityUnavailable, 20 * time.Hour},
		{"week at the floor", Agent{QuotaWeeklyUsed: 85, QuotaWeeklyResetsAt: now.Add(time.Hour)}, AvailabilityAvailable, 0},
		{"week just under", Agent{QuotaWeeklyUsed: 85.5, QuotaWeeklyResetsAt: now.Add(time.Hour)}, AvailabilityUnavailable, time.Hour},
		{"week reset", Agent{QuotaWeeklyUsed: 92, QuotaWeeklyResetsAt: now.Add(-time.Minute)}, AvailabilityAvailable, 0},
		{"session spent, far off", Agent{QuotaSessionUsed: 95, QuotaSessionResetsAt: now.Add(2 * time.Hour)}, AvailabilityUnavailable, 2 * time.Hour},
		{"session spent, an hour", Agent{QuotaSessionUsed: 95, QuotaSessionResetsAt: now.Add(time.Hour)}, AvailabilityBreak, time.Hour},
		{"session spent, soon", Agent{QuotaSessionUsed: 91, QuotaSessionResetsAt: now.Add(20 * time.Minute)}, AvailabilityBreak, 20 * time.Minute},
		{"session at the floor", Agent{QuotaSessionUsed: 90, QuotaSessionResetsAt: now.Add(20 * time.Minute)}, AvailabilityAvailable, 0},
		{"session reset", Agent{QuotaSessionUsed: 99, QuotaSessionResetsAt: now.Add(-time.Second)}, AvailabilityAvailable, 0},
		{"week beats session", Agent{QuotaSessionUsed: 99, QuotaSessionResetsAt: now.Add(time.Minute), QuotaWeeklyUsed: 99, QuotaWeeklyResetsAt: now.Add(time.Hour)}, AvailabilityUnavailable, time.Hour},
	}
	for _, c := range cases {
		av := c.agent.Availability(now)
		if av.State != c.want {
			t.Fatalf("%s: got %s, want %s (%+v)", c.name, av.State, c.want, av)
		}
		if c.until > 0 && !av.Until.Equal(now.Add(c.until)) {
			t.Fatalf("%s: until %s, want %s", c.name, av.Until, now.Add(c.until))
		}
		if c.want == AvailabilityAvailable && (av.Window != "" || !av.Until.IsZero()) {
			t.Fatalf("%s: available with a window: %+v", c.name, av)
		}
	}
	a := Agent{Enabled: true, QuotaSessionUsed: 95, QuotaSessionResetsAt: now.Add(30 * time.Minute)}
	if a.Out(now) {
		t.Fatal("a worker on break is out")
	}
	a.QuotaWeeklyUsed, a.QuotaWeeklyResetsAt = 90, now.Add(time.Hour)
	if !a.Out(now) {
		t.Fatal("an unavailable worker is in")
	}
	a = Agent{Enabled: false}
	if !a.Out(now) {
		t.Fatal("a disabled worker is in")
	}
}

// A report keeps the session window and the tightest weekly window that
// applies to the worker's model.
func TestQuotaReportPicksTheTightestWeekly(t *testing.T) {
	f := newFixture(t)
	f.tiered()
	f.report("fable", f.spentWeek())
	fable, _ := f.s.GetAgent(f.ctx, "fable")
	if fable.QuotaSessionUsed != 5 || fable.QuotaWeeklyUsed != 92 || fable.QuotaWeeklyScope != "Fable" || !fable.QuotaReportedAt.Equal(f.now) {
		t.Fatalf("fable's report: %+v", fable)
	}
	if av := fable.Availability(f.now); av.State != AvailabilityUnavailable || av.Window != QuotaWindowWeekly || av.Scope != "Fable" || av.Used != 92 {
		t.Fatalf("fable's availability: %+v", av)
	}
	// The same account report leaves opus on the account-wide week.
	f.report("opus", f.spentWeek())
	opus, _ := f.s.GetAgent(f.ctx, "opus")
	if opus.QuotaWeeklyUsed != 52 || opus.QuotaWeeklyScope != QuotaScopeAll || !opus.Availability(f.now).Available() {
		t.Fatalf("opus's report: %+v", opus)
	}
	// A scoped window looser than the account-wide one is not taken.
	q := f.spentWeek()
	q.Weekly.Used, q.Scoped[0].Used = 90, 60
	f.report("fable", q)
	if fable, _ = f.s.GetAgent(f.ctx, "fable"); fable.QuotaWeeklyUsed != 90 || fable.QuotaWeeklyScope != QuotaScopeAll {
		t.Fatalf("looser scope taken: %+v", fable)
	}
	// A codex worker reports nothing and stays available.
	sol, _ := f.s.GetAgent(f.ctx, "sol")
	if !sol.Availability(f.now).Available() || !sol.QuotaReportedAt.IsZero() {
		t.Fatalf("sol: %+v", sol)
	}
}

// Unpinned work at a tier is the lead's while it is in; the tier's backup
// pulls only while the lead is out (disabled or unavailable), and a lead on
// break leaves the pool waiting. Another tier never sees it.
func TestTierPoolLeadAndBackup(t *testing.T) {
	f := newFixture(t)
	f.tiered()
	f.project("EA")
	it := f.item("EA", "hard thing")
	if it.Tier != 11 || it.Assignee != "" {
		t.Fatalf("filed into the top tier's pool: %+v", it)
	}
	if got := f.offered("fable"); got == nil || got.ID != it.ID {
		t.Fatalf("lead not offered: %+v", got)
	}
	if got := f.offered("sol"); got != nil {
		t.Fatalf("backup offered while the lead is in: %+v", got)
	}
	if got := f.offered("opus"); got != nil {
		t.Fatalf("another tier offered: %+v", got)
	}
	f.refuse("EA-1", TransitionInput{Actor: "sol", Action: ActionClaim}, ErrInvalidTransition)
	f.refuse("EA-1", TransitionInput{Actor: "opus", Action: ActionClaim}, ErrInvalidTransition)

	// The lead's week runs out: the backup takes the pool.
	f.report("fable", f.spentWeek())
	if got := f.peek("fable"); got != nil {
		t.Fatalf("unavailable lead handed work: %+v", got)
	}
	if job, err := f.s.CheckIn(f.ctx, "fable", true, nil); err != nil || job != nil {
		t.Fatalf("unavailable lead checked in to work: %v %+v", err, job)
	}
	if got := f.offered("sol"); got == nil || got.ID != it.ID {
		t.Fatalf("backup not offered while the lead is out: %+v", got)
	}
	if got := f.offered("opus"); got != nil {
		t.Fatalf("another tier offered while the lead is out: %+v", got)
	}
	// The week resets on the clock, with no new report: the lead is back
	// and the backup steps aside.
	f.tick(27 * time.Hour)
	if got := f.offered("fable"); got == nil {
		t.Fatal("lead not back after the reset")
	}
	if got := f.offered("sol"); got != nil {
		t.Fatalf("backup offered after the reset: %+v", got)
	}

	// A break — five-hour window spent, reset within the hour — is not an
	// outage: the lead pulls nothing and the pool waits for it.
	f.report("fable", QuotaReport{Session: QuotaWindow{Used: 95, ResetsAt: f.now.Add(40 * time.Minute)}})
	if got := f.peek("fable"); got != nil {
		t.Fatalf("lead on break handed work: %+v", got)
	}
	if got := f.offered("sol"); got != nil {
		t.Fatalf("backup offered during the lead's break: %+v", got)
	}
	// The same spent window with two hours to go is an outage.
	f.report("fable", QuotaReport{Session: QuotaWindow{Used: 95, ResetsAt: f.now.Add(2 * time.Hour)}})
	if got := f.offered("sol"); got == nil {
		t.Fatal("backup not offered during the lead's session outage")
	}
	f.tick(time.Hour + time.Minute)
	if got := f.offered("sol"); got != nil {
		t.Fatalf("backup offered once the outage became a break: %+v", got)
	}
	f.tick(time.Hour)
	if got := f.peek("fable"); got == nil {
		t.Fatal("lead not back after the session reset")
	}

	// Pausing the lead hands the pool to the backup too.
	f.s.SetAgentEnabled(f.ctx, "fable", false)
	if got := f.offered("sol"); got == nil {
		t.Fatal("backup not offered while the lead is paused")
	}
	f.s.SetAgentEnabled(f.ctx, "fable", true)
	if got := f.offered("sol"); got != nil {
		t.Fatalf("backup offered after the lead was unpaused: %+v", got)
	}
	// And the lead moves: sol takes the lead, fable becomes the backup.
	f.s.SetLead(f.ctx, "sol")
	if got := f.offered("sol"); got == nil {
		t.Fatal("new lead not offered")
	}
	if got := f.offered("fable"); got != nil {
		t.Fatalf("old lead still offered: %+v", got)
	}
	// Once claimed the item is its worker's, lead or not.
	f.move("EA-1", TransitionInput{Actor: "sol", Action: ActionClaim})
	f.s.SetLead(f.ctx, "fable")
	f.move("EA-1", TransitionInput{Actor: "sol", Action: ActionRelease})
	if got := f.offered("fable"); got != nil {
		t.Fatalf("released item offered to the lead instead of its worker: %+v", got)
	}
	if got := f.offered("sol"); got == nil || got.ID != it.ID {
		t.Fatalf("released item not offered to its worker: %+v", got)
	}
}

// An explicit tier files into that tier's pool; a tier with no worker is
// refused; a pin takes the worker's tier.
func TestItemTiers(t *testing.T) {
	f := newFixture(t)
	f.tiered()
	f.project("EA")
	chore, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "tidy", Type: TypeChore, Tier: 10})
	if err != nil || chore.Tier != 10 {
		t.Fatalf("tier 10 filing: %v %+v", err, chore)
	}
	f.tick(time.Second)
	if got := f.offered("opus"); got == nil || got.ID != chore.ID {
		t.Fatalf("tier 10 item not offered to opus: %+v", got)
	}
	if got := f.offered("fable"); got != nil {
		t.Fatalf("tier 10 item offered to fable: %+v", got)
	}
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "x", Tier: 12}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty tier: %v", err)
	}
	pinned, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "for opus", Assignee: "opus", Specced: true})
	if err != nil || pinned.Tier != 10 || pinned.Assignee != "opus" {
		t.Fatalf("pin takes the worker's tier: %v %+v", err, pinned)
	}
	f.tick(time.Second)
	// The owner moves an item between pools; the pool it lands in sees
	// it, the one it left does not.
	eleven, twelve := 11, 12
	if _, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Tier: &twelve}, 0); !errors.Is(err, ErrValidation) {
		t.Fatalf("move to an empty tier: %v", err)
	}
	got, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Tier: &eleven}, 0)
	if err != nil || got.Tier != 11 {
		t.Fatalf("move to tier 11: %v %+v", err, got)
	}
	if o := f.offered("opus"); o == nil || o.ID != pinned.ID {
		t.Fatalf("opus should now see only its pinned item: %+v", o)
	}
	if o := f.offered("fable"); o == nil || o.ID != chore.ID {
		t.Fatalf("fable not offered the moved item: %+v", o)
	}
	// The top tier follows the enabled workers.
	f.s.SetAgentEnabled(f.ctx, "fable", false)
	f.s.SetAgentEnabled(f.ctx, "sol", false)
	if top, _ := f.s.TopTier(f.ctx); top != 10 {
		t.Fatalf("top tier with tier 11 paused: %d", top)
	}
	if it := f.item("EA", "while 11 is paused"); it.Tier != 10 {
		t.Fatalf("filed into a paused tier: %+v", it)
	}
}

// An unavailable worker cannot be pinned: at filing, by the owner's
// reassignment, or by the groomer.
func TestPinningAnUnavailableWorkerIsRefused(t *testing.T) {
	f := newFixture(t)
	f.tiered()
	f.project("EA")
	f.report("fable", f.spentWeek())
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "x", Assignee: "fable", Specced: true}); !errors.Is(err, ErrValidation) {
		t.Fatalf("pin an unavailable worker at filing: %v", err)
	}
	it := f.item("EA", "pool item")
	fable := "fable"
	if _, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Assignee: &fable}, 0); !errors.Is(err, ErrValidation) {
		t.Fatalf("pin an unavailable worker by patch: %v", err)
	}
	raw, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "raw", Type: TypeFeature, Description: "vague"})
	if err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	f.move("EA-2", TransitionInput{Actor: "sol", Action: ActionClaim})
	f.refuse("EA-2", TransitionInput{Actor: "sol", Action: ActionGroom, Description: "spec", Assignee: "fable"}, ErrValidation)
	f.refuse("EA-2", TransitionInput{Actor: "sol", Action: ActionGroom, Description: "spec", Tier: 12}, ErrValidation)
	// The groomer sets the tier and leaves the item in its pool.
	groomed := f.move("EA-2", TransitionInput{Actor: "sol", Action: ActionGroom, Description: "spec", Tier: 10})
	if groomed.Tier != 10 || groomed.Assignee != "" || groomed.NeedsGroom {
		t.Fatalf("groomed: %+v", groomed)
	}
	if got := f.offered("opus"); got == nil || got.ID != raw.ID {
		t.Fatalf("groomed item not in tier 10's pool: %+v", got)
	}
	// A break is pinnable.
	f.report("fable", QuotaReport{Session: QuotaWindow{Used: 95, ResetsAt: f.now.Add(30 * time.Minute)}})
	got, err := f.s.UpdateItem(f.ctx, "EA-1", ItemPatch{Assignee: &fable}, 0)
	if err != nil || got.Assignee != "fable" || got.ID != it.ID {
		t.Fatalf("pin a worker on break: %v %+v", err, got)
	}
}

// A review goes to the item's tier first — its other workers in pull order
// — and to another tier only when the whole tier is out.
func TestReviewsPreferTheTier(t *testing.T) {
	f := newFixture(t)
	f.tiered()
	f.project("EA")
	f.item("EA", "by fable")
	f.move("EA-1", TransitionInput{Actor: "fable", Action: ActionClaim})
	f.move("EA-1", TransitionInput{Actor: "fable", Action: ActionSubmit, Branch: "pm/ea-1"})
	if got := f.offered("sol"); got == nil || got.Number != 1 {
		t.Fatalf("tier-mate not offered the review: %+v", got)
	}
	if got := f.offered("opus"); got != nil {
		t.Fatalf("another tier offered the review while the tier is in: %+v", got)
	}
	f.refuse("EA-1", TransitionInput{Actor: "opus", Action: ActionClaim}, ErrInvalidTransition)
	f.refuse("EA-1", TransitionInput{Actor: "fable", Action: ActionClaim}, ErrForbidden)
	// Sol out: opus is the last resort.
	f.s.SetAgentEnabled(f.ctx, "sol", false)
	if got := f.offered("opus"); got == nil || got.Number != 1 {
		t.Fatalf("last-resort reviewer not offered: %+v", got)
	}
	f.s.SetAgentEnabled(f.ctx, "sol", true)
	if got := f.offered("opus"); got != nil {
		t.Fatalf("last-resort reviewer offered while the tier is in: %+v", got)
	}
	// Sol implements: fable reviews, and when fable's quota is gone opus
	// does rather than the review waiting out the week.
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "by sol", Assignee: "sol", Specced: true}); err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	f.move("EA-2", TransitionInput{Actor: "sol", Action: ActionClaim})
	f.move("EA-2", TransitionInput{Actor: "sol", Action: ActionSubmit, Branch: "pm/ea-2"})
	if got := f.offered("fable"); got == nil || got.Number != 2 {
		t.Fatalf("lead not offered the backup's review: %+v", got)
	}
	f.report("fable", f.spentWeek())
	if got := f.offered("opus"); got == nil || got.Number != 2 {
		t.Fatalf("opus not offered the review with fable out: %+v", got)
	}
	f.move("EA-2", TransitionInput{Actor: "opus", Action: ActionClaim})
	if it, _, _ := f.s.GetItem(f.ctx, "EA-2"); it.Reviewer != "opus" {
		t.Fatalf("review claim: %+v", it)
	}
}

// A worker runs up to its slots at once; the list of what it holds is
// derived from the leases.
func TestSlots(t *testing.T) {
	f := newFixture(t)
	f.tiered()
	f.project("EA")
	for _, title := range []string{"a", "b", "c", "d"} {
		f.item("EA", title)
	}
	for i := 1; i <= 3; i++ {
		job, err := f.s.CheckIn(f.ctx, "fable", true, nil)
		if err != nil || job == nil || job.Item.Number != i {
			t.Fatalf("slot %d: %v %+v", i, err, job)
		}
	}
	if job, err := f.s.CheckIn(f.ctx, "fable", true, nil); err != nil || job != nil {
		t.Fatalf("a fourth job past three slots: %v %+v", err, job)
	}
	f.refuse("EA-4", TransitionInput{Actor: "fable", Action: ActionClaim}, ErrInvalidTransition)
	if held := f.holding("fable"); len(held) != 3 {
		t.Fatalf("holding: %v", held)
	}
	// The backup does not take the fourth: the lead is in, just busy.
	if got := f.offered("sol"); got != nil {
		t.Fatalf("backup offered the lead's overflow: %+v", got)
	}
	f.move("EA-1", TransitionInput{Actor: "fable", Action: ActionSubmit, Branch: "pm/ea-1"})
	if job, err := f.s.CheckIn(f.ctx, "fable", true, nil); err != nil || job == nil || job.Item.Number != 4 {
		t.Fatalf("freed slot: %v %+v", err, job)
	}
	if held := f.holding("fable"); len(held) != 3 || held[0] != f.itemID("EA-2") {
		t.Fatalf("holding after the swap, oldest first: %v", held)
	}
	// A deleted worker cannot be holding.
	if err := f.s.DeleteAgent(f.ctx, "fable"); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete a holder: %v", err)
	}
}

func (f *fixture) itemID(key string) string {
	f.t.Helper()
	it, _, err := f.s.GetItem(f.ctx, key)
	if err != nil {
		f.t.Fatal(err)
	}
	return it.ID
}

// Dependencies hold under concurrency: an item waiting on another is not
// offered until that one is done, whatever the free slots, and two
// approved items in one project merge one at a time.
func TestConcurrencyKeepsOrder(t *testing.T) {
	f := newFixture(t)
	f.tiered()
	f.project("EA")
	f.item("EA", "first")
	if _, err := f.s.CreateItem(f.ctx, "EA", ItemInput{Title: "second", After: "EA-1", Specced: true}); err != nil {
		t.Fatal(err)
	}
	f.tick(time.Second)
	f.item("EA", "third")
	job, _ := f.s.CheckIn(f.ctx, "fable", true, nil)
	if job == nil || job.Item.Number != 1 {
		t.Fatalf("first: %+v", job)
	}
	// The second slot skips the gated item for the third.
	if job, _ = f.s.CheckIn(f.ctx, "fable", true, nil); job == nil || job.Item.Number != 3 {
		t.Fatalf("gated item offered before its dependency is done: %+v", job)
	}
	if job, _ = f.s.CheckIn(f.ctx, "fable", true, nil); job != nil {
		t.Fatalf("gated item offered into the third slot: %+v", job)
	}
	// Both land in review and get approved; the merges are serialized.
	for _, key := range []string{"EA-1", "EA-3"} {
		f.move(key, TransitionInput{Actor: "fable", Action: ActionSubmit, Branch: "pm/" + key})
		f.move(key, TransitionInput{Actor: "sol", Action: ActionClaim})
		f.move(key, TransitionInput{Actor: "sol", Action: ActionReview, Verdict: VerdictApprove, Body: "ok"})
		f.move(key, TransitionInput{Actor: ActorOwner, Action: ActionApprove})
	}
	if job, _ = f.s.CheckIn(f.ctx, "fable", true, nil); job == nil || job.Kind != JobMerge || job.Item.Number != 1 {
		t.Fatalf("first merge: %+v", job)
	}
	if job, _ = f.s.CheckIn(f.ctx, "fable", true, nil); job != nil {
		t.Fatalf("second merge offered while the first is under way: %+v", job)
	}
	f.refuse("EA-3", TransitionInput{Actor: "fable", Action: ActionClaim}, ErrInvalidTransition)
	// Still no second item: EA-2 waits on EA-1 being done, not approved.
	if got := f.offered("fable"); got != nil {
		t.Fatalf("offered before the merge landed: %+v", got)
	}
	f.move("EA-1", TransitionInput{Actor: "fable", Action: ActionComplete, Body: "merged"})
	job, _ = f.s.CheckIn(f.ctx, "fable", true, nil)
	if job == nil || job.Kind != JobMerge || job.Item.Number != 3 {
		t.Fatalf("second merge after the first landed: %+v", job)
	}
	f.move("EA-3", TransitionInput{Actor: "fable", Action: ActionComplete, Body: "merged"})
	if job, _ = f.s.CheckIn(f.ctx, "fable", true, nil); job == nil || job.Kind != JobImplement || job.Item.Number != 2 {
		t.Fatalf("dependent item after its dependency is done: %+v", job)
	}
}

// A backup's parked wait wakes when the lead's check-in reports it out.
func TestWaitWakesTheBackupWhenTheLeadGoesOut(t *testing.T) {
	f := newFixture(t)
	f.tiered()
	f.project("EA")
	f.item("EA", "pool item")
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	ch := f.wait(ctx, "sol", 5*time.Second)
	f.report("fable", f.spentWeek())
	if got := f.settle(ch); !got.ready || got.err != nil {
		t.Fatalf("backup not woken: %+v", got)
	}
	// And the lead's own wait is never ready while it is out.
	ch = f.wait(ctx, "fable", 30*time.Millisecond)
	if got := f.settle(ch); got.ready || got.err != nil {
		t.Fatalf("unavailable lead ready: %+v", got)
	}
}
