package main

import (
	"testing"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// eligTestTokens are reused YES/NO token IDs for eligibility tests. They are
// distinct so the owned-token check can target the YES side specifically.
const (
	eligYesID = "71902"
	eligNoID  = "52114"
)

// runNow is the fixed run time for all boundary tests. Using a concrete UTC
// instant keeps the close-time arithmetic exact and reproducible.
var runNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

// eligConfig returns a Config carrying the documented eligibility thresholds.
func eligConfig() *Config {
	c := defaultConfig()
	return &c
}

// eligMarket builds a fully-eligible binary YES/NO market: active, accepting
// orders, unresolved, closing comfortably inside the window, and liquid. Tests
// mutate the returned market to violate exactly one criterion at a time.
func eligMarket() polymarket.Market {
	// Close 7 days out: strictly inside (48h, 336h).
	closeAt := runNow.Add(7 * 24 * time.Hour)
	return polymarket.Market{
		ConditionID:     "0xelig",
		Question:        "Will it happen?",
		Active:          true,
		AcceptingOrders: true,
		Closed:          false,
		EndDate:         closeAt.Format(time.RFC3339),
		Liquidity:       "10000",
		Outcomes:        `["Yes","No"]`,
		ClobTokenIDs:    `["` + eligYesID + `","` + eligNoID + `"]`,
	}
}

// goodMidpoint is a usable NO midpoint inside the (0.89, 0.99] band.
func goodMidpoint() noMidpoint {
	return noMidpoint{BestNoBid: 0.93, BestNoAsk: 0.95, Midpoint: 0.94}
}

// evalMarket decodes tokens and runs the pure predicate with the given midpoint
// state, mirroring how discoverEligibleMarkets wires the inputs.
func evalMarket(m polymarket.Market, cfg *Config, owned map[string]bool, mid noMidpoint, haveMidpoint bool) skipReason {
	tokens, tokensReason := decodeBinaryTokens(m)
	return isMarketEligible(m, runNow, cfg, owned, tokens, tokensReason, mid, haveMidpoint)
}

func TestEligibility_TableDriven(t *testing.T) {
	cfg := eligConfig()
	noOwned := map[string]bool{}

	cases := []struct {
		name         string
		mutate       func(m *polymarket.Market)
		owned        map[string]bool
		mid          noMidpoint
		haveMidpoint bool
		want         skipReason
	}{
		{
			name:         "all pass",
			mutate:       func(m *polymarket.Market) {},
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         "",
		},
		{
			name:         "inactive",
			mutate:       func(m *polymarket.Market) { m.Active = false },
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipNotActive,
		},
		{
			name:         "not accepting orders",
			mutate:       func(m *polymarket.Market) { m.AcceptingOrders = false },
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipNotAcceptingOrders,
		},
		{
			name:         "resolved/closed",
			mutate:       func(m *polymarket.Market) { m.Closed = true },
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipMarketClosed,
		},
		{
			name:         "crypto market by question rejected",
			mutate:       func(m *polymarket.Market) { m.Question = "Will Bitcoin close above $100,000?" },
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipCryptoMarket,
		},
		{
			name:         "crypto market by tag rejected",
			mutate:       func(m *polymarket.Market) { m.Tags = []polymarket.Tag{{Slug: "crypto", Label: "Crypto"}} },
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipCryptoMarket,
		},
		{
			name:         "stock market by question rejected",
			mutate:       func(m *polymarket.Market) { m.Question = "Will Tesla stock price hit $500?" },
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipStockMarket,
		},
		{
			name: "exact score market rejected (marker in description)",
			mutate: func(m *polymarket.Market) {
				m.Question = "Will Portugal win 2-1?"
				m.Description = "This is a market to predict the exact score of the UEFA Nations League Final."
			},
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipExactScoreMarket,
		},
		{
			name:         "non-binary outcomes",
			mutate:       func(m *polymarket.Market) { m.Outcomes = `["Up","Down"]` },
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipNotYesNoLabels,
		},
		{
			name:         "duplicated token ids",
			mutate:       func(m *polymarket.Market) { m.ClobTokenIDs = `["111","111"]` },
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipDuplicateTokenIDs,
		},
		{
			name:         "missing token ids",
			mutate:       func(m *polymarket.Market) { m.ClobTokenIDs = `` },
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipTokenIDsMissing,
		},
		{
			name:         "end date missing",
			mutate:       func(m *polymarket.Market) { m.EndDate = `` },
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipEndDateMissing,
		},
		{
			name:         "end date unparseable",
			mutate:       func(m *polymarket.Market) { m.EndDate = `not-a-date` },
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipEndDateUnparsed,
		},
		{
			name:         "close in past",
			mutate:       func(m *polymarket.Market) { m.EndDate = runNow.Add(-time.Hour).Format(time.RFC3339) },
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipCloseInPast,
		},
		{
			name:         "close exactly at +48h boundary rejects",
			mutate:       func(m *polymarket.Market) { m.EndDate = runNow.Add(cfg.MinHoursToClose).Format(time.RFC3339) },
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipCloseTooSoon,
		},
		{
			name: "close just before +48h rejects (too soon)",
			mutate: func(m *polymarket.Market) {
				m.EndDate = runNow.Add(cfg.MinHoursToClose - time.Hour).Format(time.RFC3339)
			},
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipCloseTooSoon,
		},
		{
			name:         "close exactly at +14d boundary rejects",
			mutate:       func(m *polymarket.Market) { m.EndDate = runNow.Add(cfg.MaxHoursToClose).Format(time.RFC3339) },
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipCloseTooFar,
		},
		{
			name: "close beyond +14d rejects (too far)",
			mutate: func(m *polymarket.Market) {
				m.EndDate = runNow.Add(cfg.MaxHoursToClose + time.Hour).Format(time.RFC3339)
			},
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipCloseTooFar,
		},
		{
			name:         "liquidity below min",
			mutate:       func(m *polymarket.Market) { m.Liquidity = "4999.99" },
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipLiquidityTooLow,
		},
		{
			name:         "liquidity unparseable",
			mutate:       func(m *polymarket.Market) { m.Liquidity = "lots" },
			owned:        noOwned,
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipLiquidityUnparsed,
		},
		{
			name:         "no two-sided book",
			mutate:       func(m *polymarket.Market) {},
			owned:        noOwned,
			mid:          noMidpoint{},
			haveMidpoint: false,
			want:         skipNoTwoSidedBook,
		},
		{
			name:         "midpoint == 0.89 rejects (strictly greater required)",
			mutate:       func(m *polymarket.Market) {},
			owned:        noOwned,
			mid:          noMidpoint{Midpoint: 0.89},
			haveMidpoint: true,
			want:         skipMidpointTooLow,
		},
		{
			name:         "midpoint > 0.99 rejects",
			mutate:       func(m *polymarket.Market) {},
			owned:        noOwned,
			mid:          noMidpoint{Midpoint: 0.995},
			haveMidpoint: true,
			want:         skipMidpointTooHigh,
		},
		{
			name:         "midpoint == 0.99 accepted (inclusive upper)",
			mutate:       func(m *polymarket.Market) {},
			owned:        noOwned,
			mid:          noMidpoint{Midpoint: 0.99},
			haveMidpoint: true,
			want:         "",
		},
		{
			name:         "no shares owned",
			mutate:       func(m *polymarket.Market) {},
			owned:        map[string]bool{eligNoID: true},
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         skipNoSharesOwned,
		},
		{
			name:         "yes shares owned does not reject",
			mutate:       func(m *polymarket.Market) {},
			owned:        map[string]bool{eligYesID: true},
			mid:          goodMidpoint(),
			haveMidpoint: true,
			want:         "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := eligMarket()
			tc.mutate(&m)
			got := evalMarket(m, cfg, tc.owned, tc.mid, tc.haveMidpoint)
			if got != tc.want {
				t.Fatalf("reason = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEligibility_ExactBoundaries(t *testing.T) {
	cfg := eligConfig()
	noOwned := map[string]bool{}

	t.Run("close == +48h rejects", func(t *testing.T) {
		m := eligMarket()
		m.EndDate = runNow.Add(cfg.MinHoursToClose).Format(time.RFC3339)
		if got := evalMarket(m, cfg, noOwned, goodMidpoint(), true); got != skipCloseTooSoon {
			t.Fatalf("reason = %q, want %q", got, skipCloseTooSoon)
		}
	})

	t.Run("close == +14d rejects", func(t *testing.T) {
		m := eligMarket()
		m.EndDate = runNow.Add(cfg.MaxHoursToClose).Format(time.RFC3339)
		if got := evalMarket(m, cfg, noOwned, goodMidpoint(), true); got != skipCloseTooFar {
			t.Fatalf("reason = %q, want %q", got, skipCloseTooFar)
		}
	})

	t.Run("liquidity == min accepts", func(t *testing.T) {
		m := eligMarket()
		m.Liquidity = "5000"
		if got := evalMarket(m, cfg, noOwned, goodMidpoint(), true); got != "" {
			t.Fatalf("reason = %q, want eligible", got)
		}
	})

	t.Run("midpoint == 0.99 accepts", func(t *testing.T) {
		m := eligMarket()
		if got := evalMarket(m, cfg, noOwned, noMidpoint{Midpoint: 0.99}, true); got != "" {
			t.Fatalf("reason = %q, want eligible", got)
		}
	})

	t.Run("midpoint == 0.89 rejects", func(t *testing.T) {
		m := eligMarket()
		if got := evalMarket(m, cfg, noOwned, noMidpoint{Midpoint: 0.89}, true); got != skipMidpointTooLow {
			t.Fatalf("reason = %q, want %q", got, skipMidpointTooLow)
		}
	})
}

func TestEligibility_MinLiquidityConfigurable(t *testing.T) {
	noOwned := map[string]bool{}

	// A market with liquidity 3000 is rejected at the default 5000 minimum.
	m := eligMarket()
	m.Liquidity = "3000"

	cfg := eligConfig()
	if got := evalMarket(m, cfg, noOwned, goodMidpoint(), true); got != skipLiquidityTooLow {
		t.Fatalf("at default min: reason = %q, want %q", got, skipLiquidityTooLow)
	}

	// Lowering the configured minimum admits the previously-rejected market.
	lower := eligConfig()
	lower.MinLiquidityUSD = 1000
	if got := evalMarket(m, lower, noOwned, goodMidpoint(), true); got != "" {
		t.Fatalf("after lowering min: reason = %q, want eligible", got)
	}

	// Raising the minimum above a previously-eligible market's liquidity rejects
	// it.
	eligibleM := eligMarket() // liquidity 10000
	raise := eligConfig()
	raise.MinLiquidityUSD = 20000
	if got := evalMarket(eligibleM, raise, noOwned, goodMidpoint(), true); got != skipLiquidityTooLow {
		t.Fatalf("after raising min: reason = %q, want %q", got, skipLiquidityTooLow)
	}
}

func TestEligibility_EndDateLayouts(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"rfc3339", "2026-06-08T12:00:00Z", true},
		{"rfc3339 nano", "2026-06-08T12:00:00.123456789Z", true},
		{"date only", "2026-06-08", true},
		{"empty", "", false},
		{"garbage", "soon", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseEndDate(tc.raw)
			if ok != tc.want {
				t.Fatalf("ok = %v, want %v", ok, tc.want)
			}
			if ok && got.Location() != time.UTC {
				t.Errorf("parsed time not UTC: %v", got)
			}
		})
	}

	// A bare date parses to midnight UTC.
	got, ok := parseEndDate("2026-06-08")
	if !ok {
		t.Fatalf("date-only parse failed")
	}
	want := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("date-only parsed to %v, want %v", got, want)
	}
}
