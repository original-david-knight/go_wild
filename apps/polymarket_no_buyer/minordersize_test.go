package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// bookWithMin builds an OrderBookDetail carrying the given min_order_size. The
// embedded float64OrString type is unexported, so we construct via JSON — the
// same path production uses — rather than reaching into the polymarket package.
func bookWithMin(t *testing.T, minOrderSize string) *polymarket.OrderBookDetail {
	t.Helper()
	var book polymarket.OrderBookDetail
	if err := json.Unmarshal([]byte(`{"min_order_size":`+minOrderSize+`}`), &book); err != nil {
		t.Fatalf("build order book: %v", err)
	}
	return &book
}

// clobMarketWithMos builds a ClobMarket carrying the given minimum_order_size,
// again via JSON to populate the unexported float64OrString field.
func clobMarketWithMos(t *testing.T, mos string) *polymarket.ClobMarket {
	t.Helper()
	var m polymarket.ClobMarket
	if err := json.Unmarshal([]byte(`{"minimum_order_size":`+mos+`}`), &m); err != nil {
		t.Fatalf("build clob market: %v", err)
	}
	return &m
}

// minOrderTestApp builds an App wired to the fake with an optional positive
// fallback. A negative fallbackValue with fallbackSet leaves the value as-is so
// the "set but not positive" guard can be exercised too.
func minOrderTestApp(fake *fakeTradingClient, fallbackSet bool, fallbackValue float64) *App {
	cfg := defaultConfig()
	cfg.MinOrderSizeFallbackSet = fallbackSet
	cfg.MinOrderSizeFallback = fallbackValue
	return &App{
		cfg:      &cfg,
		trading:  fake,
		newRunID: seqRunID(),
		now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}
}

// Order-book min_order_size present and positive => resolved from the book and
// GetClobMarket is never called.
func TestResolveMinOrderSize_OrderBookWins(t *testing.T) {
	fake := &fakeTradingClient{}
	app := minOrderTestApp(fake, false, 0)
	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_ob")

	book := bookWithMin(t, "15")
	size, source, reason := app.resolveMinOrderSize(context.Background(), logger, "0xabc", book)
	if reason != "" {
		t.Fatalf("unexpected skip reason %q", reason)
	}
	if size != 15 || source != minOrderSourceOrderBook {
		t.Fatalf("size/source = %v/%q, want 15/%q", size, source, minOrderSourceOrderBook)
	}
	if fake.clobMarketCalls != 0 {
		t.Fatalf("GetClobMarket called %d times, want 0 (book must short-circuit)", fake.clobMarketCalls)
	}

	logs := eventsNamed(parseEvents(t, buf.String()), "min_order_size")
	if len(logs) != 1 {
		t.Fatalf("min_order_size logs = %d, want 1", len(logs))
	}
	e := logs[0]
	if e["status"] != "ok" || e["source"] != minOrderSourceOrderBook || e["min_order_size"] != float64(15) || e["condition_id"] != "0xabc" {
		t.Errorf("resolved log = %v, want ok/order_book/15/0xabc", e)
	}
}

// Book min_order_size missing/zero/negative but GetClobMarket returns positive
// mos => resolved from clob_markets_mos and GetClobMarket called exactly once.
func TestResolveMinOrderSize_FallsBackToClobMos(t *testing.T) {
	cases := []struct {
		name string
		book *polymarket.OrderBookDetail
	}{
		{"nil book", nil},
		{"zero book min", bookWithMin(t, "0")},
		{"negative book min", bookWithMin(t, "-5")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeTradingClient{
				clobMarkets: map[string]*polymarket.ClobMarket{"0xabc": clobMarketWithMos(t, "20")},
			}
			app := minOrderTestApp(fake, false, 0)
			var buf bytes.Buffer
			logger := NewLogger(&buf, "run_mos")

			size, source, reason := app.resolveMinOrderSize(context.Background(), logger, "0xabc", tc.book)
			if reason != "" {
				t.Fatalf("unexpected skip reason %q", reason)
			}
			if size != 20 || source != minOrderSourceClobMos {
				t.Fatalf("size/source = %v/%q, want 20/%q", size, source, minOrderSourceClobMos)
			}
			if fake.clobMarketCalls != 1 {
				t.Fatalf("GetClobMarket called %d times, want 1", fake.clobMarketCalls)
			}

			logs := eventsNamed(parseEvents(t, buf.String()), "min_order_size")
			if len(logs) != 1 || logs[0]["status"] != "ok" || logs[0]["source"] != minOrderSourceClobMos {
				t.Fatalf("resolved log = %v, want one ok/clob_markets_mos line", logs)
			}
		})
	}
}

