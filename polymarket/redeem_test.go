package gowild_polymarket

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestBuildRedeemTargetsFromPositions_AllRedeemable(t *testing.T) {
	positions := []Position{
		{
			ConditionID:  "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			OutcomeIndex: 0,
			Size:         10,
			CurPrice:     1,
			Redeemable:   true,
		},
		{
			ConditionID:  "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			OutcomeIndex: 1,
			Size:         5,
			CurPrice:     1,
			Redeemable:   true,
		},
		{
			ConditionID:  "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			OutcomeIndex: 0,
			Size:         2,
			CurPrice:     1,
			Redeemable:   true,
		},
		{
			ConditionID:  "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			OutcomeIndex: 0,
			Size:         3,
			Redeemable:   false,
		},
	}

	targets, err := buildRedeemTargetsFromPositions(positions, "")
	if err != nil {
		t.Fatalf("buildRedeemTargetsFromPositions failed: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 condition targets, got %d", len(targets))
	}

	if targets[0].conditionID != "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("unexpected first condition: %s", targets[0].conditionID)
	}
	if len(targets[0].indexSets) != 2 {
		t.Fatalf("expected two index sets for first condition, got %d", len(targets[0].indexSets))
	}
	if targets[0].indexSets[0].String() != "1" || targets[0].indexSets[1].String() != "2" {
		t.Fatalf("unexpected index sets for first condition: %s, %s", targets[0].indexSets[0], targets[0].indexSets[1])
	}

	if targets[1].conditionID != "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("unexpected second condition: %s", targets[1].conditionID)
	}
	if len(targets[1].indexSets) != 1 || targets[1].indexSets[0].String() != "1" {
		t.Fatalf("unexpected index sets for second condition: %+v", targets[1].indexSets)
	}
}

func TestBuildRedeemTargetsFromPositions_FilterByCondition(t *testing.T) {
	positions := []Position{
		{
			ConditionID:  "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			OutcomeIndex: 0,
			Size:         10,
			CurPrice:     1,
			Redeemable:   true,
		},
		{
			ConditionID:  "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			OutcomeIndex: 1,
			Size:         2,
			CurPrice:     1,
			Redeemable:   true,
		},
	}

	targets, err := buildRedeemTargetsFromPositions(positions, "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatalf("buildRedeemTargetsFromPositions failed: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected 1 condition target, got %d", len(targets))
	}
	if targets[0].conditionID != "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("unexpected condition ID: %s", targets[0].conditionID)
	}
	if len(targets[0].indexSets) != 1 || targets[0].indexSets[0].String() != "2" {
		t.Fatalf("unexpected index sets: %+v", targets[0].indexSets)
	}
}

func TestBuildRedeemTargetsFromPositions_ErrorsWhenMissing(t *testing.T) {
	positions := []Position{
		{
			ConditionID:  "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			OutcomeIndex: 0,
			Size:         10,
			Redeemable:   false,
		},
	}

	if _, err := buildRedeemTargetsFromPositions(positions, "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err == nil {
		t.Fatal("expected error when condition has no redeemable positions")
	}
	if _, err := buildRedeemTargetsFromPositions(positions, ""); err == nil {
		t.Fatal("expected error when there are no redeemable positions")
	}
}

func TestBuildRedeemTargetsFromPositions_InvalidFilter(t *testing.T) {
	positions := []Position{
		{
			ConditionID:  "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			OutcomeIndex: 0,
			Size:         1,
			CurPrice:     1,
			Redeemable:   true,
		},
	}

	if _, err := buildRedeemTargetsFromPositions(positions, "not-a-condition-id"); err == nil {
		t.Fatal("expected error for malformed condition filter")
	}
}

