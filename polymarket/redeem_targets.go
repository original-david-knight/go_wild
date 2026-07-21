package gowild_polymarket

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

func buildNegRiskRedeemTargetsFromPositions(
	ctx context.Context,
	client *ethclient.Client,
	ctABI abi.ABI,
	ctAddress common.Address,
	redeemer common.Address,
	positions []Position,
	conditionFilter string,
	includeLosing bool,
) ([]negRiskRedeemTarget, error) {
	normalizedFilter := ""
	if strings.TrimSpace(conditionFilter) != "" {
		normalizedFilter = normalizeConditionIDString(conditionFilter)
		if normalizedFilter == "" {
			return nil, fmt.Errorf("invalid condition_id %s: expected 32-byte hex value", conditionFilter)
		}
	}

	type amounts struct {
		yes *big.Int
		no  *big.Int
	}
	byCondition := map[string]*amounts{}
	payoutDenominatorCache := map[string]*big.Int{}
	payoutNumeratorCache := map[string]map[int]*big.Int{}

	for _, p := range positions {
		if !p.NegativeRisk || !p.Redeemable || p.Size <= 0 {
			continue
		}
		normalizedCondition := normalizeConditionIDString(p.ConditionID)
		if normalizedCondition == "" {
			continue
		}
		if normalizedFilter != "" && normalizedCondition != normalizedFilter {
			continue
		}

		slot, ok := negativeRiskOutcomeSlot(p)
		if !ok {
			continue
		}

		conditionHash, err := parseConditionID(normalizedCondition)
		if err != nil {
			return nil, err
		}

		denominator := payoutDenominatorCache[normalizedCondition]
		if denominator == nil {
			denominator, err = callUint256Method(ctx, client, ctABI, ctAddress, "payoutDenominator", conditionHash)
			if err != nil {
				return nil, fmt.Errorf("failed to inspect payout denominator for condition %s: %w", normalizedCondition, err)
			}
			payoutDenominatorCache[normalizedCondition] = denominator
		}
		if denominator.Sign() == 0 {
			// Oracle hasn't finalized — on-chain redeemPositions will revert.
			continue
		}
		if !includeLosing {
			numeratorBySlot := payoutNumeratorCache[normalizedCondition]
			if numeratorBySlot == nil {
				numeratorBySlot = map[int]*big.Int{}
				payoutNumeratorCache[normalizedCondition] = numeratorBySlot
			}
			numerator := numeratorBySlot[slot]
			if numerator == nil {
				numerator, err = callUint256Method(
					ctx,
					client,
					ctABI,
					ctAddress,
					"payoutNumerators",
					conditionHash,
					big.NewInt(int64(slot)),
				)
				if err != nil {
					return nil, fmt.Errorf("failed to inspect payout numerator for condition %s: %w", normalizedCondition, err)
				}
				numeratorBySlot[slot] = numerator
			}
			if numerator.Sign() == 0 {
				continue
			}
		}

		positionID, ok := new(big.Int).SetString(strings.TrimSpace(p.Asset), 10)
		if !ok || positionID == nil {
			continue
		}
		balance, err := callUint256Method(ctx, client, ctABI, ctAddress, "balanceOf", redeemer, positionID)
		if err != nil {
			return nil, fmt.Errorf("failed to inspect balance for neg-risk position %s (condition %s): %w", p.Asset, normalizedCondition, err)
		}
		if balance.Sign() <= 0 {
			continue
		}

		entry := byCondition[normalizedCondition]
		if entry == nil {
			entry = &amounts{
				yes: big.NewInt(0),
				no:  big.NewInt(0),
			}
			byCondition[normalizedCondition] = entry
		}
		if slot == 0 {
			entry.yes.Add(entry.yes, balance)
		} else {
			entry.no.Add(entry.no, balance)
		}
	}

	if len(byCondition) == 0 {
		return nil, nil
	}

	conditions := make([]string, 0, len(byCondition))
	for conditionID := range byCondition {
		conditions = append(conditions, conditionID)
	}
	sort.Strings(conditions)

	targets := make([]negRiskRedeemTarget, 0, len(conditions))
	for _, conditionID := range conditions {
		entry := byCondition[conditionID]
		if entry == nil {
			continue
		}
		if entry.yes.Sign() <= 0 && entry.no.Sign() <= 0 {
			continue
		}
		targets = append(targets, negRiskRedeemTarget{
			conditionID: conditionID,
			yesAmount:   new(big.Int).Set(entry.yes),
			noAmount:    new(big.Int).Set(entry.no),
		})
	}
	return targets, nil
}

func negativeRiskOutcomeSlot(p Position) (int, bool) {
	outcome := strings.ToLower(strings.TrimSpace(p.Outcome))
	switch outcome {
	case "yes":
		return 0, true
	case "no":
		return 1, true
	}

	if p.OutcomeIndex == 0 || p.OutcomeIndex == 1 {
		return p.OutcomeIndex, true
	}
	return 0, false
}

func isNoRedeemablePositionsError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no redeemable positions found")
}

