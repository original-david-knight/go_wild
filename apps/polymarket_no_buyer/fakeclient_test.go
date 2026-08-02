package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	gowild_crypto "github.com/original-david-knight/go_wild/crypto"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

// fakeTradingClient is a configurable in-memory tradingClient for tests. It
// records the ordered sequence of method calls and lets each test inject
// results and per-key failures. Later rungs extend it with the order-book,
// order, and placement methods they need.
type fakeTradingClient struct {
	mu    sync.Mutex
	calls []string

	positions    []polymarket.Position
	positionsErr error

	redeemErrByCondition map[string]error
	redeemResult         *polymarket.RedeemWinningsResult
	redeemCalls          []string

	// sweepResult is what SweepCollateralToPUSD returns (default: an empty sweep —
	// no stray collateral found). sweepErr, when set, fails the sweep.
	sweepResult *polymarket.CollateralSweepResult
	sweepErr    error
	sweepCalls  int

	// Per-token order books and fetch failures keyed by token ID. bookErrByToken
	// fails EVERY GetOrderBookDetailed for the keyed token. bookErrAfterByToken fails
	// the keyed token only AFTER the given number of successful fetches have been
	// served (e.g. value 1 ⇒ the first fetch succeeds, every later one errors), so a
	// market can pass discovery yet fail the FRESH reconcile re-fetch. bookFetchCount
	// records the per-token fetch count that drives the "after" threshold.
	books               map[string]*polymarket.OrderBookDetail
	bookErrByToken      map[string]error
	bookErrAfterByToken map[string]error
	bookFetchCount      map[string]int
	bookCalls           []string

	// Per-condition clob-market metadata and fetch failures keyed by condition
	// ID. clobMarketCalls counts every GetClobMarket invocation so tests can
	// assert the order-book short-circuit avoided the call entirely.
	clobMarkets         map[string]*polymarket.ClobMarket
	clobMarketErrByCond map[string]error
	clobMarketCalls     int

	// markets is the fixed candidate slice ListMarkets pages over: it returns the
	// requested window [offset, offset+limit) and an empty slice once offset is
	// past the end, so the discovery loop sees a short final page. listMarketsErr,
	// when set, fails every ListMarkets call. listMarketsCalls records each
	// (limit, offset) pair so tests can assert the paging contract.
	markets                  []polymarket.Market
	listMarketsErr           error
	listMarketsCalls         [][2]int
	listMarketsKeysetCursors []string

	// openOrders is the account's open-order slice GetOrders("") returns.
	// getOrdersErr, when set, fails every GetOrders call.
	openOrders   []polymarket.Order
	getOrdersErr error

	// gammaMarkets is the per-condition Gamma Market map GetMarket reads.
	// gammaMarketErrByCond fails GetMarket for the keyed condition.
	gammaMarkets         map[string]*polymarket.Market
	gammaMarketErrByCond map[string]error

	// canceledOrders records every order ID passed to CancelOrder, in call order.
	// cancelErrByOrderID forces a CancelOrder failure for the keyed order ID.
	canceledOrders     []string
	cancelErrByOrderID map[string]error

	// placedOrders records every order submitted via PlaceOrderWithExpiration, in
	// call order. placeErr, when set, fails every placement; placeErrByToken, when
	// set, fails placement only for the keyed YES token ID (so one market's placement
	// can fail while siblings succeed); placeResp, when set, is the response returned
	// on success (default: success with a synthetic ID).
	placedOrders    []placedOrder
	placeErr        error
	placeErrByToken map[string]error
	placeResp       *polymarket.PlaceOrderResponse
}

// placedOrder records the arguments of a single PlaceOrderWithExpiration call so a
// test can assert the token, price, size, side, and GTD expiration the pass chose.
type placedOrder struct {
	tokenID    string
	price      float64
	size       float64
	side       string
	expiration int64
}

