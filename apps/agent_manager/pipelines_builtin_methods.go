package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	spec "github.com/original-david-knight/go_wild/apps/agent_manager/internal/pipelinespec"
	"github.com/original-david-knight/go_wild/crypto"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

type builtinPipelineMethodHandler func(ctx context.Context, pe *PipelineEngine, run *data.PipelineRun, step PipelineStep, params map[string]any) (map[string]any, error)

type builtinPolymarketClient interface {
	ListEvents(ctx context.Context, limit, offset int) ([]polymarket.Event, error)
	SearchMarkets(ctx context.Context, query string, limit int) ([]polymarket.Market, error)
	ListMarkets(ctx context.Context, limit, offset int) ([]polymarket.Market, error)
	GetMarket(ctx context.Context, conditionID string) (*polymarket.Market, error)
	GetPositions(ctx context.Context) ([]polymarket.Position, error)
	GetOrders(ctx context.Context, market string) ([]polymarket.Order, error)
	GetPrice(ctx context.Context, tokenID, side string) (string, error)
	GetOrderBook(ctx context.Context, tokenID string) (*polymarket.OrderBook, error)
	PlaceOrder(ctx context.Context, tokenID string, price, size float64, side, orderType string, negRisk bool) (*polymarket.PlaceOrderResponse, error)
	CancelOrder(ctx context.Context, orderID string) error
}

type builtinPolymarketSnapshot struct {
	companyID     string
	positions     []polymarket.Position
	orders        []polymarket.Order
	reviewContext builtinPolymarketReviewContext
}

type builtinPolymarketFindMarketCandidate struct {
	market     polymarket.Market
	endAt      time.Time
	lastNoteAt string
	noteCount  int
	score      float64
	spread     float64
	daysToEnd  float64
}

type builtinPolymarketPayload struct {
	ConditionID          string                      `json:"condition_id"`
	EstimatedProbability float64                     `json:"estimated_probability"`
	Confidence           float64                     `json:"confidence"`
	Question             string                      `json:"question"`
	Reasoning            string                      `json:"reasoning"`
	RemainingCapacity    float64                     `json:"remaining_capacity"`
	ResolutionDate       string                      `json:"resolution_date"`
	AUM                  float64                     `json:"aum"`
	MaxAllowed           float64                     `json:"max_allowed"`
	CurrentPosition      float64                     `json:"current_position"`
	Tokens               []builtinPolymarketTokenRef `json:"tokens"`
}

type builtinPolymarketTokenRef struct {
	Outcome string `json:"outcome"`
	TokenID string `json:"token_id"`
}

func (r *builtinPolymarketTokenRef) UnmarshalJSON(data []byte) error {
	type alias builtinPolymarketTokenRef

	var structured alias
	if err := json.Unmarshal(data, &structured); err == nil {
		*r = builtinPolymarketTokenRef{
			Outcome: strings.TrimSpace(structured.Outcome),
			TokenID: strings.TrimSpace(structured.TokenID),
		}
		return nil
	}

	var tokenID string
	if err := json.Unmarshal(data, &tokenID); err == nil {
		*r = builtinPolymarketTokenRef{TokenID: strings.TrimSpace(tokenID)}
		return nil
	}

	return fmt.Errorf("expected token reference object or string")
}

type builtinPolymarketTradeCandidate struct {
	Side         string
	TokenID      string
	AskPrice     float64
	BidPrice     float64
	AbsoluteEdge float64
	RelativeEdge float64
}

type builtinPolymarketExposure struct {
	YesHeldShares       float64
	NoHeldShares        float64
	YesOpenBuyShares    float64
	NoOpenBuyShares     float64
	YesLockedSellShares float64
	NoLockedSellShares  float64
}

type builtinPolymarketQuote struct {
	TokenID  string
	AskPrice float64
	BidPrice float64
	HasAsk   bool
	HasBid   bool
}

type builtinPolymarketQuotes struct {
	Yes builtinPolymarketQuote
	No  builtinPolymarketQuote
}

type builtinPolymarketExecutionMetrics struct {
	TokenID      string
	BestBid      float64
	BestAsk      float64
	BestBidSize  float64
	BestAskSize  float64
	Spread       float64
	Midpoint     float64
	HasBid       bool
	HasAsk       bool
	HasOrderBook bool
}

