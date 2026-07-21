package pipelinespec

import (
	"encoding/json"
	"strings"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
)

func TestCloneAndNormalize(t *testing.T) {
	orig := Definition{
		ID:   "  pipeline_1  ",
		Name: " ",
		Steps: []Step{
			{
				OnMethod:   " seed_method ",
				OnStatus:   " succeeded ",
				FromRole:   " * ",
				ToRole:     " target ",
				NextMethod: " run_next ",
				ParamMap:   map[string]string{"$": "payload"},
				FanOut:     true,
				FanOutKey:  " items ",
			},
		},
	}

	cloned := Clone(orig)
	cloned.Steps[0].ParamMap["$"] = "changed"
	if got := orig.Steps[0].ParamMap["$"]; got != "payload" {
		t.Fatalf("Clone() should deep copy ParamMap, got %q", got)
	}

	normalized := Normalize(orig)
	if normalized.ID != "pipeline_1" {
		t.Fatalf("normalized ID = %q, want %q", normalized.ID, "pipeline_1")
	}
	if normalized.Name != "pipeline_1" {
		t.Fatalf("normalized Name = %q, want %q", normalized.Name, "pipeline_1")
	}
	step := normalized.Steps[0]
	if step.OnMethod != "seed_method" || step.OnStatus != "succeeded" || step.FromRole != "*" || step.ToRole != "target" || step.NextMethod != "run_next" || step.FanOutKey != "items" {
		t.Fatalf("unexpected normalized step: %+v", step)
	}
}

