package main

import (
	"context"
	"testing"
	"time"

	data "github.com/original-david-knight/go_wild/agent_data"
	polymarket "github.com/original-david-knight/go_wild/polymarket"
)

func TestSubmitStepJobInternalSanitizesConfiguredMethodParams(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	systemSvc := data.NewAgentService(db, "system")
	const methodName = "custom_market_research"

	targetAgent, err := svc.CreateAgent(ctx, "poly-research-target")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	if _, err := systemSvc.CreateA2AMethodWithConfig(ctx, methodName, "research custom market", "", "", "", false, false, true, false, false); err != nil {
		t.Fatalf("CreateA2AMethodWithConfig failed: %v", err)
	}

	run := &data.PipelineRun{
		ID:           "run-sanitize-a2a",
		PipelineID:   "sanitize-a2a",
		TriggerJobID: "manual",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	targetID := engine.submitStepJobInternal(ctx, systemSvc, run, PipelineStep{
		ToAgentID:  targetAgent.ID,
		NextMethod: methodName,
	}, 0, nil, map[string]any{
		"payload": map[string]any{
			"condition_id":          "cond-1",
			"question":              "Will it happen?",
			"best_bid":              0.41,
			"reevaluation_notional": 144.5,
			"position": polymarket.Position{
				ConditionID:  "cond-1",
				Outcome:      "Yes",
				Size:         100,
				AvgPrice:     0.22,
				CurPrice:     0.41,
				CurrentValue: 144.5,
			},
		},
	}, "", false)
	if targetID != targetAgent.ID {
		t.Fatalf("target agent = %q, want %q", targetID, targetAgent.ID)
	}

	stepRuns, err := systemSvc.ListStepRunsForRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListStepRunsForRun failed: %v", err)
	}
	if len(stepRuns) != 1 {
		t.Fatalf("expected 1 step run, got %d", len(stepRuns))
	}

	job, err := newLocalA2AQueue(db).GetJob(ctx, stepRuns[0].A2AJobID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	request, _ := job["request"].(map[string]any)
	params, _ := request["params"].(map[string]any)
	payload, _ := params["payload"].(map[string]any)
	if payload["condition_id"] != "cond-1" {
		t.Fatalf("payload.condition_id = %#v", payload["condition_id"])
	}
	if _, exists := payload["best_bid"]; exists {
		t.Fatalf("payload.best_bid should be redacted: %#v", payload["best_bid"])
	}
	if _, exists := payload["reevaluation_notional"]; exists {
		t.Fatalf("payload.reevaluation_notional should be redacted: %#v", payload["reevaluation_notional"])
	}
	position, ok := payload["position"].(map[string]any)
	if !ok {
		t.Fatalf("payload.position type = %T", payload["position"])
	}
	if _, exists := position["curPrice"]; exists {
		t.Fatalf("position.curPrice should be redacted: %#v", position["curPrice"])
	}
	if _, exists := position["currentValue"]; exists {
		t.Fatalf("position.currentValue should be redacted: %#v", position["currentValue"])
	}
	if got, _ := position["avgPrice"].(float64); got != 0.22 {
		t.Fatalf("position.avgPrice = %#v, want 0.22", position["avgPrice"])
	}
}

func TestExecuteClaudeCodeStepSanitizesConfiguredMethodParams(t *testing.T) {
	t.Setenv("CLAUDE_SMART_MODEL", "claude-sonnet-test")

	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)
	systemSvc := data.NewAgentService(db, "system")
	const methodName = "custom_claude_market_research"

	targetAgent, err := svc.CreateAgent(ctx, "poly-research-claude")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	if _, err := systemSvc.CreateA2AMethodWithConfig(ctx, methodName, "research custom market", "", "", "", false, false, true, false, false); err != nil {
		t.Fatalf("CreateA2AMethodWithConfig failed: %v", err)
	}

	if _, err := engine.UpsertPipelineDefinition(ctx, Pipeline{
		ID:   "sanitize_claude",
		Name: "Sanitize Claude",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerClaudeCode,
				OnMethod:   "seed",
				ToAgentID:  targetAgent.ID,
				NextMethod: methodName,
			},
		},
	}, true); err != nil {
		t.Fatalf("UpsertPipelineDefinition failed: %v", err)
	}

	run := &data.PipelineRun{
		ID:           "run-sanitize-claude",
		PipelineID:   "sanitize_claude",
		TriggerJobID: "manual",
		CurrentStep:  0,
		Status:       "running",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := systemSvc.CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	sawSanitizedRequest := false
	engine.claudeCodeRunner = func(ctx context.Context, agentID, prompt, systemPrompt, model string, budgetUSD float64) (string, error) {
		stepRuns, err := systemSvc.ListStepRunsForRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("ListStepRunsForRun failed: %v", err)
		}
		if len(stepRuns) != 1 {
			t.Fatalf("expected 1 step run, got %d", len(stepRuns))
		}

		job, err := newLocalA2AQueue(db).GetJob(ctx, stepRuns[0].A2AJobID)
		if err != nil {
			t.Fatalf("GetJob failed: %v", err)
		}
		request, _ := job["request"].(map[string]any)
		params, _ := request["params"].(map[string]any)
		payload, _ := params["payload"].(map[string]any)
		position, _ := payload["position"].(map[string]any)

		if _, exists := payload["best_ask"]; exists {
			t.Fatalf("payload.best_ask should be redacted: %#v", payload["best_ask"])
		}
		if _, exists := position["curPrice"]; exists {
			t.Fatalf("position.curPrice should be redacted: %#v", position["curPrice"])
		}
		if _, exists := position["currentValue"]; exists {
			t.Fatalf("position.currentValue should be redacted: %#v", position["currentValue"])
		}
		if payload["condition_id"] != "cond-2" {
			t.Fatalf("payload.condition_id = %#v", payload["condition_id"])
		}

		sawSanitizedRequest = true
		return `{"status":"succeeded","result":{"condition_id":"cond-2"}}`, nil
	}

	if ok := engine.executeClaudeCodeStep(ctx, systemSvc, run, PipelineStep{
		Runner:     pipelineStepRunnerClaudeCode,
		ToAgentID:  targetAgent.ID,
		NextMethod: methodName,
	}, 0, nil, map[string]any{
		"payload": map[string]any{
			"condition_id": "cond-2",
			"best_ask":     0.58,
			"position": polymarket.Position{
				ConditionID:  "cond-2",
				Outcome:      "No",
				Size:         75,
				CurPrice:     0.58,
				CurrentValue: 43.5,
			},
		},
	}); !ok {
		t.Fatalf("executeClaudeCodeStep returned false")
	}
	if !sawSanitizedRequest {
		t.Fatalf("expected sanitized claude request to be observed")
	}
}

