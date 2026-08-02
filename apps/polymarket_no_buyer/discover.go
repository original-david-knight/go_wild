package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// gammaNoMidpoint returns the NO outcome's current price from a market's listing
// metadata (the index-aligned Outcomes / OutcomePrices arrays) without an
// order-book fetch. It returns (0,false) when the arrays are missing, malformed,
// not a binary YES/NO pair, or the NO price is unparseable or outside (0,1). This
// lets discovery filter the NO-midpoint band cheaply; reconcile still re-checks the
// precise live-book midpoint before placing any order.
func gammaNoMidpoint(m polymarket.Market) (float64, bool) {
	rawOutcomes := strings.TrimSpace(m.Outcomes)
	rawPrices := strings.TrimSpace(m.OutcomePrices)
	if rawOutcomes == "" || rawPrices == "" {
		return 0, false
	}
	var outcomes, prices []string
	if json.Unmarshal([]byte(rawOutcomes), &outcomes) != nil {
		return 0, false
	}
	if json.Unmarshal([]byte(rawPrices), &prices) != nil {
		return 0, false
	}
	if len(outcomes) != 2 || len(prices) != 2 {
		return 0, false
	}
	noIdx := -1
	for i, o := range outcomes {
		if strings.EqualFold(strings.TrimSpace(o), "NO") {
			noIdx = i
		}
	}
	if noIdx < 0 {
		return 0, false
	}
	px, err := strconv.ParseFloat(strings.TrimSpace(prices[noIdx]), 64)
	if err != nil || px <= 0 || px >= 1 {
		return 0, false
	}
	return px, true
}

// discoverPageLimit is the per-page market fetch size. discoverMaxPages caps the
// total number of pages scanned so a misbehaving upstream cannot loop forever.
const (
	discoverPageLimit = 100
	discoverMaxPages  = 50
)

// eligibleMarket is a discovered YES-buying candidate that passed every
// eligibility criterion. It carries the source market plus the derived values
// later rungs reuse — the decoded YES/NO token IDs, the parsed close time, and
// the computed NO signal midpoint. The signal intentionally remains NO-based so
// this reversal targets exactly the markets the old strategy would have bought.
type eligibleMarket struct {
	Market   polymarket.Market
	Tokens   binaryTokens
	CloseAt  time.Time
	Midpoint noMidpoint
}