// ensureYesBooks gives legacy test fixtures an execution book for each synthetic
// NO signal book. Individual tests can provide a distinct YES book explicitly;
// this helper never overwrites one.
func ensureYesBooks(f *fakeTradingClient) {
	if f == nil || f.books == nil {
		return
	}
	for tokenID, book := range f.books {
		if !strings.HasPrefix(tokenID, "no") {
			continue
		}
		yesTokenID := "yes" + strings.TrimPrefix(tokenID, "no")
		if _, exists := f.books[yesTokenID]; !exists {
			f.books[yesTokenID] = book
		}
	}
}

func (f *fakeTradingClient) record(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
}

func (f *fakeTradingClient) callOrder() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeTradingClient) GetPositions(ctx context.Context) ([]polymarket.Position, error) {
	f.record("GetPositions")
	return f.positions, f.positionsErr
}

func (f *fakeTradingClient) RedeemWinnings(ctx context.Context, conditionID string, indexSets []int, collateralTokenAddress string, includeLosing bool) (*polymarket.RedeemWinningsResult, error) {
	f.record("RedeemWinnings:" + conditionID)
	f.mu.Lock()
	f.redeemCalls = append(f.redeemCalls, conditionID)
	f.mu.Unlock()
	// An explicitly-injected result wins, letting a test control the breakdown.
	if f.redeemResult != nil {
		return f.redeemResult, nil
	}
	// Single-condition form: honor a per-condition injected error.
	if conditionID != "" {
		if err := f.redeemErrByCondition[conditionID]; err != nil {
			return nil, err
		}
		return &polymarket.RedeemWinningsResult{ConditionsRedeemed: 1, ConditionsSubmitted: 1, TotalCollateralPayout: "1"}, nil
	}
	// Redeem-all form (empty conditionID): mirror the real client by settling every
	// redeemable position in one call, returning a per-condition transaction
	// breakdown. redeemErrByCondition entries become per-transaction failures so a
	// bad condition is isolated rather than aborting the whole call.
	return f.synthesizeRedeemAll(), nil
}

func (f *fakeTradingClient) SweepCollateralToPUSD(ctx context.Context) (*polymarket.CollateralSweepResult, error) {
	f.record("SweepCollateralToPUSD")
	f.mu.Lock()
	f.sweepCalls++
	f.mu.Unlock()
	if f.sweepErr != nil {
		return nil, f.sweepErr
	}
	if f.sweepResult != nil {
		return f.sweepResult, nil
	}
	return &polymarket.CollateralSweepResult{}, nil
}

// synthesizeRedeemAll builds a redeem-all result from the fake's redeemable
// positions, mirroring polymarket.Client.RedeemWinnings(ctx, "", ...).
func (f *fakeTradingClient) synthesizeRedeemAll() *polymarket.RedeemWinningsResult {
	res := &polymarket.RedeemWinningsResult{}
	payout := 0
	for _, rc := range selectRedeemableConditions(f.positions) {
		tx := polymarket.RedeemWinningsTx{ConditionID: rc.ConditionID, ReceiptStatus: "confirmed"}
		if err := f.redeemErrByCondition[rc.ConditionID]; err != nil {
			tx.Error = err.Error()
			res.ConditionsFailed++
		} else {
			tx.CollateralPayout = "1"
			payout++
			res.ConditionsRedeemed++
		}
		res.Transactions = append(res.Transactions, tx)
	}
	res.ConditionsSubmitted = len(res.Transactions)
	res.TotalCollateralPayout = fmt.Sprintf("%d", payout)
	return res
}

