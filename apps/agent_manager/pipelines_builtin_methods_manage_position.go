package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func pipelineBuiltinPolymarketManagePosition(ctx context.Context, pe *PipelineEngine, run *data.PipelineRun, _ PipelineStep, params map[string]any) (result map[string]any, err error) {
	payload, ok, err := extractBuiltinPolymarketPayload(params)
	if err != nil {
		return nil, err
	}
	if !ok {
		if looksLikeLegacyPolymarketTradeParams(params) {
			return pipelineBuiltinPolymarketLegacyTrade(ctx, pe, run, params)
		}
		return pipelineBuiltinPolymarketLegacyStateView(ctx, pe, run, params)
	}

	companyID, err := resolvePolymarketRunCompanyID(run, stringParam(params, "company_id"))
	if err != nil {
		return nil, err
	}
	client, resolvedCompanyID, err := getBuiltinPolymarketClient(ctx, pe, companyID)
	if err != nil {
		return nil, err
	}
	refreshedMarket := refreshBuiltinPolymarketPayloadFromMarket(ctx, client, &payload)

	result = newBuiltinPolymarketManageResult(resolvedCompanyID, payload)
	if refreshedMarket != nil {
		result["volume"] = roundBuiltinPolymarketFloat(builtinPolymarketMarketVolume(*refreshedMarket), 2)
	}
	var (
		cachedUSDCBalance float64
		loadedUSDCBalance bool
	)
	loadUSDCBalance := func() (float64, error) {
		if loadedUSDCBalance {
			return cachedUSDCBalance, nil
		}
		balance, balanceErr := getBuiltinPolymarketUSDCBalance(ctx, pe, resolvedCompanyID)
		if balanceErr != nil {
			return 0, balanceErr
		}
		cachedUSDCBalance = balance
		loadedUSDCBalance = true
		return cachedUSDCBalance, nil
	}
	actionNotes := make([]string, 0, 4)
	defer func() {
		if err != nil || result == nil {
			return
		}
		result = maybeAnnotateBuiltinPolymarketResult(ctx, pe, resolvedCompanyID, payload, refreshedMarket, result, actionNotes)
		setBuiltinPolymarketManageCompletionProperties(ctx, pe, resolvedCompanyID, payload.ConditionID, result)
	}()
	noteMinimumOrderBlock := func(side, action string, size float64) {
		size = roundBuiltinPolymarketFloat(math.Max(size, 0), 4)
		if size <= 0 {
			return
		}
		result["min_order_blocked"] = true
		result["min_order_size"] = builtinPolymarketMinOrderShares
		result["min_order_blocked_side"] = strings.ToUpper(strings.TrimSpace(side))
		result["min_order_blocked_shares"] = size
		if currentPosition, ok := floatParam(result, "current_position"); ok && currentPosition > 0 {
			result["status"] = "held"
			actionNotes = append(actionNotes, fmt.Sprintf("Held %s exposure. Required %s of %.4f shares is below the venue minimum order size of %.0f shares.", strings.ToUpper(strings.TrimSpace(side)), action, size, builtinPolymarketMinOrderShares))
			return
		}
		result["status"] = "neutral"
		actionNotes = append(actionNotes, fmt.Sprintf("No action. Required %s of %.4f shares is below the venue minimum order size of %.0f shares.", action, size, builtinPolymarketMinOrderShares))
	}

	if strings.TrimSpace(payload.ConditionID) == "" {
		return nil, fmt.Errorf("payload.condition_id is required")
	}

	allPositions, positions, orders, exposure, yesTokenID, noTokenID, err := loadBuiltinPolymarketManageState(ctx, client, payload)
	if err != nil {
		return builtinPolymarketFailedResult(result, err.Error(), nil), nil
	}
	result["sizing_context_source"] = hydrateBuiltinPolymarketManageSizingContext(ctx, pe, resolvedCompanyID, &payload, positions, allPositions)
	result["aum"] = roundBuiltinPolymarketFloat(math.Max(payload.AUM, 0), 4)
	latestThesisNote, latestThesisMetadata, thesisErr := loadBuiltinPolymarketLatestThesisNote(ctx, pe, resolvedCompanyID, payload.ConditionID)
	if thesisErr != nil {
		result["thesis_note_error"] = strings.TrimSpace(thesisErr.Error())
	}
	thesisInputSource := "payload"
	if !builtinPolymarketPayloadIncludesAny(params, "estimated_probability", "confidence", "reasoning") {
		if hydrateBuiltinPolymarketPayloadFromLatestNote(&payload, latestThesisMetadata) {
			thesisInputSource = "latest_note"
		} else {
			thesisInputSource = "missing"
		}
	}
	result["thesis_input_source"] = thesisInputSource
	result["estimated_probability"] = roundBuiltinPolymarketFloat(payload.EstimatedProbability, 4)
	result["confidence"] = roundBuiltinPolymarketFloat(payload.Confidence, 4)
	if thesisInputSource == "missing" {
		return builtinPolymarketFailedResult(result, "missing research inputs: provide estimated_probability/confidence or keep a prior structured thesis note for this market", nil), nil
	}

	currentPosition := exposure.YesHeldShares + exposure.NoHeldShares
	maxAllowed, remainingCapacity := deriveBuiltinPolymarketCapacity(payload, currentPosition)
	result["current_position"] = roundBuiltinPolymarketFloat(currentPosition, 4)
	result["max_allowed"] = roundBuiltinPolymarketFloat(maxAllowed, 4)
	result["remaining_capacity"] = roundBuiltinPolymarketFloat(remainingCapacity, 4)
	baseConfidenceScale := builtinPolymarketConfidenceScale(payload.Confidence)
	positionScale := baseConfidenceScale

	// Pricing must come from live orderbook quotes, not cached market metadata.
	quotes, err := loadBuiltinPolymarketQuotes(ctx, client, payload.ConditionID, yesTokenID, noTokenID)
	if err != nil {
		if builtinPolymarketIsUnavailableQuoteError(err) {
			errText := strings.TrimSpace(err.Error())
			result["status"] = "neutral"
			result["pricing_unavailable"] = true
			result["pricing_error"] = errText
			actionNotes = append(actionNotes, "No action. Live Polymarket pricing is unavailable for this market, so the branch was skipped.")
			return result, nil
		}
		return builtinPolymarketFailedResult(result, err.Error(), nil), nil
	}

	candidate := selectBuiltinPolymarketTradeCandidate(payload, quotes)
	if candidate.Side == "" {
		candidate.Side = fallbackBuiltinPolymarketSide(payload.EstimatedProbability)
	}
	if candidate.Side != "" {
		result["side"] = candidate.Side
		result["market_price"] = roundBuiltinPolymarketFloat(candidate.AskPrice, 4)
		result["edge"] = roundBuiltinPolymarketFloat(math.Max(candidate.AbsoluteEdge, 0), 4)
		result["relative_edge"] = roundBuiltinPolymarketFloat(math.Max(candidate.RelativeEdge, 0), 4)
	}

	targetSide := candidate.Side
	targetTokenID := tokenIDForBuiltinPolymarketSide(targetSide, yesTokenID, noTokenID)
	oppositeSide := oppositeBuiltinPolymarketSide(targetSide)
	oppositeTokenID := tokenIDForBuiltinPolymarketSide(oppositeSide, yesTokenID, noTokenID)
	thesisDrift := evaluateBuiltinPolymarketThesisDrift(payload, targetSide, latestThesisNote, latestThesisMetadata)
	positionScale = roundBuiltinPolymarketFloat(baseConfidenceScale*thesisDrift.RetentionScale, 6)
	executionMetrics, executionMetricsErr := loadBuiltinPolymarketExecutionMetrics(ctx, client, targetTokenID)
	if executionMetricsErr == nil {
		applyBuiltinPolymarketExecutionMetrics(result, executionMetrics)
	} else if strings.TrimSpace(targetTokenID) != "" {
		result["orderbook_error"] = strings.TrimSpace(executionMetricsErr.Error())
	}
	targetHeldSharesForSizing := heldSharesForBuiltinPolymarketSide(targetSide, exposure)
	provisionalTargetPosition := deriveBuiltinPolymarketTargetPosition(payload, maxAllowed, targetHeldSharesForSizing, positionScale)
	executionSignal := evaluateBuiltinPolymarketExecutionSignal(candidate, executionMetrics, math.Max(provisionalTargetPosition-targetHeldSharesForSizing, 0))
	applyBuiltinPolymarketThesisDrift(result, baseConfidenceScale, positionScale, thesisDrift)
	applyBuiltinPolymarketExecutionSignal(result, executionSignal)
	targetPosition := provisionalTargetPosition
	result["target_side_position"] = roundBuiltinPolymarketFloat(targetHeldSharesForSizing, 4)
	result["target_position"] = roundBuiltinPolymarketFloat(targetPosition, 4)
	result["target_gap"] = roundBuiltinPolymarketFloat(targetPosition-targetHeldSharesForSizing, 4)

	ordersCancelled := 0
	ordersUpdated := 0
	sellOrdersPlaced := 0
	buyOrdersPlaced := 0
	lastAmount := 0.0
	lastPrice := 0.0
	lastOrderID := "none"

	reloadState := func() error {
		var reloadErr error
		allPositions, positions, orders, exposure, yesTokenID, noTokenID, reloadErr = loadBuiltinPolymarketManageState(ctx, client, payload)
		if reloadErr != nil {
			return reloadErr
		}
		currentPosition = exposure.YesHeldShares + exposure.NoHeldShares
		maxAllowed, remainingCapacity = deriveBuiltinPolymarketCapacity(payload, currentPosition)
		result["current_position"] = roundBuiltinPolymarketFloat(currentPosition, 4)
		result["max_allowed"] = roundBuiltinPolymarketFloat(maxAllowed, 4)
		result["remaining_capacity"] = roundBuiltinPolymarketFloat(remainingCapacity, 4)
		quotes, reloadErr = loadBuiltinPolymarketQuotes(ctx, client, payload.ConditionID, yesTokenID, noTokenID)
		if reloadErr != nil {
			return reloadErr
		}
		candidate = selectBuiltinPolymarketTradeCandidate(payload, quotes)
		if candidate.Side == "" {
			candidate.Side = fallbackBuiltinPolymarketSide(payload.EstimatedProbability)
		}
		targetSide = candidate.Side
		targetTokenID = tokenIDForBuiltinPolymarketSide(targetSide, yesTokenID, noTokenID)
		oppositeSide = oppositeBuiltinPolymarketSide(targetSide)
		oppositeTokenID = tokenIDForBuiltinPolymarketSide(oppositeSide, yesTokenID, noTokenID)
		thesisDrift = evaluateBuiltinPolymarketThesisDrift(payload, targetSide, latestThesisNote, latestThesisMetadata)
		positionScale = roundBuiltinPolymarketFloat(baseConfidenceScale*thesisDrift.RetentionScale, 6)
		executionMetrics, executionMetricsErr = loadBuiltinPolymarketExecutionMetrics(ctx, client, targetTokenID)
		if executionMetricsErr == nil {
			applyBuiltinPolymarketExecutionMetrics(result, executionMetrics)
			delete(result, "orderbook_error")
		} else if strings.TrimSpace(targetTokenID) != "" {
			result["orderbook_error"] = strings.TrimSpace(executionMetricsErr.Error())
		}
		targetHeldSharesForSizing = heldSharesForBuiltinPolymarketSide(targetSide, exposure)
		provisionalTargetPosition = deriveBuiltinPolymarketTargetPosition(payload, maxAllowed, targetHeldSharesForSizing, positionScale)
		executionSignal = evaluateBuiltinPolymarketExecutionSignal(candidate, executionMetrics, math.Max(provisionalTargetPosition-targetHeldSharesForSizing, 0))
		applyBuiltinPolymarketThesisDrift(result, baseConfidenceScale, positionScale, thesisDrift)
		applyBuiltinPolymarketExecutionSignal(result, executionSignal)
		targetPosition = provisionalTargetPosition
		result["side"] = targetSide
		result["market_price"] = roundBuiltinPolymarketFloat(candidate.AskPrice, 4)
		result["edge"] = roundBuiltinPolymarketFloat(math.Max(candidate.AbsoluteEdge, 0), 4)
		result["relative_edge"] = roundBuiltinPolymarketFloat(math.Max(candidate.RelativeEdge, 0), 4)
		result["target_side_position"] = roundBuiltinPolymarketFloat(targetHeldSharesForSizing, 4)
		result["target_position"] = roundBuiltinPolymarketFloat(targetPosition, 4)
		result["target_gap"] = roundBuiltinPolymarketFloat(targetPosition-targetHeldSharesForSizing, 4)
		return nil
	}

	cancelOrders := func(items []polymarket.Order, countAsUpdate bool, note string) error {
		if len(items) == 0 {
			return nil
		}
		for _, order := range items {
			if err := client.CancelOrder(ctx, strings.TrimSpace(order.ID)); err != nil {
				return fmt.Errorf("failed to cancel polymarket order %s: %w", strings.TrimSpace(order.ID), err)
			}
			ordersCancelled++
			if countAsUpdate {
				ordersUpdated++
			}
			lastOrderID = strings.TrimSpace(order.ID)
		}
		if strings.TrimSpace(note) != "" {
			actionNotes = append(actionNotes, note)
		}
		return nil
	}

	orderRejectedError := func(resp *polymarket.PlaceOrderResponse, err error) string {
		if err != nil {
			return strings.TrimSpace(err.Error())
		}
		if resp == nil {
			return "polymarket order returned no response"
		}
		if resp.Success {
			return ""
		}
		msg := strings.TrimSpace(resp.ErrorMsg)
		if msg == "" {
			msg = "order rejected"
		}
		return msg
	}

	noteBuyBlockedByBalance := func(side, action string, requestedSize, price float64, holdExisting bool, rejection string) {
		availableBalance := 0.0
		affordableSize := 0.0
		if balance, balanceErr := loadUSDCBalance(); balanceErr == nil {
			availableBalance = math.Max(balance, 0)
			result["available_usdc_balance"] = roundBuiltinPolymarketFloat(availableBalance, 4)
			if price > 0 {
				affordableSize = roundBuiltinPolymarketFloat(availableBalance/price, 4)
				result["affordable_buy_shares"] = affordableSize
			}
		}
		if requestedSize > 0 && price > 0 {
			result["required_usdc"] = roundBuiltinPolymarketFloat(requestedSize*price, 4)
		}
		result["insufficient_usdc_balance"] = true
		if strings.TrimSpace(rejection) != "" {
			result["order_rejection"] = strings.TrimSpace(rejection)
		}
		if holdExisting || currentPosition > 0 {
			result["status"] = "held"
		} else {
			result["status"] = "neutral"
		}

		sideLabel := strings.ToUpper(strings.TrimSpace(side))
		if sideLabel == "" {
			sideLabel = "TARGET"
		}
		action = strings.TrimSpace(action)
		if action == "" {
			action = "entry"
		}
		switch {
		case strings.TrimSpace(rejection) != "":
			actionNotes = append(actionNotes, fmt.Sprintf("No action. Polymarket rejected the %s BUY %s for insufficient balance/allowance.", sideLabel, action))
		case availableBalance <= 0:
			actionNotes = append(actionNotes, fmt.Sprintf("No action. Available USDC balance is 0, so no %s BUY %s can be placed.", sideLabel, action))
		case affordableSize > 0:
			actionNotes = append(actionNotes, fmt.Sprintf("No action. Available USDC balance %.4f only supports %.4f %s shares, below the executable %s size.", availableBalance, affordableSize, sideLabel, action))
		default:
			actionNotes = append(actionNotes, fmt.Sprintf("No action. Available USDC balance %.4f is insufficient for the %s BUY %s.", availableBalance, sideLabel, action))
		}
	}

	prepareBuySize := func(side, action string, requestedSize, price float64, holdExisting bool) (float64, map[string]any) {
		requestedSize = roundBuiltinPolymarketFloat(math.Max(requestedSize, 0), 4)
		if requestedSize <= 0 || price <= 0 {
			return requestedSize, nil
		}

		usdcBalance, balanceErr := loadUSDCBalance()
		if balanceErr != nil {
			result["available_usdc_balance_error"] = strings.TrimSpace(balanceErr.Error())
			return requestedSize, nil
		}
		usdcBalance = math.Max(usdcBalance, 0)
		result["available_usdc_balance"] = roundBuiltinPolymarketFloat(usdcBalance, 4)
		affordableSize := roundBuiltinPolymarketFloat(usdcBalance/price, 4)
		result["affordable_buy_shares"] = affordableSize
		if affordableSize < requestedSize {
			result["balance_capped"] = true
			result["requested_buy_shares"] = requestedSize
			requestedSize = affordableSize
		}
		if requestedSize <= 0 || builtinPolymarketOrderBelowVenueMinimum(requestedSize) {
			noteBuyBlockedByBalance(side, action, requestedSize, price, holdExisting, "")
			return 0, nil
		}
		return requestedSize, nil
	}

	placeSell := func(side string, requestedSize float64) (bool, map[string]any) {
		tokenID := tokenIDForBuiltinPolymarketSide(side, yesTokenID, noTokenID)
		if tokenID == "" {
			return false, builtinPolymarketFailedResult(result, fmt.Sprintf("missing %s token_id", strings.ToUpper(side)), nil)
		}

		requestedSize = roundBuiltinPolymarketFloat(requestedSize, 4)
		if requestedSize <= 0 {
			return false, nil
		}

		heldShares := heldSharesForBuiltinPolymarketSide(side, exposure)
		if heldShares <= 0 {
			return false, nil
		}

		sameSideSellOrders := selectBuiltinPolymarketOrders(orders, tokenID, polymarket.Sell)
		lockedShares := lockedSellSharesForBuiltinPolymarketSide(side, exposure)
		sellableShares := math.Max(heldShares-lockedShares, 0)

		if requestedSize >= heldShares*0.8 || requestedSize > sellableShares {
			if err := cancelOrders(sameSideSellOrders, true, fmt.Sprintf("Cancelled %d locking %s SELL order(s) before resizing inventory.", len(sameSideSellOrders), strings.ToUpper(side))); err != nil {
				return false, builtinPolymarketFailedResult(result, err.Error(), nil)
			}
			if len(sameSideSellOrders) > 0 {
				if err := reloadState(); err != nil {
					return false, builtinPolymarketFailedResult(result, err.Error(), nil)
				}
				heldShares = heldSharesForBuiltinPolymarketSide(side, exposure)
				lockedShares = lockedSellSharesForBuiltinPolymarketSide(side, exposure)
				sellableShares = math.Max(heldShares-lockedShares, 0)
			}
		}

		size := roundBuiltinPolymarketFloat(math.Min(requestedSize, sellableShares), 4)
		if size <= 0 {
			return false, nil
		}
		if builtinPolymarketOrderBelowVenueMinimum(size) {
			noteMinimumOrderBlock(side, "trim", size)
			return false, nil
		}

		quote := quoteForBuiltinPolymarketSide(side, quotes)
		if !quote.HasBid || quote.BidPrice <= 0 {
			return false, builtinPolymarketFailedResult(result, fmt.Sprintf("missing executable sell price for %s", strings.ToUpper(side)), nil)
		}

		price := roundBuiltinPolymarketFloat(quote.BidPrice, 4)
		resp, err := client.PlaceOrder(ctx, tokenID, price, size, polymarket.Sell, polymarket.GTC, false)
		errText := orderRejectedError(resp, err)
		if errText == "" {
			sellOrdersPlaced++
			lastAmount = size
			lastPrice = price
			lastOrderID = strings.TrimSpace(resp.OrderID)
			actionNotes = append(actionNotes, fmt.Sprintf("Placed %s SELL order for %.4f shares at %.4f.", strings.ToUpper(side), size, price))
			return true, nil
		}
		if builtinPolymarketIsVenueMinimumError(errText) {
			noteMinimumOrderBlock(side, "trim", size)
			return false, nil
		}

		if !builtinPolymarketIsInsufficientBalanceError(errText) {
			return false, builtinPolymarketFailedResult(result, errText, nil)
		}

		if err := reloadState(); err != nil {
			return false, builtinPolymarketFailedResult(result, err.Error(), nil)
		}
		sameSideSellOrders = selectBuiltinPolymarketOrders(orders, tokenID, polymarket.Sell)
		if len(sameSideSellOrders) > 0 {
			if err := cancelOrders(sameSideSellOrders, true, fmt.Sprintf("Cancelled %d additional %s SELL order(s) after balance error.", len(sameSideSellOrders), strings.ToUpper(side))); err != nil {
				return false, builtinPolymarketFailedResult(result, err.Error(), nil)
			}
			if err := reloadState(); err != nil {
				return false, builtinPolymarketFailedResult(result, err.Error(), nil)
			}
		}

		heldShares = heldSharesForBuiltinPolymarketSide(side, exposure)
		lockedShares = lockedSellSharesForBuiltinPolymarketSide(side, exposure)
		sellableShares = math.Max(heldShares-lockedShares, 0)
		retrySize := roundBuiltinPolymarketFloat(math.Min(requestedSize, sellableShares), 4)
		if retrySize <= 0 || math.Abs(retrySize-size) < 0.0001 {
			return false, builtinPolymarketFailedResult(result, errText, map[string]any{
				"held_side_shares":     roundBuiltinPolymarketFloat(heldShares, 4),
				"locked_side_shares":   roundBuiltinPolymarketFloat(lockedShares, 4),
				"sellable_side_shares": roundBuiltinPolymarketFloat(sellableShares, 4),
				"token_id_used":        tokenID,
			})
		}
		if builtinPolymarketOrderBelowVenueMinimum(retrySize) {
			noteMinimumOrderBlock(side, "trim", retrySize)
			return false, nil
		}

		resp, err = client.PlaceOrder(ctx, tokenID, price, retrySize, polymarket.Sell, polymarket.GTC, false)
		errText = orderRejectedError(resp, err)
		if errText == "" {
			sellOrdersPlaced++
			lastAmount = retrySize
			lastPrice = price
			lastOrderID = strings.TrimSpace(resp.OrderID)
			actionNotes = append(actionNotes, fmt.Sprintf("Retried %s SELL order for %.4f shares at %.4f after balance error.", strings.ToUpper(side), retrySize, price))
			return true, nil
		}
		if builtinPolymarketIsVenueMinimumError(errText) {
			noteMinimumOrderBlock(side, "trim", retrySize)
			return false, nil
		}

		diag := map[string]any{
			"held_side_shares":     roundBuiltinPolymarketFloat(heldShares, 4),
			"locked_side_shares":   roundBuiltinPolymarketFloat(lockedShares, 4),
			"sellable_side_shares": roundBuiltinPolymarketFloat(sellableShares, 4),
			"token_id_used":        tokenID,
		}
		return false, builtinPolymarketFailedResult(result, errText, diag)
	}

	expired := builtinPolymarketResolutionPassed(payload.ResolutionDate)
	if expired {
		openBuyOrders := selectBuiltinPolymarketOrdersByOrderSide(orders, polymarket.Buy)
		if err := cancelOrders(openBuyOrders, true, fmt.Sprintf("Cancelled %d open BUY order(s) because the resolution date has passed.", len(openBuyOrders))); err != nil {
			return builtinPolymarketFailedResult(result, err.Error(), nil), nil
		}
		result["orders_cancelled"] = ordersCancelled
		result["orders_updated"] = ordersUpdated
		result["status"] = "neutral"
		if len(actionNotes) == 0 {
			actionNotes = append(actionNotes, "No action. The supplied resolution date is already in the past.")
		}
		result["action_taken"] = strings.Join(actionNotes, " ")
		result["order_id"] = lastOrderID
		return result, nil
	}

	if targetTokenID != "" && oppositeTokenID != "" {
		conflictingBuyOrders := selectBuiltinPolymarketOrders(orders, oppositeTokenID, polymarket.Buy)
		if err := cancelOrders(conflictingBuyOrders, false, fmt.Sprintf("Cancelled %d conflicting %s BUY order(s).", len(conflictingBuyOrders), strings.ToUpper(oppositeSide))); err != nil {
			return builtinPolymarketFailedResult(result, err.Error(), nil), nil
		}
		if len(conflictingBuyOrders) > 0 {
			if err := reloadState(); err != nil {
				return builtinPolymarketFailedResult(result, err.Error(), nil), nil
			}
		}
	}

	targetHeldShares := heldSharesForBuiltinPolymarketSide(targetSide, exposure)
	oppositeHeldShares := heldSharesForBuiltinPolymarketSide(oppositeSide, exposure)
	targetExposureGap := roundBuiltinPolymarketFloat(math.Max(targetPosition-targetHeldShares, 0), 4)
	thesisBlocksAdds := thesisDrift.BlockNewExposure && targetExposureGap > 0.01

	if oppositeHeldShares > 0 {
		placed, failure := placeSell(oppositeSide, oppositeHeldShares)
		if failure != nil {
			return failure, nil
		}
		if placed {
			result["status"] = "updated"
		}
	}

	if targetHeldShares > targetPosition+0.01 {
		excessTargetShares := roundBuiltinPolymarketFloat(targetHeldShares-targetPosition, 4)
		placed, failure := placeSell(targetSide, excessTargetShares)
		if failure != nil {
			return failure, nil
		}
		if placed {
			result["status"] = "updated"
		}
	}

	edgeEligibleTier1 := executionSignal.NetEdge >= 0.05 && candidate.RelativeEdge >= 0.15
	edgeEligibleTier2 := executionSignal.NetEdge >= 0.02 && candidate.RelativeEdge >= 0.10 && candidate.RelativeEdge < 0.15
	wantsNewExposure := targetTokenID != "" && targetExposureGap > 0.01 && !thesisBlocksAdds && executionSignal.Scale > 0
	sideProbability := builtinPolymarketProbabilityForSide(targetSide, payload.EstimatedProbability)
	midpointEdge := candidate.AbsoluteEdge
	if executionMetrics.Midpoint > 0 {
		midpointEdge = sideProbability - executionMetrics.Midpoint
		result["midpoint_price"] = roundBuiltinPolymarketFloat(executionMetrics.Midpoint, 4)
		result["midpoint_edge"] = roundBuiltinPolymarketFloat(midpointEdge, 4)
	}
	tightBandTier2Eligible := false
	if candidate.AbsoluteEdge < 0.03 && wantsNewExposure && edgeEligibleTier2 && payload.AUM > 0 && midpointEdge >= 0.015 {
		usdcBalance, balanceErr := loadUSDCBalance()
		if balanceErr != nil {
			return builtinPolymarketFailedResult(result, balanceErr.Error(), nil), nil
		}
		tightBandTier2Eligible = usdcBalance > payload.AUM*0.25
	}

	if (candidate.AbsoluteEdge < 0.03 && !tightBandTier2Eligible) || !wantsNewExposure || thesisBlocksAdds {
		targetBuyOrders := selectBuiltinPolymarketOrders(orders, targetTokenID, polymarket.Buy)
		if len(targetBuyOrders) > 0 {
			reason := "Cancelled stale aligned BUY orders because the current edge no longer supports re-entry."
			if thesisBlocksAdds {
				reason = "Cancelled aligned BUY orders because the recent thesis weakened materially versus the latest note."
			} else if executionSignal.Scale == 0 && candidate.AbsoluteEdge > 0 {
				reason = "Cancelled aligned BUY orders because net edge after spread and slippage no longer supports re-entry."
			} else if !wantsNewExposure && positionScale == 0 {
				reason = "Cancelled aligned BUY orders because confidence is below the minimum trading threshold."
			} else if !wantsNewExposure && remainingCapacity <= 0 {
				reason = "Cancelled aligned BUY orders because there is no remaining capacity."
			} else if !wantsNewExposure {
				reason = "Cancelled aligned BUY orders because the target position is already filled."
			}
			if err := cancelOrders(targetBuyOrders, true, reason); err != nil {
				return builtinPolymarketFailedResult(result, err.Error(), nil), nil
			}
		}
	}

	if (candidate.AbsoluteEdge >= 0.03 || tightBandTier2Eligible) && targetTokenID != "" && heldSharesForBuiltinPolymarketSide(targetSide, exposure) > 0 {
		targetSellOrders := selectBuiltinPolymarketOrders(orders, targetTokenID, polymarket.Sell)
		if len(targetSellOrders) > 0 && !(targetHeldShares > targetPosition+0.01) {
			if err := cancelOrders(targetSellOrders, true, fmt.Sprintf("Cancelled %d stale %s SELL order(s) locking target inventory.", len(targetSellOrders), strings.ToUpper(targetSide))); err != nil {
				return builtinPolymarketFailedResult(result, err.Error(), nil), nil
			}
			if len(targetSellOrders) > 0 {
				if err := reloadState(); err != nil {
					return builtinPolymarketFailedResult(result, err.Error(), nil), nil
				}
			}
		}
	}

	tier := 0
	orderType := ""
	desiredPrice := 0.0
	spreadTooWide := executionMetrics.HasBid && executionMetrics.HasAsk && executionMetrics.Spread > math.Max(0.04, candidate.AbsoluteEdge*0.9)
	bookThinForAggressive := executionMetrics.HasAsk && executionMetrics.BestAskSize > 0 && targetExposureGap > executionMetrics.BestAskSize*8
	if edgeEligibleTier1 && !spreadTooWide && !bookThinForAggressive {
		tier = 1
		orderType = polymarket.GTC
		desiredPrice = candidate.AskPrice
		if executionMetrics.HasAsk && executionMetrics.BestAsk > 0 {
			desiredPrice = roundBuiltinPolymarketFloat(executionMetrics.BestAsk, 4)
		}
	} else if edgeEligibleTier2 && payload.AUM > 0 {
		usdcBalance, balanceErr := loadUSDCBalance()
		if balanceErr != nil {
			return builtinPolymarketFailedResult(result, balanceErr.Error(), nil), nil
		}
		if usdcBalance > payload.AUM*0.25 && midpointEdge >= 0.015 {
			tier = 2
			orderType = polymarket.GTD
			desiredPrice = roundBuiltinPolymarketFloat(builtinPolymarketMidpoint(quoteForBuiltinPolymarketSide(targetSide, quotes)), 4)
			if executionMetrics.Midpoint > 0 {
				desiredPrice = roundBuiltinPolymarketFloat(executionMetrics.Midpoint, 4)
			}
		}
	}

	if tier > 0 && wantsNewExposure {
		// desiredSize is the remaining gap between live target exposure and the
		// already-held target-side shares. This avoids compounding buys on
		// repeated runs while still allowing AUM/max_allowed changes to resize.
		desiredSize := roundBuiltinPolymarketFloat(targetExposureGap*executionSignal.Scale, 4)
		if desiredSize > 0 && desiredPrice > 0 {
			if usdcBalance, balanceErr := loadUSDCBalance(); balanceErr == nil && desiredPrice > 0 {
				result["available_usdc_balance"] = roundBuiltinPolymarketFloat(math.Max(usdcBalance, 0), 4)
				affordableSize := roundBuiltinPolymarketFloat(usdcBalance/desiredPrice, 4)
				if affordableSize > 0 && affordableSize < desiredSize {
					result["balance_capped"] = true
					result["requested_buy_shares"] = desiredSize
					desiredSize = affordableSize
				}
			}
		}
		if tier == 1 && executionMetrics.HasAsk && executionMetrics.BestAskSize > 0 {
			aggressiveCap := roundBuiltinPolymarketFloat(math.Max(executionMetrics.BestAskSize*5, executionMetrics.BestAskSize+1), 4)
			if aggressiveCap > 0 && desiredSize > aggressiveCap {
				desiredSize = aggressiveCap
				result["depth_capped"] = true
				result["depth_cap_shares"] = aggressiveCap
			}
		}

		targetBuyOrders := selectBuiltinPolymarketOrders(orders, targetTokenID, polymarket.Buy)
		staleTargetBuyOrders := make([]polymarket.Order, 0, len(targetBuyOrders))
		alignedTargetBuyOrders := make([]polymarket.Order, 0, len(targetBuyOrders))
		for _, order := range targetBuyOrders {
			if builtinPolymarketBuyOrderNeedsUpdate(order, desiredPrice, orderType, tier) {
				staleTargetBuyOrders = append(staleTargetBuyOrders, order)
				continue
			}
			alignedTargetBuyOrders = append(alignedTargetBuyOrders, order)
		}

		if len(staleTargetBuyOrders) > 0 {
			if err := cancelOrders(staleTargetBuyOrders, true, fmt.Sprintf("Cancelled %d stale %s BUY order(s) to refresh pricing.", len(staleTargetBuyOrders), strings.ToUpper(targetSide))); err != nil {
				return builtinPolymarketFailedResult(result, err.Error(), nil), nil
			}
			if err := reloadState(); err != nil {
				return builtinPolymarketFailedResult(result, err.Error(), nil), nil
			}
			targetBuyOrders = selectBuiltinPolymarketOrders(orders, targetTokenID, polymarket.Buy)
			alignedTargetBuyOrders = alignedTargetBuyOrders[:0]
			for _, order := range targetBuyOrders {
				if !builtinPolymarketBuyOrderNeedsUpdate(order, desiredPrice, orderType, tier) {
					alignedTargetBuyOrders = append(alignedTargetBuyOrders, order)
				}
			}
		}

		alignedBuyRemaining := roundBuiltinPolymarketFloat(sumBuiltinPolymarketOrderRemainingSize(alignedTargetBuyOrders), 4)
		if len(alignedTargetBuyOrders) > 0 && desiredSize >= 0 && alignedBuyRemaining > desiredSize+0.01 {
			if err := cancelOrders(alignedTargetBuyOrders, true, fmt.Sprintf("Cancelled %d oversized %s BUY order(s) exceeding the share cap target.", len(alignedTargetBuyOrders), strings.ToUpper(targetSide))); err != nil {
				return builtinPolymarketFailedResult(result, err.Error(), nil), nil
			}
			if err := reloadState(); err != nil {
				return builtinPolymarketFailedResult(result, err.Error(), nil), nil
			}
			targetBuyOrders = selectBuiltinPolymarketOrders(orders, targetTokenID, polymarket.Buy)
			alignedTargetBuyOrders = alignedTargetBuyOrders[:0]
			for _, order := range targetBuyOrders {
				if !builtinPolymarketBuyOrderNeedsUpdate(order, desiredPrice, orderType, tier) {
					alignedTargetBuyOrders = append(alignedTargetBuyOrders, order)
				}
			}
			alignedBuyRemaining = roundBuiltinPolymarketFloat(sumBuiltinPolymarketOrderRemainingSize(alignedTargetBuyOrders), 4)
		}

		if len(alignedTargetBuyOrders) > 0 {
			additionalSize := roundBuiltinPolymarketFloat(math.Max(desiredSize-alignedBuyRemaining, 0), 4)
			if additionalSize > 0.01 && desiredPrice > 0 {
				preparedSize, failure := prepareBuySize(targetSide, "add", additionalSize, desiredPrice, true)
				if failure != nil {
					return failure, nil
				}
				if preparedSize <= 0 {
					return result, nil
				}
				additionalSize = preparedSize
				resp, err := client.PlaceOrder(ctx, targetTokenID, desiredPrice, additionalSize, polymarket.Buy, orderType, false)
				errText := orderRejectedError(resp, err)
				if errText != "" {
					if builtinPolymarketIsVenueMinimumError(errText) {
						noteMinimumOrderBlock(targetSide, "add", additionalSize)
						return result, nil
					}
					if builtinPolymarketIsInsufficientBalanceError(errText) {
						noteBuyBlockedByBalance(targetSide, "add", additionalSize, desiredPrice, true, errText)
						return result, nil
					}
					return builtinPolymarketFailedResult(result, errText, nil), nil
				}
				buyOrdersPlaced++
				lastAmount = additionalSize
				lastPrice = desiredPrice
				lastOrderID = strings.TrimSpace(resp.OrderID)
				result["execution_tier"] = tier
				result["status"] = "updated"
				actionNotes = append(actionNotes, fmt.Sprintf("Increased aligned %s BUY order(s) by %.4f shares at %.4f to match the updated share cap.", strings.ToUpper(targetSide), additionalSize, desiredPrice))
			} else {
				actionNotes = append(actionNotes, fmt.Sprintf("Held aligned %s BUY order(s) totaling %.4f shares.", strings.ToUpper(targetSide), alignedBuyRemaining))
				result["status"] = "held"
			}
		} else {
			if desiredSize > 0 && desiredPrice > 0 {
				preparedSize, failure := prepareBuySize(targetSide, "entry", desiredSize, desiredPrice, false)
				if failure != nil {
					return failure, nil
				}
				if preparedSize <= 0 {
					return result, nil
				}
				desiredSize = preparedSize
				resp, err := client.PlaceOrder(ctx, targetTokenID, desiredPrice, desiredSize, polymarket.Buy, orderType, false)
				errText := orderRejectedError(resp, err)
				if errText != "" {
					if builtinPolymarketIsVenueMinimumError(errText) {
						noteMinimumOrderBlock(targetSide, "entry", desiredSize)
						return result, nil
					}
					if builtinPolymarketIsInsufficientBalanceError(errText) {
						noteBuyBlockedByBalance(targetSide, "entry", desiredSize, desiredPrice, false, errText)
						return result, nil
					}
					return builtinPolymarketFailedResult(result, errText, nil), nil
				}
				buyOrdersPlaced++
				lastAmount = desiredSize
				lastPrice = desiredPrice
				lastOrderID = strings.TrimSpace(resp.OrderID)
				result["execution_tier"] = tier
				result["status"] = "placed"
				actionNotes = append(actionNotes, fmt.Sprintf("Placed Tier %d %s BUY order for %.4f shares at %.4f.", tier, strings.ToUpper(targetSide), desiredSize, desiredPrice))
			}
		}
	}

	if len(actionNotes) == 0 {
		switch {
		case currentPosition > 0 && targetExposureGap > 0.01 && executionSignal.Scale == 0 && candidate.AbsoluteEdge > 0:
			result["status"] = "held"
			actionNotes = append(actionNotes, fmt.Sprintf("Held %s exposure. Target exceeds current shares, but net edge after spread and slippage is too thin to add.", strings.ToUpper(targetSide)))
		case currentPosition > 0 && targetExposureGap > 0.01 && candidate.AbsoluteEdge > 0:
			result["status"] = "held"
			actionNotes = append(actionNotes, fmt.Sprintf("Held %s exposure. Target exceeds current shares, but execution and liquidity conditions blocked an add.", strings.ToUpper(targetSide)))
		case currentPosition > 0:
			result["status"] = "held"
			actionNotes = append(actionNotes, fmt.Sprintf("Held %s exposure with no trade required.", strings.ToUpper(targetSide)))
		case thesisBlocksAdds:
			result["status"] = "neutral"
			actionNotes = append(actionNotes, "No action. The latest thesis weakened materially versus the prior note, so new exposure is paused until conviction improves.")
		case executionSignal.Scale == 0 && candidate.AbsoluteEdge > 0:
			result["status"] = "neutral"
			actionNotes = append(actionNotes, "No action. Net edge after spread and slippage is too thin to justify new exposure.")
		case candidate.AbsoluteEdge >= 0.03 && wantsNewExposure && tier == 0:
			result["status"] = "neutral"
			actionNotes = append(actionNotes, "No action. Edge met the absolute threshold but not the execution/liquidity requirements.")
		default:
			result["status"] = "neutral"
			actionNotes = append(actionNotes, "No action. Research edge or capacity did not justify a new order.")
		}
	}

	result["orders_cancelled"] = ordersCancelled
	result["orders_updated"] = ordersUpdated
	result["sell_orders_placed"] = sellOrdersPlaced
	result["buy_orders_placed"] = buyOrdersPlaced
	result["amount"] = roundBuiltinPolymarketFloat(lastAmount, 4)
	result["price"] = roundBuiltinPolymarketFloat(lastPrice, 4)
	result["order_id"] = lastOrderID
	result["action_taken"] = strings.Join(actionNotes, " ")

	return result, nil
}

