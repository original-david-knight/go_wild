package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// renderHuman turns a structured event into a concise human-readable line and
// reports whether it should be shown. Action events (redeem, cancel, place,
// maintain, account-value summary, errors) are always shown; per-market skips and
// diagnostic detail are shown only when verbose is true. Long IDs are shortened
// and a market's question/title is preferred over its condition ID.
func renderHuman(runID, event string, fields map[string]any, verbose bool) (string, bool) {
	switch event {

	// ---- run framing ----
	case "run_start":
		mode := fstr(fields, "mode")
		tag := runTag(runID)
		suffix := " · live"
		if fbool(fields, "dry_run") {
			suffix = " · DRY-RUN (no orders submitted)"
		}
		return fmt.Sprintf("\n── run %s · %s%s ──", tag, mode, suffix), true
	case "run_done":
		return "── run complete ──", true
	case "run_init":
		return "", verbose // emitted only in tests

	// ---- redemption ----
	case "redeem_scan":
		n := fint(fields, "redeemable_conditions")
		if n <= 0 {
			return "Redeem: no resolved positions to redeem.", verbose
		}
		return fmt.Sprintf("Redeem: %d resolved position(s)…", n), true
	case "redeem_attempt":
		label := marketLabel(fields)
		switch fstr(fields, "status") {
		case "would_redeem":
			return fmt.Sprintf("  • would redeem %s (%s, %s shares)", label, fstr(fields, "outcome"), num(ffloat(fields, "size"))), true
		case "succeeded":
			return fmt.Sprintf("  ✓ redeemed %s — %s collateral", label, usd(fstrFloat(fields, "collateral_payout"))), true
		case "failed":
			return fmt.Sprintf("  ✗ redeem failed %s: %s", label, fstr(fields, "reason")), true
		}
	case "redeem_result":
		failed := fint(fields, "conditions_failed")
		line := fmt.Sprintf("  ↳ redeemed %d/%d condition(s) — %s collateral total",
			fint(fields, "conditions_redeemed"), fint(fields, "conditions_submitted"),
			usd(fstrFloat(fields, "total_collateral_payout")))
		if failed > 0 {
			line += fmt.Sprintf(" (%d failed)", failed)
		}
		return line, true

	// ---- collateral sweep (USDC.e / USDC -> pUSD) ----
	case "sweep":
		amount := num(ffloat(fields, "amount"))
		symbol := fstr(fields, "symbol")
		switch fstr(fields, "status") {
		case "wrapped":
			return fmt.Sprintf("  ✓ wrapped %s %s → pUSD", amount, symbol), true
		case "would_wrap":
			return fmt.Sprintf("  • would wrap %s %s → pUSD", amount, symbol), true
		}
	case "sweep_result":
		if fint(fields, "assets_swept") <= 0 {
			return "Sweep: no stray USDC to wrap.", verbose
		}
		if fbool(fields, "dry_run") {
			return "Sweep: stray USDC found (dry-run, not wrapped).", true
		}
		return fmt.Sprintf("Sweep: wrapped %s into pUSD.", usd(ffloat(fields, "total_usdc_wrapped"))), true
	case "sweep_error":
		return fmt.Sprintf("  ✗ sweep failed: %s", fstr(fields, "error")), true

	// ---- stale-order cancellation ----
	case "stale_scan":
		return fmt.Sprintf("Checking %d open order(s) for stale entries…", fint(fields, "open_orders")), verbose
	case "stale_order":
		label := marketLabel(fields)
		reason := humanReason(fstr(fields, "reason"))
		switch fstr(fields, "status") {
		case "canceled":
			return fmt.Sprintf("  ✓ canceled stale order on %s (%s)", label, reason), true
		case "would_cancel":
			return fmt.Sprintf("  • would cancel order on %s (%s)", label, reason), true
		case "failed":
			return fmt.Sprintf("  ✗ cancel failed on %s: %s", label, fstr(fields, "error")), true
		case "kept", "kept_unverified":
			return fmt.Sprintf("  = kept matching order on %s", label), verbose
		}
	case "stale_error":
		return fmt.Sprintf("  ! could not evaluate %s for stale orders: %s", marketLabel(fields), fstr(fields, "error")), true

	// ---- account value ----
	case "account_value":
		if fstr(fields, "status") == "aborted" {
			return fmt.Sprintf("✗ account value unavailable (%s): %s", fstr(fields, "stage"), fstr(fields, "reason")), true
		}
		return fmt.Sprintf("Account value: %s  (wallet %s + positions %s)",
			usd(ffloat(fields, "total")), usd(ffloat(fields, "wallet_usdc")), usd(ffloat(fields, "positions_value"))), true
	case "account_value_abort":
		return fmt.Sprintf("✗ buy pass aborted: %s", fstr(fields, "error")), true
	case "position_value":
		return fmt.Sprintf("  valued %s at %s (%s)", marketLabel(fields), usd(ffloat(fields, "price")), fstr(fields, "price_source")), verbose

	// ---- discovery ----
	case "discover_summary":
		line := fmt.Sprintf("Scanned %d market(s) · %d eligible.", fint(fields, "markets_scanned"), fint(fields, "markets_eligible"))
		if br := rejectionBreakdown(fields["rejections"]); br != "" {
			line += "\n  filtered out: " + br
		}
		return line, true
	case "discover_error", "discover_abort":
		return fmt.Sprintf("✗ market discovery failed (%s): %s", fstr(fields, "stage"), fstr(fields, "error")), true
	case "market_eligibility":
		label := marketLabel(fields)
		if fstr(fields, "status") == "eligible" {
			return fmt.Sprintf("  ✓ eligible: %s — NO mid %s", label, price(ffloat(fields, "no_midpoint"))), verbose
		}
		return fmt.Sprintf("  – skip %s: %s", label, humanReason(fstr(fields, "reason"))), verbose

	// ---- midpoint / sizing diagnostics (verbose) ----
	case "no_midpoint":
		if fstr(fields, "status") == "skipped" {
			return fmt.Sprintf("  – %s: %s", marketLabel(fields), humanReason(fstr(fields, "skip_reason"))), verbose
		}
		return fmt.Sprintf("  %s: NO mid %s (bid %s / ask %s)", marketLabel(fields),
			price(ffloat(fields, "no_midpoint")), price(ffloat(fields, "best_no_bid")), price(ffloat(fields, "best_no_ask"))), verbose
	case "min_order_size":
		if fstr(fields, "status") == "skipped" {
			return fmt.Sprintf("  – %s: %s", marketLabel(fields), humanReason(fstr(fields, "skip_reason"))), verbose
		}
		return fmt.Sprintf("  %s: min order %s (%s)", marketLabel(fields), num(ffloat(fields, "min_order_size")), fstr(fields, "source")), verbose
	case "sizing", "funding", "funding_plan_start", "funding_plan_done":
		return "", verbose // internal planning detail

	// ---- reconciliation (place / maintain / cancel-replace) ----
	case "reconcile_start", "reconcile_done":
		return "", verbose
	case "reconcile_skip":
		return fmt.Sprintf("  – skip %s: %s", marketLabel(fields), humanReason(fstr(fields, "reason"))), verbose
	case "reconcile_error":
		return fmt.Sprintf("✗ reconcile error (%s): %s", fstr(fields, "stage"), fstr(fields, "error")), true
	case "reconcile_order":
		return renderReconcileOrder(fields)

	// ---- init / generic errors ----
	case "init_error", "redeem_error":
		return fmt.Sprintf("✗ %s", fstr(fields, "error")), true
	}

	// Unknown event: only in verbose, as a compact fallback.
	if verbose {
		return fmt.Sprintf("  · %s %s", event, compactFields(fields)), true
	}
	return "", false
}