func TestBuildRedeemTargetsFromPositions_SkipsNegativeRisk(t *testing.T) {
	standardCondition := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	negRiskCondition := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	positions := []Position{
		{
			ConditionID:  standardCondition,
			OutcomeIndex: 0,
			Size:         10,
			CurPrice:     1,
			Redeemable:   true,
		},
		{
			ConditionID:  negRiskCondition,
			OutcomeIndex: 1,
			Size:         2,
			CurPrice:     1,
			Redeemable:   true,
			NegativeRisk: true,
		},
	}

	targets, err := buildRedeemTargetsFromPositions(positions, "")
	if err != nil {
		t.Fatalf("buildRedeemTargetsFromPositions failed: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("expected only non-neg-risk condition, got %d targets", len(targets))
	}
	if targets[0].conditionID != standardCondition {
		t.Fatalf("expected standard condition %s, got %s", standardCondition, targets[0].conditionID)
	}

	if _, err := buildRedeemTargetsFromPositions(positions, negRiskCondition); err == nil {
		t.Fatal("expected filtered neg-risk condition to be excluded from standard redeem targets")
	}
}

func TestBuildNegRiskRedeemTargetsFromPositions_FilterNoMatchReturnsEmpty(t *testing.T) {
	standardCondition := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	filterCondition := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	positions := []Position{
		{
			ConditionID:  standardCondition,
			OutcomeIndex: 0,
			Size:         1,
			CurPrice:     1,
			Redeemable:   true,
			NegativeRisk: false,
		},
	}

	targets, err := buildNegRiskRedeemTargetsFromPositions(
		context.Background(),
		nil,
		abi.ABI{},
		common.Address{},
		common.Address{},
		positions,
		filterCondition,
		false,
	)
	if err != nil {
		t.Fatalf("buildNegRiskRedeemTargetsFromPositions returned unexpected error: %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("expected no neg-risk targets, got %d", len(targets))
	}
}

func TestSanitizeExplicitIndexSets(t *testing.T) {
	indexSets, err := sanitizeExplicitIndexSets([]int{2, 1, 2})
	if err != nil {
		t.Fatalf("sanitizeExplicitIndexSets failed: %v", err)
	}
	if len(indexSets) != 2 {
		t.Fatalf("expected deduplicated sets length 2, got %d", len(indexSets))
	}
	if indexSets[0].String() != "1" || indexSets[1].String() != "2" {
		t.Fatalf("unexpected normalized sets: %s, %s", indexSets[0], indexSets[1])
	}

	if _, err := sanitizeExplicitIndexSets([]int{0}); err == nil {
		t.Fatal("expected error for non-positive index set")
	}
}

func TestIndexSetToSingleOutcomeIndex(t *testing.T) {
	tests := []struct {
		name     string
		indexSet *big.Int
		want     int
		ok       bool
	}{
		{name: "index 0", indexSet: big.NewInt(1), want: 0, ok: true},
		{name: "index 1", indexSet: big.NewInt(2), want: 1, ok: true},
		{name: "index 7", indexSet: big.NewInt(128), want: 7, ok: true},
		{name: "not power of two", indexSet: big.NewInt(3), ok: false},
		{name: "zero", indexSet: big.NewInt(0), ok: false},
		{name: "nil", indexSet: nil, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := indexSetToSingleOutcomeIndex(tt.indexSet)
			if ok != tt.ok {
				t.Fatalf("expected ok=%v, got %v", tt.ok, ok)
			}
			if ok && got != tt.want {
				t.Fatalf("expected outcome index %d, got %d", tt.want, got)
			}
		})
	}
}

func TestNegativeRiskOutcomeSlot(t *testing.T) {
	tests := []struct {
		name string
		pos  Position
		want int
		ok   bool
	}{
		{name: "yes by label", pos: Position{Outcome: "Yes", OutcomeIndex: 1}, want: 0, ok: true},
		{name: "no by label", pos: Position{Outcome: "No", OutcomeIndex: 0}, want: 1, ok: true},
		{name: "fallback index 0", pos: Position{Outcome: "", OutcomeIndex: 0}, want: 0, ok: true},
		{name: "fallback index 1", pos: Position{Outcome: "", OutcomeIndex: 1}, want: 1, ok: true},
		{name: "invalid", pos: Position{Outcome: "Maybe", OutcomeIndex: 2}, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := negativeRiskOutcomeSlot(tt.pos)
			if ok != tt.ok {
				t.Fatalf("expected ok=%v, got %v", tt.ok, ok)
			}
			if ok && got != tt.want {
				t.Fatalf("expected slot %d, got %d", tt.want, got)
			}
		})
	}
}

