package gowild_polymarket

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// redeemAllWinnings redeems all currently redeemable winnings for the wallet.
func (c *Client) redeemAllWinnings(ctx context.Context) (*RedeemWinningsResult, error) {
	return c.RedeemWinnings(ctx, "", nil, "", false)
}

// RedeemWinnings redeems winnings from resolved Polymarket positions on Polygon.
// Standard markets are settled via ConditionalTokens.redeemPositions; negative-risk
// markets are settled via NegRiskAdapter.redeemPositions.
//
// If conditionID is empty, this redeems all redeemable positions returned by the
// Data API. If conditionID is provided and indexSets is empty, index sets are
// inferred from redeemable positions for that condition.
func (c *Client) RedeemWinnings(ctx context.Context, conditionID string, indexSets []int, collateralTokenAddress string, includeLosing bool) (*RedeemWinningsResult, error) {
	var noRedeemableTargetsErr error
	targets, err := c.buildRedeemTargets(ctx, conditionID, indexSets)
	if err != nil {
		if !isNoRedeemablePositionsError(err) {
			return nil, err
		}
		noRedeemableTargetsErr = err
		targets = nil
	}

	requestedCollateral := strings.TrimSpace(collateralTokenAddress)
	if requestedCollateral != "" && !common.IsHexAddress(requestedCollateral) {
		return nil, fmt.Errorf("invalid collateral token address: %s", requestedCollateral)
	}

	ctABI, err := abi.JSON(strings.NewReader(conditionalTokensABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse conditional tokens ABI: %w", err)
	}
	adapterABI, err := abi.JSON(strings.NewReader(negRiskAdapterABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse neg-risk adapter ABI: %w", err)
	}

	client, rpcURL, err := c.connectPolygonRPC(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	chainID := big.NewInt(polygonChainID)
	from := common.HexToAddress(c.address)
	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return nil, fmt.Errorf("failed to get nonce for %s: %w", from.Hex(), err)
	}

	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to suggest gas price: %w", err)
	}

	ctAddress := common.HexToAddress(conditionalTokensAddress)
	positions, err := c.GetPositions(ctx)
	if err != nil {
		if len(targets) == 0 {
			return nil, fmt.Errorf("failed to list positions: %w", err)
		}
		positions = nil
	}
	negRiskTargets, err := buildNegRiskRedeemTargetsFromPositions(ctx, client, ctABI, ctAddress, from, positions, conditionID, includeLosing)
	if err != nil {
		return nil, err
	}

	parentCollectionID := common.Hash{}
	collateralAddress := common.HexToAddress(USDCAddress)
	filteredTargets := make([]redeemTarget, 0, len(targets))
	var selectionErr error
	if len(targets) > 0 {
		collateralAddress, filteredTargets, err = selectRedeemCollateralAndTargets(
			ctx,
			client,
			ctABI,
			ctAddress,
			from,
			parentCollectionID,
			targets,
			requestedCollateral,
			includeLosing,
		)
		if err != nil {
			selectionErr = err
			if len(negRiskTargets) == 0 {
				return nil, err
			}
		}
	}
	if len(filteredTargets) == 0 && len(negRiskTargets) == 0 {
		if selectionErr != nil {
			return nil, selectionErr
		}
		if noRedeemableTargetsErr != nil {
			return nil, noRedeemableTargetsErr
		}
		return nil, fmt.Errorf("no redeemable positions found")
	}

	result := &RedeemWinningsResult{
		Address:                from.Hex(),
		RPCURL:                 rpcURL,
		CollateralTokenAddress: collateralAddress.Hex(),
		Transactions:           make([]RedeemWinningsTx, 0, len(filteredTargets)+len(negRiskTargets)),
	}
	totalPayout := big.NewInt(0)
	zeroPayoutConditions := make([]string, 0)

	for _, target := range filteredTargets {
		indexSetStrings := make([]string, 0, len(target.indexSets))
		for _, set := range target.indexSets {
			indexSetStrings = append(indexSetStrings, set.String())
		}

		conditionHash, err := parseConditionID(target.conditionID)
		if err != nil {
			result.ConditionsFailed++
			result.Transactions = append(result.Transactions, RedeemWinningsTx{
				ConditionID:   target.conditionID,
				IndexSets:     indexSetStrings,
				ReceiptStatus: "failed",
				Error:         fmt.Sprintf("invalid condition ID: %v", err),
			})
			continue
		}

		data, err := ctABI.Pack("redeemPositions", collateralAddress, parentCollectionID, conditionHash, target.indexSets)
		if err != nil {
			result.ConditionsFailed++
			result.Transactions = append(result.Transactions, RedeemWinningsTx{
				ConditionID:   target.conditionID,
				IndexSets:     indexSetStrings,
				ReceiptStatus: "failed",
				Error:         fmt.Sprintf("encode error: %v", err),
			})
			continue
		}

		msg := ethereum.CallMsg{
			From: from,
			To:   &ctAddress,
			Data: data,
		}
		gasLimit, err := client.EstimateGas(ctx, msg)
		if err != nil {
			result.ConditionsFailed++
			result.Transactions = append(result.Transactions, RedeemWinningsTx{
				ConditionID:   target.conditionID,
				IndexSets:     indexSetStrings,
				ReceiptStatus: "simulate_reverted",
				Error:         fmt.Sprintf("transaction would revert: %v", err),
			})
			continue
		}

		tx := types.NewTransaction(nonce, ctAddress, big.NewInt(0), gasLimit, gasPrice, data)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), c.privateKey)
		if err != nil {
			result.ConditionsFailed++
			result.Transactions = append(result.Transactions, RedeemWinningsTx{
				ConditionID:   target.conditionID,
				IndexSets:     indexSetStrings,
				ReceiptStatus: "failed",
				Error:         fmt.Sprintf("sign error: %v", err),
			})
			continue
		}
		if err := client.SendTransaction(ctx, signedTx); err != nil {
			result.ConditionsFailed++
			txHash := signedTx.Hash().Hex()
			result.Transactions = append(result.Transactions, RedeemWinningsTx{
				ConditionID:     target.conditionID,
				IndexSets:       indexSetStrings,
				TransactionHash: txHash,
				ExplorerURL:     fmt.Sprintf("https://polygonscan.com/tx/%s", txHash),
				ReceiptStatus:   "send_failed",
				Error:           err.Error(),
			})
			continue
		}
		nonce++

		receiptCtx, cancel := context.WithTimeout(ctx, redeemReceiptWaitTimeout)
		receipt, err := waitForReceipt(receiptCtx, client, signedTx.Hash())
		cancel()
		txHash := signedTx.Hash().Hex()
		if err != nil {
			result.ConditionsFailed++
			result.Transactions = append(result.Transactions, RedeemWinningsTx{
				ConditionID:     target.conditionID,
				IndexSets:       indexSetStrings,
				TransactionHash: txHash,
				ExplorerURL:     fmt.Sprintf("https://polygonscan.com/tx/%s", txHash),
				ReceiptStatus:   "pending",
				Error:           fmt.Sprintf("receipt wait failed (tx was submitted): %v", err),
			})
			continue
		}
		if receipt.Status != types.ReceiptStatusSuccessful {
			result.ConditionsFailed++
			result.Transactions = append(result.Transactions, RedeemWinningsTx{
				ConditionID:     target.conditionID,
				IndexSets:       indexSetStrings,
				TransactionHash: txHash,
				ExplorerURL:     fmt.Sprintf("https://polygonscan.com/tx/%s", txHash),
				ReceiptStatus:   "reverted",
				Error:           "transaction reverted on chain",
			})
			continue
		}

		payout, err := parsePayoutRedemption(ctABI, receipt, ctAddress, from, collateralAddress, parentCollectionID, conditionHash)
		if err != nil {
			result.ConditionsFailed++
			result.Transactions = append(result.Transactions, RedeemWinningsTx{
				ConditionID:     target.conditionID,
				IndexSets:       indexSetStrings,
				TransactionHash: txHash,
				ExplorerURL:     fmt.Sprintf("https://polygonscan.com/tx/%s", txHash),
				ReceiptStatus:   "confirmed",
				Error:           fmt.Sprintf("payout parse error: %v", err),
			})
			continue
		}
		totalPayout.Add(totalPayout, payout)
		if payout.Sign() > 0 {
			result.ConditionsRedeemed++
		} else {
			zeroPayoutConditions = append(zeroPayoutConditions, target.conditionID)
		}

		result.Transactions = append(result.Transactions, RedeemWinningsTx{
			ConditionID:      target.conditionID,
			IndexSets:        indexSetStrings,
			TransactionHash:  txHash,
			ExplorerURL:      fmt.Sprintf("https://polygonscan.com/tx/%s", txHash),
			CollateralPayout: payout.String(),
			ReceiptStatus:    "confirmed",
		})
	}

	adapterAddress := common.HexToAddress(negRiskAdapterAddress)
	if len(negRiskTargets) > 0 {
		approved, err := erc1155IsApprovedForAll(ctx, client, ctABI, ctAddress, from, adapterAddress)
		if err != nil {
			return result, fmt.Errorf("failed to check NegRiskAdapter approval: %w", err)
		}
		if !approved {
			log.Printf("NegRiskAdapter not approved on ConditionalTokens, sending setApprovalForAll...")
			if err := c.sendSetApprovalForAllTx(ctx, client, ctABI, ctAddress, from, adapterAddress, nonce, gasPrice, chainID); err != nil {
				return result, fmt.Errorf("failed to approve NegRiskAdapter: %w", err)
			}
			nonce++
		}
	}
	for _, target := range negRiskTargets {
		negRiskIndexSets := []string{"yes_amount=" + target.yesAmount.String(), "no_amount=" + target.noAmount.String()}

		conditionHash, err := parseConditionID(target.conditionID)
		if err != nil {
			result.ConditionsFailed++
			result.Transactions = append(result.Transactions, RedeemWinningsTx{
				ConditionID:   target.conditionID,
				IndexSets:     negRiskIndexSets,
				ReceiptStatus: "failed",
				Error:         fmt.Sprintf("invalid condition ID: %v", err),
			})
			continue
		}

		amounts := []*big.Int{
			new(big.Int).Set(target.yesAmount),
			new(big.Int).Set(target.noAmount),
		}
		data, err := adapterABI.Pack("redeemPositions", conditionHash, amounts)
		if err != nil {
			result.ConditionsFailed++
			result.Transactions = append(result.Transactions, RedeemWinningsTx{
				ConditionID:   target.conditionID,
				IndexSets:     negRiskIndexSets,
				ReceiptStatus: "failed",
				Error:         fmt.Sprintf("encode error: %v", err),
			})
			continue
		}

		msg := ethereum.CallMsg{
			From: from,
			To:   &adapterAddress,
			Data: data,
		}
		gasLimit, err := client.EstimateGas(ctx, msg)
		if err != nil {
			result.ConditionsFailed++
			result.Transactions = append(result.Transactions, RedeemWinningsTx{
				ConditionID:   target.conditionID,
				IndexSets:     negRiskIndexSets,
				ReceiptStatus: "simulate_reverted",
				Error:         fmt.Sprintf("transaction would revert: %v", err),
			})
			continue
		}

		tx := types.NewTransaction(nonce, adapterAddress, big.NewInt(0), gasLimit, gasPrice, data)
		signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), c.privateKey)
		if err != nil {
			result.ConditionsFailed++
			result.Transactions = append(result.Transactions, RedeemWinningsTx{
				ConditionID:   target.conditionID,
				IndexSets:     negRiskIndexSets,
				ReceiptStatus: "failed",
				Error:         fmt.Sprintf("sign error: %v", err),
			})
			continue
		}
		if err := client.SendTransaction(ctx, signedTx); err != nil {
			result.ConditionsFailed++
			txHash := signedTx.Hash().Hex()
			result.Transactions = append(result.Transactions, RedeemWinningsTx{
				ConditionID:     target.conditionID,
				IndexSets:       negRiskIndexSets,
				TransactionHash: txHash,
				ExplorerURL:     fmt.Sprintf("https://polygonscan.com/tx/%s", txHash),
				ReceiptStatus:   "send_failed",
				Error:           err.Error(),
			})
			continue
		}
		nonce++

		receiptCtx, cancel := context.WithTimeout(ctx, redeemReceiptWaitTimeout)
		receipt, err := waitForReceipt(receiptCtx, client, signedTx.Hash())
		cancel()
		txHash := signedTx.Hash().Hex()
		if err != nil {
			result.ConditionsFailed++
			result.Transactions = append(result.Transactions, RedeemWinningsTx{
				ConditionID:     target.conditionID,
				IndexSets:       negRiskIndexSets,
				TransactionHash: txHash,
				ExplorerURL:     fmt.Sprintf("https://polygonscan.com/tx/%s", txHash),
				ReceiptStatus:   "pending",
				Error:           fmt.Sprintf("receipt wait failed (tx was submitted): %v", err),
			})
			continue
		}
		if receipt.Status != types.ReceiptStatusSuccessful {
			result.ConditionsFailed++
			result.Transactions = append(result.Transactions, RedeemWinningsTx{
				ConditionID:     target.conditionID,
				IndexSets:       negRiskIndexSets,
				TransactionHash: txHash,
				ExplorerURL:     fmt.Sprintf("https://polygonscan.com/tx/%s", txHash),
				ReceiptStatus:   "reverted",
				Error:           "transaction reverted on chain",
			})
			continue
		}

		payout, err := parseNegRiskPayoutRedemption(adapterABI, receipt, adapterAddress, from, conditionHash)
		if err != nil {
			result.ConditionsFailed++
			result.Transactions = append(result.Transactions, RedeemWinningsTx{
				ConditionID:     target.conditionID,
				IndexSets:       negRiskIndexSets,
				TransactionHash: txHash,
				ExplorerURL:     fmt.Sprintf("https://polygonscan.com/tx/%s", txHash),
				ReceiptStatus:   "confirmed",
				Error:           fmt.Sprintf("payout parse error: %v", err),
			})
			continue
		}
		totalPayout.Add(totalPayout, payout)
		if payout.Sign() > 0 {
			result.ConditionsRedeemed++
		} else {
			zeroPayoutConditions = append(zeroPayoutConditions, target.conditionID)
		}

		result.Transactions = append(result.Transactions, RedeemWinningsTx{
			ConditionID:      target.conditionID,
			IndexSets:        negRiskIndexSets,
			TransactionHash:  txHash,
			ExplorerURL:      fmt.Sprintf("https://polygonscan.com/tx/%s", txHash),
			CollateralPayout: payout.String(),
			ReceiptStatus:    "confirmed",
		})
	}

	result.ConditionsSubmitted = len(result.Transactions)
	result.TotalCollateralPayout = totalPayout.String()
	if len(zeroPayoutConditions) > 0 {
		result.ZeroPayoutConditions = zeroPayoutConditions
	}
	// If every condition failed, return an error with the result embedded.
	if result.ConditionsFailed > 0 && result.ConditionsRedeemed == 0 && len(zeroPayoutConditions) == 0 {
		return result, fmt.Errorf("%d/%d conditions failed to redeem", result.ConditionsFailed, result.ConditionsSubmitted)
	}
	if result.ConditionsRedeemed == 0 && !includeLosing {
		return nil, zeroPayoutRedeemError(result)
	}
	return result, nil
}