func pipelineBuiltinPolymarketLegacyTrade(ctx context.Context, pe *PipelineEngine, run *data.PipelineRun, params map[string]any) (map[string]any, error) {
	companyID, err := resolvePolymarketRunCompanyID(run, stringParam(params, "company_id"))
	if err != nil {
		return nil, err
	}
	client, resolvedCompanyID, err := getBuiltinPolymarketClient(ctx, pe, companyID)
	if err != nil {
		return nil, err
	}

	action := strings.ToLower(firstStringParam(params, "action"))
	tokenID := firstStringParam(params, "token_id", "asset", "asset_id")
	orderID := firstStringParam(params, "cancel_order_id", "order_id")
	if action == "" {
		if orderID != "" && tokenID == "" {
			action = "cancel"
		} else {
			action = "place"
		}
	}

	switch action {
	case "place", "execute", "trade":
		if tokenID == "" {
			return nil, fmt.Errorf("token_id is required")
		}
		price, ok := floatParam(params, "price")
		if !ok {
			return nil, fmt.Errorf("price is required")
		}
		size, ok := floatParam(params, "size")
		if !ok {
			return nil, fmt.Errorf("size is required")
		}
		side := normalizePolymarketSide(firstStringParam(params, "side"))
		if side == "" {
			return nil, fmt.Errorf("side must be BUY or SELL")
		}
		rawOrderType := firstStringParam(params, "order_type")
		orderType := normalizePolymarketOrderType(rawOrderType)
		if rawOrderType != "" && orderType == "" {
			return nil, fmt.Errorf("order_type must be GTC, FOK, or GTD")
		}
		if orderType == "" {
			orderType = polymarket.GTC
		}
		negRisk, _ := boolParam(params, "neg_risk")

		resp, err := client.PlaceOrder(ctx, tokenID, price, size, side, orderType, negRisk)
		if err != nil {
			return nil, fmt.Errorf("failed to place polymarket order: %w", err)
		}
		if resp == nil {
			return nil, fmt.Errorf("polymarket order returned no response")
		}
		if !resp.Success {
			msg := strings.TrimSpace(resp.ErrorMsg)
			if msg == "" {
				msg = "order rejected"
			}
			return nil, fmt.Errorf("polymarket order rejected: %s", msg)
		}

		return map[string]any{
			"source":        "polymarket",
			"company_id":    resolvedCompanyID,
			"action":        "place",
			"status":        "placed",
			"success":       true,
			"token_id":      tokenID,
			"asset":         tokenID,
			"side":          side,
			"price":         price,
			"size":          size,
			"order_type":    orderType,
			"neg_risk":      negRisk,
			"order_id":      strings.TrimSpace(resp.OrderID),
			"error_message": strings.TrimSpace(resp.ErrorMsg),
			"response": map[string]any{
				"success":       resp.Success,
				"order_id":      strings.TrimSpace(resp.OrderID),
				"error_message": strings.TrimSpace(resp.ErrorMsg),
			},
		}, nil
	case "cancel", "cancel_order":
		if orderID == "" {
			return nil, fmt.Errorf("order_id is required for cancel")
		}
		if err := client.CancelOrder(ctx, orderID); err != nil {
			return nil, fmt.Errorf("failed to cancel polymarket order: %w", err)
		}
		return map[string]any{
			"source":     "polymarket",
			"company_id": resolvedCompanyID,
			"action":     "cancel",
			"status":     "cancelled",
			"success":    true,
			"order_id":   orderID,
			"response": map[string]any{
				"status":   "cancelled",
				"order_id": orderID,
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported polymarket trade action %q", action)
	}
}

func pipelineBuiltinPolymarketLegacyStateView(ctx context.Context, pe *PipelineEngine, run *data.PipelineRun, params map[string]any) (map[string]any, error) {
	snapshot, err := loadBuiltinPolymarketSnapshot(ctx, pe, run, params)
	if err != nil {
		return nil, err
	}

	conditionID := firstStringParam(params, "condition_id", "market")
	asset := firstStringParam(params, "asset", "token_id", "asset_id")
	orderID := firstStringParam(params, "order_id")
	positions := filterPolymarketPositions(snapshot.positions, conditionID, asset)
	orders := filterPolymarketOrders(snapshot.orders, conditionID, asset, orderID)
	items := buildBuiltinPolymarketItems(snapshot.companyID, positions, orders, snapshot.reviewContext)

	result := map[string]any{
		"source":          "polymarket",
		"company_id":      snapshot.companyID,
		"condition_id":    conditionID,
		"market":          conditionID,
		"asset":           asset,
		"order_id":        orderID,
		"positions":       positions,
		"orders":          orders,
		"items":           items,
		"positions_found": len(positions),
		"orders_found":    len(orders),
		"position_found":  len(positions) > 0,
		"order_found":     len(orders) > 0,
	}
	if len(positions) > 0 {
		result["position"] = positions[0]
	}
	if len(orders) > 0 {
		result["order"] = orders[0]
	}
	return result, nil
}

func extractBuiltinPolymarketPayload(params map[string]any) (builtinPolymarketPayload, bool, error) {
	if raw, ok := params["payload"]; ok {
		var payload builtinPolymarketPayload
		if err := decodeBuiltinPolymarketValue(raw, &payload); err != nil {
			return builtinPolymarketPayload{}, false, fmt.Errorf("invalid payload: %w", err)
		}
		payload = normalizeBuiltinPolymarketPayload(payload)
		return payload, true, nil
	}

	for _, key := range []string{"estimated_probability", "confidence", "reasoning", "remaining_capacity", "resolution_date", "tokens", "question", "aum", "max_allowed", "current_position"} {
		if _, ok := params[key]; ok {
			var payload builtinPolymarketPayload
			if err := decodeBuiltinPolymarketValue(params, &payload); err != nil {
				return builtinPolymarketPayload{}, false, fmt.Errorf("invalid polymarket params: %w", err)
			}
			payload = normalizeBuiltinPolymarketPayload(payload)
			return payload, true, nil
		}
	}

	return builtinPolymarketPayload{}, false, nil
}

func normalizeBuiltinPolymarketPayload(payload builtinPolymarketPayload) builtinPolymarketPayload {
	payload.ConditionID = strings.TrimSpace(payload.ConditionID)
	payload.Question = strings.TrimSpace(payload.Question)
	payload.Reasoning = strings.TrimSpace(payload.Reasoning)
	payload.ResolutionDate = strings.TrimSpace(payload.ResolutionDate)
	payload.Tokens = normalizeBuiltinPolymarketTokenRefs(payload.Tokens)
	return payload
}

func normalizeBuiltinPolymarketTokenRefs(tokens []builtinPolymarketTokenRef) []builtinPolymarketTokenRef {
	if len(tokens) == 0 {
		return nil
	}

	normalized := make([]builtinPolymarketTokenRef, 0, len(tokens))
	for _, token := range tokens {
		outcome := strings.TrimSpace(token.Outcome)
		tokenID := strings.TrimSpace(token.TokenID)
		if outcome == "" && tokenID == "" {
			continue
		}
		normalized = append(normalized, builtinPolymarketTokenRef{
			Outcome: outcome,
			TokenID: tokenID,
		})
	}
	if len(normalized) != 2 {
		return normalized
	}

	firstSide := normalizeBuiltinPolymarketSide(normalized[0].Outcome)
	secondSide := normalizeBuiltinPolymarketSide(normalized[1].Outcome)
	switch {
	case firstSide == "" && secondSide == "" && normalized[0].TokenID != "" && normalized[1].TokenID != "":
		normalized[0].Outcome = "Yes"
		normalized[1].Outcome = "No"
	case firstSide == "yes" && secondSide == "" && normalized[1].TokenID != "":
		normalized[1].Outcome = "No"
	case firstSide == "no" && secondSide == "" && normalized[1].TokenID != "":
		normalized[1].Outcome = "Yes"
	case secondSide == "yes" && firstSide == "" && normalized[0].TokenID != "":
		normalized[0].Outcome = "No"
	case secondSide == "no" && firstSide == "" && normalized[0].TokenID != "":
		normalized[0].Outcome = "Yes"
	}
	return normalized
}

func builtinPolymarketPayloadInputMap(params map[string]any) map[string]any {
	if params == nil {
		return nil
	}
	if raw, ok := params["payload"]; ok {
		switch typed := raw.(type) {
		case map[string]any:
			return typed
		case string:
			var decoded map[string]any
			if err := json.Unmarshal([]byte(typed), &decoded); err == nil {
				return decoded
			}
		}
	}
	return params
}

func builtinPolymarketPayloadIncludesAny(params map[string]any, keys ...string) bool {
	input := builtinPolymarketPayloadInputMap(params)
	if len(input) == 0 {
		return false
	}
	for _, key := range keys {
		if _, ok := input[key]; ok {
			return true
		}
	}
	return false
}

func refreshBuiltinPolymarketPayloadFromMarket(ctx context.Context, client builtinPolymarketClient, payload *builtinPolymarketPayload) *polymarket.Market {
	if client == nil || payload == nil {
		return nil
	}
	conditionID := strings.TrimSpace(payload.ConditionID)
	if conditionID == "" {
		return nil
	}

	market, err := client.GetMarket(ctx, conditionID)
	if err != nil || market == nil {
		return nil
	}

	if question := strings.TrimSpace(market.Question); question != "" {
		payload.Question = question
	}
	if endAt, ok := parseBuiltinPolymarketTime(strings.TrimSpace(market.EndDate)); ok {
		payload.ResolutionDate = endAt.UTC().Format("2006-01-02")
	}

	liveTokens, _ := buildBuiltinPolymarketFindMarketTokens(*market)
	if len(liveTokens) == 0 {
		return market
	}
	refs := make([]builtinPolymarketTokenRef, 0, len(liveTokens))
	for _, token := range liveTokens {
		outcome := strings.TrimSpace(fmt.Sprint(token["outcome"]))
		tokenID := strings.TrimSpace(fmt.Sprint(token["token_id"]))
		if outcome == "" || tokenID == "" {
			continue
		}
		refs = append(refs, builtinPolymarketTokenRef{
			Outcome: outcome,
			TokenID: tokenID,
		})
	}
	if len(refs) > 0 {
		payload.Tokens = refs
	}
	return market
}

func looksLikeLegacyPolymarketTradeParams(params map[string]any) bool {
	if params == nil {
		return false
	}
	if _, ok := params["action"]; ok {
		return true
	}
	for _, key := range []string{"price", "size", "side", "cancel_order_id"} {
		if _, ok := params[key]; ok {
			return true
		}
	}
	return false
}

func loadBuiltinPolymarketManageState(ctx context.Context, client builtinPolymarketClient, payload builtinPolymarketPayload) ([]polymarket.Position, []polymarket.Position, []polymarket.Order, builtinPolymarketExposure, string, string, error) {
	allPositions, err := client.GetPositions(ctx)
	if err != nil {
		return nil, nil, nil, builtinPolymarketExposure{}, "", "", fmt.Errorf("failed to list polymarket positions: %w", err)
	}
	positions := filterPolymarketPositions(allPositions, payload.ConditionID, "")

	orders, err := client.GetOrders(ctx, payload.ConditionID)
	if err != nil {
		return nil, nil, nil, builtinPolymarketExposure{}, "", "", fmt.Errorf("failed to list polymarket orders: %w", err)
	}
	orders = filterPolymarketOrders(orders, payload.ConditionID, "", "")

	yesTokenID, noTokenID := resolveBuiltinPolymarketTokenIDs(payload, positions, orders)
	exposure := buildBuiltinPolymarketExposure(positions, orders, yesTokenID, noTokenID)
	return allPositions, positions, orders, exposure, yesTokenID, noTokenID, nil
}

func resolveBuiltinPolymarketTokenIDs(payload builtinPolymarketPayload, positions []polymarket.Position, orders []polymarket.Order) (string, string) {
	var yesTokenID string
	var noTokenID string

	for _, token := range payload.Tokens {
		switch normalizeBuiltinPolymarketSide(token.Outcome) {
		case "yes":
			if yesTokenID == "" {
				yesTokenID = strings.TrimSpace(token.TokenID)
			}
		case "no":
			if noTokenID == "" {
				noTokenID = strings.TrimSpace(token.TokenID)
			}
		}
	}

	if yesTokenID == "" || noTokenID == "" {
		for _, position := range positions {
			switch normalizeBuiltinPolymarketSide(position.Outcome) {
			case "yes":
				if yesTokenID == "" {
					yesTokenID = strings.TrimSpace(position.Asset)
				}
			case "no":
				if noTokenID == "" {
					noTokenID = strings.TrimSpace(position.Asset)
				}
			}
		}
	}

	if yesTokenID == "" || noTokenID == "" {
		for _, order := range orders {
			switch normalizeBuiltinPolymarketSide(order.Outcome) {
			case "yes":
				if yesTokenID == "" {
					yesTokenID = strings.TrimSpace(order.AssetID)
				}
			case "no":
				if noTokenID == "" {
					noTokenID = strings.TrimSpace(order.AssetID)
				}
			}
		}
	}

	return yesTokenID, noTokenID
}

func buildBuiltinPolymarketExposure(positions []polymarket.Position, orders []polymarket.Order, yesTokenID, noTokenID string) builtinPolymarketExposure {
	var exposure builtinPolymarketExposure

	for _, position := range positions {
		switch classifyBuiltinPolymarketPosition(position, yesTokenID, noTokenID) {
		case "yes":
			exposure.YesHeldShares += math.Max(position.Size, 0)
		case "no":
			exposure.NoHeldShares += math.Max(position.Size, 0)
		}
	}

	for _, order := range orders {
		if !isBuiltinPolymarketOrderOpen(order) {
			continue
		}
		remainingSize := builtinPolymarketOrderRemainingSize(order)
		if remainingSize <= 0 {
			continue
		}

		side := classifyBuiltinPolymarketOrder(order, yesTokenID, noTokenID)
		switch {
		case side == "yes" && strings.EqualFold(strings.TrimSpace(order.Side), polymarket.Buy):
			exposure.YesOpenBuyShares += remainingSize
		case side == "no" && strings.EqualFold(strings.TrimSpace(order.Side), polymarket.Buy):
			exposure.NoOpenBuyShares += remainingSize
		case side == "yes" && strings.EqualFold(strings.TrimSpace(order.Side), polymarket.Sell):
			exposure.YesLockedSellShares += remainingSize
		case side == "no" && strings.EqualFold(strings.TrimSpace(order.Side), polymarket.Sell):
			exposure.NoLockedSellShares += remainingSize
		}
	}

	return exposure
}

func loadBuiltinPolymarketQuotes(ctx context.Context, client builtinPolymarketClient, conditionID, yesTokenID, noTokenID string) (builtinPolymarketQuotes, error) {
	var quotes builtinPolymarketQuotes
	quotes.Yes.TokenID = strings.TrimSpace(yesTokenID)
	quotes.No.TokenID = strings.TrimSpace(noTokenID)

	loadQuote := func(tokenID string, label string) (float64, float64, bool, bool, error) {
		tokenID = strings.TrimSpace(tokenID)
		if tokenID == "" {
			return 0, 0, false, false, nil
		}

		askRaw, err := client.GetPrice(ctx, tokenID, "buy")
		if err != nil {
			return 0, 0, false, false, fmt.Errorf("failed to get %s ask price: %w", label, err)
		}
		bidRaw, err := client.GetPrice(ctx, tokenID, "sell")
		if err != nil {
			return 0, 0, false, false, fmt.Errorf("failed to get %s bid price: %w", label, err)
		}

		ask, err := strconv.ParseFloat(strings.TrimSpace(askRaw), 64)
		if err != nil {
			return 0, 0, false, false, fmt.Errorf("failed to parse %s ask price %q: %w", label, strings.TrimSpace(askRaw), err)
		}
		bid, err := strconv.ParseFloat(strings.TrimSpace(bidRaw), 64)
		if err != nil {
			return 0, 0, false, false, fmt.Errorf("failed to parse %s bid price %q: %w", label, strings.TrimSpace(bidRaw), err)
		}
		return ask, bid, true, true, nil
	}

	type quoteResult struct {
		side   string
		ask    float64
		bid    float64
		hasAsk bool
		hasBid bool
		err    error
	}

	results := make(chan quoteResult, 2)
	loadAsync := func(side, tokenID, label string) {
		go func() {
			ask, bid, hasAsk, hasBid, err := loadQuote(tokenID, label)
			results <- quoteResult{
				side:   side,
				ask:    ask,
				bid:    bid,
				hasAsk: hasAsk,
				hasBid: hasBid,
				err:    err,
			}
		}()
	}

	loadAsync("yes", yesTokenID, "YES")
	loadAsync("no", noTokenID, "NO")

	var yesErr error
	var noErr error
	for i := 0; i < 2; i++ {
		result := <-results
		switch result.side {
		case "yes":
			quotes.Yes.AskPrice = result.ask
			quotes.Yes.BidPrice = result.bid
			quotes.Yes.HasAsk = result.hasAsk
			quotes.Yes.HasBid = result.hasBid
			yesErr = result.err
		case "no":
			quotes.No.AskPrice = result.ask
			quotes.No.BidPrice = result.bid
			quotes.No.HasAsk = result.hasAsk
			quotes.No.HasBid = result.hasBid
			noErr = result.err
		}
	}
	if yesErr == nil && noErr == nil {
		return quotes, nil
	}

	fallbackQuotes, ok, fallbackErr := loadBuiltinPolymarketQuotesFromMarket(ctx, client, conditionID, quotes)
	if ok {
		return fallbackQuotes, nil
	}
	if yesErr != nil {
		if fallbackErr != nil {
			return builtinPolymarketQuotes{}, fmt.Errorf("%v; fallback market quote lookup failed: %w", yesErr, fallbackErr)
		}
		return builtinPolymarketQuotes{}, yesErr
	}
	if fallbackErr != nil {
		return builtinPolymarketQuotes{}, fmt.Errorf("%v; fallback market quote lookup failed: %w", noErr, fallbackErr)
	}
	return builtinPolymarketQuotes{}, noErr
}

func loadBuiltinPolymarketQuotesFromMarket(ctx context.Context, client builtinPolymarketClient, conditionID string, quotes builtinPolymarketQuotes) (builtinPolymarketQuotes, bool, error) {
	conditionID = strings.TrimSpace(conditionID)
	if client == nil || conditionID == "" {
		return builtinPolymarketQuotes{}, false, nil
	}

	market, err := client.GetMarket(ctx, conditionID)
	if err != nil {
		return builtinPolymarketQuotes{}, false, err
	}
	if market == nil {
		return builtinPolymarketQuotes{}, false, nil
	}

	liveTokens, probability := buildBuiltinPolymarketFindMarketTokens(*market)
	yesTokenID := strings.TrimSpace(quotes.Yes.TokenID)
	noTokenID := strings.TrimSpace(quotes.No.TokenID)
	for _, token := range liveTokens {
		outcome := normalizeBuiltinPolymarketSide(fmt.Sprint(token["outcome"]))
		tokenID := strings.TrimSpace(fmt.Sprint(token["token_id"]))
		switch outcome {
		case "yes":
			if yesTokenID == "" {
				yesTokenID = tokenID
			}
		case "no":
			if noTokenID == "" {
				noTokenID = tokenID
			}
		}
	}

	yesAsk := math.Max(market.BestAsk, 0)
	yesBid := math.Max(market.BestBid, 0)
	noAsk := 0.0
	noBid := 0.0
	if yesBid > 0 && yesBid < 1 {
		noAsk = roundBuiltinPolymarketFloat(1-yesBid, 4)
	}
	if yesAsk > 0 && yesAsk < 1 {
		noBid = roundBuiltinPolymarketFloat(1-yesAsk, 4)
	}

	outcomePrices := decodeBuiltinPolymarketFloatList(strings.TrimSpace(market.OutcomePrices))
	if len(outcomePrices) >= 2 {
		if yesAsk <= 0 {
			yesAsk = roundBuiltinPolymarketFloat(math.Max(outcomePrices[0], 0), 4)
		}
		if yesBid <= 0 {
			yesBid = roundBuiltinPolymarketFloat(math.Max(outcomePrices[0], 0), 4)
		}
		if noAsk <= 0 {
			noAsk = roundBuiltinPolymarketFloat(math.Max(outcomePrices[1], 0), 4)
		}
		if noBid <= 0 {
			noBid = roundBuiltinPolymarketFloat(math.Max(outcomePrices[1], 0), 4)
		}
	} else if probability >= 0 && probability <= 1 {
		if yesAsk <= 0 {
			yesAsk = roundBuiltinPolymarketFloat(probability, 4)
		}
		if yesBid <= 0 {
			yesBid = roundBuiltinPolymarketFloat(probability, 4)
		}
		if noAsk <= 0 {
			noAsk = roundBuiltinPolymarketFloat(1-probability, 4)
		}
		if noBid <= 0 {
			noBid = roundBuiltinPolymarketFloat(1-probability, 4)
		}
	}

	if yesAsk <= 0 && yesBid <= 0 && noAsk <= 0 && noBid <= 0 {
		return builtinPolymarketQuotes{}, false, nil
	}

	fallback := builtinPolymarketQuotes{}
	fallback.Yes.TokenID = yesTokenID
	fallback.Yes.AskPrice = yesAsk
	fallback.Yes.BidPrice = yesBid
	fallback.Yes.HasAsk = yesAsk > 0
	fallback.Yes.HasBid = yesBid > 0
	fallback.No.TokenID = noTokenID
	fallback.No.AskPrice = noAsk
	fallback.No.BidPrice = noBid
	fallback.No.HasAsk = noAsk > 0
	fallback.No.HasBid = noBid > 0
	return fallback, true, nil
}

func loadBuiltinPolymarketExecutionMetrics(ctx context.Context, client builtinPolymarketClient, tokenID string) (builtinPolymarketExecutionMetrics, error) {
	metrics := builtinPolymarketExecutionMetrics{TokenID: strings.TrimSpace(tokenID)}
	tokenID = strings.TrimSpace(tokenID)
	if client == nil || tokenID == "" {
		return metrics, nil
	}

	book, err := client.GetOrderBook(ctx, tokenID)
	if err != nil {
		return metrics, err
	}
	if book == nil {
		return metrics, nil
	}

	bids, err := parseOrderBookSide(book.Bids, "bid")
	if err != nil {
		return metrics, err
	}
	asks, err := parseOrderBookSide(book.Asks, "ask")
	if err != nil {
		return metrics, err
	}
	if len(bids) > 0 {
		sort.Slice(bids, func(i, j int) bool { return bids[i].price > bids[j].price })
		metrics.BestBid = bids[0].price
		metrics.BestBidSize = bids[0].size
		metrics.HasBid = true
	}
	if len(asks) > 0 {
		sort.Slice(asks, func(i, j int) bool { return asks[i].price < asks[j].price })
		metrics.BestAsk = asks[0].price
		metrics.BestAskSize = asks[0].size
		metrics.HasAsk = true
	}
	metrics.HasOrderBook = metrics.HasBid || metrics.HasAsk
	if metrics.HasBid && metrics.HasAsk && metrics.BestAsk >= metrics.BestBid {
		metrics.Spread = roundBuiltinPolymarketFloat(metrics.BestAsk-metrics.BestBid, 6)
		metrics.Midpoint = roundBuiltinPolymarketFloat((metrics.BestAsk+metrics.BestBid)/2, 6)
	} else if metrics.HasAsk {
		metrics.Midpoint = metrics.BestAsk
	} else if metrics.HasBid {
		metrics.Midpoint = metrics.BestBid
	}
	return metrics, nil
}

func selectBuiltinPolymarketTradeCandidate(payload builtinPolymarketPayload, quotes builtinPolymarketQuotes) builtinPolymarketTradeCandidate {
	probability := clampBuiltinPolymarketProbability(payload.EstimatedProbability)

	candidate := builtinPolymarketTradeCandidate{}
	bestEdge := math.Inf(-1)

	if quotes.Yes.HasAsk && quotes.Yes.TokenID != "" {
		edge := probability - quotes.Yes.AskPrice
		relative := 0.0
		if quotes.Yes.AskPrice > 0 {
			relative = edge / quotes.Yes.AskPrice
		}
		bestEdge = edge
		candidate = builtinPolymarketTradeCandidate{
			Side:         "yes",
			TokenID:      quotes.Yes.TokenID,
			AskPrice:     quotes.Yes.AskPrice,
			BidPrice:     quotes.Yes.BidPrice,
			AbsoluteEdge: edge,
			RelativeEdge: relative,
		}
	}

	if quotes.No.HasAsk && quotes.No.TokenID != "" {
		noProbability := 1 - probability
		edge := noProbability - quotes.No.AskPrice
		relative := 0.0
		if quotes.No.AskPrice > 0 {
			relative = edge / quotes.No.AskPrice
		}
		if edge > bestEdge {
			candidate = builtinPolymarketTradeCandidate{
				Side:         "no",
				TokenID:      quotes.No.TokenID,
				AskPrice:     quotes.No.AskPrice,
				BidPrice:     quotes.No.BidPrice,
				AbsoluteEdge: edge,
				RelativeEdge: relative,
			}
		}
	}

	return candidate
}

func deriveBuiltinPolymarketCapacity(payload builtinPolymarketPayload, currentPosition float64) (float64, float64) {
	maxAllowed := math.Max(payload.MaxAllowed, 0)
	if payload.AUM > 0 {
		// Treat AUM as the freshest cap signal when it is present so position
		// sizing can expand or contract with the latest capital base.
		maxAllowed = payload.AUM / 20
	}
	if maxAllowed <= 0 && payload.RemainingCapacity > 0 {
		baseCurrent := math.Max(payload.CurrentPosition, 0)
		if baseCurrent <= 0 {
			baseCurrent = currentPosition
		}
		maxAllowed = baseCurrent + payload.RemainingCapacity
	}
	if maxAllowed <= 0 {
		maxAllowed = currentPosition
	}

	// remaining_capacity is derivative state. Recompute it from the live held
	// position whenever we have a cap so AUM/max_allowed changes can both upsize
	// and downsize exposure.
	remainingCapacity := math.Max(maxAllowed-currentPosition, 0)
	if maxAllowed <= 0 {
		remainingCapacity = math.Max(payload.RemainingCapacity, 0)
	}
	return maxAllowed, remainingCapacity
}

func deriveBuiltinPolymarketTargetPosition(payload builtinPolymarketPayload, maxAllowed, currentTargetPosition, confidenceScale float64) float64 {
	currentTargetPosition = math.Max(currentTargetPosition, 0)
	if payload.AUM <= 0 && payload.MaxAllowed <= 0 && payload.RemainingCapacity <= 0 {
		return currentTargetPosition
	}
	if maxAllowed <= 0 || confidenceScale <= 0 {
		return 0
	}
	targetPosition := math.Max(maxAllowed*confidenceScale, 0)
	if targetPosition > maxAllowed {
		targetPosition = maxAllowed
	}
	return roundBuiltinPolymarketFloat(targetPosition, 4)
}

func builtinPolymarketConfidenceScale(confidence float64) float64 {
	confidence = clampBuiltinPolymarketProbability(confidence)
	switch {
	case confidence <= 0.45:
		return 0
	case confidence <= 0.50:
		return roundBuiltinPolymarketFloat(builtinPolymarketLerp((confidence-0.45)/0.05, 0, 0.25), 6)
	case confidence <= 0.60:
		return roundBuiltinPolymarketFloat(builtinPolymarketLerp((confidence-0.50)/0.10, 0.25, 0.50), 6)
	case confidence <= 0.80:
		return roundBuiltinPolymarketFloat(builtinPolymarketLerp((confidence-0.60)/0.20, 0.50, 1.0), 6)
	default:
		return 1.0
	}
}

func builtinPolymarketLerp(progress, start, end float64) float64 {
	if progress <= 0 {
		return start
	}
	if progress >= 1 {
		return end
	}
	return start + ((end - start) * progress)
}

func builtinPolymarketOrderBelowVenueMinimum(size float64) bool {
	size = roundBuiltinPolymarketFloat(math.Max(size, 0), 4)
	return size > 0 && size+0.0001 < builtinPolymarketMinOrderShares
}

func builtinPolymarketIsVenueMinimumError(errText string) bool {
	lower := strings.ToLower(strings.TrimSpace(errText))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "lower than the minimum") || strings.Contains(lower, "minimum order size")
}

func builtinPolymarketIsInsufficientBalanceError(errText string) bool {
	lower := strings.ToLower(strings.TrimSpace(errText))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "not enough balance") ||
		strings.Contains(lower, "balance is not enough") ||
		(strings.Contains(lower, "balance") && strings.Contains(lower, "allowance"))
}