func TestSanitizePipelineStepParamsLeavesUnconfiguredMethodsUnchanged(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	systemSvc := data.NewAgentService(db, "system")

	params := map[string]any{
		"payload": map[string]any{
			"best_bid": 0.44,
		},
	}
	sanitized := sanitizePipelineStepParams(ctx, systemSvc, "ordinary_custom_method", params)
	payload, _ := sanitized["payload"].(map[string]any)
	if payload["best_bid"] != 0.44 {
		t.Fatalf("expected unconfigured method params to remain unchanged, got %#v", payload["best_bid"])
	}
}

func TestSanitizePipelineStepParamsUsesImplicitRedactionForLegacyPolymarketResearchPosition(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	systemSvc := data.NewAgentService(db, "system")

	if _, err := systemSvc.CreateA2AMethod(ctx, "polymarket_research_position", "legacy research method", "", ""); err != nil {
		t.Fatalf("CreateA2AMethod failed: %v", err)
	}

	sanitized := sanitizePipelineStepParams(ctx, systemSvc, "polymarket_research_position", map[string]any{
		"payload": map[string]any{
			"condition_id": "cond-legacy",
			"best_bid":     0.52,
			"position": map[string]any{
				"curPrice":     0.52,
				"currentValue": 104.0,
				"avgPrice":     0.41,
				"initialValue": 82.0,
				"size":         200.0,
				"totalBought":  200.0,
			},
		},
	})

	payload, _ := sanitized["payload"].(map[string]any)
	if _, exists := payload["best_bid"]; exists {
		t.Fatalf("payload.best_bid should be redacted: %#v", payload["best_bid"])
	}
	position, ok := payload["position"].(map[string]any)
	if !ok {
		t.Fatalf("payload.position type = %T", payload["position"])
	}
	if _, exists := position["curPrice"]; exists {
		t.Fatalf("position.curPrice should be redacted: %#v", position["curPrice"])
	}
	if _, exists := position["currentValue"]; exists {
		t.Fatalf("position.currentValue should be redacted: %#v", position["currentValue"])
	}
	if _, exists := position["avgPrice"]; exists {
		t.Fatalf("position.avgPrice should be redacted: %#v", position["avgPrice"])
	}
	if _, exists := position["initialValue"]; exists {
		t.Fatalf("position.initialValue should be redacted: %#v", position["initialValue"])
	}
	if _, exists := position["size"]; exists {
		t.Fatalf("position.size should be redacted: %#v", position["size"])
	}
	if _, exists := position["totalBought"]; exists {
		t.Fatalf("position.totalBought should be redacted: %#v", position["totalBought"])
	}
}

