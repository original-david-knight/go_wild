package gowild_agentic_loop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"google.golang.org/genai"
)

func (l *AgenticLoop) Run(ctx context.Context, history []Message) <-chan Event {
	events := make(chan Event, 100)

	go func() {
		defer close(events)
		l.runLoop(ctx, history, events)
	}()

	return events
}

func (l *AgenticLoop) runLoop(ctx context.Context, history []Message, events chan<- Event) {
	// Convert history to Gemini contents
	contents := make([]*genai.Content, 0, len(history))
	for _, msg := range history {
		contents = append(contents, msg.Content)
	}

	config := &GenerateContentConfig{
		SystemInstruction: l.systemPrompt,
		Tools:             toGeminiTools(l.tools),
		ThinkingBudget:    l.thinkingBudget,
	}

	var totalUsage ModelUsage
	var finalText string
	turn := 0
	lastTurnHadToolCalls := false
	toolCallCounts := make(map[string]int) // per-tool call counter for loop detection

	for turn < l.maxTurns {
		turn++

		// Check for context cancellation
		select {
		case <-ctx.Done():
			events <- ErrorEvent{Err: ctx.Err()}
			return
		default:
		}

		// Emit thinking event to indicate waiting for model response
		events <- ThinkingEvent{Turn: turn}

		// Generate response with retry for transient errors
		resp, err := l.generateWithRetry(ctx, contents, config, 3)
		if err != nil {
			events <- ErrorEvent{Err: fmt.Errorf("generation error: %w", err)}
			// Still emit DoneEvent so callers get accumulated text and history
			// from previous turns (prevents "empty response" on method jobs).
			events <- DoneEvent{
				Usage:      totalUsage,
				FinalText:  finalText,
				TurnCount:  turn,
				StopReason: "error",
				History:    contentsToMessages(contents),
			}
			return
		}

		// Track usage
		if resp.Usage != nil {
			totalUsage.PromptTokens = resp.Usage.PromptTokens // Use latest, not cumulative
			totalUsage.CompletionTokens += resp.Usage.CompletionTokens
			totalUsage.TotalTokens = resp.Usage.TotalTokens
		}

		// Check context limit - use 90% threshold to leave room for next turn
		if l.maxContextTokens > 0 && resp.Usage != nil {
			threshold := l.maxContextTokens * 90 / 100
			if resp.Usage.PromptTokens > threshold {
				events <- ContextLimitEvent{
					PromptTokens: resp.Usage.PromptTokens,
					MaxTokens:    l.maxContextTokens,
				}
				// Still emit done event with what we have
				events <- DoneEvent{
					Usage:      totalUsage,
					FinalText:  finalText,
					TurnCount:  turn,
					StopReason: "context_limit",
					History:    contentsToMessages(contents),
				}
				return
			}
		}

		// Process response
		var functionCalls []*genai.FunctionCall
		if resp.Content != nil {
			for _, part := range resp.Content.Parts {
				// Emit text
				if part.Text != "" {
					events <- TextDeltaEvent{Text: part.Text}
					finalText += part.Text
				}

				// Collect function calls
				if part.FunctionCall != nil {
					functionCalls = append(functionCalls, part.FunctionCall)
					events <- ToolCallEvent{
						ID:    part.FunctionCall.ID,
						Name:  part.FunctionCall.Name,
						Input: part.FunctionCall.Args,
					}
				}
			}

			// Add model response to history (preserves thought signatures)
			contents = append(contents, resp.Content)
		}

		// If no function calls, we're done — unless the model produced no text
		// after tool calls (e.g. Gemini responded with only thinking/thought
		// signatures). In that case, retry once without tools to force text output.
		if len(functionCalls) == 0 {
			turnHadText := false
			if resp.Content != nil {
				for _, part := range resp.Content.Parts {
					if part.Text != "" {
						turnHadText = true
						break
					}
				}
			}

			if !turnHadText && lastTurnHadToolCalls {
				// Model produced no text after tool results — force a text-only call
				textOnlyConfig := &GenerateContentConfig{
					SystemInstruction: config.SystemInstruction,
					Temperature:       config.Temperature,
					ThinkingBudget:    config.ThinkingBudget,
					// Tools deliberately omitted to force text output
				}
				events <- ThinkingEvent{Turn: turn}
				retryResp, retryErr := l.generateWithRetry(ctx, contents, textOnlyConfig, 3)
				if retryErr == nil {
					if retryResp.Usage != nil {
						totalUsage.PromptTokens = retryResp.Usage.PromptTokens
						totalUsage.CompletionTokens += retryResp.Usage.CompletionTokens
						totalUsage.TotalTokens = retryResp.Usage.TotalTokens
					}
					if retryResp.Content != nil {
						for _, part := range retryResp.Content.Parts {
							if part.Text != "" {
								events <- TextDeltaEvent{Text: part.Text}
								finalText += part.Text
							}
						}
						contents = append(contents, retryResp.Content)
					}
					resp = retryResp
				}
			}

			events <- DoneEvent{
				Usage:      totalUsage,
				FinalText:  finalText,
				TurnCount:  turn,
				StopReason: resp.FinishReason,
				History:    contentsToMessages(contents),
			}
			return
		}

		// Execute tool calls in parallel
		toolResults := l.executeToolsParallel(ctx, functionCalls, events, toolCallCounts)
		lastTurnHadToolCalls = true

		// Add tool results to history
		for _, tr := range toolResults {
			content := buildToolResultContent(tr.Name, tr.ID, tr.Result)
			contents = append(contents, content)
		}

		// Mid-run compaction: estimate actual token count including new tool results,
		// not just the stale prompt token count from before tools executed.
		// Without this, large tool results (e.g. multiple HTTP responses) can push
		// the context past the hard limit before the next API call.
		if l.compactFunc != nil && l.compactTokens > 0 && resp.Usage != nil {
			// Estimate tokens added by the model response + tool results since
			// the last API call reported resp.Usage.PromptTokens.
			newContentTokens := 0
			for _, tr := range toolResults {
				newContentTokens += estimateToolResultTokens(tr.Result)
			}
			estimatedTotal := resp.Usage.PromptTokens + newContentTokens

			if estimatedTotal >= l.compactTokens {
				messages := contentsToMessages(contents)
				compacted, err := l.compactFunc(messages, estimatedTotal)
				if err == nil && len(compacted) > 0 {
					events <- CompactionEvent{
						PromptTokensBefore: estimatedTotal,
						MessagesCompacted:  len(messages) - len(compacted),
					}
					contents = make([]*genai.Content, 0, len(compacted))
					for _, msg := range compacted {
						contents = append(contents, msg.Content)
					}
				}
			}
		}
	}

	// Max turns reached — if the last turn had tool calls, make one final LLM call
	// so the model can respond to the tool results instead of leaving them orphaned.
	// We inject a notice telling the model it has hit the turn limit so it should
	// summarize progress in text rather than requesting more tool calls.
	if lastTurnHadToolCalls {
		// Add a user message telling the model about the turn limit
		contents = append(contents, &genai.Content{
			Role: string(genai.RoleUser),
			Parts: []*genai.Part{
				{Text: fmt.Sprintf("[System: You have reached the maximum number of tool call turns (%d). Do NOT call any more tools. Instead, provide a text response summarizing what you accomplished and what remains to be done.]", l.maxTurns)},
			},
		})

		// Make one final LLM call without tools so the model must respond with text
		finalConfig := &GenerateContentConfig{
			SystemInstruction: config.SystemInstruction,
			Temperature:       config.Temperature,
			ThinkingBudget:    config.ThinkingBudget,
			// Tools deliberately omitted to prevent further tool calls
		}

		events <- ThinkingEvent{Turn: turn + 1}
		resp, err := l.generateWithRetry(ctx, contents, finalConfig, 3)
		if err == nil {
			if resp.Usage != nil {
				totalUsage.PromptTokens = resp.Usage.PromptTokens
				totalUsage.CompletionTokens += resp.Usage.CompletionTokens
				totalUsage.TotalTokens = resp.Usage.TotalTokens
			}
			if resp.Content != nil {
				for _, part := range resp.Content.Parts {
					if part.Text != "" {
						events <- TextDeltaEvent{Text: part.Text}
						finalText += part.Text
					}
				}
				contents = append(contents, resp.Content)
			}
		}
	}

	events <- DoneEvent{
		Usage:      totalUsage,
		FinalText:  finalText,
		TurnCount:  turn,
		StopReason: "max_turns",
		History:    contentsToMessages(contents),
	}
}

