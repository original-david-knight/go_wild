package main

import (
	"context"
	"strings"
	"testing"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

func TestMaybeCompactHistory_BelowThreshold_NoOp(t *testing.T) {
	history := []loop.Message{
		loop.NewUserMessage("hello"),
		createToolResultMessage("tool1", map[string]any{"result": strings.Repeat("x", 200)}),
	}

	currentTokens := estimateHistoryTokens(history)
	result, err := maybeCompactHistory(context.Background(), history, currentTokens, currentTokens+1, 10, 1, "test-model")
	if err != nil {
		t.Fatalf("maybeCompactHistory returned error: %v", err)
	}
	if result.Applied {
		t.Fatalf("expected no compaction below threshold")
	}
	if result.TokensBefore != currentTokens || result.TokensAfter != currentTokens {
		t.Fatalf("expected tokens to remain unchanged, got %d -> %d", result.TokensBefore, result.TokensAfter)
	}
	if len(result.History) != len(history) {
		t.Fatalf("expected history length %d, got %d", len(history), len(result.History))
	}
}

func TestMaybeCompactHistory_MasksHistoryWhenHigh(t *testing.T) {
	blob := strings.Repeat("x", 1200)
	history := []loop.Message{
		loop.NewUserMessage("task 1"),
		createToolResultMessage("tool1", map[string]any{"result": blob}),
		loop.NewModelTextMessage("working"),
		createToolResultMessage("tool2", map[string]any{"result": blob}),
		loop.NewModelTextMessage("still working"),
		createToolResultMessage("tool3", map[string]any{"result": blob}),
		loop.NewUserMessage("finish up"),
		createToolResultMessage("tool4", map[string]any{"result": blob}),
	}

	currentTokens := estimateHistoryTokens(history)
	targetTokens := currentTokens - 400
	result, err := maybeCompactHistory(context.Background(), history, currentTokens, currentTokens-1, targetTokens, 1, "test-model")
	if err != nil {
		t.Fatalf("maybeCompactHistory returned error: %v", err)
	}
	if !result.Applied {
		t.Fatalf("expected compaction to apply")
	}
	if result.MaskedCount != 3 {
		t.Fatalf("expected 3 masked tool results, got %d", result.MaskedCount)
	}
	if result.KeptFullCount != 1 {
		t.Fatalf("expected 1 full tool result kept, got %d", result.KeptFullCount)
	}
	if result.TokensAfter >= result.TokensBefore {
		t.Fatalf("expected token estimate to shrink, got %d -> %d", result.TokensBefore, result.TokensAfter)
	}
	if result.TokensAfter > targetTokens {
		t.Fatalf("expected token estimate to reach target %d, got %d", targetTokens, result.TokensAfter)
	}

	firstMasked := result.History[1].Content.Parts[0].FunctionResponse.Response["_masked"]
	if firstMasked == nil {
		t.Fatalf("expected oldest tool result to be masked")
	}
	lastMasked := result.History[7].Content.Parts[0].FunctionResponse.Response["_masked"]
	if lastMasked != nil {
		t.Fatalf("expected newest tool result to remain unmasked")
	}
}

func TestMaybeCompactHistory_UsesSummaryFallback(t *testing.T) {
	originalFallback := summarizeOlderHistory
	t.Cleanup(func() {
		summarizeOlderHistory = originalFallback
	})

	called := false
	summarizeOlderHistory = func(ctx context.Context, history []loop.Message, model string) (*summaryFallbackResult, error) {
		called = true
		return &summaryFallbackResult{
			History: []loop.Message{
				loop.NewUserMessage("<summary>\ncondensed\n</summary>"),
			},
			SummarizedCount:  len(history),
			EstimatedTokens:  5,
			SummaryGenerated: true,
		}, nil
	}

	history := []loop.Message{
		loop.NewUserMessage(strings.Repeat("a", 1200)),
		loop.NewUserMessage(strings.Repeat("b", 1200)),
	}

	currentTokens := estimateHistoryTokens(history)
	result, err := maybeCompactHistory(context.Background(), history, currentTokens, 1, 10, 0, "test-model")
	if err != nil {
		t.Fatalf("maybeCompactHistory returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected summary fallback to be invoked")
	}
	if !result.Applied || !result.UsedFallback {
		t.Fatalf("expected fallback compaction to apply")
	}
	if result.SummarizedCount != len(history) {
		t.Fatalf("expected %d summarized messages, got %d", len(history), result.SummarizedCount)
	}
	if result.TokensAfter != 5 {
		t.Fatalf("expected fallback token estimate 5, got %d", result.TokensAfter)
	}
	if len(result.History) != 1 || extractText(result.History[0]) == "" {
		t.Fatalf("expected summarized history to replace original messages")
	}
}