func TestNormalizeBuiltinMethodNames(t *testing.T) {
	cases := map[string]string{
		"builtin_polymarket_find_markets":    "builtin_polymarket_find_markets",
		"polymarket_find_markets":            "builtin_polymarket_find_markets",
		"/polymarket_find_markets":           "builtin_polymarket_find_markets",
		"builtin_polymarket_manage_position": "builtin_polymarket_manage_position",
		"polymarket_manage_position":         "builtin_polymarket_manage_position",
		"/polymarket_manage_position":        "builtin_polymarket_manage_position",
	}
	for raw, want := range cases {
		if got := NormalizeBuiltinMethod(raw); got != want {
			t.Fatalf("NormalizeBuiltinMethod(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestValidateBuiltinAliases(t *testing.T) {
	validCases := []Definition{
		{
			ID:   "builtin_find_markets_name",
			Name: "Builtin Find Markets Name",
			Steps: []Step{
				{Runner: RunnerBuiltin, OnMethod: "seed", NextMethod: "polymarket_find_markets"},
			},
		},
		{
			ID:   "builtin_pipe_alias",
			Name: "Builtin Pipe Alias",
			Steps: []Step{
				{Runner: RunnerBuiltin, OnMethod: "seed", NextMethod: "/polymarket_review_positions"},
			},
		},
		{
			ID:   "builtin_manage_name",
			Name: "Builtin Manage Name",
			Steps: []Step{
				{Runner: RunnerBuiltin, OnMethod: "seed", NextMethod: "polymarket_manage_position"},
			},
		},
	}
	for _, valid := range validCases {
		if err := Validate(valid); err != nil {
			t.Fatalf("Validate(valid builtin alias %s) unexpected error: %v", valid.ID, err)
		}
	}

	err := Validate(Definition{
		ID:   "missing_builtin",
		Name: "Missing Builtin",
		Steps: []Step{
			{Runner: RunnerBuiltin, OnMethod: "seed", NextMethod: "builtin_missing"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown builtin method") {
		t.Fatalf("expected unknown builtin method error, got %v", err)
	}

	for _, removed := range []string{"polymarket_execute_trade", "/polymarket_update_position"} {
		err := Validate(Definition{
			ID:   "removed_" + strings.ReplaceAll(strings.TrimLeft(removed, "/"), "/", "_"),
			Name: "Removed Builtin",
			Steps: []Step{
				{Runner: RunnerBuiltin, OnMethod: "seed", NextMethod: removed},
			},
		})
		if err == nil || !strings.Contains(err.Error(), "unknown builtin method") {
			t.Fatalf("expected removed builtin name %q to be rejected, got %v", removed, err)
		}
	}
}

func TestValidateClaudeCodeRules(t *testing.T) {
	valid := Definition{
		ID:   "claude_valid",
		Name: "Claude Valid",
		Steps: []Step{
			{Runner: RunnerClaudeCode, OnMethod: "seed", ToAgentID: "agent-1", NextMethod: "analyze_markets"},
		},
	}
	if err := Validate(valid); err != nil {
		t.Fatalf("Validate(valid claude-code) unexpected error: %v", err)
	}

	missingAgent := Clone(valid)
	missingAgent.ID = "claude_missing_agent"
	missingAgent.Steps[0].ToAgentID = ""
	if err := Validate(missingAgent); err == nil || err.Error() != "step 0: to_agent_id is required for claude-code runner" {
		t.Fatalf("expected exact missing to_agent_id error, got %v", err)
	}

	builtinMethod := Clone(valid)
	builtinMethod.ID = "claude_builtin"
	builtinMethod.Steps[0].NextMethod = BuiltinPolymarketSnapshot
	if err := Validate(builtinMethod); err == nil || err.Error() != "step 0: builtin methods are not supported for claude-code runner" {
		t.Fatalf("expected exact builtin claude-code rejection, got %v", err)
	}
}

func TestValidateCodexRules(t *testing.T) {
	valid := Definition{
		ID:   "codex_valid",
		Name: "Codex Valid",
		Steps: []Step{
			{Runner: RunnerCodex, OnMethod: "seed", ToAgentID: "agent-1", NextMethod: "analyze_markets"},
		},
	}
	if err := Validate(valid); err != nil {
		t.Fatalf("Validate(valid codex) unexpected error: %v", err)
	}

	missingAgent := Clone(valid)
	missingAgent.ID = "codex_missing_agent"
	missingAgent.Steps[0].ToAgentID = ""
	if err := Validate(missingAgent); err == nil || err.Error() != "step 0: to_agent_id is required for codex runner" {
		t.Fatalf("expected exact missing to_agent_id error, got %v", err)
	}

	builtinMethod := Clone(valid)
	builtinMethod.ID = "codex_builtin"
	builtinMethod.Steps[0].NextMethod = BuiltinPolymarketSnapshot
	if err := Validate(builtinMethod); err == nil || err.Error() != "step 0: builtin methods are not supported for codex runner" {
		t.Fatalf("expected exact builtin codex rejection, got %v", err)
	}

	missingMethod := Clone(valid)
	missingMethod.ID = "codex_missing_method"
	missingMethod.Steps[0].NextMethod = ""
	if err := Validate(missingMethod); err == nil || err.Error() != "step 0: next_method is required" {
		t.Fatalf("expected exact missing next_method error, got %v", err)
	}
}

func TestFromDefinition(t *testing.T) {
	stepsJSON, err := json.Marshal([]Step{{OnMethod: "seed", ToRole: "target", NextMethod: "run"}})
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	p, err := FromDefinition(data.PipelineDefinition{
		ID:        "pipe",
		Name:      "Pipe",
		StepsJSON: string(stepsJSON),
	})
	if err != nil {
		t.Fatalf("FromDefinition() unexpected error: %v", err)
	}
	if p.ID != "pipe" || len(p.Steps) != 1 || p.Steps[0].ToRole != "target" {
		t.Fatalf("unexpected parsed pipeline: %+v", p)
	}

	if _, err := FromDefinition(data.PipelineDefinition{ID: "bad", Name: "Bad", StepsJSON: "{"}); err == nil {
		t.Fatalf("expected invalid JSON to fail")
	}
}