type builtinPolymarketThesisDrift struct {
	HasPrior               bool
	CurrentSide            string
	PriorSide              string
	PriorConfidence        float64
	PriorEstimatedProb     float64
	PriorSideProbability   float64
	CurrentSideProbability float64
	ConfidenceDelta        float64
	ProbabilityDelta       float64
	Severity               float64
	RetentionScale         float64
	ThesisChanged          bool
	SideChanged            bool
	BlockNewExposure       bool
	Reason                 string
	ReferenceTimestamp     time.Time
}

type builtinPolymarketExecutionSignal struct {
	DesiredShares   float64
	SpreadCost      float64
	SlippagePenalty float64
	NetEdge         float64
	Scale           float64
}

type builtinPolymarketReviewContext struct {
	BalanceLoaded            bool
	USDCBalance              float64
	LiquidUSDBalance         float64
	PositionValue            float64
	AUM                      float64
	MaxAllowed               float64
	CurrentSharesByCondition map[string]float64
}

const (
	builtinPolymarketFindMarketsDefaultLimit    = 10
	builtinPolymarketFindMarketsMaxLimit        = 50
	builtinPolymarketFindMarketsRecentNoteDays  = 7
	builtinPolymarketFindMarketsListPageSize    = 100
	builtinPolymarketFindMarketsMaxSearchWindow = 100
	builtinPolymarketFindMarketsMinVolume       = 50000.0
	builtinPolymarketMinOrderShares             = 5.0
	builtinPolymarketSizingCacheTTL             = 15 * time.Second
	builtinPolymarketThesisActiveDays           = 14
)

type builtinPolymarketSizingCacheEntry struct {
	reviewContext builtinPolymarketReviewContext
	createdAt     time.Time
}

var builtinPolymarketSizingCache = struct {
	mu      sync.Mutex
	entries map[string]builtinPolymarketSizingCacheEntry
}{
	entries: make(map[string]builtinPolymarketSizingCacheEntry),
}

var builtinPipelineMethodHandlers = map[string]builtinPipelineMethodHandler{
	spec.BuiltinPolymarketFindMarkets:    pipelineBuiltinPolymarketFindMarkets,
	spec.BuiltinPolymarketSnapshot:       pipelineBuiltinPolymarketSnapshot,
	spec.BuiltinPolymarketManagePosition: pipelineBuiltinPolymarketManagePosition,
}

var getBuiltinPolymarketClient = func(ctx context.Context, pe *PipelineEngine, companyID string) (builtinPolymarketClient, string, error) {
	if pe == nil || pe.service == nil {
		return nil, "", fmt.Errorf("pipeline service is not configured")
	}
	poly := pe.getPolymarketHelper()
	if poly == nil {
		return nil, "", fmt.Errorf("pipeline polymarket helper is not configured")
	}
	client, resolvedCompanyID, err := poly.getClientForCompany(ctx, companyID)
	if err != nil {
		return nil, "", err
	}
	return client, resolvedCompanyID, nil
}

