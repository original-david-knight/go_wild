package deepresearch

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestCodexSynthesizerShortCircuitsWithoutSchema: synthesis is schema-driven.
// With an empty Schema there is no target to produce, so the synthesizer
// must not burn a codex call — it just returns an empty SynthesisResult.
// A stray codex run here would fabricate structure the caller never asked
// for; the pre-check is the correctness guarantee, not a cost optimization.
func TestCodexSynthesizerShortCircuitsWithoutSchema(t *testing.T) {
	called := false
	s := &codexDeepResearchSynthesizer{
		generate: func(context.Context, string, string) (string, error) {
			called = true
			return "", errors.New("must not be invoked without a schema")
		},
	}
	result, err := s.Synthesize(context.Background(), SynthesisRequest{})
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if called {
		t.Fatalf("synthesizer must skip codex when Schema is empty")
	}
	if result.Output != nil || result.Summary != "" {
		t.Fatalf("expected empty result, got %#v", result)
	}
}

// TestCodexSynthesizerWrappedOutputAndSummary covers the preferred response
// envelope: the model wraps its answer as {"output": ..., "summary": "..."}.
// The synthesizer must unwrap Output as arbitrary JSON (so any schema shape
// survives) AND trim the Summary. This is what flows into Result.Output and
// Result.Summary at the end of a run, so regressions here silently corrupt
// every downstream consumer.
func TestCodexSynthesizerWrappedOutputAndSummary(t *testing.T) {
	payload := `{
        "output": {"probability": 0.42, "sources": ["a", "b"]},
        "summary": "  short summary  "
    }`
	var gotPrompt string
	s := &codexDeepResearchSynthesizer{
		generate: func(_ context.Context, prompt, _ string) (string, error) {
			gotPrompt = prompt
			return payload, nil
		},
	}

	result, err := s.Synthesize(context.Background(), SynthesisRequest{
		Schema: map[string]any{"type": "object"},
	})
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if !strings.HasSuffix(gotPrompt, codexJSONSuffix) {
		t.Fatalf("prompt must end with the codex JSON-only suffix")
	}
	if result.Summary != "short summary" {
		t.Fatalf("summary must be trimmed; got %q", result.Summary)
	}
	// Output is unmarshalled as `any`, so JSON objects come back as
	// map[string]any and JSON numbers as float64.
	out, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("Output should unmarshal to map[string]any; got %T (%#v)", result.Output, result.Output)
	}
	if got := out["probability"]; got != 0.42 {
		t.Fatalf("probability = %v, want 0.42", got)
	}
	sources, _ := out["sources"].([]any)
	if !reflect.DeepEqual(sources, []any{"a", "b"}) {
		t.Fatalf("sources = %#v", sources)
	}
}

// TestCodexSynthesizerWrappedOutputIgnoresBadSummary covers a real corner
// case from the parser: the envelope unmarshal happens in two steps — first
// the outer map, then output and summary are parsed independently. If
// summary is present but not a string (e.g. null, number, object), the
// synthesizer MUST still return the output rather than discard everything.
// This test pins that best-effort behavior: a valid output alongside a
// malformed summary yields the output and an empty summary, not an error.
func TestCodexSynthesizerWrappedOutputIgnoresBadSummary(t *testing.T) {
	payload := `{"output": {"ok": true}, "summary": 12345}`
	s := &codexDeepResearchSynthesizer{
		generate: func(context.Context, string, string) (string, error) {
			return payload, nil
		},
	}
	result, err := s.Synthesize(context.Background(), SynthesisRequest{
		Schema: map[string]any{"type": "object"},
	})
	if err != nil {
		t.Fatalf("Synthesize() error = %v (should tolerate bad summary)", err)
	}
	out, ok := result.Output.(map[string]any)
	if !ok || out["ok"] != true {
		t.Fatalf("Output should have survived a bad summary; got %#v", result.Output)
	}
	if result.Summary != "" {
		t.Fatalf("bad summary type should yield empty string; got %q", result.Summary)
	}
}

