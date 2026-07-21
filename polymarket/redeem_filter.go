package gowild_polymarket

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

func selectRedeemCollateralAndTargets(
	ctx context.Context,
	client *ethclient.Client,
	ctABI abi.ABI,
	ctAddress common.Address,
	redeemer common.Address,
	parentCollectionID common.Hash,
	targets []redeemTarget,
	requestedCollateral string,
	includeLosing bool,
) (common.Address, []redeemTarget, error) {
	candidates := make([]common.Address, 0, 2)
	if strings.TrimSpace(requestedCollateral) != "" {
		candidates = append(candidates, common.HexToAddress(requestedCollateral))
	} else {
		candidates = append(candidates, common.HexToAddress(USDCAddress))
		candidates = append(candidates, common.HexToAddress(NativeUSDCAddress))
	}

	seen := map[string]struct{}{}
	dedupedCandidates := make([]common.Address, 0, len(candidates))
	for _, candidate := range candidates {
		key := strings.ToLower(candidate.Hex())
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		dedupedCandidates = append(dedupedCandidates, candidate)
	}

	bestCollateral := common.Address{}
	bestTargets := []redeemTarget(nil)
	bestTotalBalance := big.NewInt(-1)
	tried := make([]string, 0, len(dedupedCandidates))

	for _, candidate := range dedupedCandidates {
		filteredTargets, totalBalance, err := filterRedeemTargetsByCTBalance(
			ctx,
			client,
			ctABI,
			ctAddress,
			redeemer,
			candidate,
			parentCollectionID,
			targets,
			includeLosing,
		)
		if err != nil {
			return common.Address{}, nil, err
		}
		tried = append(tried, candidate.Hex())
		if totalBalance.Cmp(bestTotalBalance) > 0 {
			bestTotalBalance = totalBalance
			bestCollateral = candidate
			bestTargets = filteredTargets
		}
	}

	if len(bestTargets) == 0 {
		if strings.TrimSpace(requestedCollateral) != "" {
			return common.Address{}, nil, fmt.Errorf("no redeemable winning conditional token balances found for requested collateral %s", common.HexToAddress(requestedCollateral).Hex())
		}
		return common.Address{}, nil, fmt.Errorf("no redeemable winning conditional token balances found for redeemable positions using known Polygon USDC collaterals (%s)", strings.Join(tried, ", "))
	}
	return bestCollateral, bestTargets, nil
}

func filterRedeemTargetsByCTBalance(
	ctx context.Context,
	client *ethclient.Client,
	ctABI abi.ABI,
	ctAddress common.Address,
	redeemer common.Address,
	collateral common.Address,
	parentCollectionID common.Hash,
	targets []redeemTarget,
	includeLosing bool,
) ([]redeemTarget, *big.Int, error) {
	filteredTargets := make([]redeemTarget, 0, len(targets))
	totalBalance := big.NewInt(0)

	for _, target := range targets {
		conditionHash, err := parseConditionID(target.conditionID)
		if err != nil {
			return nil, nil, err
		}
		payoutDenominator, err := callUint256Method(ctx, client, ctABI, ctAddress, "payoutDenominator", conditionHash)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to inspect payout denominator for condition %s: %w", target.conditionID, err)
		}
		hasResolvedPayouts := payoutDenominator.Sign() > 0
		if !hasResolvedPayouts {
			// Oracle hasn't finalized — on-chain redeemPositions will revert.
			continue
		}
		winningOutcomeCache := map[int]bool{}

		filteredSets := make([]*big.Int, 0, len(target.indexSets))
		for _, indexSet := range target.indexSets {
			if hasResolvedPayouts && !includeLosing {
				hasPayout, err := indexSetHasPositivePayout(
					ctx,
					client,
					ctABI,
					ctAddress,
					conditionHash,
					indexSet,
					winningOutcomeCache,
				)
				if err != nil {
					return nil, nil, fmt.Errorf("failed to inspect payout numerator for condition %s index_set %s: %w", target.conditionID, indexSet.String(), err)
				}
				if !hasPayout {
					continue
				}
			}
			balance, err := conditionalTokenBalanceForIndexSet(
				ctx,
				client,
				ctABI,
				ctAddress,
				redeemer,
				collateral,
				parentCollectionID,
				conditionHash,
				indexSet,
			)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to inspect conditional token balance for condition %s index_set %s: %w", target.conditionID, indexSet.String(), err)
			}
			if balance.Sign() > 0 {
				filteredSets = append(filteredSets, new(big.Int).Set(indexSet))
				totalBalance.Add(totalBalance, balance)
			}
		}

		if len(filteredSets) == 0 {
			continue
		}
		filteredTargets = append(filteredTargets, redeemTarget{
			conditionID: target.conditionID,
			indexSets:   filteredSets,
		})
	}

	return filteredTargets, totalBalance, nil
}

func conditionalTokenBalanceForIndexSet(
	ctx context.Context,
	client *ethclient.Client,
	ctABI abi.ABI,
	ctAddress common.Address,
	redeemer common.Address,
	collateral common.Address,
	parentCollectionID common.Hash,
	conditionHash common.Hash,
	indexSet *big.Int,
) (*big.Int, error) {
	collectionID, err := callBytes32Method(ctx, client, ctABI, ctAddress, "getCollectionId", parentCollectionID, conditionHash, indexSet)
	if err != nil {
		return nil, err
	}
	positionID, err := callUint256Method(ctx, client, ctABI, ctAddress, "getPositionId", collateral, collectionID)
	if err != nil {
		return nil, err
	}
	return callUint256Method(ctx, client, ctABI, ctAddress, "balanceOf", redeemer, positionID)
}

func indexSetHasPositivePayout(
	ctx context.Context,
	client *ethclient.Client,
	ctABI abi.ABI,
	ctAddress common.Address,
	conditionHash common.Hash,
	indexSet *big.Int,
	cache map[int]bool,
) (bool, error) {
	outcomeIndex, ok := indexSetToSingleOutcomeIndex(indexSet)
	if !ok {
		return true, nil
	}
	if hasPayout, exists := cache[outcomeIndex]; exists {
		return hasPayout, nil
	}

	numerator, err := callUint256Method(
		ctx,
		client,
		ctABI,
		ctAddress,
		"payoutNumerators",
		conditionHash,
		big.NewInt(int64(outcomeIndex)),
	)
	if err != nil {
		return false, err
	}
	hasPayout := numerator.Sign() > 0
	cache[outcomeIndex] = hasPayout
	return hasPayout, nil
}

func indexSetToSingleOutcomeIndex(indexSet *big.Int) (int, bool) {
	if indexSet == nil || indexSet.Sign() <= 0 || indexSet.BitLen() > 256 {
		return 0, false
	}

	one := big.NewInt(1)
	minusOne := new(big.Int).Sub(new(big.Int).Set(indexSet), one)
	if new(big.Int).And(new(big.Int).Set(indexSet), minusOne).Sign() != 0 {
		return 0, false
	}

	return indexSet.BitLen() - 1, true
}