// renderReconcileOrder formats the place/maintain/cancel-replace order actions.
func renderReconcileOrder(fields map[string]any) (string, bool) {
	label := marketLabel(fields)
	flags := ""
	if fbool(fields, "min_order_exception") {
		flags += " [min-order]"
	}
	if fbool(fields, "partial_fill") {
		flags += " [partial]"
	}
	switch fstr(fields, "status") {
	case "placed":
		return fmt.Sprintf("  ✓ placed NO buy: %s — %s @ %s (%s), expires %s%s",
			label, num(ffloat(fields, "shares")), price(ffloat(fields, "price")),
			usd(ffloat(fields, "notional")), expiry(fields["expiration"]), flags), true
	case "would_place":
		return fmt.Sprintf("  • would place NO buy: %s — %s @ %s (%s), expires %s%s",
			label, num(ffloat(fields, "shares")), price(ffloat(fields, "price")),
			usd(ffloat(fields, "notional")), expiry(fields["expiration"]), flags), true
	case "maintained":
		return fmt.Sprintf("  = unchanged: %s — %s @ %s", label, num(ffloat(fields, "shares")), price(ffloat(fields, "price"))), true
	case "canceled":
		return fmt.Sprintf("  ✓ canceled order on %s (%s)", label, humanReason(fstr(fields, "reason"))), true
	case "would_cancel":
		return fmt.Sprintf("  • would cancel order on %s (%s)", label, humanReason(fstr(fields, "reason"))), true
	case "place_failed", "place_rejected":
		return fmt.Sprintf("  ✗ place failed %s: %s", label, fstr(fields, "error")), true
	case "cancel_failed":
		return fmt.Sprintf("  ✗ cancel failed on %s: %s", label, fstr(fields, "error")), true
	}
	return "", false
}