func TestParseConditionID(t *testing.T) {
	cond := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hash, err := parseConditionID(cond)
	if err != nil {
		t.Fatalf("parseConditionID failed: %v", err)
	}
	if hash.Hex() != cond {
		t.Fatalf("unexpected hash: %s", hash.Hex())
	}

	if _, err := parseConditionID("0xabc"); err == nil {
		t.Fatal("expected error for short condition id")
	}
}

func TestParsePayoutRedemption_MatchingEvent(t *testing.T) {
	ctABI, err := abi.JSON(strings.NewReader(conditionalTokensABI))
	if err != nil {
		t.Fatalf("parse ABI failed: %v", err)
	}
	event := ctABI.Events["PayoutRedemption"]

	redeemer := common.HexToAddress("0x1111111111111111111111111111111111111111")
	collateral := common.HexToAddress(USDCAddress)
	parentCollectionID := common.Hash{}
	conditionHash := common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	data, err := event.Inputs.NonIndexed().Pack(conditionHash, []*big.Int{big.NewInt(1), big.NewInt(2)}, big.NewInt(123456))
	if err != nil {
		t.Fatalf("pack event data failed: %v", err)
	}

	receipt := &types.Receipt{
		Logs: []*types.Log{
			{
				Address: common.HexToAddress(conditionalTokensAddress),
				Topics: []common.Hash{
					event.ID,
					addressTopic(redeemer),
					addressTopic(collateral),
					parentCollectionID,
				},
				Data: data,
			},
		},
	}

	payout, err := parsePayoutRedemption(
		ctABI,
		receipt,
		common.HexToAddress(conditionalTokensAddress),
		redeemer,
		collateral,
		parentCollectionID,
		conditionHash,
	)
	if err != nil {
		t.Fatalf("parsePayoutRedemption failed: %v", err)
	}
	if payout.Cmp(big.NewInt(123456)) != 0 {
		t.Fatalf("unexpected payout %s", payout.String())
	}
}

func TestParsePayoutRedemption_NoMatch(t *testing.T) {
	ctABI, err := abi.JSON(strings.NewReader(conditionalTokensABI))
	if err != nil {
		t.Fatalf("parse ABI failed: %v", err)
	}
	event := ctABI.Events["PayoutRedemption"]

	redeemer := common.HexToAddress("0x1111111111111111111111111111111111111111")
	collateral := common.HexToAddress(USDCAddress)
	parentCollectionID := common.Hash{}
	conditionHash := common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	otherCondition := common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	data, err := event.Inputs.NonIndexed().Pack(otherCondition, []*big.Int{big.NewInt(1)}, big.NewInt(50))
	if err != nil {
		t.Fatalf("pack event data failed: %v", err)
	}

	receipt := &types.Receipt{
		Logs: []*types.Log{
			{
				Address: common.HexToAddress(conditionalTokensAddress),
				Topics: []common.Hash{
					event.ID,
					addressTopic(redeemer),
					addressTopic(collateral),
					parentCollectionID,
				},
				Data: data,
			},
		},
	}

	if _, err := parsePayoutRedemption(
		ctABI,
		receipt,
		common.HexToAddress(conditionalTokensAddress),
		redeemer,
		collateral,
		parentCollectionID,
		conditionHash,
	); err == nil {
		t.Fatal("expected parsePayoutRedemption to fail when no matching condition event exists")
	}
}

func TestParseNegRiskPayoutRedemption_MatchingEvent(t *testing.T) {
	adapterABI, err := abi.JSON(strings.NewReader(negRiskAdapterABI))
	if err != nil {
		t.Fatalf("parse ABI failed: %v", err)
	}
	event := adapterABI.Events["PayoutRedemption"]

	redeemer := common.HexToAddress("0x1111111111111111111111111111111111111111")
	adapter := common.HexToAddress(negRiskAdapterAddress)
	conditionHash := common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	data, err := event.Inputs.NonIndexed().Pack([]*big.Int{big.NewInt(10), big.NewInt(0)}, big.NewInt(10))
	if err != nil {
		t.Fatalf("pack event data failed: %v", err)
	}

	receipt := &types.Receipt{
		Logs: []*types.Log{
			{
				Address: adapter,
				Topics: []common.Hash{
					event.ID,
					addressTopic(redeemer),
					conditionHash,
				},
				Data: data,
			},
		},
	}

	payout, err := parseNegRiskPayoutRedemption(adapterABI, receipt, adapter, redeemer, conditionHash)
	if err != nil {
		t.Fatalf("parseNegRiskPayoutRedemption failed: %v", err)
	}
	if payout.Cmp(big.NewInt(10)) != 0 {
		t.Fatalf("unexpected payout %s", payout.String())
	}
}

