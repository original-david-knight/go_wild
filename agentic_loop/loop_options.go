package gowild_agentic_loop

import (
	"time"
)

// Option configures an AgenticLoop.
type Option func(*AgenticLoop)

// WithSystemPrompt sets the system instruction for the loop.
func WithSystemPrompt(prompt string) Option {
	return func(l *AgenticLoop) {
		l.systemPrompt = prompt
	}
}

// WithMaxTurns sets the maximum number of agentic turns.
func WithMaxTurns(maxTurns int) Option {
	return func(l *AgenticLoop) {
		l.maxTurns = maxTurns
	}
}

// WithTools adds tools to the loop.
func WithTools(tools ...Tool) Option {
	return func(l *AgenticLoop) {
		l.AddTools(tools...)
	}
}

// WithMaxContextTokens sets the maximum context token limit.
// When the prompt token count exceeds this limit, the loop will stop and emit
// a ContextLimitEvent before the next API call. Set to 0 to disable (default).
func WithMaxContextTokens(maxTokens int) Option {
	return func(l *AgenticLoop) {
		l.maxContextTokens = maxTokens
	}
}

// WithResponseTimeout sets a timeout for each Gemini API call.
// If a response takes longer than this duration, it will be cancelled and retried.
// Set to 0 to disable (default).
func WithResponseTimeout(timeout time.Duration) Option {
	return func(l *AgenticLoop) {
		l.responseTimeout = timeout
	}
}

// WithCompaction sets a mid-run compaction callback and token threshold.
// When prompt tokens exceed the threshold during a tool-calling loop, the callback
// is invoked to compact the conversation history before the next API call.
// This prevents long tool-call sequences from hitting the hard context limit.
func WithCompaction(threshold int, fn CompactFunc) Option {
	return func(l *AgenticLoop) {
		l.compactTokens = threshold
		l.compactFunc = fn
	}
}

// WithLLMClient sets a custom LLM client for the loop.
// When set, the loop will use this client instead of creating a GeminiClient.
// This allows callers to provide alternative backends (e.g., a broker proxy).
func WithLLMClient(client LLMClient) Option {
	return func(l *AgenticLoop) {
		l.client = client
	}
}