func TestSanitizePipelineStepParamsRedactsMarketNoteFieldsWhenAugmentationDisabled(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	systemSvc := data.NewAgentService(db, "system")

	if _, err := systemSvc.CreateA2AMethodWithConfig(ctx, "custom_market_review", "review a market", "", "", "", false, false, false, false, true); err != nil {
		t.Fatalf("CreateA2AMethodWithConfig failed: %v", err)
	}

	sanitized := sanitizePipelineStepParams(ctx, systemSvc, "custom_market_review", map[string]any{
		"payload": map[string]any{
			"condition_id": "cond-notes",
			"best_bid":     0.41,
			"notes":        []any{"n1"},
			"note_counts":  map[string]any{"cond-notes": 2},
			"position": map[string]any{
				"cur_price":         0.39,
				"latest_note":       "old note",
				"latest_note_count": 4,
			},
		},
	})

	payload, _ := sanitized["payload"].(map[string]any)
	if _, exists := payload["notes"]; exists {
		t.Fatalf("payload.notes should be redacted: %#v", payload["notes"])
	}
	if _, exists := payload["note_counts"]; exists {
		t.Fatalf("payload.note_counts should be redacted: %#v", payload["note_counts"])
	}
	position, _ := payload["position"].(map[string]any)
	if _, exists := position["latest_note"]; exists {
		t.Fatalf("position.latest_note should be redacted: %#v", position["latest_note"])
	}
	if _, exists := position["latest_note_count"]; exists {
		t.Fatalf("position.latest_note_count should be redacted: %#v", position["latest_note_count"])
	}
	if got, _ := payload["best_bid"].(float64); got != 0.41 {
		t.Fatalf("payload.best_bid = %#v, want 0.41", payload["best_bid"])
	}
	if got, _ := position["cur_price"].(float64); got != 0.39 {
		t.Fatalf("position.cur_price = %#v, want 0.39", position["cur_price"])
	}
}

func TestSanitizePipelineStepParamsKeepsMarketPricesWhenOnlyNoteAugmentationDisabled(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	systemSvc := data.NewAgentService(db, "system")

	if _, err := systemSvc.CreateA2AMethodWithConfig(ctx, "custom_market_review", "review a market", "", "", "", false, false, false, false, true); err != nil {
		t.Fatalf("CreateA2AMethodWithConfig failed: %v", err)
	}

	sanitized := sanitizePipelineStepParams(ctx, systemSvc, "custom_market_review", map[string]any{
		"payload": map[string]any{
			"best_bid": 0.41,
			"position": map[string]any{
				"cur_price": 0.39,
			},
		},
	})

	payload, _ := sanitized["payload"].(map[string]any)
	if got, _ := payload["best_bid"].(float64); got != 0.41 {
		t.Fatalf("payload.best_bid = %#v, want 0.41", payload["best_bid"])
	}
	position, _ := payload["position"].(map[string]any)
	if got, _ := position["cur_price"].(float64); got != 0.39 {
		t.Fatalf("position.cur_price = %#v, want 0.39", position["cur_price"])
	}
}