func TestIsNoRedeemablePositionsError(t *testing.T) {
	if !isNoRedeemablePositionsError(errors.New("no redeemable positions found")) {
		t.Fatal("expected no-redeemable message to be detected")
	}
	if isNoRedeemablePositionsError(errors.New("something else failed")) {
		t.Fatal("expected unrelated error not to be detected")
	}
}

func TestIsRPCAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "tenant disabled", err: errors.New(`401 Unauthorized: {"error":"message: API key disabled, reason: tenant disabled, json-rpc code: -32051, rest code: 403"}`), want: true},
		{name: "api key disabled", err: errors.New("API key disabled, reason: tenant disabled"), want: true},
		{name: "status 401", err: errors.New("rpc returned status 401"), want: true},
		{name: "rest code 403", err: errors.New("rest code: 403"), want: true},
		{name: "unauthorized", err: errors.New("401 Unauthorized"), want: true},
		{name: "rate limit", err: errors.New("too many requests"), want: false},
		{name: "timeout", err: errors.New("i/o timeout"), want: false},
		{name: "nil", err: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRPCAuthError(tt.err)
			if got != tt.want {
				t.Fatalf("isRPCAuthError(%q) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsRetryableRPCError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "rate limit", err: errors.New("Too many requests, reason: call rate limit exhausted, retry in 10s"), want: true},
		{name: "429", err: errors.New("rpc error: 429"), want: true},
		{name: "timeout", err: errors.New("i/o timeout"), want: true},
		{name: "non retryable", err: errors.New("execution reverted"), want: false},
		{name: "nil", err: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryableRPCError(tt.err)
			if got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestRetryDelayFromRPCError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		fallback time.Duration
		want     time.Duration
	}{
		{
			name:     "extract retry delay",
			err:      errors.New("Too many requests, reason: call rate limit exhausted, retry in 10s"),
			fallback: 3 * time.Second,
			want:     10 * time.Second,
		},
		{
			name:     "fallback on missing retry hint",
			err:      errors.New("too many requests"),
			fallback: 4 * time.Second,
			want:     4 * time.Second,
		},
		{
			name:     "fallback on nil error",
			err:      nil,
			fallback: 6 * time.Second,
			want:     6 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := retryDelayFromRPCError(tt.err, tt.fallback)
			if got != tt.want {
				t.Fatalf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

func addressTopic(addr common.Address) common.Hash {
	return common.BytesToHash(common.LeftPadBytes(addr.Bytes(), 32))
}

func TestZeroPayoutRedeemError_IncludesDebugDetails(t *testing.T) {
	err := zeroPayoutRedeemError(&RedeemWinningsResult{
		CollateralTokenAddress: USDCAddress,
		ConditionsSubmitted:    1,
		Transactions: []RedeemWinningsTx{
			{
				ConditionID:      "0xabc",
				TransactionHash:  "0x123",
				IndexSets:        []string{"1", "2"},
				CollateralPayout: "0",
			},
		},
	})
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "condition=0xabc") {
		t.Fatalf("expected condition details in error, got: %s", msg)
	}
	if !strings.Contains(msg, "tx=0x123") {
		t.Fatalf("expected tx hash in error, got: %s", msg)
	}
	if !strings.Contains(msg, "index_sets=[1,2]") {
		t.Fatalf("expected index sets in error, got: %s", msg)
	}
	if !strings.Contains(msg, USDCAddress) {
		t.Fatalf("expected collateral address in error, got: %s", msg)
	}
}