// discoverEligibleMarkets fetches candidate markets, fetches positions once to
// build the owned-token set, then runs the pure eligibility predicate over every
// candidate. It returns the surviving eligible markets sorted by close time
// ascending (stable for ties). It mutates no account state: there is no
// order placement or cancellation in this pass.
//
// A market that cannot be evaluated (non-binary, unparseable close time, no
// two-sided book, etc.) is logged with the deciding criterion and skipped; it
// never aborts the scan. A failure to list markets or fetch positions is fatal
// for the pass and returned, since discovery cannot proceed without them.
func (a *App) discoverEligibleMarkets(ctx context.Context, logger *Logger) ([]eligibleMarket, error) {
	markets, err := a.listAllMarkets(ctx)
	if err != nil {
		logger.Event("discover_error", map[string]any{
			"stage": "list_markets",
			"error": err.Error(),
		})
		return nil, err
	}

	positions, err := a.trading.GetPositions(ctx)
	if err != nil {
		logger.Event("discover_error", map[string]any{
			"stage": "get_positions",
			"error": err.Error(),
		})
		return nil, err
	}
	owned := ownedTokenIDs(positions)

	now := a.now()
	eligible := make([]eligibleMarket, 0)

	// Tally rejections by reason so the summary shows WHY markets were filtered out
	// (e.g. closing outside the window vs. NO midpoint outside the band).
	rejections := map[string]int{}
	reject := func(m polymarket.Market, reason skipReason) {
		rejections[reason.String()]++
		logger.Event("market_eligibility", map[string]any{
			"condition_id": m.ConditionID,
			"question":     m.Question,
			"status":       "rejected",
			"reason":       reason.String(),
		})
	}

	bookFetches := 0
	for _, m := range markets {
		tokens, tokensReason := decodeBinaryTokens(m)

		// Cheap filters first (state, binary, close window, liquidity, NO owned) —
		// no network call.
		if reason := marketPreCheck(m, now, a.cfg, owned, tokens, tokensReason); reason != "" {
			reject(m, reason)
			continue
		}

		// NO midpoint: prefer the listing's outcome price (free, already fetched) and
		// only fall back to a live order-book fetch when the listing has no usable
		// price. Discovery is a candidate filter — reconcile re-fetches the live book
		// and re-checks the precise band before placing any order, so a slightly stale
		// listing price here never causes an out-of-band order.
		var mid noMidpoint
		var haveMidpoint bool
		if px, ok := gammaNoMidpoint(m); ok {
			mid = noMidpoint{Midpoint: px}
			haveMidpoint = true
		} else {
			bookFetches++
			var midReason skipReason
			mid, midReason = a.fetchNoMidpoint(ctx, logger, m.ConditionID, tokens)
			haveMidpoint = midReason == ""
		}
		if reason := midpointInBand(mid, haveMidpoint, a.cfg); reason != "" {
			reject(m, reason)
			continue
		}

		closeAt, _ := parseEndDate(m.EndDate)
		eligible = append(eligible, eligibleMarket{
			Market:   m,
			Tokens:   tokens,
			CloseAt:  closeAt,
			Midpoint: mid,
		})
		logger.Event("market_eligibility", map[string]any{
			"condition_id": m.ConditionID,
			"question":     m.Question,
			"status":       "eligible",
			"close_at":     closeAt.Format(time.RFC3339),
			"no_midpoint":  mid.Midpoint,
		})
	}

	// Sort eligible markets by close time ascending; ties keep their scan order
	// (sort.SliceStable).
	sort.SliceStable(eligible, func(i, j int) bool {
		return eligible[i].CloseAt.Before(eligible[j].CloseAt)
	})

	logger.Event("discover_summary", map[string]any{
		"markets_scanned":  len(markets),
		"book_fetches":     bookFetches,
		"markets_eligible": len(eligible),
		"rejections":       rejections,
	})
	return eligible, nil
}

// listAllMarkets fetches exactly the markets whose close time falls within the
// configured window and that meet the minimum liquidity, filtered server-side and
// ordered by close date ascending. This scans only the in-window markets (a few
// hundred) instead of paging the entire active set (tens of thousands), so a run
// is both complete (no newest-N truncation) and fast. It pages until a short page
// or missing next cursor signals the end, with a page cap as a runaway backstop.
func (a *App) listAllMarkets(ctx context.Context) ([]polymarket.Market, error) {
	now := a.now()
	minClose := now.Add(a.cfg.MinHoursToClose)
	maxClose := now.Add(a.cfg.MaxHoursToClose)

	var all []polymarket.Market
	afterCursor := ""
	seenCursors := map[string]struct{}{"": {}}
	for page := 0; page < discoverMaxPages; page++ {
		marketPage, err := a.trading.ListMarketsClosingBetweenKeyset(ctx, minClose, maxClose, a.cfg.MinLiquidityUSD, discoverPageLimit, afterCursor)
		if err != nil {
			return nil, err
		}
		batch := marketPage.Markets
		nextCursor := strings.TrimSpace(marketPage.NextCursor)
		if nextCursor != "" && nextCursor == afterCursor && len(batch) >= discoverPageLimit {
			return nil, fmt.Errorf("market keyset pagination did not advance from cursor %q", afterCursor)
		}
		all = append(all, batch...)
		if len(batch) < discoverPageLimit || nextCursor == "" || nextCursor == "LTE=" {
			break
		}
		if _, ok := seenCursors[nextCursor]; ok {
			return nil, fmt.Errorf("market keyset pagination repeated cursor %q", nextCursor)
		}
		seenCursors[nextCursor] = struct{}{}
		afterCursor = nextCursor
	}
	return all, nil
}

// ownedTokenIDs builds the set of CLOB token IDs the account currently holds with
// a positive size. The predicate checks a market's NO token ID against this set
// to reject markets where the opposing outcome is already owned. A zero/negative size is
// treated as not owned.
func ownedTokenIDs(positions []polymarket.Position) map[string]bool {
	owned := make(map[string]bool, len(positions))
	for _, p := range positions {
		if p.Asset == "" || p.Size <= 0 {
			continue
		}
		owned[p.Asset] = true
	}
	return owned
}
