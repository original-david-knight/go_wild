package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func normalizeBuiltinPolymarketSide(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "yes":
		return "yes"
	case "no":
		return "no"
	default:
		return ""
	}
}

func fallbackBuiltinPolymarketSide(probability float64) string {
	if clampBuiltinPolymarketProbability(probability) >= 0.5 {
		return "yes"
	}
	return "no"
}

func oppositeBuiltinPolymarketSide(side string) string {
	switch normalizeBuiltinPolymarketSide(side) {
	case "yes":
		return "no"
	case "no":
		return "yes"
	default:
		return ""
	}
}

func tokenIDForBuiltinPolymarketSide(side, yesTokenID, noTokenID string) string {
	switch normalizeBuiltinPolymarketSide(side) {
	case "yes":
		return strings.TrimSpace(yesTokenID)
	case "no":
		return strings.TrimSpace(noTokenID)
	default:
		return ""
	}
}

func quoteForBuiltinPolymarketSide(side string, quotes builtinPolymarketQuotes) builtinPolymarketQuote {
	switch normalizeBuiltinPolymarketSide(side) {
	case "yes":
		return quotes.Yes
	case "no":
		return quotes.No
	default:
		return builtinPolymarketQuote{}
	}
}

func heldSharesForBuiltinPolymarketSide(side string, exposure builtinPolymarketExposure) float64 {
	switch normalizeBuiltinPolymarketSide(side) {
	case "yes":
		return exposure.YesHeldShares
	case "no":
		return exposure.NoHeldShares
	default:
		return 0
	}
}

func lockedSellSharesForBuiltinPolymarketSide(side string, exposure builtinPolymarketExposure) float64 {
	switch normalizeBuiltinPolymarketSide(side) {
	case "yes":
		return exposure.YesLockedSellShares
	case "no":
		return exposure.NoLockedSellShares
	default:
		return 0
	}
}

func classifyBuiltinPolymarketPosition(position polymarket.Position, yesTokenID, noTokenID string) string {
	if asset := strings.TrimSpace(position.Asset); asset != "" {
		switch {
		case yesTokenID != "" && strings.EqualFold(asset, yesTokenID):
			return "yes"
		case noTokenID != "" && strings.EqualFold(asset, noTokenID):
			return "no"
		}
	}
	return normalizeBuiltinPolymarketSide(position.Outcome)
}

func classifyBuiltinPolymarketOrder(order polymarket.Order, yesTokenID, noTokenID string) string {
	if asset := strings.TrimSpace(order.AssetID); asset != "" {
		switch {
		case yesTokenID != "" && strings.EqualFold(asset, yesTokenID):
			return "yes"
		case noTokenID != "" && strings.EqualFold(asset, noTokenID):
			return "no"
		}
	}
	return normalizeBuiltinPolymarketSide(order.Outcome)
}

func isBuiltinPolymarketOrderOpen(order polymarket.Order) bool {
	status := strings.ToLower(strings.TrimSpace(order.Status))
	switch status {
	case "canceled", "cancelled", "filled", "matched", "closed":
		return false
	default:
		return true
	}
}

func builtinPolymarketOrderRemainingSize(order polymarket.Order) float64 {
	original, err := strconv.ParseFloat(strings.TrimSpace(order.OriginalSize), 64)
	if err != nil {
		return 0
	}
	matched := 0.0
	if strings.TrimSpace(order.SizeMatched) != "" {
		if parsed, parseErr := strconv.ParseFloat(strings.TrimSpace(order.SizeMatched), 64); parseErr == nil {
			matched = parsed
		}
	}
	return math.Max(original-matched, 0)
}

func sumBuiltinPolymarketOrderRemainingSize(orders []polymarket.Order) float64 {
	total := 0.0
	for _, order := range orders {
		total += builtinPolymarketOrderRemainingSize(order)
	}
	return total
}

func selectBuiltinPolymarketOrders(orders []polymarket.Order, tokenID, orderSide string) []polymarket.Order {
	tokenID = strings.TrimSpace(tokenID)
	orderSide = strings.ToUpper(strings.TrimSpace(orderSide))
	selected := make([]polymarket.Order, 0, len(orders))
	for _, order := range orders {
		if tokenID != "" && !strings.EqualFold(strings.TrimSpace(order.AssetID), tokenID) {
			continue
		}
		if orderSide != "" && !strings.EqualFold(strings.TrimSpace(order.Side), orderSide) {
			continue
		}
		if !isBuiltinPolymarketOrderOpen(order) {
			continue
		}
		selected = append(selected, order)
	}
	return selected
}

func selectBuiltinPolymarketOrdersByOrderSide(orders []polymarket.Order, orderSide string) []polymarket.Order {
	return selectBuiltinPolymarketOrders(orders, "", orderSide)
}

