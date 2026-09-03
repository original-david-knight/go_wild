package gowild_projects

import (
	"context"
	"sort"
	"time"

	gowild_data "github.com/original-david-knight/go_wild/data"
	gowild_dbx "github.com/original-david-knight/go_wild/data/dbx"
)

// Tiers and pulls. Workers sit in tiers, higher is stronger, and unpinned
// work is pulled by the workers of its own tier: the lead while it is in,
// the others only while the lead is out. A review goes to the item's tier
// first — its other workers in the same order — and to another tier only
// when the whole tier is out, strongest tier first. A worker runs up to
// Slots jobs at once.

// DefaultTier is the baseline tier: a worker created without one lands
// here, weaker models go below and stronger ones above.
const DefaultTier = 10

// DefaultSlots is how many jobs a worker runs at once when its row says
// nothing.
const DefaultSlots = 3

// TierOrDefault is the worker's tier, the baseline for a row that has none.
func (a *Agent) TierOrDefault() int {
	if a.Tier <= 0 {
		return DefaultTier
	}
	return a.Tier
}

// SlotsOrDefault is the worker's slot count, DefaultSlots for a row that
// has none.
func (a *Agent) SlotsOrDefault() int {
	if a.Slots <= 0 {
		return DefaultSlots
	}
	return a.Slots
}

// sortByPreference orders workers for a pull: lead first, then by name.
func sortByPreference(rows []*Agent) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Lead != rows[j].Lead {
			return rows[i].Lead
		}
		return rows[i].ID < rows[j].ID
	})
}

// tierWorkers is the tier's workers in pull order.
func tierWorkers(rows []*Agent, tier int) []*Agent {
	var out []*Agent
	for _, a := range rows {
		if a.TierOrDefault() == tier {
			out = append(out, a)
		}
	}
	sortByPreference(out)
	return out
}

// tiersOf lists the tiers that have a worker, strongest first.
func tiersOf(rows []*Agent) []int {
	seen := map[int]bool{}
	var out []int
	for _, a := range rows {
		if t := a.TierOrDefault(); !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out
}

// topTier is the strongest tier with an enabled worker, else the strongest
// tier at all, else zero when there are no workers. It is what an item
// filed without a tier gets.
func topTier(rows []*Agent) int {
	top, any := 0, 0
	for _, a := range rows {
		t := a.TierOrDefault()
		if t > any {
			any = t
		}
		if a.Enabled && t > top {
			top = t
		}
	}
	if top == 0 {
		return any
	}
	return top
}

func tierExists(rows []*Agent, tier int) bool {
	for _, a := range rows {
		if a.TierOrDefault() == tier {
			return true
		}
	}
	return false
}

func agentIn(rows []*Agent, name string) *Agent {
	for _, a := range rows {
		if a.ID == name {
			return a
		}
	}
	return nil
}

// firstIn walks workers in order and reports whether agent is the first
// one that is in (not out). A worker not in the list is never first.
func firstIn(order []*Agent, agent string, now time.Time) bool {
	for _, a := range order {
		if a.ID == agent {
			return !a.Out(now)
		}
		if !a.Out(now) {
			return false
		}
	}
	return false
}

// mayPull reports whether agent may take unpinned work at tier: it is in
// that tier and every tier-mate ahead of it in pull order is out.
func mayPull(rows []*Agent, agent string, tier int, now time.Time) bool {
	return firstIn(tierWorkers(rows, tier), agent, now)
}

// mayReview reports whether agent may review an item at tier that
// implementer built: the item's tier goes first, in pull order, then the
// other tiers strongest first, and the implementer never reviews itself.
func mayReview(rows []*Agent, agent, implementer string, tier int, now time.Time) bool {
	if agent == implementer {
		return false
	}
	var order []*Agent
	add := func(t int) {
		for _, a := range tierWorkers(rows, t) {
			if a.ID != implementer {
				order = append(order, a)
			}
		}
	}
	add(tier)
	for _, t := range tiersOf(rows) {
		if t != tier {
			add(t)
		}
	}
	return firstIn(order, agent, now)
}

// itemTier is the tier an item is pulled and reviewed at: its own, else —
// for a row from before tiers — its worker's, else the baseline.
func itemTier(it *Item, rows []*Agent) int {
	if it.Tier > 0 {
		return it.Tier
	}
	for _, name := range []string{it.Assignee, it.Implementer} {
		if a := agentIn(rows, name); a != nil {
			return a.TierOrDefault()
		}
	}
	return DefaultTier
}

// TopTier is the tier an item filed without one gets: the strongest tier
// with an enabled worker. Zero when there are no workers.
func (s *Service) TopTier(ctx context.Context) (int, error) {
	db, err := s.database()
	if err != nil {
		return 0, err
	}
	rows, err := gowild_dbx.All[Agent](ctx, db, gowild_data.QueryOpts{})
	if err != nil {
		return 0, err
	}
	return topTier(rows), nil
}

// Holding lists the items the worker has a live lease on, oldest claim
// first: what it is running now.
func (s *Service) Holding(ctx context.Context, agent string) ([]*Item, error) {
	db, err := s.database()
	if err != nil {
		return nil, err
	}
	return s.liveLeasesForAgent(ctx, db, agent, s.Now())
}

// ensureLeads gives every non-empty tier without a lead one: its first
// worker by name. It runs after a create, a delete or a tier move.
func ensureLeads(ctx context.Context, db gowild_data.Database, rows []*Agent) error {
	for _, t := range tiersOf(rows) {
		workers := tierWorkers(rows, t)
		if workers[0].Lead {
			continue
		}
		workers[0].Lead = true
		if err := db.Table(Agent{}).Update(ctx, workers[0]); err != nil {
			return err
		}
	}
	return nil
}