func (f *fakeTradingClient) GetOrderBookDetailed(ctx context.Context, tokenID string) (*polymarket.OrderBookDetail, error) {
	f.record("GetOrderBookDetailed:" + tokenID)
	f.mu.Lock()
	f.bookCalls = append(f.bookCalls, tokenID)
	if f.bookFetchCount == nil {
		f.bookFetchCount = map[string]int{}
	}
	f.bookFetchCount[tokenID]++
	count := f.bookFetchCount[tokenID]
	f.mu.Unlock()
	if f.bookErrByToken != nil {
		if err := f.bookErrByToken[tokenID]; err != nil {
			return nil, err
		}
	}
	if f.bookErrAfterByToken != nil {
		if err := f.bookErrAfterByToken[tokenID]; err != nil && count > 1 {
			return nil, err
		}
	}
	if f.books != nil {
		if book, ok := f.books[tokenID]; ok {
			return book, nil
		}
	}
	return nil, fmt.Errorf("no order book configured for token %q", tokenID)
}

func (f *fakeTradingClient) GetClobMarket(ctx context.Context, conditionID string) (*polymarket.ClobMarket, error) {
	f.record("GetClobMarket:" + conditionID)
	f.mu.Lock()
	f.clobMarketCalls++
	f.mu.Unlock()
	if f.clobMarketErrByCond != nil {
		if err := f.clobMarketErrByCond[conditionID]; err != nil {
			return nil, err
		}
	}
	if f.clobMarkets != nil {
		if m, ok := f.clobMarkets[conditionID]; ok {
			return m, nil
		}
	}
	return nil, fmt.Errorf("no clob market configured for condition %q", conditionID)
}

func (f *fakeTradingClient) GetOrders(ctx context.Context, market string) ([]polymarket.Order, error) {
	f.record("GetOrders:" + market)
	if f.getOrdersErr != nil {
		return nil, f.getOrdersErr
	}
	return append([]polymarket.Order(nil), f.openOrders...), nil
}

func (f *fakeTradingClient) GetMarket(ctx context.Context, conditionID string) (*polymarket.Market, error) {
	f.record("GetMarket:" + conditionID)
	if f.gammaMarketErrByCond != nil {
		if err := f.gammaMarketErrByCond[conditionID]; err != nil {
			return nil, err
		}
	}
	if f.gammaMarkets != nil {
		if m, ok := f.gammaMarkets[conditionID]; ok {
			return m, nil
		}
	}
	return nil, fmt.Errorf("no gamma market configured for condition %q", conditionID)
}

func (f *fakeTradingClient) CancelOrder(ctx context.Context, orderID string) error {
	f.record("CancelOrder:" + orderID)
	f.mu.Lock()
	f.canceledOrders = append(f.canceledOrders, orderID)
	f.mu.Unlock()
	if f.cancelErrByOrderID != nil {
		if err := f.cancelErrByOrderID[orderID]; err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeTradingClient) PlaceOrderWithExpiration(ctx context.Context, tokenID string, price, size float64, side string, expirationUnix int64) (*polymarket.PlaceOrderResponse, error) {
	f.record("PlaceOrderWithExpiration:" + tokenID)
	f.mu.Lock()
	f.placedOrders = append(f.placedOrders, placedOrder{
		tokenID: tokenID,
		price:   price,
		// Record the size AS THE VENUE WOULD store it: the real client truncates
		// order size to the 2-decimal grid (round toward zero) when building the
		// order. Simulating that here means any caller that forgets to floor before
		// placing is caught by the tests — the recorded size never carries sub-cent
		// precision the venue would have dropped.
		size:       floorToSizePrecision(size),
		side:       side,
		expiration: expirationUnix,
	})
	f.mu.Unlock()
	if f.placeErr != nil {
		return nil, f.placeErr
	}
	if f.placeErrByToken != nil {
		if err := f.placeErrByToken[tokenID]; err != nil {
			return nil, err
		}
	}
	if f.placeResp != nil {
		return f.placeResp, nil
	}
	return &polymarket.PlaceOrderResponse{Success: true, OrderID: "placed-" + tokenID}, nil
}

// fakeWallet is a configurable in-memory walletClient for tests. It returns a
// fixed human-readable balance string (or an injected error) and records the
// (chain, tokenAddress) of every GetTokenBalance call so tests can assert the
// snapshot sourced USDC through the wallet helper against the configured token.
type fakeWallet struct {
	mu sync.Mutex

	balance string
	err     error

	calls []walletBalanceCall
}

type walletBalanceCall struct {
	chain        gowild_crypto.Chain
	tokenAddress string
}

func (w *fakeWallet) GetTokenBalance(ctx context.Context, chain gowild_crypto.Chain, tokenAddress string) (*gowild_crypto.BalanceResult, error) {
	w.mu.Lock()
	w.calls = append(w.calls, walletBalanceCall{chain: chain, tokenAddress: tokenAddress})
	w.mu.Unlock()
	if w.err != nil {
		return nil, w.err
	}
	return &gowild_crypto.BalanceResult{
		Chain:        chain,
		Balance:      w.balance,
		Symbol:       "USDC",
		Decimals:     6,
		TokenAddress: tokenAddress,
	}, nil
}

func (w *fakeWallet) balanceCalls() []walletBalanceCall {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]walletBalanceCall(nil), w.calls...)
}