// Both undeterminable AND fallback unset => skip signal, market excluded from
// live ordering, and a log line names condition_id + reason.
func TestResolveMinOrderSize_UndeterminableNoFallback(t *testing.T) {
	fake := &fakeTradingClient{
		clobMarkets: map[string]*polymarket.ClobMarket{"0xabc": clobMarketWithMos(t, "0")},
	}
	app := minOrderTestApp(fake, false, 0)
	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_skip")

	size, source, reason := app.resolveMinOrderSize(context.Background(), logger, "0xabc", nil)
	if reason != skipMinOrderSizeUndeterminable {
		t.Fatalf("reason = %q, want %q", reason, skipMinOrderSizeUndeterminable)
	}
	if size != 0 || source != "" {
		t.Errorf("size/source = %v/%q, want 0/\"\" on skip", size, source)
	}

	logs := eventsNamed(parseEvents(t, buf.String()), "min_order_size")
	if len(logs) != 1 {
		t.Fatalf("min_order_size logs = %d, want 1", len(logs))
	}
	e := logs[0]
	if e["status"] != "skipped" || e["skip_reason"] != skipMinOrderSizeUndeterminable.String() || e["condition_id"] != "0xabc" {
		t.Errorf("skip log = %v, want status=skipped reason=%q condition_id=0xabc", e, skipMinOrderSizeUndeterminable.String())
	}
	if _, ok := e["min_order_size"]; ok {
		t.Errorf("skip log must not carry a min_order_size field: %v", e)
	}
}

// A GetClobMarket fetch error fails closed: skip with the undeterminable reason
// and a fetch_error on the log line, rather than guessing a minimum.
func TestResolveMinOrderSize_ClobFetchError(t *testing.T) {
	fake := &fakeTradingClient{
		clobMarketErrByCond: map[string]error{"0xabc": errors.New("boom")},
	}
	app := minOrderTestApp(fake, true, 9) // fallback set but must not rescue a fetch error
	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_err")

	_, _, reason := app.resolveMinOrderSize(context.Background(), logger, "0xabc", nil)
	if reason != skipMinOrderSizeUndeterminable {
		t.Fatalf("reason = %q, want %q", reason, skipMinOrderSizeUndeterminable)
	}
	logs := eventsNamed(parseEvents(t, buf.String()), "min_order_size")
	if len(logs) != 1 || logs[0]["status"] != "skipped" || logs[0]["fetch_error"] != "boom" {
		t.Fatalf("expected one skipped log carrying fetch_error=boom, got %v", logs)
	}
}

// Both venue sources undeterminable BUT a positive fallback is set => resolved
// from the fallback. The companion sub-case proves the path is NOT taken when
// the flag is false (it skips instead).
func TestResolveMinOrderSize_FallbackGate(t *testing.T) {
	t.Run("fallback set and positive is used", func(t *testing.T) {
		fake := &fakeTradingClient{
			clobMarkets: map[string]*polymarket.ClobMarket{"0xabc": clobMarketWithMos(t, "0")},
		}
		app := minOrderTestApp(fake, true, 7)
		var buf bytes.Buffer
		logger := NewLogger(&buf, "run_fb")

		size, source, reason := app.resolveMinOrderSize(context.Background(), logger, "0xabc", nil)
		if reason != "" {
			t.Fatalf("unexpected skip reason %q", reason)
		}
		if size != 7 || source != minOrderSourceFallback {
			t.Fatalf("size/source = %v/%q, want 7/%q", size, source, minOrderSourceFallback)
		}
		logs := eventsNamed(parseEvents(t, buf.String()), "min_order_size")
		if len(logs) != 1 || logs[0]["source"] != minOrderSourceFallback || logs[0]["min_order_size"] != float64(7) {
			t.Fatalf("resolved log = %v, want one fallback/7 line", logs)
		}
	})

	t.Run("fallback unset is not taken", func(t *testing.T) {
		fake := &fakeTradingClient{
			clobMarkets: map[string]*polymarket.ClobMarket{"0xabc": clobMarketWithMos(t, "0")},
		}
		// Value present in the struct but flag false: must be ignored.
		app := minOrderTestApp(fake, false, 7)
		var buf bytes.Buffer
		logger := NewLogger(&buf, "run_nofb")

		_, source, reason := app.resolveMinOrderSize(context.Background(), logger, "0xabc", nil)
		if reason != skipMinOrderSizeUndeterminable {
			t.Fatalf("reason = %q, want %q (unset fallback must not be used)", reason, skipMinOrderSizeUndeterminable)
		}
		if source == minOrderSourceFallback {
			t.Errorf("source = %q, fallback must not be taken when flag is false", source)
		}
	})
}

// Fail-closed precedence: a present positive order-book min_order_size is used
// even when a positive fallback value is also configured. Real data wins.
func TestResolveMinOrderSize_RealDataBeatsFallback(t *testing.T) {
	fake := &fakeTradingClient{}
	app := minOrderTestApp(fake, true, 99) // fallback set and large, but must lose
	var buf bytes.Buffer
	logger := NewLogger(&buf, "run_pref")

	book := bookWithMin(t, "15")
	size, source, reason := app.resolveMinOrderSize(context.Background(), logger, "0xabc", book)
	if reason != "" {
		t.Fatalf("unexpected skip reason %q", reason)
	}
	if size != 15 || source != minOrderSourceOrderBook {
		t.Fatalf("size/source = %v/%q, want 15/%q (real book min beats fallback)", size, source, minOrderSourceOrderBook)
	}
	if fake.clobMarketCalls != 0 {
		t.Errorf("GetClobMarket called %d times, want 0", fake.clobMarketCalls)
	}
}