func builtinPolymarketIsUnavailableQuoteError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "no orderbook exists") ||
		strings.Contains(lower, "market not found")
}

func loadBuiltinPolymarketLatestThesisNote(ctx context.Context, pe *PipelineEngine, companyID, conditionID string) (*data.MarketNote, *data.MarketNoteMetadata, error) {
	if pe == nil || pe.db == nil {
		return nil, nil, nil
	}
	companyID = strings.TrimSpace(companyID)
	conditionID = strings.TrimSpace(conditionID)
	if companyID == "" || conditionID == "" {
		return nil, nil, nil
	}

	notes, err := data.ListMarketNotes(ctx, pe.db, companyID, conditionID, 25)
	if err != nil {
		return nil, nil, err
	}
	for _, note := range notes {
		metadata := data.ParseMarketNoteMetadata(note)
		if !builtinPolymarketNoteMetadataHasReusableThesis(metadata) {
			continue
		}
		return note, metadata, nil
	}
	return nil, nil, nil
}

func builtinPolymarketNoteMetadataHasReusableThesis(metadata *data.MarketNoteMetadata) bool {
	if metadata == nil || metadata.EstimatedProbability == nil || metadata.Confidence == nil {
		return false
	}
	probability := *metadata.EstimatedProbability
	confidence := *metadata.Confidence
	if probability < 0 || probability > 1 || confidence < 0 || confidence > 1 {
		return false
	}
	if confidence > 0 {
		return true
	}
	return strings.TrimSpace(metadata.Reasoning) != ""
}

