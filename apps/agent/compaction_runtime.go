package main

import (
	"context"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

var summarizeOlderHistory = summarizeOlderHistoryFallback

type historyCompactionResult struct {
	History         []loop.Message
	Applied         bool
	MaskedCount     int
	KeptFullCount   int
	TokensBefore    int
	TokensAfter     int
	SummarizedCount int
	UsedFallback    bool
}

func estimateHistoryTokens(history []loop.Message) int {
	return estimateHistorySize(history) / 4
}

func maybeCompactHistory(
	ctx context.Context,
	history []loop.Message,
	currentTokens int,
	triggerTokens int,
	targetTokens int,
	keepRecentOutputs int,
	model string,
) (*historyCompactionResult, error) {
	result := &historyCompactionResult{
		History:      history,
		TokensBefore: currentTokens,
		TokensAfter:  currentTokens,
	}

	if len(history) == 0 || triggerTokens <= 0 || targetTokens <= 0 || currentTokens < triggerTokens {
		return result, nil
	}

	return compactHistoryTowardsTarget(ctx, history, currentTokens, targetTokens, keepRecentOutputs, model)
}

func compactHistoryTowardsTarget(
	ctx context.Context,
	history []loop.Message,
	currentTokens int,
	targetTokens int,
	keepRecentOutputs int,
	model string,
) (*historyCompactionResult, error) {
	result := &historyCompactionResult{
		History:      history,
		TokensBefore: currentTokens,
		TokensAfter:  currentTokens,
	}
	if len(history) == 0 || targetTokens <= 0 {
		return result, nil
	}

	originalHistory := history
	maskResult := maskObservations(originalHistory, keepRecentOutputs)
	result.History = maskResult.MaskedHistory
	result.MaskedCount = maskResult.MaskedCount
	result.KeptFullCount = maskResult.KeptFullCount
	result.TokensAfter = estimateCompactedTokens(currentTokens, originalHistory, result.History)
	result.Applied = maskResult.MaskedCount > 0

	if result.TokensAfter > targetTokens && keepRecentOutputs > 0 {
		maskResult = maskObservations(originalHistory, 0)
		result.History = maskResult.MaskedHistory
		result.MaskedCount = maskResult.MaskedCount
		result.KeptFullCount = maskResult.KeptFullCount
		result.TokensAfter = estimateCompactedTokens(currentTokens, originalHistory, result.History)
		result.Applied = result.Applied || maskResult.MaskedCount > 0
	}

	if result.TokensAfter > targetTokens {
		summaryResult, err := summarizeOlderHistory(ctx, result.History, model)
		if err != nil {
			return result, err
		}
		if summaryResult != nil && summaryResult.SummaryGenerated {
			result.History = summaryResult.History
			result.TokensAfter = summaryResult.EstimatedTokens
			result.SummarizedCount = summaryResult.SummarizedCount
			result.UsedFallback = true
			result.Applied = true
		}
	}

	return result, nil
}

func estimateCompactedTokens(currentTokens int, original, compacted []loop.Message) int {
	tokensSaved := estimateTokenSavings(original, compacted)
	estimatedNewTokens := currentTokens - tokensSaved
	if estimatedNewTokens < 0 {
		return currentTokens / 2
	}
	return estimatedNewTokens
}

func compactHistoryAndReport(
	ctx context.Context,
	history []loop.Message,
	currentTokens int,
	triggerTokens int,
	targetTokens int,
	keepRecentOutputs int,
	model string,
	stage string,
) []loop.Message {
	result, err := maybeCompactHistory(ctx, history, currentTokens, triggerTokens, targetTokens, keepRecentOutputs, model)
	if err != nil {
		if stage == "" {
			output.SystemWarning("History compaction failed: %v", err)
		} else {
			output.SystemWarning("%s history compaction failed: %v", stage, err)
		}
		return history
	}
	if result == nil || !result.Applied {
		return history
	}

	if stage != "" {
		output.System("%s history compaction (%d -> %d tokens)", stage, result.TokensBefore, result.TokensAfter)
	}
	output.Compaction(result.MaskedCount, result.KeptFullCount, result.TokensBefore, result.TokensAfter)
	if result.UsedFallback {
		output.System("Applied summary fallback (%d messages summarized).", result.SummarizedCount)
	}
	return result.History
}