type toolResultPair struct {
	ID     string
	Name   string
	Result *ToolResult
}

func (l *AgenticLoop) executeToolsParallel(
	ctx context.Context,
	calls []*genai.FunctionCall,
	events chan<- Event,
	toolCallCounts map[string]int,
) []toolResultPair {
	results := make([]toolResultPair, len(calls))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, call := range calls {
		wg.Add(1)
		go func(i int, call *genai.FunctionCall) {
			defer wg.Done()

			// Check per-tool call limit
			mu.Lock()
			toolCallCounts[call.Name]++
			count := toolCallCounts[call.Name]
			mu.Unlock()

			var result *ToolResult
			maxCallsForTool := l.maxCallsForTool(call.Name)
			if maxCallsForTool > 0 && count > maxCallsForTool {
				result = NewErrorResult(fmt.Sprintf(
					"Tool %q has been called %d times (limit: %d). "+
						"You are stuck in a loop. Stop calling this tool and "+
						"synthesize a response from the results you already have.",
					call.Name, count, maxCallsForTool))
			} else {
				result = l.executeTool(ctx, call)
			}

			mu.Lock()
			results[i] = toolResultPair{
				ID:     call.ID,
				Name:   call.Name,
				Result: result,
			}
			mu.Unlock()

			events <- ToolResultEvent{
				ID:     call.ID,
				Name:   call.Name,
				Result: result,
			}
		}(i, call)
	}

	wg.Wait()
	return results
}

func (l *AgenticLoop) maxCallsForTool(name string) int {
	if isDeepResearchToolName(name) {
		return DefaultMaxToolCalls
	}
	return 0
}

func isDeepResearchToolName(name string) bool {
	return strings.HasPrefix(strings.TrimSpace(name), "deep_research_")
}

func (l *AgenticLoop) executeTool(ctx context.Context, call *genai.FunctionCall) *ToolResult {
	tool, ok := l.toolMap[call.Name]
	if !ok {
		return NewErrorResult(fmt.Sprintf("unknown tool: %s", call.Name))
	}

	result, err := tool.Execute(ctx, call.Args)
	if err != nil {
		return NewErrorResult(fmt.Sprintf("tool execution error: %v", err))
	}

	return result
}

// estimateToolResultTokens estimates the token count of a tool result
// using ~4 characters per token (standard heuristic for JSON/English text).
func estimateToolResultTokens(result *ToolResult) int {
	if result == nil {
		return 0
	}
	data, err := json.Marshal(result.ToMap())
	if err != nil {
		return 0
	}
	return len(data) / 4
}
