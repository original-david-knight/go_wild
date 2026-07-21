package main

import (
	"context"
	"encoding/json"
	"strings"

	data "github.com/original-david-knight/go_wild/agent_data"
)

var marketPriceRedactedKeys = map[string]struct{}{
	"price":                {},
	"curprice":             {},
	"currentprice":         {},
	"bestbid":              {},
	"bestask":              {},
	"bookbestbid":          {},
	"bookbestask":          {},
	"bestbidsize":          {},
	"bestasksize":          {},
	"midprice":             {},
	"midpoint":             {},
	"reevaluationnotional": {},
	"currentvalue":         {},
	"positionvalue":        {},
	"cashpnl":              {},
	"percentpnl":           {},
	"realizedpnl":          {},
	"aum":                  {},
	"currentposition":      {},
	"maxallowed":           {},
	"remainingcapacity":    {},
	"markprice":            {},
	"lastprice":            {},
}

var pipelineMethodsWithImplicitMarketPriceRedaction = map[string]struct{}{
	"polymarket_research_position": {},
}

var pipelineMethodSpecificRedactedKeys = map[string]map[string]struct{}{
	"polymarket_research_position": {
		"avgprice":     {},
		"initialvalue": {},
		"size":         {},
		"totalbought":  {},
	},
}

var marketNoteRedactedKeys = map[string]struct{}{
	"notes":           {},
	"notesbymarket":   {},
	"notecounts":      {},
	"latestnote":      {},
	"latestnotecount": {},
}

func sanitizePipelineStepParams(ctx context.Context, svc *data.AgentService, method string, params map[string]any) map[string]any {
	if len(params) == 0 {
		return params
	}

	needsBaseRedaction := shouldRedactMarketPricesForMethod(ctx, svc, method)
	extraKeys := mergedRedactedKeys(redactedKeysForPipelineMethod(method), marketNoteRedactedKeysForMethod(ctx, svc, method))
	if !needsBaseRedaction && len(extraKeys) == 0 {
		return params
	}
	return sanitizePipelineParamsWithRedactionPolicy(params, needsBaseRedaction, extraKeys)
}

// sanitizePipelinePriorResult redacts sensitive fields from the previous
// step's result before it is injected into the current step's mission prompt
// as "Context from Prior Step". Shared across all pipeline runners (Claude
// Code, Codex, …) — the redaction policy is derived from the target method's
// A2AMethod configuration, never from the runner.
//
// Empty / nil priors short-circuit and are returned as-is; the policy
// branches below only apply to non-empty priors:
//
//   - FreshContext: true drops the entire prior result (returns nil). The
//     step wants to run with no leaked context from upstream.
//   - RedactMarketPrices (explicit on the method, or implicit via
//     pipelineMethodsWithImplicitMarketPriceRedaction) strips live price,
//     order-book, PnL, and position-value fields so the model cannot condition
//     its reasoning on transient market state that would have changed by the
//     time the step actually runs.
//   - PolymarketNoteAugmentationDisabled adds marketNoteRedactedKeys on top of
//     any method-specific extra keys so stale notes don't bleed across steps
//     when augmentation is off for the method.
//
// When no policy applies, the input map is returned by reference (no clone)
// so callers that never mutate the prior avoid an unnecessary allocation on
// the hot path. Only uses method-definition fields and the method name —
// nothing provider-specific. A method like polymarket_research_position has
// the same redaction regardless of whether Claude Code or Codex runs it.
func sanitizePipelinePriorResult(method string, methodDef *data.A2AMethod, priorResult map[string]any) map[string]any {
	if len(priorResult) == 0 {
		return priorResult
	}
	if methodDef != nil && methodDef.FreshContext {
		return nil
	}
	extraKeys := redactedKeysForPipelineMethod(method)
	if methodDef != nil && methodDef.PolymarketNoteAugmentationDisabled() {
		extraKeys = mergedRedactedKeys(extraKeys, marketNoteRedactedKeys)
	}
	redactMarketPriceKeys := false
	if methodDef != nil && methodDef.RedactMarketPrices {
		redactMarketPriceKeys = true
	} else if shouldImplicitlyRedactMarketPrices(method) {
		redactMarketPriceKeys = true
	}
	if !redactMarketPriceKeys && len(extraKeys) == 0 {
		return priorResult
	}
	cloned, ok := clonePipelineStepParams(priorResult)
	if !ok {
		return nil
	}
	redactPipelineParamKeys(cloned, redactMarketPriceKeys, extraKeys)
	return cloned
}