// TestSanitizePipelinePriorResult pins the redaction contract of the shared
// prior-result sanitizer used by every pipeline runner (Claude Code, Codex,
// etc.) after the rename from sanitizeClaudeCodePriorResult. Each policy
// branch (FreshContext, explicit RedactMarketPrices, implicit market-price
// redaction via method name, PolymarketNoteAugmentationDisabled,
// method-specific extras, early returns) is pinned so a future refactor can't
// silently change what prior-step context is leaked into a runner's prompt.
// The existing TestSanitizePipelinePriorResultRedactsMarketNoteFieldsWhenAugmentationDisabled
// in claude_code_runner_test.go covers the combined note-augmentation case
// against a real input shape; this test covers the individual branches.
func TestSanitizePipelinePriorResult(t *testing.T) {
	t.Run("empty prior result returns input unchanged", func(t *testing.T) {
		// Short-circuit: no allocation, no clone, nothing to redact.
		got := sanitizePipelinePriorResult("polymarket_research_position", &data.A2AMethod{FreshContext: true}, nil)
		if got != nil {
			t.Fatalf("nil prior = %#v, want nil", got)
		}
		empty := map[string]any{}
		got = sanitizePipelinePriorResult("polymarket_research_position", &data.A2AMethod{FreshContext: true}, empty)
		if len(got) != 0 {
			t.Fatalf("empty prior = %#v, want empty", got)
		}
	})

	t.Run("fresh context drops entire prior result", func(t *testing.T) {
		// FreshContext means the step wants to run with no upstream leakage.
		// Returning nil (not a cloned-and-redacted map) is what prevents the
		// shared mission-prompt builder from appending a "Context from Prior
		// Step" section at all.
		got := sanitizePipelinePriorResult("any_method", &data.A2AMethod{FreshContext: true}, map[string]any{
			"anything": "value",
		})
		if got != nil {
			t.Fatalf("fresh context prior = %#v, want nil", got)
		}
	})

	t.Run("no methodDef and non-implicit method returns input by reference", func(t *testing.T) {
		// If there's no redaction policy to apply, the function must return
		// the input map by reference (same underlying map) — callers
		// (mission-prompt builder) rely on this to avoid an unnecessary clone
		// on the hot path. Pin both the contents and the aliasing: a future
		// refactor that always clones would silently regress the hot path
		// without a visible test failure unless aliasing is asserted.
		prior := map[string]any{"price": 0.41, "notes": []any{"n1"}}
		got := sanitizePipelinePriorResult("method_with_no_policy", nil, prior)
		if got == nil {
			t.Fatal("non-policy method prior = nil, want input back")
		}
		if _, ok := got["price"]; !ok {
			t.Fatalf("price was redacted for non-policy method: %#v", got)
		}
		if _, ok := got["notes"]; !ok {
			t.Fatalf("notes was redacted for non-policy method: %#v", got)
		}
		// Aliasing check: mutating got must be visible on prior, and vice
		// versa, proving the same underlying map was returned.
		got["sentinel"] = "alias-probe"
		if prior["sentinel"] != "alias-probe" {
			t.Fatalf("non-policy pass-through was cloned, want same map by reference (prior=%#v)", prior)
		}
	})

	t.Run("explicit RedactMarketPrices redacts price keys", func(t *testing.T) {
		methodDef := &data.A2AMethod{RedactMarketPrices: true}
		got := sanitizePipelinePriorResult("custom_market_review", methodDef, map[string]any{
			"question":  "Will it happen?",
			"best_bid":  0.41,
			"best_ask":  0.43,
			"mid_price": 0.42,
		})
		if _, ok := got["best_bid"]; ok {
			t.Fatalf("best_bid should be redacted: %#v", got)
		}
		if _, ok := got["best_ask"]; ok {
			t.Fatalf("best_ask should be redacted: %#v", got)
		}
		if _, ok := got["mid_price"]; ok {
			t.Fatalf("mid_price should be redacted: %#v", got)
		}
		if got["question"] != "Will it happen?" {
			t.Fatalf("question dropped: %#v", got)
		}
	})

	t.Run("implicit redaction via polymarket_research_position method name", func(t *testing.T) {
		// polymarket_research_position is listed in
		// pipelineMethodsWithImplicitMarketPriceRedaction for a historical
		// reason: the method existed before the RedactMarketPrices flag did,
		// and its migration adds the flag but the name-based fallback is
		// still load-bearing for method rows that predate the migration.
		got := sanitizePipelinePriorResult("polymarket_research_position", nil, map[string]any{
			"condition_id": "cond-1",
			"best_bid":     0.41,
			"avg_price":    0.38, // method-specific extra
		})
		if _, ok := got["best_bid"]; ok {
			t.Fatalf("best_bid should be redacted via implicit list: %#v", got)
		}
		if _, ok := got["avg_price"]; ok {
			t.Fatalf("avg_price should be redacted via method-specific extras: %#v", got)
		}
		if got["condition_id"] != "cond-1" {
			t.Fatalf("condition_id dropped: %#v", got)
		}
	})

	t.Run("unmarshalable prior result returns nil", func(t *testing.T) {
		// When redaction is required the function clones via JSON round-trip
		// before mutating. If the prior result contains a non-marshalable
		// value (channel, func), the clone fails and the function returns
		// nil rather than mutating the caller's map in place or returning a
		// partially-sanitized input. In practice the shared executor feeds
		// this function from `parsedResult.Payload` (itself parsed from
		// JSON), so the clone failure isn't reachable today — but a direct
		// caller bypassing the parser could hit it, and returning nil
		// (signalling "drop the prior context") is safer than silently
		// surfacing the unredacted input.
		methodDef := &data.A2AMethod{RedactMarketPrices: true}
		got := sanitizePipelinePriorResult("custom_market_review", methodDef, map[string]any{
			"best_bid": 0.41,
			"ch":       make(chan int), // json.Marshal errors on channels
		})
		if got != nil {
			t.Fatalf("unmarshalable prior = %#v, want nil", got)
		}
	})

	t.Run("nested maps and arrays are redacted recursively", func(t *testing.T) {
		// The prior result commonly wraps the method payload in a top-level
		// key like "payload" or nests positions inside a list — the shared
		// redactor walks maps and slices alike. Dropping a nested price key
		// is what makes this safe for fan-out steps whose inputs are
		// arbitrarily shaped.
		methodDef := &data.A2AMethod{RedactMarketPrices: true}
		got := sanitizePipelinePriorResult("custom_market_review", methodDef, map[string]any{
			"payload": map[string]any{
				"best_bid": 0.41,
				"positions": []any{
					map[string]any{"condition_id": "cond-1", "cur_price": 0.39},
					map[string]any{"condition_id": "cond-2", "cur_price": 0.27},
				},
			},
		})
		payload, _ := got["payload"].(map[string]any)
		if _, ok := payload["best_bid"]; ok {
			t.Fatalf("nested best_bid should be redacted: %#v", payload)
		}
		positions, _ := payload["positions"].([]any)
		if len(positions) != 2 {
			t.Fatalf("positions lost: %#v", positions)
		}
		for i, p := range positions {
			pm, _ := p.(map[string]any)
			if _, ok := pm["cur_price"]; ok {
				t.Fatalf("positions[%d].cur_price should be redacted: %#v", i, pm)
			}
			if _, ok := pm["condition_id"]; !ok {
				t.Fatalf("positions[%d].condition_id should be kept: %#v", i, pm)
			}
		}
	})
}