func hydrateBuiltinPolymarketPayloadFromLatestNote(payload *builtinPolymarketPayload, metadata *data.MarketNoteMetadata) bool {
	if payload == nil || metadata == nil {
		return false
	}
	if metadata.EstimatedProbability == nil || metadata.Confidence == nil {
		return false
	}

	if payload.Question == "" {
		payload.Question = strings.TrimSpace(metadata.Question)
	}
	if payload.Reasoning == "" {
		payload.Reasoning = strings.TrimSpace(metadata.Reasoning)
	}
	if payload.ResolutionDate == "" {
		payload.ResolutionDate = strings.TrimSpace(metadata.ResolutionDate)
	}
	payload.EstimatedProbability = clampBuiltinPolymarketProbability(*metadata.EstimatedProbability)
	payload.Confidence = clampBuiltinPolymarketProbability(*metadata.Confidence)
	return true
}

func evaluateBuiltinPolymarketThesisDrift(payload builtinPolymarketPayload, currentSide string, note *data.MarketNote, metadata *data.MarketNoteMetadata) builtinPolymarketThesisDrift {
	drift := builtinPolymarketThesisDrift{
		CurrentSide:    normalizeBuiltinPolymarketSide(currentSide),
		RetentionScale: 1.0,
	}
	if drift.CurrentSide == "" {
		drift.CurrentSide = fallbackBuiltinPolymarketSide(payload.EstimatedProbability)
	}
	if note == nil || metadata == nil {
		return drift
	}

	referenceAt := note.CreatedAt.UTC()
	if !metadata.CapturedAt.IsZero() {
		referenceAt = metadata.CapturedAt.UTC()
	}
	drift.ReferenceTimestamp = referenceAt
	if referenceAt.IsZero() || time.Since(referenceAt) > builtinPolymarketThesisActiveDays*24*time.Hour {
		return drift
	}

	drift.HasPrior = true
	drift.PriorSide = normalizeBuiltinPolymarketSide(metadata.Side)
	if drift.PriorSide == "" && metadata.EstimatedProbability != nil {
		drift.PriorSide = fallbackBuiltinPolymarketSide(*metadata.EstimatedProbability)
	}
	drift.ThesisChanged = metadata.ThesisHash != "" && metadata.ThesisHash != builtinPolymarketPayloadThesisHash(payload, drift.CurrentSide)

	if metadata.Confidence != nil {
		drift.PriorConfidence = roundBuiltinPolymarketFloat(*metadata.Confidence, 6)
		drift.ConfidenceDelta = roundBuiltinPolymarketFloat(payload.Confidence-*metadata.Confidence, 6)
	}
	if metadata.EstimatedProbability != nil {
		drift.PriorEstimatedProb = roundBuiltinPolymarketFloat(*metadata.EstimatedProbability, 6)
	}

	if drift.PriorSide == "" || metadata.EstimatedProbability == nil {
		if drift.ThesisChanged {
			drift.Reason = "recent thesis changed"
		}
		return drift
	}

	drift.SideChanged = drift.PriorSide != "" && drift.CurrentSide != "" && drift.PriorSide != drift.CurrentSide
	if drift.SideChanged {
		drift.Reason = "recent thesis side flipped"
		return drift
	}

	drift.PriorSideProbability = roundBuiltinPolymarketFloat(builtinPolymarketProbabilityForSide(drift.CurrentSide, *metadata.EstimatedProbability), 6)
	drift.CurrentSideProbability = roundBuiltinPolymarketFloat(builtinPolymarketProbabilityForSide(drift.CurrentSide, payload.EstimatedProbability), 6)
	drift.ProbabilityDelta = roundBuiltinPolymarketFloat(drift.CurrentSideProbability-drift.PriorSideProbability, 6)

	negativeProbabilityDrop := math.Max(drift.PriorSideProbability-drift.CurrentSideProbability, 0)
	negativeConfidenceDrop := math.Max(-drift.ConfidenceDelta, 0)
	severity := (negativeProbabilityDrop / 0.18 * 0.65) + (negativeConfidenceDrop / 0.35 * 0.35)
	if severity < 0 {
		severity = 0
	}
	if severity > 1 {
		severity = 1
	}
	drift.Severity = roundBuiltinPolymarketFloat(severity, 6)
	if drift.Severity <= 0 {
		if drift.ThesisChanged {
			drift.Reason = "recent thesis changed"
		}
		return drift
	}

	drift.RetentionScale = roundBuiltinPolymarketFloat(math.Max(0.35, 1-(drift.Severity*0.6)), 6)
	if drift.Severity >= 0.6 && negativeProbabilityDrop >= 0.05 {
		drift.BlockNewExposure = true
	}
	if drift.BlockNewExposure {
		drift.Reason = "recent thesis weakened materially"
	} else {
		drift.Reason = "recent thesis weakened"
	}
	return drift
}