func (f *fakeTradingClient) ListMarkets(ctx context.Context, limit, offset int) ([]polymarket.Market, error) {
	f.record("ListMarkets")
	f.mu.Lock()
	f.listMarketsCalls = append(f.listMarketsCalls, [2]int{limit, offset})
	f.mu.Unlock()
	if f.listMarketsErr != nil {
		return nil, f.listMarketsErr
	}
	if offset >= len(f.markets) {
		return nil, nil
	}
	end := offset + limit
	if end > len(f.markets) {
		end = len(f.markets)
	}
	return append([]polymarket.Market(nil), f.markets[offset:end]...), nil
}

// ListMarketsClosingBetweenKeyset returns the configured markets paged by a
// numeric string cursor. An empty cursor starts at zero; the next cursor is the
// next start index.
func (f *fakeTradingClient) ListMarketsClosingBetweenKeyset(ctx context.Context, minClose, maxClose time.Time, minLiquidity float64, limit int, afterCursor string) (polymarket.MarketPage, error) {
	f.record("ListMarketsClosingBetweenKeyset")
	f.mu.Lock()
	f.listMarketsKeysetCursors = append(f.listMarketsKeysetCursors, afterCursor)
	f.mu.Unlock()
	if f.listMarketsErr != nil {
		return polymarket.MarketPage{}, f.listMarketsErr
	}

	offset := 0
	if afterCursor != "" {
		parsed, err := strconv.Atoi(afterCursor)
		if err != nil {
			return polymarket.MarketPage{}, fmt.Errorf("bad fake keyset cursor %q", afterCursor)
		}
		offset = parsed
	}
	if offset >= len(f.markets) {
		return polymarket.MarketPage{}, nil
	}
	end := offset + limit
	if end > len(f.markets) {
		end = len(f.markets)
	}
	nextCursor := ""
	if end < len(f.markets) {
		nextCursor = strconv.Itoa(end)
	}
	return polymarket.MarketPage{
		Markets:    append([]polymarket.Market(nil), f.markets[offset:end]...),
		NextCursor: nextCursor,
	}, nil
}

// ListMarketsClosingBetween returns the configured markets (tests set in-window
// fixtures), paged like ListMarkets. The window/liquidity args are accepted but
// not re-filtered here — server-side filtering is exercised against the live API.
func (f *fakeTradingClient) ListMarketsClosingBetween(ctx context.Context, minClose, maxClose time.Time, minLiquidity float64, limit, offset int) ([]polymarket.Market, error) {
	f.record("ListMarketsClosingBetween")
	f.mu.Lock()
	f.listMarketsCalls = append(f.listMarketsCalls, [2]int{limit, offset})
	f.mu.Unlock()
	if f.listMarketsErr != nil {
		return nil, f.listMarketsErr
	}
	if offset >= len(f.markets) {
		return nil, nil
	}
	end := offset + limit
	if end > len(f.markets) {
		end = len(f.markets)
	}
	return append([]polymarket.Market(nil), f.markets[offset:end]...), nil
}
