package gowild_agentic_loop

import (
	"context"
	"time"

	"google.golang.org/genai"
)

const (
	// DefaultModel is the default Gemini model to use.
	DefaultModel = "gemini-3-flash-preview"

	// DefaultSystemPrompt is the default system instruction.
	DefaultSystemPrompt = "You are a helpful AI assistant. You have access to tools that can help you complete tasks. Use them when appropriate."

	// DefaultMaxTurns is the default maximum number of agentic turns.
	DefaultMaxTurns = 10

	// DefaultMaxToolCalls is the default cap for deep-research tools, which are
	// materially more expensive than ordinary broker-backed read tools.
	DefaultMaxToolCalls = 10
)

// LLMClient is the interface for LLM backends used by the agentic loop.
// GeminiClient implements this interface, but callers can provide alternative
// implementations (e.g., a broker proxy client) via WithLLMClient.
type LLMClient interface {
	GenerateContent(ctx context.Context, contents []*genai.Content, config *GenerateContentConfig) (*GenerateResponse, error)
	SetModel(model string)
	GetModel() string
	Close() error
}

// CompactFunc is a callback that compacts conversation history to reduce token usage.
// It receives the current history and prompt token count, and returns compacted history.
// The compacted history must preserve message roles and ordering that the LLM expects.
type CompactFunc func(history []Message, promptTokens int) ([]Message, error)

// AgenticLoop orchestrates conversations with the Gemini model and tool execution.
type AgenticLoop struct {
	client           LLMClient
	tools            []Tool
	toolMap          map[string]Tool
	systemPrompt     string
	maxTurns         int
	thinkingBudget   int32         // Token budget for extended thinking (0 = disabled)
	maxContextTokens int           // Maximum context tokens before stopping (0 = no limit)
	responseTimeout  time.Duration // Timeout for each API call (0 = no timeout)
	compactFunc      CompactFunc   // Optional mid-run compaction callback
	compactTokens    int           // Token threshold to trigger mid-run compaction (0 = disabled)
}