func applyBuiltinPolymarketThesisDrift(result map[string]any, baseScale, positionScale float64, drift builtinPolymarketThesisDrift) {
	if result == nil {
		return
	}
	result["base_confidence_scale"] = roundBuiltinPolymarketFloat(baseScale, 4)
	result["position_scale"] = roundBuiltinPolymarketFloat(positionScale, 4)
	if !drift.HasPrior {
		return
	}
	result["thesis_retention_scale"] = roundBuiltinPolymarketFloat(drift.RetentionScale, 4)
	result["thesis_drift_score"] = roundBuiltinPolymarketFloat(drift.Severity, 4)
	result["thesis_changed"] = drift.ThesisChanged
	result["thesis_side_changed"] = drift.SideChanged
	result["thesis_blocks_adds"] = drift.BlockNewExposure
	if drift.PriorSide != "" {
		result["prior_thesis_side"] = drift.PriorSide
	}
	if drift.PriorConfidence > 0 {
		result["prior_thesis_confidence"] = roundBuiltinPolymarketFloat(drift.PriorConfidence, 4)
	}
	if drift.PriorEstimatedProb > 0 {
		result["prior_thesis_probability"] = roundBuiltinPolymarketFloat(drift.PriorEstimatedProb, 4)
	}
	if drift.PriorSideProbability > 0 {
		result["prior_side_probability"] = roundBuiltinPolymarketFloat(drift.PriorSideProbability, 4)
	}
	if drift.CurrentSideProbability > 0 {
		result["current_side_probability"] = roundBuiltinPolymarketFloat(drift.CurrentSideProbability, 4)
	}
	if drift.ConfidenceDelta != 0 {
		result["thesis_confidence_delta"] = roundBuiltinPolymarketFloat(drift.ConfidenceDelta, 4)
	}
	if drift.ProbabilityDelta != 0 {
		result["thesis_probability_delta"] = roundBuiltinPolymarketFloat(drift.ProbabilityDelta, 4)
	}
	if !drift.ReferenceTimestamp.IsZero() {
		result["prior_thesis_at"] = drift.ReferenceTimestamp.UTC().Format(time.RFC3339)
	}
	if drift.Reason != "" {
		result["thesis_reason"] = drift.Reason
	}
}