// --- label + reason helpers ---

// marketLabel returns a short, human-friendly name for a market: its question or
// title when available, otherwise a shortened condition/token ID.
func marketLabel(fields map[string]any) string {
	if q := strings.TrimSpace(fstr(fields, "question")); q != "" {
		return quote(truncate(q, 64))
	}
	if t := strings.TrimSpace(fstr(fields, "title")); t != "" {
		return quote(truncate(t, 64))
	}
	if c := strings.TrimSpace(fstr(fields, "condition_id")); c != "" {
		return shortID(c)
	}
	if t := strings.TrimSpace(fstr(fields, "no_token_id")); t != "" {
		return shortID(t)
	}
	return "(market)"
}

// rejectionBreakdown formats a reason->count map (as carried in discover_summary)
// into a compact, descending-by-count summary like
// "no_midpoint_too_low ×1840, liquidity_below_min ×900, …".
func rejectionBreakdown(v any) string {
	m, ok := v.(map[string]int)
	if !ok || len(m) == 0 {
		return ""
	}
	type kv struct {
		reason string
		count  int
	}
	pairs := make([]kv, 0, len(m))
	for r, c := range m {
		pairs = append(pairs, kv{r, c})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].reason < pairs[j].reason
	})
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, fmt.Sprintf("%s ×%d", humanReason(p.reason), p.count))
	}
	return strings.Join(parts, ", ")
}

// humanReason converts a snake_case skip/cancel reason slug into a short phrase.
func humanReason(r string) string {
	r = strings.TrimSpace(r)
	if r == "" {
		return "ineligible"
	}
	return strings.ReplaceAll(r, "_", " ")
}

// runTag returns a short, stable tag for a run ID (the last 6 chars of its random
// suffix) so runs are distinguishable without printing the full identifier.
func runTag(runID string) string {
	if i := strings.LastIndexByte(runID, '_'); i >= 0 && i+1 < len(runID) {
		s := runID[i+1:]
		if len(s) > 6 {
			return s[len(s)-6:]
		}
		return s
	}
	if len(runID) > 6 {
		return runID[len(runID)-6:]
	}
	return runID
}

// shortID shortens a long hex/numeric identifier to a readable prefix.
func shortID(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:10] + "…"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n-1]) + "…"
}

func quote(s string) string { return "\"" + s + "\"" }

// --- value formatting ---

func usd(f float64) string   { return "$" + commaFloat(f, 2) }
func price(f float64) string { return "$" + trimFloat(f) }
func num(f float64) string   { return trimFloat(f) }

// commaFloat formats f with the given decimals and thousands separators.
func commaFloat(f float64, decimals int) string {
	s := fmt.Sprintf("%.*f", decimals, f)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	intPart, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, d := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	out := b.String()
	if frac != "" {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

// trimFloat formats a float with up to 4 decimals, trimming trailing zeros.
func trimFloat(f float64) string {
	s := fmt.Sprintf("%.4f", f)
	s = strings.TrimRight(s, "0")
	s = strings.TrimSuffix(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

// expiry formats a unix-seconds timestamp (int/int64/float64) as a readable UTC time.
func expiry(v any) string {
	var sec int64
	switch t := v.(type) {
	case int64:
		sec = t
	case int:
		sec = int64(t)
	case float64:
		sec = int64(t)
	default:
		return "?"
	}
	if sec <= 0 {
		return "?"
	}
	return time.Unix(sec, 0).UTC().Format("Jan 2 15:04 MST")
}

// --- field accessors (the human path receives the original Go values) ---

func fstr(f map[string]any, k string) string {
	v, ok := f[k]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func ffloat(f map[string]any, k string) float64 {
	switch v := f[k].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}

func fint(f map[string]any, k string) int64 {
	switch v := f[k].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	}
	return 0
}

func fbool(f map[string]any, k string) bool {
	b, _ := f[k].(bool)
	return b
}

// fstrFloat parses a numeric value that may be carried as a string (e.g. a
// collateral payout) into a float.
func fstrFloat(f map[string]any, k string) float64 {
	if v := ffloat(f, k); v != 0 {
		return v
	}
	var x float64
	if _, err := fmt.Sscanf(strings.TrimSpace(fstr(f, k)), "%f", &x); err == nil {
		return x
	}
	return 0
}

// compactFields renders a small key=value summary for unknown verbose events.
func compactFields(f map[string]any) string {
	var parts []string
	for k, v := range f {
		if k == "run_id" || k == "ts" || k == "event" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, " ")
}
