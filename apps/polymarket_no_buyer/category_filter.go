package main

import (
	"strings"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// Topic (category) exclusion. This strategy does not trade whole categories of
// markets regardless of their economics — currently crypto-price markets,
// stock-price markets, and sports exact-score (scoreline) props. Classification
// is purely cheap: it uses only the listing metadata (tags plus the question/
// description/slug text) that discovery already has in hand, so it never costs a
// network call.
//
// Note on tags: the Gamma /markets listing endpoint that discovery pages does
// NOT return per-market tags (they live on the parent event), so in practice the
// keyword/text matching is what fires here. The tag checks are kept as cheap
// belt-and-braces for any code path whose markets do carry tags.
//
// The keyword and tag sets are kept deliberately in sync with the find-markets
// classifier in apps/agent_manager (builtinPolymarketMarketLooksCrypto /
// builtinPolymarketMarketLooksStocks) so the two services agree on what counts
// as a crypto/stock market. That classifier is package-private in a separate
// module and cannot be imported; if these ever need to diverge or a single
// source of truth is wanted, lift this into the polymarket library and have both
// callers use it. More categories are expected to be added here later.

// marketLooksCrypto reports whether a market is about cryptocurrency (price or
// otherwise). It matches Polymarket's own crypto tags first, then falls back to
// word-boundary keyword matching so the filter still fires when tags are absent
// from the listing payload.
func marketLooksCrypto(m polymarket.Market) bool {
	if marketTagMatches(m, "crypto", "crypto-prices", "bitcoin", "ethereum", "solana") {
		return true
	}
	return marketTextMatches(m,
		" crypto ",
		" cryptocurrency ",
		" bitcoin ",
		" btc ",
		" ethereum ",
		" eth ",
		" solana ",
		" xrp ",
		" dogecoin ",
		" doge ",
		" cardano ",
		" ada ",
		" memecoin ",
		" token price ",
		" coin price ",
	)
}

// marketLooksStocks reports whether a market is about stock/equity prices. Like
// marketLooksCrypto it matches Polymarket's stock tags first, then falls back to
// word-boundary keyword matching.
func marketLooksStocks(m polymarket.Market) bool {
	if marketTagMatches(m, "stock-prices", "stocks", "equities", "stock-market") {
		return true
	}
	return marketTextMatches(m,
		" stock price ",
		" stock prices ",
		" share price ",
		" share prices ",
		" equities ",
		" equity ",
		" stock market ",
		" close at ",
	)
}

// marketLooksExactScore reports whether a market is a sports "Exact Score"
// (exact-scoreline) prop — e.g. "Will Chelsea win 1-0?", "Will Portugal win
// 2-1?", "Will Hurricanes lead the Stanley Cup 3-0 through 3 games?". The
// individual binary market's question is just the scoreline, but Polymarket's
// description follows a fixed template ("...a market to predict the exact score
// of...", or "...the exact series score of..." for best-of-N series), so the
// marker lives reliably in the description rather than the question or slug.
// Matching the phrase alone is sufficient — "exact score" is intrinsically a
// sports concept — so it is not ANDed with a sports check (which would risk
// missing scoreline markets whose text omits the sport's name).
func marketLooksExactScore(m polymarket.Market) bool {
	if marketTagMatches(m, "exact-score") {
		return true
	}
	return marketTextMatches(m,
		" exact score ",
		" exact series score ",
	)
}

// marketTagMatches reports whether any of the market's Gamma tags matches one of
// the given slugs, comparing case-insensitively against both the tag slug and the
// human-readable label.
func marketTagMatches(m polymarket.Market, slugs ...string) bool {
	if len(m.Tags) == 0 || len(slugs) == 0 {
		return false
	}
	allowed := make(map[string]struct{}, len(slugs))
	for _, slug := range slugs {
		slug = strings.TrimSpace(strings.ToLower(slug))
		if slug != "" {
			allowed[slug] = struct{}{}
		}
	}
	for _, tag := range m.Tags {
		slug := strings.TrimSpace(strings.ToLower(tag.Slug))
		label := strings.TrimSpace(strings.ToLower(tag.Label))
		if _, ok := allowed[slug]; ok {
			return true
		}
		if _, ok := allowed[label]; ok {
			return true
		}
	}
	return false
}

// marketTextMatches reports whether the market's normalized question/description/
// slug text contains any of the given keywords. Both the haystack and each
// keyword are run through normalizeSearchText, so a keyword padded with spaces
// (e.g. " btc ") matches only on word boundaries — " btc " will not match inside
// "btcusd" and " eth " will not match inside "whether".
func marketTextMatches(m polymarket.Market, keywords ...string) bool {
	normalized := normalizeSearchText(strings.Join([]string{
		strings.TrimSpace(m.Question),
		strings.TrimSpace(m.Description),
		strings.TrimSpace(m.Slug),
	}, " "))
	if normalized == "" {
		return false
	}
	for _, keyword := range keywords {
		if strings.Contains(normalized, normalizeSearchText(keyword)) {
			return true
		}
	}
	return false
}

// normalizeSearchText lowercases the input and replaces every run of
// non-alphanumeric characters with a single space, padding both ends with a
// space. The padding lets callers match on word boundaries by wrapping a keyword
// in spaces.
func normalizeSearchText(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw) + 2)
	b.WriteByte(' ')
	lastSpace := true
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	if !lastSpace {
		b.WriteByte(' ')
	}
	return b.String()
}