func evaluateBuiltinPolymarketExecutionSignal(candidate builtinPolymarketTradeCandidate, metrics builtinPolymarketExecutionMetrics, desiredShares float64) builtinPolymarketExecutionSignal {
	signal := builtinPolymarketExecutionSignal{
		DesiredShares: roundBuiltinPolymarketFloat(math.Max(desiredShares, 0), 4),
		Scale:         1.0,
	}

	absoluteEdge := math.Max(candidate.AbsoluteEdge, 0)
	if absoluteEdge <= 0 {
		signal.Scale = 0
		return signal
	}

	spread := 0.0
	switch {
	case metrics.HasBid && metrics.HasAsk && metrics.Spread > 0:
		spread = metrics.Spread
	case candidate.AskPrice > 0 && candidate.BidPrice > 0 && candidate.AskPrice >= candidate.BidPrice:
		spread = candidate.AskPrice - candidate.BidPrice
	}
	signal.SpreadCost = roundBuiltinPolymarketFloat(math.Max(spread/2, 0), 6)

	if signal.DesiredShares > 0 {
		switch {
		case metrics.HasAsk && metrics.BestAskSize > 0:
			pressure := signal.DesiredShares / metrics.BestAskSize
			switch {
			case pressure <= 1:
				signal.SlippagePenalty = 0
			case pressure <= 2:
				signal.SlippagePenalty = roundBuiltinPolymarketFloat(math.Max(spread, 0)*0.25*(pressure-1), 6)
			default:
				signal.SlippagePenalty = roundBuiltinPolymarketFloat((math.Max(spread, 0)*0.25)+math.Min(0.05, 0.01*(pressure-2)), 6)
			}
		case metrics.HasOrderBook:
			signal.SlippagePenalty = roundBuiltinPolymarketFloat(math.Max(math.Max(spread*0.5, 0.01), 0), 6)
		}
	}

	signal.NetEdge = roundBuiltinPolymarketFloat(math.Max(absoluteEdge-signal.SpreadCost-signal.SlippagePenalty, 0), 6)
	signal.Scale = builtinPolymarketNetEdgeScale(signal.NetEdge)
	return signal
}