func getBuiltinPolymarketBalanceWallet(ctx context.Context, pe *PipelineEngine, companyID string) (*gowild_crypto.Wallet, error) {
	if pe == nil || pe.service == nil {
		return nil, fmt.Errorf("pipeline service is not configured")
	}
	companyID = strings.TrimSpace(companyID)
	if companyID == "" {
		return nil, fmt.Errorf("company_id is required")
	}

	seedPhrase, err := pe.service.EnsureCompanyWalletSeedPhrase(ctx, companyID)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure company wallet seed phrase: %w", err)
	}
	derived, err := gowild_crypto.DeriveKeysFromMnemonic(seedPhrase, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to derive wallet keys: %w", err)
	}

	wallet, err := gowild_crypto.NewWallet(gowild_crypto.WalletConfig{
		EthPrivateKey: derived.EthPrivateKey,
		EthRPCURL:     pe.getWalletHelper().resolvePolygonRPCURL(ctx, companyID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create wallet client: %w", err)
	}
	return wallet, nil
}

func getBuiltinPolymarketTokenBalance(ctx context.Context, pe *PipelineEngine, companyID, tokenAddress string) (float64, error) {
	wallet, err := getBuiltinPolymarketBalanceWallet(ctx, pe, companyID)
	if err != nil {
		return 0, err
	}
	balance, err := wallet.GetTokenBalance(ctx, gowild_crypto.ChainEthereum, tokenAddress)
	if err != nil {
		return 0, fmt.Errorf("failed to get polygon token balance: %w", err)
	}
	amount, err := strconv.ParseFloat(strings.TrimSpace(balance.Balance), 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse polygon token balance %q: %w", strings.TrimSpace(balance.Balance), err)
	}
	return amount, nil
}

var getBuiltinPolymarketUSDCBalance = func(ctx context.Context, pe *PipelineEngine, companyID string) (float64, error) {
	amount, err := getBuiltinPolymarketTokenBalance(ctx, pe, companyID, polygonUSDCeTokenAddress())
	if err != nil {
		return 0, fmt.Errorf("failed to get polygon usdc balance: %w", err)
	}
	return amount, nil
}

var getBuiltinPolymarketLiquidUSDBalance = func(ctx context.Context, pe *PipelineEngine, companyID string) (float64, error) {
	usdcBalance, err := getBuiltinPolymarketUSDCBalance(ctx, pe, companyID)
	if err != nil {
		return 0, err
	}
	total := usdcBalance
	if usdteBalance, usdteErr := getBuiltinPolymarketTokenBalance(ctx, pe, companyID, polygonUSDTeTokenAddress()); usdteErr == nil {
		total += usdteBalance
	}
	return total, nil
}

func init() {
	spec.SetBuiltinMethodValidator(func(method string) bool {
		_, ok := builtinPipelineMethodHandlers[spec.NormalizeBuiltinMethod(method)]
		return ok
	})
}

func executeBuiltinPipelineMethod(ctx context.Context, pe *PipelineEngine, run *data.PipelineRun, step PipelineStep, params map[string]any) (map[string]any, error) {
	method := normalizeBuiltinPipelineMethod(step.NextMethod)
	handler, ok := builtinPipelineMethodHandlers[method]
	if !ok {
		return nil, fmt.Errorf("unknown builtin method %q", method)
	}
	return handler(ctx, pe, run, step, params)
}

func firstStringParam(params map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringParam(params, key); value != "" {
			return value
		}
	}
	return ""
}

func stringParam(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	v, ok := params[key]
	if !ok {
		return ""
	}
	switch typed := v.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	}
	return ""
}

func boolParam(params map[string]any, key string) (bool, bool) {
	if params == nil {
		return false, false
	}
	v, ok := params[key]
	if !ok {
		return false, false
	}
	switch typed := v.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

func floatParam(params map[string]any, key string) (float64, bool) {
	if params == nil {
		return 0, false
	}
	v, ok := params[key]
	if !ok {
		return 0, false
	}
	switch typed := v.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case json.Number:
		f, err := typed.Float64()
		if err == nil {
			return f, true
		}
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return f, true
		}
	}
	return 0, false
}

func intParam(params map[string]any, key string) (int, bool) {
	if params == nil {
		return 0, false
	}
	v, ok := params[key]
	if !ok {
		return 0, false
	}
	switch typed := v.(type) {
	case int:
		return typed, true
	case int8:
		return int(typed), true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case uint:
		return int(typed), true
	case uint8:
		return int(typed), true
	case uint16:
		return int(typed), true
	case uint32:
		return int(typed), true
	case uint64:
		return int(typed), true
	case float64:
		return int(typed), true
	case float32:
		return int(typed), true
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed), true
		}
		if parsed, err := typed.Float64(); err == nil {
			return int(parsed), true
		}
	case string:
		raw := strings.TrimSpace(typed)
		if raw == "" {
			return 0, false
		}
		if parsed, err := strconv.Atoi(raw); err == nil {
			return parsed, true
		}
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
			return int(parsed), true
		}
	}
	return 0, false
}

func normalizePolymarketSide(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case polymarket.Buy:
		return polymarket.Buy
	case polymarket.Sell:
		return polymarket.Sell
	default:
		return ""
	}
}

func normalizePolymarketOrderType(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "":
		return ""
	case polymarket.GTC:
		return polymarket.GTC
	case polymarket.FOK:
		return polymarket.FOK
	case polymarket.GTD:
		return polymarket.GTD
	default:
		return ""
	}
}

func roundBuiltinPolymarketFloat(value float64, places int) float64 {
	if places < 0 {
		return value
	}
	pow := math.Pow(10, float64(places))
	return math.Round(value*pow) / pow
}