// TestCodexSynthesizerUnwrappedJSONAcceptedAsOutput covers the alternate
// response shape: the model returns its JSON answer directly (no wrapping
// object with "output" and "summary"). The synthesizer falls back to
// treating the entire payload AS the output. Without this fallback, schemas
// that look like `{"type": "array"}` or `{"type": "string"}` — where the
// whole result is the answer — could never be produced.
func TestCodexSynthesizerUnwrappedJSONAcceptedAsOutput(t *testing.T) {
	t.Run("bare array", func(t *testing.T) {
		s := &codexDeepResearchSynthesizer{
			generate: func(context.Context, string, string) (string, error) {
				return `[1, 2, 3]`, nil
			},
		}
		result, err := s.Synthesize(context.Background(), SynthesisRequest{
			Schema: map[string]any{"type": "array"},
		})
		if err != nil {
			t.Fatalf("Synthesize() error = %v", err)
		}
		arr, ok := result.Output.([]any)
		if !ok || len(arr) != 3 {
			t.Fatalf("expected 3-element array, got %#v", result.Output)
		}
	})

	t.Run("unwrapped object without output field", func(t *testing.T) {
		// This is the ambiguous path: the payload is a valid object but
		// doesn't have an "output" key. The synthesizer's envelope branch
		// does nothing (no "output" to pluck) and we fall through to
		// treating the whole thing as Output. Regressing this path would
		// make the synthesizer reject any bare-object answer.
		s := &codexDeepResearchSynthesizer{
			generate: func(context.Context, string, string) (string, error) {
				return `{"answer": "yes", "confidence": 0.9}`, nil
			},
		}
		result, err := s.Synthesize(context.Background(), SynthesisRequest{
			Schema: map[string]any{"type": "object"},
		})
		if err != nil {
			t.Fatalf("Synthesize() error = %v", err)
		}
		out, ok := result.Output.(map[string]any)
		if !ok || out["answer"] != "yes" {
			t.Fatalf("expected whole object as output; got %#v", result.Output)
		}
	})
}

// TestCodexSynthesizerFencedJSONTolerant covers extractJSON integration: the
// codex CLI sometimes wraps replies in a markdown fence despite the suffix
// asking otherwise. Synthesize must still parse it.
func TestCodexSynthesizerFencedJSONTolerant(t *testing.T) {
	payload := "```json\n{\"output\": {\"ok\": true}, \"summary\": \"s\"}\n```"
	s := &codexDeepResearchSynthesizer{
		generate: func(context.Context, string, string) (string, error) {
			return payload, nil
		},
	}
	result, err := s.Synthesize(context.Background(), SynthesisRequest{
		Schema: map[string]any{"type": "object"},
	})
	if err != nil {
		t.Fatalf("Synthesize() error = %v (fenced JSON must parse)", err)
	}
	if result.Summary != "s" {
		t.Fatalf("summary = %q, want s", result.Summary)
	}
	if out, _ := result.Output.(map[string]any); out["ok"] != true {
		t.Fatalf("Output did not survive fence unwrap: %#v", result.Output)
	}
}

// TestCodexSynthesizerInvalidJSONErrors locks in the "neither envelope nor
// bare JSON parses" path: the synthesizer must return a descriptive error
// rather than an empty SynthesisResult, because an empty result would cause
// the engine to ship a run with schema_satisfied=false and no diagnostic.
func TestCodexSynthesizerInvalidJSONErrors(t *testing.T) {
	s := &codexDeepResearchSynthesizer{
		generate: func(context.Context, string, string) (string, error) {
			return `not json at all`, nil
		},
	}
	_, err := s.Synthesize(context.Background(), SynthesisRequest{
		Schema: map[string]any{"type": "object"},
	})
	if err == nil {
		t.Fatalf("expected invalid-JSON error")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("error should name the JSON failure mode; got %v", err)
	}
}

// TestCodexSynthesizerSurfacesGenerateError pins the transport-failure
// contract: when codex itself errors (CLI crash, context timeout, etc.)
// Synthesize wraps and propagates the error. Returning a zero-value
// SynthesisResult on error would paper over real infrastructure problems.
func TestCodexSynthesizerSurfacesGenerateError(t *testing.T) {
	want := errors.New("codex CLI died")
	s := &codexDeepResearchSynthesizer{
		generate: func(context.Context, string, string) (string, error) {
			return "", want
		},
	}
	_, err := s.Synthesize(context.Background(), SynthesisRequest{
		Schema: map[string]any{"type": "object"},
	})
	if err == nil {
		t.Fatalf("expected generate error to propagate")
	}
	if !errors.Is(err, want) {
		t.Fatalf("error chain should wrap the generate error; got %v", err)
	}
	if !strings.Contains(err.Error(), "codex synthesizer") {
		t.Fatalf("wrapped error should name the role for log readability; got %v", err)
	}
}