func applyBuiltinPolymarketExecutionSignal(result map[string]any, signal builtinPolymarketExecutionSignal) {
	if result == nil {
		return
	}
	result["gross_desired_shares"] = roundBuiltinPolymarketFloat(signal.DesiredShares, 4)
	result["ev_desired_shares"] = roundBuiltinPolymarketFloat(signal.DesiredShares*signal.Scale, 4)
	result["spread_cost"] = roundBuiltinPolymarketFloat(signal.SpreadCost, 4)
	result["slippage_penalty"] = roundBuiltinPolymarketFloat(signal.SlippagePenalty, 4)
	result["net_edge"] = roundBuiltinPolymarketFloat(signal.NetEdge, 4)
	result["signal_scale"] = roundBuiltinPolymarketFloat(signal.Scale, 4)
}

func builtinPolymarketNetEdgeScale(netEdge float64) float64 {
	switch {
	case netEdge <= 0.01:
		return 0
	case netEdge >= 0.06:
		return 1
	default:
		return roundBuiltinPolymarketFloat((netEdge-0.01)/0.05, 6)
	}
}

func builtinPolymarketProbabilityForSide(side string, estimatedProbability float64) float64 {
	switch normalizeBuiltinPolymarketSide(side) {
	case "no":
		return 1 - clampBuiltinPolymarketProbability(estimatedProbability)
	default:
		return clampBuiltinPolymarketProbability(estimatedProbability)
	}
}

func applyBuiltinPolymarketExecutionMetrics(result map[string]any, metrics builtinPolymarketExecutionMetrics) {
	if result == nil || !metrics.HasOrderBook {
		return
	}
	result["book_best_bid"] = roundBuiltinPolymarketFloat(metrics.BestBid, 4)
	result["book_best_ask"] = roundBuiltinPolymarketFloat(metrics.BestAsk, 4)
	result["book_best_bid_size"] = roundBuiltinPolymarketFloat(metrics.BestBidSize, 4)
	result["book_best_ask_size"] = roundBuiltinPolymarketFloat(metrics.BestAskSize, 4)
	result["book_spread"] = roundBuiltinPolymarketFloat(metrics.Spread, 4)
	result["book_midpoint"] = roundBuiltinPolymarketFloat(metrics.Midpoint, 4)
}

func builtinPolymarketBuyOrderNeedsUpdate(order polymarket.Order, desiredPrice float64, desiredType string, tier int) bool {
	orderPrice := 0.0
	if strings.TrimSpace(order.Price) != "" {
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(order.Price), 64); err == nil {
			orderPrice = parsed
		}
	}
	if orderPrice <= 0 {
		return true
	}

	currentType := strings.ToUpper(strings.TrimSpace(order.Type))
	if currentType != "" && desiredType != "" && currentType != strings.ToUpper(strings.TrimSpace(desiredType)) {
		return true
	}

	tolerance := 0.005
	if tier == 1 {
		return math.Abs(orderPrice-desiredPrice) > tolerance
	}
	return math.Abs(orderPrice-desiredPrice) > tolerance
}