func sanitizePipelineMethodRequest(ctx context.Context, svc *data.AgentService, method string, request map[string]any) map[string]any {
	if len(request) == 0 {
		return request
	}
	params, ok := request["params"].(map[string]any)
	if !ok || len(params) == 0 {
		return request
	}

	cloned := make(map[string]any, len(request))
	for key, value := range request {
		cloned[key] = value
	}
	cloned["params"] = sanitizePipelineStepParams(ctx, svc, method, params)
	return cloned
}

func shouldRedactMarketPricesForMethod(ctx context.Context, svc *data.AgentService, method string) bool {
	method = strings.TrimSpace(method)
	if method == "" {
		return false
	}
	if svc != nil {
		if ctx == nil {
			ctx = context.Background()
		}
		def, err := svc.GetA2AMethod(ctx, method)
		if err == nil && def != nil && def.RedactMarketPrices {
			return true
		}
	}
	return shouldImplicitlyRedactMarketPrices(method)
}

func shouldImplicitlyRedactMarketPrices(method string) bool {
	method = strings.TrimSpace(method)
	if method == "" {
		return false
	}
	_, ok := pipelineMethodsWithImplicitMarketPriceRedaction[method]
	return ok
}

func sanitizeMarketPriceParams(params map[string]any, extraKeys map[string]struct{}) map[string]any {
	return sanitizePipelineParamsWithRedactionPolicy(params, true, extraKeys)
}

func sanitizePipelineParamsWithRedactionPolicy(params map[string]any, redactMarketPriceKeys bool, extraKeys map[string]struct{}) map[string]any {
	cloned, ok := clonePipelineStepParams(params)
	if !ok {
		return params
	}
	redactPipelineParamKeys(cloned, redactMarketPriceKeys, extraKeys)
	return cloned
}

func clonePipelineStepParams(params map[string]any) (map[string]any, bool) {
	blob, err := json.Marshal(params)
	if err != nil {
		return nil, false
	}

	var cloned map[string]any
	if err := json.Unmarshal(blob, &cloned); err != nil {
		return nil, false
	}
	return cloned, true
}

func redactMarketPrices(value any, extraKeys map[string]struct{}) any {
	return redactPipelineParamKeys(value, true, extraKeys)
}

func redactPipelineParamKeys(value any, redactMarketPriceKeys bool, extraKeys map[string]struct{}) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			normalized := normalizePipelineParamKey(key)
			if redactMarketPriceKeys {
				if _, ok := marketPriceRedactedKeys[normalized]; ok {
					delete(typed, key)
					continue
				}
			}
			if _, ok := extraKeys[normalized]; ok {
				delete(typed, key)
				continue
			}
			typed[key] = redactPipelineParamKeys(nested, redactMarketPriceKeys, extraKeys)
		}
	case []any:
		for i, nested := range typed {
			typed[i] = redactPipelineParamKeys(nested, redactMarketPriceKeys, extraKeys)
		}
	}
	return value
}

func redactedKeysForPipelineMethod(method string) map[string]struct{} {
	method = strings.TrimSpace(method)
	if method == "" {
		return nil
	}
	return pipelineMethodSpecificRedactedKeys[method]
}

func marketNoteRedactedKeysForMethod(ctx context.Context, svc *data.AgentService, method string) map[string]struct{} {
	method = strings.TrimSpace(method)
	if method == "" || svc == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	methodDef, err := svc.GetA2AMethod(ctx, method)
	if err != nil || methodDef == nil || !methodDef.PolymarketNoteAugmentationDisabled() {
		return nil
	}
	return marketNoteRedactedKeys
}

func mergedRedactedKeys(base map[string]struct{}, extra map[string]struct{}) map[string]struct{} {
	switch {
	case len(base) == 0 && len(extra) == 0:
		return nil
	case len(base) == 0:
		return extra
	case len(extra) == 0:
		return base
	}
	out := make(map[string]struct{}, len(base)+len(extra))
	for key := range base {
		out[key] = struct{}{}
	}
	for key := range extra {
		out[key] = struct{}{}
	}
	return out
}

func normalizePipelineParamKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "_", "")
	return key
}
