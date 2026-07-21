package main

import (
	"testing"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func TestMarketLooksCrypto(t *testing.T) {
	cases := []struct {
		name string
		m    polymarket.Market
		want bool
	}{
		{"bitcoin question", polymarket.Market{Question: "Will Bitcoin reach $150k by year end?"}, true},
		{"btc ticker", polymarket.Market{Question: "BTC up or down today?"}, true},
		{"ethereum question", polymarket.Market{Question: "Ethereum above $4,000 on Friday?"}, true},
		{"crypto tag, neutral text", polymarket.Market{Question: "Will it go up?", Tags: []polymarket.Tag{{Slug: "crypto", Label: "Crypto"}}}, true},
		{"bitcoin tag by label", polymarket.Market{Question: "Will it go up?", Tags: []polymarket.Tag{{Slug: "", Label: "Bitcoin"}}}, true},
		{"keyword only in slug", polymarket.Market{Slug: "solana-price-prediction"}, true},
		{"memecoin", polymarket.Market{Question: "Will this memecoin 10x?"}, true},
		// Negatives: must not false-positive on substrings or unrelated topics.
		{"eth inside whether", polymarket.Market{Question: "Whether the election is contested?"}, false},
		{"btc inside btcs-like word", polymarket.Market{Question: "Will the abtc merger close?"}, false},
		{"unrelated politics", polymarket.Market{Question: "Will the incumbent win the election?"}, false},
		{"empty", polymarket.Market{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := marketLooksCrypto(tc.m); got != tc.want {
				t.Fatalf("marketLooksCrypto = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMarketLooksStocks(t *testing.T) {
	cases := []struct {
		name string
		m    polymarket.Market
		want bool
	}{
		{"stock price keyword", polymarket.Market{Question: "Will Tesla stock price exceed $500?"}, true},
		{"share price keyword", polymarket.Market{Question: "Nvidia share price above $200?"}, true},
		{"stocks tag, neutral text", polymarket.Market{Question: "Will it rise?", Tags: []polymarket.Tag{{Slug: "stocks", Label: "Stocks"}}}, true},
		{"equities keyword", polymarket.Market{Description: "A market on equities performance."}, true},
		{"close at phrasing", polymarket.Market{Question: "Will AAPL close at or above $250?"}, true},
		// Negatives.
		{"unrelated weather", polymarket.Market{Question: "Will it rain in NYC tomorrow?"}, false},
		{"empty", polymarket.Market{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := marketLooksStocks(tc.m); got != tc.want {
				t.Fatalf("marketLooksStocks = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMarketLooksExactScore(t *testing.T) {
	// Descriptions are the real Polymarket templates: the binary market's question
	// is just the scoreline, and "exact score" lives in the description.
	cases := []struct {
		name string
		m    polymarket.Market
		want bool
	}{
		{
			name: "soccer exact score (marker in description)",
			m: polymarket.Market{
				Question:    "Will Chelsea win 1-0?",
				Slug:        "will-chelsea-win-1-0-729",
				Description: "This is a market to predict the exact score of the FIFA Club World Cup, Final match between Chelsea and PSG.",
			},
			want: true,
		},
		{
			name: "nhl exact series score",
			m: polymarket.Market{
				Question:    "Will Hurricanes lead the 2026 Stanley Cup 3-0 through 3 games?",
				Description: "This market will resolve according to the exact series score of the 2026 Stanley Cup Finals.",
			},
			want: true,
		},
		{"exact-score tag", polymarket.Market{Question: "Will Portugal win 2-1?", Tags: []polymarket.Tag{{Slug: "exact-score", Label: "Exact Score"}}}, true},
		{"marker in slug", polymarket.Market{Slug: "uefa-nations-league-final-exact-score-338"}, true},
		// Negatives: a scoreline alone, or an unrelated market, must not match.
		{"spread market, no exact-score marker", polymarket.Market{Question: "Will the Lakers win by 5 or more?", Description: "Resolves YES if the Lakers win by 5+ points."}, false},
		{"unrelated politics", polymarket.Market{Question: "Will Israel launch a major ground offensive by December 31?", Description: "This market resolves YES if a major ground offensive begins."}, false},
		{"empty", polymarket.Market{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := marketLooksExactScore(tc.m); got != tc.want {
				t.Fatalf("marketLooksExactScore = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNormalizeSearchTextWordBoundary(t *testing.T) {
	// " eth " must not match inside "whether"; it must match a standalone "eth".
	if got := normalizeSearchText("whether"); got != " whether " {
		t.Fatalf("normalize(whether) = %q", got)
	}
	if got := normalizeSearchText("Buy ETH now!"); got != " buy eth now " {
		t.Fatalf("normalize(Buy ETH now!) = %q", got)
	}
}