func decodeBuiltinPolymarketStringList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var direct []string
	if err := json.Unmarshal([]byte(raw), &direct); err == nil {
		out := make([]string, 0, len(direct))
		for _, item := range direct {
			out = append(out, strings.TrimSpace(item))
		}
		return out
	}

	var generic []any
	if err := json.Unmarshal([]byte(raw), &generic); err == nil {
		out := make([]string, 0, len(generic))
		for _, item := range generic {
			out = append(out, strings.TrimSpace(fmt.Sprint(item)))
		}
		return out
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.Trim(strings.TrimSpace(part), `"'[]`)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func decodeBuiltinPolymarketFloatList(raw string) []float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var direct []float64
	if err := json.Unmarshal([]byte(raw), &direct); err == nil {
		return direct
	}

	var stringValues []string
	if err := json.Unmarshal([]byte(raw), &stringValues); err == nil {
		out := make([]float64, 0, len(stringValues))
		for _, item := range stringValues {
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(item), 64); err == nil {
				out = append(out, parsed)
			}
		}
		return out
	}

	var generic []any
	if err := json.Unmarshal([]byte(raw), &generic); err == nil {
		out := make([]float64, 0, len(generic))
		for _, item := range generic {
			switch typed := item.(type) {
			case float64:
				out = append(out, typed)
			case string:
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
					out = append(out, parsed)
				}
			}
		}
		return out
	}

	return nil
}

func parseBuiltinPolymarketTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func parseBuiltinPolymarketFloatString(raw string) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func filterPolymarketPositions(positions []polymarket.Position, conditionID, asset string) []polymarket.Position {
	conditionID = strings.TrimSpace(conditionID)
	asset = strings.TrimSpace(asset)
	if conditionID == "" && asset == "" {
		return positions
	}
	filtered := make([]polymarket.Position, 0, len(positions))
	for _, position := range positions {
		if conditionID != "" && !strings.EqualFold(strings.TrimSpace(position.ConditionID), conditionID) {
			continue
		}
		if asset != "" && !strings.EqualFold(strings.TrimSpace(position.Asset), asset) {
			continue
		}
		filtered = append(filtered, position)
	}
	return filtered
}

func filterPolymarketOrders(orders []polymarket.Order, conditionID, asset, orderID string) []polymarket.Order {
	conditionID = strings.TrimSpace(conditionID)
	asset = strings.TrimSpace(asset)
	orderID = strings.TrimSpace(orderID)
	if conditionID == "" && asset == "" && orderID == "" {
		return orders
	}
	filtered := make([]polymarket.Order, 0, len(orders))
	for _, order := range orders {
		if conditionID != "" && !strings.EqualFold(strings.TrimSpace(order.Market), conditionID) {
			continue
		}
		if asset != "" && !strings.EqualFold(strings.TrimSpace(order.AssetID), asset) {
			continue
		}
		if orderID != "" && !strings.EqualFold(strings.TrimSpace(order.ID), orderID) {
			continue
		}
		filtered = append(filtered, order)
	}
	return filtered
}

func resolvePolymarketRunCompanyID(run *data.PipelineRun, requestedCompanyID string) (string, error) {
	requestedCompanyID = strings.TrimSpace(requestedCompanyID)
	if run == nil {
		if requestedCompanyID == "" {
			return "", fmt.Errorf("company_id is required")
		}
		return requestedCompanyID, nil
	}

	scopeMode := strings.TrimSpace(run.ScopeMode)
	scopeCompanyID := strings.TrimSpace(run.ScopeCompanyID)
	if scopeMode == "company" {
		if scopeCompanyID == "" {
			return "", fmt.Errorf("pipeline run %q is company-scoped but missing scope_company_id", strings.TrimSpace(run.ID))
		}
		if requestedCompanyID != "" && requestedCompanyID != scopeCompanyID {
			return "", fmt.Errorf("company_id must match pipeline run scope_company_id")
		}
		return scopeCompanyID, nil
	}

	if requestedCompanyID == "" {
		return "", fmt.Errorf("company_id is required in global scope")
	}
	return requestedCompanyID, nil
}

func decodeBuiltinPolymarketValue(value any, dest any) error {
	switch typed := value.(type) {
	case nil:
		return fmt.Errorf("value is nil")
	case string:
		return json.Unmarshal([]byte(typed), dest)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		return json.Unmarshal(encoded, dest)
	}
}

func polymarketStableHash(values ...string) string {
	joined := make([]string, 0, len(values))
	for _, value := range values {
		value = polymarketStableText(value)
		if value == "" {
			continue
		}
		joined = append(joined, value)
	}
	if len(joined) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(joined, "\n")))
	return hex.EncodeToString(sum[:8])
}

func polymarketStableText(raw string) string {
	if raw == "" {
		return ""
	}
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(raw))), " ")
}

func formatBuiltinPolymarketHashFloat(value *float64) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%.6f", *value)
}

func truncateBuiltinPolymarketString(raw string, limit int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || limit <= 0 {
		return ""
	}
	runes := []rune(raw)
	if len(runes) <= limit {
		return raw
	}
	return string(runes[:limit-3]) + "..."
}

func clampBuiltinPolymarketProbability(probability float64) float64 {
	switch {
	case probability < 0:
		return 0
	case probability > 1:
		return 1
	default:
		return probability
	}
}

func builtinPolymarketFloatPointer(value float64) *float64 {
	value = roundBuiltinPolymarketFloat(value, 6)
	return &value
}