func (c *Client) buildRedeemTargets(ctx context.Context, conditionID string, explicitIndexSets []int) ([]redeemTarget, error) {
	conditionID = strings.TrimSpace(conditionID)

	if conditionID != "" {
		if len(explicitIndexSets) > 0 {
			indexSets, err := sanitizeExplicitIndexSets(explicitIndexSets)
			if err != nil {
				return nil, err
			}
			return []redeemTarget{{conditionID: conditionID, indexSets: indexSets}}, nil
		}

		positions, err := c.GetPositions(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list positions: %w", err)
		}
		targets, err := buildRedeemTargetsFromPositions(positions, conditionID)
		if err != nil {
			return nil, err
		}
		return targets, nil
	}

	if len(explicitIndexSets) > 0 {
		return nil, fmt.Errorf("index_sets requires condition_id")
	}

	positions, err := c.GetPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list positions: %w", err)
	}
	targets, err := buildRedeemTargetsFromPositions(positions, "")
	if err != nil {
		return nil, err
	}
	return targets, nil
}

func buildRedeemTargetsFromPositions(positions []Position, conditionFilter string) ([]redeemTarget, error) {
	conditionToSets := map[string]map[string]*big.Int{}
	normalizedFilter := ""
	if strings.TrimSpace(conditionFilter) != "" {
		normalizedFilter = normalizeConditionIDString(conditionFilter)
		if normalizedFilter == "" {
			return nil, fmt.Errorf("invalid condition_id %s: expected 32-byte hex value", conditionFilter)
		}
	}

	for _, p := range positions {
		if p.NegativeRisk || !p.Redeemable || p.Size <= 0 {
			continue
		}
		if p.ConditionID == "" {
			continue
		}
		normalizedCondition := normalizeConditionIDString(p.ConditionID)
		if normalizedCondition == "" {
			continue
		}
		if normalizedFilter != "" && normalizedCondition != normalizedFilter {
			continue
		}

		indexSet, err := outcomeIndexToIndexSet(p.OutcomeIndex)
		if err != nil {
			return nil, fmt.Errorf("invalid outcome index %d for condition %s: %w", p.OutcomeIndex, p.ConditionID, err)
		}

		setMap := conditionToSets[normalizedCondition]
		if setMap == nil {
			setMap = map[string]*big.Int{}
			conditionToSets[normalizedCondition] = setMap
		}
		setMap[indexSet.String()] = indexSet
	}

	if normalizedFilter != "" {
		if _, ok := conditionToSets[normalizedFilter]; !ok {
			return nil, fmt.Errorf("no redeemable positions found for condition %s", conditionFilter)
		}
	}

	if len(conditionToSets) == 0 {
		return nil, fmt.Errorf("no redeemable positions found")
	}

	conditions := make([]string, 0, len(conditionToSets))
	for conditionID := range conditionToSets {
		conditions = append(conditions, conditionID)
	}
	sort.Strings(conditions)

	targets := make([]redeemTarget, 0, len(conditions))
	for _, conditionID := range conditions {
		setMap := conditionToSets[conditionID]
		indexSetValues := make([]*big.Int, 0, len(setMap))
		for _, set := range setMap {
			indexSetValues = append(indexSetValues, new(big.Int).Set(set))
		}
		sort.Slice(indexSetValues, func(i, j int) bool {
			return indexSetValues[i].Cmp(indexSetValues[j]) < 0
		})
		targets = append(targets, redeemTarget{
			conditionID: conditionID,
			indexSets:   indexSetValues,
		})
	}

	return targets, nil
}

func sanitizeExplicitIndexSets(indexSets []int) ([]*big.Int, error) {
	dedup := map[string]*big.Int{}
	for _, set := range indexSets {
		if set <= 0 {
			return nil, fmt.Errorf("index_sets entries must be positive integers")
		}
		value := big.NewInt(int64(set))
		dedup[value.String()] = value
	}
	if len(dedup) == 0 {
		return nil, fmt.Errorf("index_sets cannot be empty")
	}

	values := make([]*big.Int, 0, len(dedup))
	for _, set := range dedup {
		values = append(values, new(big.Int).Set(set))
	}
	sort.Slice(values, func(i, j int) bool {
		return values[i].Cmp(values[j]) < 0
	})
	return values, nil
}

func parseConditionID(conditionID string) (common.Hash, error) {
	if strings.TrimSpace(conditionID) == "" {
		return common.Hash{}, fmt.Errorf("condition_id is required")
	}

	normalized := normalizeConditionIDString(conditionID)
	if normalized == "" {
		return common.Hash{}, fmt.Errorf("invalid condition_id %s: expected 32-byte hex value", conditionID)
	}

	trimmed := strings.TrimPrefix(normalized, "0x")
	raw, err := hex.DecodeString(trimmed)
	if err != nil {
		return common.Hash{}, fmt.Errorf("invalid condition_id %s: %w", conditionID, err)
	}
	if len(raw) != 32 {
		return common.Hash{}, fmt.Errorf("invalid condition_id %s: expected 32 bytes", conditionID)
	}
	return common.BytesToHash(raw), nil
}

func normalizeConditionIDString(conditionID string) string {
	conditionID = strings.TrimSpace(strings.ToLower(conditionID))
	if conditionID == "" {
		return ""
	}
	conditionID = strings.TrimPrefix(conditionID, "0x")
	if len(conditionID) != 64 {
		return ""
	}
	return "0x" + conditionID
}

func outcomeIndexToIndexSet(outcomeIndex int) (*big.Int, error) {
	if outcomeIndex < 0 {
		return nil, fmt.Errorf("outcome index must be >= 0")
	}
	if outcomeIndex > 255 {
		return nil, fmt.Errorf("outcome index must be <= 255")
	}
	return new(big.Int).Lsh(big.NewInt(1), uint(outcomeIndex)), nil
}