func zeroPayoutRedeemError(result *RedeemWinningsResult) error {
	if result == nil {
		return fmt.Errorf("redeem transactions were confirmed but paid out 0 collateral")
	}

	details := make([]string, 0, len(result.Transactions))
	for _, tx := range result.Transactions {
		indexSets := "[]"
		if len(tx.IndexSets) > 0 {
			indexSets = "[" + strings.Join(tx.IndexSets, ",") + "]"
		}
		detail := "condition=" + tx.ConditionID + ",tx=" + tx.TransactionHash + ",index_sets=" + indexSets + ",payout=" + tx.CollateralPayout
		details = append(details, detail)
	}

	suffix := ""
	if len(details) > 0 {
		const maxDetails = 3
		preview := details
		if len(preview) > maxDetails {
			preview = preview[:maxDetails]
		}
		suffix = "; tx_details=" + strings.Join(preview, " | ")
		if len(details) > maxDetails {
			suffix += " | +" + strconv.Itoa(len(details)-maxDetails) + " more"
		}
	}

	return fmt.Errorf(
		"redeem transactions were confirmed but paid out 0 collateral for all %d condition(s) using collateral %s%s; likely causes: non-winning index set, already redeemed/no balance, or wrong collateral token",
		result.ConditionsSubmitted,
		result.CollateralTokenAddress,
		suffix,
	)
}
