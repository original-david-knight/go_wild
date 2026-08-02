package main

import (
	"encoding/json"
	"strings"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// skipReason is a typed, structured non-eligibility reason. Passes log it as a
// stable string so a market that cannot be safely traded is observably skipped
// rather than panicking or producing a midpoint from incomplete data. New skip
// reasons are added as constants here so the set is closed and auditable.
type skipReason string

const (
	// Token/outcome decode skips.
	skipOutcomesMissing   skipReason = "outcomes_missing"
	skipTokenIDsMissing   skipReason = "token_ids_missing"
	skipOutcomesUnparsed  skipReason = "outcomes_unparseable"
	skipTokenIDsUnparsed  skipReason = "token_ids_unparseable"
	skipNotTwoOutcomes    skipReason = "outcomes_not_exactly_two"
	skipNotTwoTokenIDs    skipReason = "token_ids_not_exactly_two"
	skipDuplicateTokenIDs skipReason = "token_ids_duplicated"
	skipDuplicateOutcomes skipReason = "outcomes_duplicated"
	skipNotYesNoLabels    skipReason = "outcomes_not_yes_no"

	// NO order-book / midpoint skip.
	skipNoTwoSidedBook skipReason = "no usable two-sided NO order book"

	// YES order-book / midpoint skip. The NO book remains the strategy signal,
	// while the reversed strategy prices and submits orders on the YES book.
	skipYesTwoSidedBook skipReason = "no usable two-sided YES order book"

	// Minimum order size resolution skip: the venue minimum could not be
	// determined from the order book or clob-market metadata, and no test-only
	// fallback was configured. The market is excluded from live ordering.
	skipMinOrderSizeUndeterminable skipReason = "min_order_size_undeterminable"
)

// cancelReason is a typed, structured reason an open order is canceled (or, in
// dry-run, "would be canceled") by the stale-order pass. Like skipReason it is a
// closed, auditable enum logged as a stable string. Market-level ineligibility is
// propagated via the underlying skipReason; these reasons cover the order-level
// criteria the eligibility predicate does not express.
type cancelReason string

const (
	// Market-level ineligibility: the order's market failed isMarketEligible for a
	// market-level reason (closed/resolved/inactive/not-accepting/outside the
	// close window/below liquidity/non-binary). The deciding skipReason is logged
	// alongside this so the specific market fault is preserved.
	cancelMarketIneligible cancelReason = "market_ineligible"

	// The account owns NO shares for the order's market, so a YES order on it is
	// no longer part of the strategy.
	cancelNoSharesOwned cancelReason = "no_shares_owned"

	// The order is not a YES buy: wrong side (Side != "BUY").
	cancelWrongSide cancelReason = "wrong_side"

	// The order is on the wrong asset: AssetID is not the market's YES token ID.
	cancelWrongAsset cancelReason = "wrong_asset"

	// The YES buy order's normalized limit price differs from the normalized latest
	// YES midpoint price.
	cancelPriceMismatch cancelReason = "price_mismatch"

	// The YES buy order's expiration differs from close_time - OrderExpiryBeforeClose.
	cancelExpirationMismatch cancelReason = "expiration_mismatch"

	// A duplicate/conflicting matching YES buy order for the same market: at most one
	// candidate is kept for the reconciliation pass; the rest are canceled.
	cancelDuplicateOrder cancelReason = "duplicate_order"
)

// String returns the cancel reason as a plain string for logging.
func (c cancelReason) String() string { return string(c) }

// String returns the skip reason as a plain string for logging.
func (s skipReason) String() string { return string(s) }

// binaryTokens is the resolved YES/NO token mapping for a binary market. The two
// token IDs are decoded from the market's index-aligned outcome/token arrays and
// exposed separately so a pass can act on the NO side without re-parsing JSON.
type binaryTokens struct {
	YesTokenID string
	NoTokenID  string
}

// decodeBinaryTokens deterministically resolves the YES and NO CLOB token IDs
// for a binary market. Market.Outcomes and Market.ClobTokenIDs are JSON-encoded
// array strings (e.g. `["Yes","No"]` and `["7190...","5211..."]`) that are
// index-aligned: outcome[i] corresponds to token[i].
//
// It returns a non-empty skipReason (the second return) instead of a mapping
// when the market is not a clean binary YES/NO market: missing/empty arrays,
// unparseable JSON, not exactly two of each, duplicated token IDs, duplicated
// outcome labels, or labels that are not a case-insensitive {YES, NO} pair.
// The caller logs the reason and skips the market — it never panics.
func decodeBinaryTokens(m polymarket.Market) (binaryTokens, skipReason) {
	rawOutcomes := strings.TrimSpace(m.Outcomes)
	if rawOutcomes == "" {
		return binaryTokens{}, skipOutcomesMissing
	}
	rawTokenIDs := strings.TrimSpace(m.ClobTokenIDs)
	if rawTokenIDs == "" {
		return binaryTokens{}, skipTokenIDsMissing
	}

	var outcomes []string
	if err := json.Unmarshal([]byte(rawOutcomes), &outcomes); err != nil {
		return binaryTokens{}, skipOutcomesUnparsed
	}
	var tokenIDs []string
	if err := json.Unmarshal([]byte(rawTokenIDs), &tokenIDs); err != nil {
		return binaryTokens{}, skipTokenIDsUnparsed
	}

	if len(outcomes) != 2 {
		return binaryTokens{}, skipNotTwoOutcomes
	}
	if len(tokenIDs) != 2 {
		return binaryTokens{}, skipNotTwoTokenIDs
	}

	// Reject empty token IDs the same way as a missing array: an empty string is
	// not a usable token ID.
	id0 := strings.TrimSpace(tokenIDs[0])
	id1 := strings.TrimSpace(tokenIDs[1])
	if id0 == "" || id1 == "" {
		return binaryTokens{}, skipTokenIDsMissing
	}
	if id0 == id1 {
		return binaryTokens{}, skipDuplicateTokenIDs
	}

	// Normalize labels to uppercase for a case-insensitive YES/NO match.
	label0 := strings.ToUpper(strings.TrimSpace(outcomes[0]))
	label1 := strings.ToUpper(strings.TrimSpace(outcomes[1]))
	if label0 == label1 {
		return binaryTokens{}, skipDuplicateOutcomes
	}

	var tokens binaryTokens
	switch {
	case label0 == "YES" && label1 == "NO":
		tokens.YesTokenID = id0
		tokens.NoTokenID = id1
	case label0 == "NO" && label1 == "YES":
		tokens.NoTokenID = id0
		tokens.YesTokenID = id1
	default:
		return binaryTokens{}, skipNotYesNoLabels
	}
	return tokens, ""
}
