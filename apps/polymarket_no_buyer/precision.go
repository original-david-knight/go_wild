package main

import (
	"math"
	"strconv"
	"strings"
)

// sizeDecimals is the Polymarket size precision: order sizes are quantized to two
// decimal places. The matching scale (100) is used to round sizes to that grid.
const sizeDecimals = 2

// sizeScale is 10^sizeDecimals — the integer grid sizes are rounded onto.
const sizeScale = 100.0

// priceTickCount returns the integer number of ticks a price represents on the
// venue grid, i.e. round(price/tickSize). Comparing tick counts (integers) rather
// than re-multiplied floats avoids float64 rounding error at exact half-ticks
// (e.g. 0.585/0.01 evaluating to 58.4999…). Returns (0, false) when the tick is
// undeterminable so callers can fail closed.
func priceTickCount(price, tickSize float64) (int64, bool) {
	if tickSize <= 0 {
		return 0, false
	}
	return int64(math.Round(price / tickSize)), true
}

// normalizePrice quantizes a limit price to the market's accepted price tick.
// Polymarket only accepts prices that are integer multiples of the venue tick
// (e.g. 0.01 or 0.001). It snaps to the nearest tick via the integer tick count so
// the result is the exact on-grid price the venue would accept (used when placing
// orders at the normalized midpoint).
//
// tickSize <= 0 means the venue tick is undeterminable. The price grid is unknown,
// so the input price is returned unchanged: the caller must NOT treat the value as
// a normalized, comparable price. The stale-cancel pass relies on this — it refuses
// to cancel on a price mismatch when the tick is unknown (fail closed: an unprovable
// staleness is not a reason to cancel a live order).
func normalizePrice(price, tickSize float64) float64 {
	ticks, ok := priceTickCount(price, tickSize)
	if !ok {
		return price
	}
	return float64(ticks) * tickSize
}

// normalizeSize quantizes an order size to Polymarket's two-decimal size precision
// so two raw sizes that round to the same accepted size compare equal. Rounding is
// half-away-from-zero via math.Round on the scaled value.
//
// WARNING: this ROUNDS, while the venue FLOORS. Do NOT use it for size logic that
// must agree with what the venue stores when an order is placed — use
// floorToSizePrecision for that. normalizeSize is retained only for non-size-grid
// comparisons where round-half semantics are intended.
func normalizeSize(size float64) float64 {
	return math.Round(size*sizeScale) / sizeScale
}

// floorToSizePrecision truncates a size to Polymarket's 2-decimal size grid the
// same way the client builds orders (round toward zero), so the value the app
// reasons about is byte-identical to what the venue stores. It mirrors the client's
// string-based truncation (polymarket/order.go scaleDecimalString with
// roundTowardNegativeInfinity): format the float to a full-precision decimal string
// and keep at most two fractional digits, then parse back. This avoids the float
// error of math.Floor(x*100)/100, which can land on the wrong cent at exact
// boundaries.
func floorToSizePrecision(size float64) float64 {
	s := strconv.FormatFloat(size, 'f', -1, 64)
	if dot := strings.IndexByte(s, '.'); dot >= 0 && len(s) > dot+3 {
		s = s[:dot+3]
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// pricesEqual reports whether two prices land on the same accepted tick. It
// compares integer tick counts (not re-multiplied floats) so two prices that are
// the same accepted price are never judged different by float rounding, and two
// genuinely different ticks are never judged equal. It returns false (treat as
// unequal/unprovable) when the tick is undeterminable, so callers fail closed
// rather than comparing on an unknown grid.
func pricesEqual(a, b, tickSize float64) bool {
	ta, oka := priceTickCount(a, tickSize)
	tb, okb := priceTickCount(b, tickSize)
	if !oka || !okb {
		return false
	}
	return ta == tb
}