func newBuiltinPolymarketManageResult(companyID string, payload builtinPolymarketPayload) map[string]any {
	return map[string]any{
		"source":                "polymarket",
		"company_id":            strings.TrimSpace(companyID),
		"condition_id":          strings.TrimSpace(payload.ConditionID),
		"question":              strings.TrimSpace(payload.Question),
		"side":                  "",
		"aum":                   roundBuiltinPolymarketFloat(math.Max(payload.AUM, 0), 4),
		"confidence":            roundBuiltinPolymarketFloat(payload.Confidence, 4),
		"estimated_probability": roundBuiltinPolymarketFloat(payload.EstimatedProbability, 4),
		"market_price":          0.0,
		"current_position":      roundBuiltinPolymarketFloat(math.Max(payload.CurrentPosition, 0), 4),
		"max_allowed":           roundBuiltinPolymarketFloat(math.Max(payload.MaxAllowed, 0), 4),
		"remaining_capacity":    roundBuiltinPolymarketFloat(math.Max(payload.RemainingCapacity, 0), 4),
		"target_side_position":  0.0,
		"target_position":       0.0,
		"target_gap":            0.0,
		"action_taken":          "",
		"orders_cancelled":      0,
		"orders_updated":        0,
		"sell_orders_placed":    0,
		"buy_orders_placed":     0,
		"amount":                0.0,
		"price":                 0.0,
		"order_id":              "none",
		"status":                "neutral",
		"edge":                  0.0,
		"relative_edge":         0.0,
		"sizing_context_source": "payload",
		"thesis_input_source":   "payload",
	}
}

func builtinPolymarketFailedResult(base map[string]any, errText string, diagnostics map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	errText = strings.TrimSpace(errText)
	if errText == "" {
		errText = "polymarket action failed"
	}
	base["status"] = "FAILED: " + errText
	base["error"] = errText
	if currentAction := strings.TrimSpace(stringParam(base, "action_taken")); currentAction == "" {
		base["action_taken"] = errText
	}
	for key, value := range diagnostics {
		base[key] = value
	}
	return base
}

func maybeAnnotateBuiltinPolymarketResult(ctx context.Context, pe *PipelineEngine, companyID string, payload builtinPolymarketPayload, market *polymarket.Market, result map[string]any, actionNotes []string) map[string]any {
	if pe == nil || pe.db == nil || strings.TrimSpace(companyID) == "" || strings.TrimSpace(payload.ConditionID) == "" {
		return result
	}
	if market != nil && builtinPolymarketMarketVolume(*market) < builtinPolymarketFindMarketsMinVolume {
		result["market_note_skipped"] = "low_volume"
		return result
	}

	notes := builtinPolymarketAnnotationNotes(actionNotes, result)
	if len(notes) == 0 {
		return result
	}

	note := buildBuiltinPolymarketNote(payload, result, notes)
	if note == "" {
		return result
	}
	created, err := data.AddMarketNoteWithMetadata(
		ctx,
		pe.db,
		strings.TrimSpace(companyID),
		"builtin_polymarket_manage_position",
		strings.TrimSpace(payload.ConditionID),
		note,
		buildBuiltinPolymarketNoteMetadata(payload, market, result),
	)
	if err != nil {
		result["market_note_error"] = strings.TrimSpace(err.Error())
		return result
	}
	result["market_note_id"] = created.ID
	return result
}

func buildBuiltinPolymarketNote(payload builtinPolymarketPayload, result map[string]any, actionNotes []string) string {
	lines := []string{
		"Builtin polymarket_manage_position execution",
	}
	if question := strings.TrimSpace(payload.Question); question != "" {
		lines = append(lines, "Question: "+question)
	}
	if status := strings.TrimSpace(stringParam(result, "status")); status != "" {
		lines = append(lines, "Status: "+status)
	}
	if len(actionNotes) > 0 {
		lines = append(lines, strings.Join(actionNotes, " "))
	}

	if side := strings.TrimSpace(stringParam(result, "side")); side != "" {
		lines = append(lines, fmt.Sprintf("Side: %s", strings.ToUpper(side)))
	}
	if price, ok := floatParam(result, "price"); ok && price > 0 {
		lines = append(lines, fmt.Sprintf("Price: %.4f", price))
	}
	if amount, ok := floatParam(result, "amount"); ok && amount > 0 {
		lines = append(lines, fmt.Sprintf("Amount: %.4f shares", amount))
	}
	if edge, ok := floatParam(result, "edge"); ok && edge > 0 {
		relative, _ := floatParam(result, "relative_edge")
		lines = append(lines, fmt.Sprintf("Edge: %.4f absolute / %.4f relative", edge, relative))
	}
	if reasoning := truncateBuiltinPolymarketString(payload.Reasoning, 500); reasoning != "" {
		lines = append(lines, "Research: "+reasoning)
	}
	if errText := truncateBuiltinPolymarketString(stringParam(result, "error"), 500); errText != "" {
		lines = append(lines, "Error: "+errText)
	}

	note := strings.Join(lines, "\n")
	return truncateBuiltinPolymarketString(note, 1900)
}

func buildBuiltinPolymarketNoteMetadata(payload builtinPolymarketPayload, market *polymarket.Market, result map[string]any) *data.MarketNoteMetadata {
	thesisInputSource := strings.ToLower(strings.TrimSpace(stringParam(result, "thesis_input_source")))
	metadata := &data.MarketNoteMetadata{
		Kind:           "builtin_polymarket_manage_position",
		Status:         strings.TrimSpace(stringParam(result, "status")),
		Action:         strings.TrimSpace(stringParam(result, "action_taken")),
		Side:           normalizeBuiltinPolymarketSide(stringParam(result, "side")),
		Question:       strings.TrimSpace(payload.Question),
		Reasoning:      truncateBuiltinPolymarketString(payload.Reasoning, 1000),
		Invalidation:   "Reevaluate if market pricing, liquidity, or resolution details move materially away from this thesis.",
		ResolutionDate: strings.TrimSpace(payload.ResolutionDate),
		CapturedAt:     time.Now().UTC(),
	}
	if thesisInputSource != "missing" && payload.EstimatedProbability >= 0 && payload.EstimatedProbability <= 1 {
		metadata.EstimatedProbability = builtinPolymarketFloatPointer(payload.EstimatedProbability)
	}
	if thesisInputSource != "missing" && payload.Confidence >= 0 && payload.Confidence <= 1 {
		metadata.Confidence = builtinPolymarketFloatPointer(payload.Confidence)
	}
	if value, ok := floatParam(result, "current_position"); ok {
		metadata.CurrentPosition = builtinPolymarketFloatPointer(value)
	}
	if value, ok := floatParam(result, "max_allowed"); ok {
		metadata.MaxAllowed = builtinPolymarketFloatPointer(value)
	}
	if value, ok := floatParam(result, "remaining_capacity"); ok {
		metadata.RemainingCapacity = builtinPolymarketFloatPointer(value)
	}
	if value, ok := floatParam(result, "price"); ok {
		metadata.Price = builtinPolymarketFloatPointer(value)
	} else if value, ok := floatParam(result, "market_price"); ok {
		metadata.Price = builtinPolymarketFloatPointer(value)
	}
	if value, ok := floatParam(result, "edge"); ok {
		metadata.Edge = builtinPolymarketFloatPointer(value)
	}
	if value, ok := floatParam(result, "relative_edge"); ok {
		metadata.RelativeEdge = builtinPolymarketFloatPointer(value)
	}
	if value, ok := floatParam(result, "book_spread"); ok {
		metadata.Spread = builtinPolymarketFloatPointer(value)
	}
	if value, ok := floatParam(result, "book_best_ask_size"); ok {
		metadata.OrderbookDepth = builtinPolymarketFloatPointer(value)
	} else if value, ok := floatParam(result, "book_best_bid_size"); ok {
		metadata.OrderbookDepth = builtinPolymarketFloatPointer(value)
	}
	if market != nil {
		marketProbability := builtinPolymarketMarketProbability(*market)
		if marketProbability >= 0 {
			metadata.MarketProbability = builtinPolymarketFloatPointer(marketProbability)
		}
		if volume := builtinPolymarketMarketVolume(*market); volume > 0 {
			metadata.MarketVolume = builtinPolymarketFloatPointer(volume)
		}
		if volume24hr := math.Max(market.Volume24hr, 0); volume24hr > 0 {
			metadata.MarketVolume24hr = builtinPolymarketFloatPointer(volume24hr)
		}
		if liquidity := parseBuiltinPolymarketFloatString(market.Liquidity); liquidity > 0 {
			metadata.Liquidity = builtinPolymarketFloatPointer(liquidity)
		}
		if spread := builtinPolymarketMarketSpread(*market); spread >= 0 {
			metadata.Spread = builtinPolymarketFloatPointer(spread)
		}
		if endAt, ok := parseBuiltinPolymarketTime(market.EndDate); ok {
			metadata.DaysToEnd = builtinPolymarketFloatPointer(math.Max(endAt.Sub(time.Now().UTC()).Hours()/24, 0))
			if metadata.ResolutionDate == "" {
				metadata.ResolutionDate = endAt.UTC().Format(time.RFC3339)
			}
		}
		metadata.MarketFingerprint = builtinPolymarketMarketFingerprint(*market)
	}
	if thesisInputSource != "missing" {
		metadata.ThesisHash = builtinPolymarketPayloadThesisHash(payload, metadata.Side)
	}
	return metadata
}

func builtinPolymarketAnnotationNotes(actionNotes []string, result map[string]any) []string {
	notes := make([]string, 0, len(actionNotes)+1)
	for _, note := range actionNotes {
		note = strings.TrimSpace(note)
		if note != "" {
			notes = append(notes, note)
		}
	}
	if len(notes) > 0 {
		return notes
	}
	if fallback := strings.TrimSpace(stringParam(result, "action_taken")); fallback != "" {
		return []string{fallback}
	}
	if fallback := strings.TrimSpace(stringParam(result, "status")); fallback != "" {
		return []string{fallback}
	}
	return nil
}

func builtinPolymarketResolutionPassed(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return time.Now().UTC().After(parsed.UTC())
		}
	}
	return false
}

func builtinPolymarketMidpoint(quote builtinPolymarketQuote) float64 {
	switch {
	case quote.HasAsk && quote.HasBid:
		return (quote.AskPrice + quote.BidPrice) / 2
	case quote.HasAsk:
		return quote.AskPrice
	case quote.HasBid:
		return quote.BidPrice
	default:
		return 0
	}
}

func setBuiltinPolymarketManageCompletionProperties(ctx context.Context, pe *PipelineEngine, companyID, conditionID string, result map[string]any) {
	if pe == nil || pe.db == nil {
		return
	}
	companyID = strings.TrimSpace(companyID)
	conditionID = strings.TrimSpace(conditionID)
	if companyID == "" || conditionID == "" {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = data.SetMarketProperty(ctx, pe.db, companyID, conditionID, "last_managed_at", now, data.MarketPropertyTypeDatetime)

	succeeded := !strings.HasPrefix(strings.TrimSpace(stringParam(result, "status")), "FAILED")
	_, _ = data.SetMarketProperty(ctx, pe.db, companyID, conditionID, "management_result", fmt.Sprintf("%t", succeeded), data.MarketPropertyTypeBool)
}

func builtinPolymarketPayloadThesisHash(payload builtinPolymarketPayload, side string) string {
	side = normalizeBuiltinPolymarketSide(side)
	if side == "" {
		side = fallbackBuiltinPolymarketSide(payload.EstimatedProbability)
	}
	estimatedProbability := builtinPolymarketFloatPointer(payload.EstimatedProbability)
	confidence := builtinPolymarketFloatPointer(payload.Confidence)
	return polymarketStableHash(
		strings.TrimSpace(payload.ConditionID),
		strings.TrimSpace(payload.Question),
		truncateBuiltinPolymarketString(payload.Reasoning, 1000),
		formatBuiltinPolymarketHashFloat(estimatedProbability),
		formatBuiltinPolymarketHashFloat(confidence),
		side,
		strings.TrimSpace(payload.ResolutionDate),
	)
}
