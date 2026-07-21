package deepresearch

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestCodexCompletenessCheckerShortCircuitsWithoutObjectives guards the
// trivial "nothing to check → complete" path. Skipping the codex call here
// is not just an optimization: an empty Objectives slice means the caller
// asked for no coverage at all, and invoking the model would waste budget
// on a question the checker can already answer structurally.
func TestCodexCompletenessCheckerShortCircuitsWithoutObjectives(t *testing.T) {
	called := false
	c := &codexDeepResearchCompletenessChecker{
		generate: func(context.Context, string, string) (string, error) {
			called = true
			return "", errors.New("generate must not be called when there are no objectives")
		},
	}
	result, err := c.Check(context.Background(), CompletenessRequest{})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if called {
		t.Fatalf("checker must skip codex when Objectives is empty")
	}
	if !result.Complete {
		t.Fatalf("empty-objectives request must be reported complete; got %#v", result)
	}
}

// TestCodexCompletenessCheckerParsesAndTrims covers the happy path for the
// JSON envelope the checker expects: complete flag flows through, reasoning
// is trimmed, and missing_objectives entries with empty keys are dropped
// while non-empty ones are trimmed. Question trimming matters because the
// next planner round interpolates these strings into its prompt.
func TestCodexCompletenessCheckerParsesAndTrims(t *testing.T) {
	payload := `{
        "complete": false,
        "reasoning": "  need more evidence  ",
        "missing_objectives": [
            {"objective_key": "  research  ", "question": "  what about X?  "},
            {"objective_key": "", "question": "dropped: empty key"},
            {"objective_key": "context", "question": ""}
        ]
    }`
	var gotPrompt string
	c := &codexDeepResearchCompletenessChecker{
		generate: func(_ context.Context, prompt, _ string) (string, error) {
			gotPrompt = prompt
			return payload, nil
		},
	}

	result, err := c.Check(context.Background(), CompletenessRequest{
		Query:      "q",
		Objectives: []Objective{{Key: "research"}, {Key: "context"}},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !strings.HasSuffix(gotPrompt, codexJSONSuffix) {
		t.Fatalf("prompt must end with the codex JSON-only suffix")
	}
	if result.Complete {
		t.Fatalf("expected Complete=false")
	}
	if result.Reasoning != "need more evidence" {
		t.Fatalf("reasoning must be trimmed; got %q", result.Reasoning)
	}
	if len(result.MissingObjectives) != 2 {
		t.Fatalf("expected 2 missing objectives (empty-key row dropped), got %d: %#v", len(result.MissingObjectives), result.MissingObjectives)
	}
	if result.MissingObjectives[0].ObjectiveKey != "research" || result.MissingObjectives[0].Question != "what about X?" {
		t.Fatalf("first missing objective not trimmed: %#v", result.MissingObjectives[0])
	}
	if result.MissingObjectives[1].ObjectiveKey != "context" || result.MissingObjectives[1].Question != "" {
		t.Fatalf("empty question is allowed but key must survive: %#v", result.MissingObjectives[1])
	}
}

// TestCodexCompletenessCheckerTolerantToFencedJSON mirrors the planner /
// synthesizer coverage: the codex CLI occasionally wraps its answer in a
// markdown fence despite the prompt suffix asking otherwise. Check() must
// still parse it, because the checker sits on the engine's exit path — a
// parse failure here stops the whole run.
func TestCodexCompletenessCheckerTolerantToFencedJSON(t *testing.T) {
	payload := "```json\n{\"complete\": true, \"reasoning\": \"ok\"}\n```"
	c := &codexDeepResearchCompletenessChecker{
		generate: func(context.Context, string, string) (string, error) {
			return payload, nil
		},
	}
	result, err := c.Check(context.Background(), CompletenessRequest{
		Objectives: []Objective{{Key: "research"}},
	})
	if err != nil {
		t.Fatalf("Check() error = %v (fenced JSON must parse)", err)
	}
	if !result.Complete {
		t.Fatalf("expected Complete=true after unwrap; got %#v", result)
	}
}

// TestCodexCompletenessCheckerCompleteTrueAccepted covers the positive path:
// when the model says the run is complete, the checker returns it as-is
// without synthesizing missing objectives.
func TestCodexCompletenessCheckerCompleteTrueAccepted(t *testing.T) {
	c := &codexDeepResearchCompletenessChecker{
		generate: func(context.Context, string, string) (string, error) {
			return `{"complete": true, "reasoning": "all satisfied"}`, nil
		},
	}
	result, err := c.Check(context.Background(), CompletenessRequest{
		Objectives: []Objective{{Key: "research"}},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !result.Complete {
		t.Fatalf("expected Complete=true")
	}
	if len(result.MissingObjectives) != 0 {
		t.Fatalf("expected no missing objectives, got %#v", result.MissingObjectives)
	}
}

// TestCodexCompletenessCheckerFallbackFillsMissingFromResults is the crucial
// safety-net test: when the model says complete=false but forgets to
// populate missing_objectives, the checker MUST synthesize a list from the
// unsatisfied ObjectiveResults. Without this fallback the engine would exit
// without knowing what to go search next, silently dropping coverage that
// the completeness pass just said was insufficient. Objectives that ARE
// satisfied must not appear in the synthesized list.
func TestCodexCompletenessCheckerFallbackFillsMissingFromResults(t *testing.T) {
	c := &codexDeepResearchCompletenessChecker{
		generate: func(context.Context, string, string) (string, error) {
			return `{"complete": false, "reasoning": "more work needed"}`, nil
		},
	}
	result, err := c.Check(context.Background(), CompletenessRequest{
		Objectives: []Objective{{Key: "a"}, {Key: "b"}, {Key: "c"}},
		ObjectiveResults: []ObjectiveResult{
			{Objective: Objective{Key: "a"}, Status: ObjectiveStatusSatisfied},
			{Objective: Objective{Key: "b"}, Status: ObjectiveStatusPartial},
			{Objective: Objective{Key: "c"}, Status: ObjectiveStatusMissing},
			{Objective: Objective{Key: ""}, Status: ObjectiveStatusMissing}, // empty key → dropped
		},
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(result.MissingObjectives) != 2 {
		t.Fatalf("expected 2 synthesized missing entries (b, c), got %d: %#v", len(result.MissingObjectives), result.MissingObjectives)
	}
	keys := map[string]bool{}
	for _, m := range result.MissingObjectives {
		keys[m.ObjectiveKey] = true
	}
	if !keys["b"] || !keys["c"] {
		t.Fatalf("expected b and c as synthesized missing, got %#v", result.MissingObjectives)
	}
	if keys["a"] {
		t.Fatalf("satisfied objective must not be marked missing: %#v", result.MissingObjectives)
	}
	// Each synthesized entry must carry a human-readable prompt so the
	// planner has something to interpolate next round.
	for _, m := range result.MissingObjectives {
		if strings.TrimSpace(m.Question) == "" {
			t.Fatalf("synthesized entry needs a question: %#v", m)
		}
	}
}

// TestCodexCompletenessCheckerSurfacesGenerateError ensures transport-level
// failures (codex CLI crash, timeout, web-search guard) are NOT swallowed —
// returning a default CompletenessResult would make the engine think the
// run was complete.
func TestCodexCompletenessCheckerSurfacesGenerateError(t *testing.T) {
	want := errors.New("codex CLI died")
	c := &codexDeepResearchCompletenessChecker{
		generate: func(context.Context, string, string) (string, error) {
			return "", want
		},
	}
	_, err := c.Check(context.Background(), CompletenessRequest{
		Objectives: []Objective{{Key: "research"}},
	})
	if err == nil {
		t.Fatalf("expected error to propagate")
	}
	if !errors.Is(err, want) {
		t.Fatalf("error chain should wrap the generate error; got %v", err)
	}
}

// TestCodexCompletenessCheckerInvalidJSONIncludesSnippet locks in the
// diagnostic raw-text snippet: when the model returns malformed JSON, the
// checker must fail and the error message must include a prefix of the
// offending payload so operators can see what went wrong in the logs. The
// snippet rule also truncates at ~280 chars so enormous responses don't
// blow up log lines.
func TestCodexCompletenessCheckerInvalidJSONIncludesSnippet(t *testing.T) {
	t.Run("short garbage", func(t *testing.T) {
		c := &codexDeepResearchCompletenessChecker{
			generate: func(context.Context, string, string) (string, error) {
				return `not json at all`, nil
			},
		}
		_, err := c.Check(context.Background(), CompletenessRequest{
			Objectives: []Objective{{Key: "research"}},
		})
		if err == nil {
			t.Fatalf("expected invalid-JSON error")
		}
		if !strings.Contains(err.Error(), "invalid JSON") {
			t.Fatalf("error must name the JSON failure mode; got %v", err)
		}
		if !strings.Contains(err.Error(), "not json at all") {
			t.Fatalf("error must include the raw snippet for debugging; got %v", err)
		}
	})

	t.Run("long garbage gets truncated", func(t *testing.T) {
		// 800-byte payload is comfortably above the 280-char snippet cap so a
		// working truncation drops it to 280 chars + "..." in the error. If
		// the cap drifts upward (e.g., someone raises it to 500), the
		// tight length check below will notice.
		long := strings.Repeat("x", 800)
		c := &codexDeepResearchCompletenessChecker{
			generate: func(context.Context, string, string) (string, error) {
				return long, nil
			},
		}
		_, err := c.Check(context.Background(), CompletenessRequest{
			Objectives: []Objective{{Key: "research"}},
		})
		if err == nil {
			t.Fatalf("expected invalid-JSON error")
		}
		if !strings.Contains(err.Error(), "...") {
			t.Fatalf("long payload should be truncated with ellipsis; got %v", err)
		}
		// Tight bound: snippet cap is 280 chars; the wrapper text ("codex
		// completeness checker returned invalid JSON: <cause> (raw=...)")
		// adds ~120 chars with json.Unmarshal's standard error. 420 leaves
		// ~20-char headroom; a regression that raised the cap to 500 would
		// blow past this, and a leaked 800-byte payload would be 920+.
		if got := len(err.Error()); got > 420 {
			t.Fatalf("error should be truncated to ~280-char snippet; got len=%d, err=%v", got, err)
		}
	})
}
