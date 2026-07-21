package gowild_agentic_loop

import (
	"context"
	"time"

	"github.com/original-david-knight/go_wild/my"
)

// New creates a new AgenticLoop.
// If apiKey is empty, it uses the GEMINI_API_KEY environment variable.
// If model is empty, it defaults to DefaultModel.
// Automatically loads .env file if present.
// If a custom LLMClient is provided via WithLLMClient, the apiKey is not required.
func New(ctx context.Context, apiKey string, model string, opts ...Option) (*AgenticLoop, error) {
	// Load .env file if present
	gowild_my.LoadEnv()

	if model == "" {
		model = DefaultModel
	}

	loop := &AgenticLoop{
		tools:        []Tool{},
		toolMap:      make(map[string]Tool),
		systemPrompt: DefaultSystemPrompt,
		maxTurns:     DefaultMaxTurns,
	}

	// Apply options first so WithLLMClient can set the client
	for _, opt := range opts {
		opt(loop)
	}

	// Only create a GeminiClient if no custom client was provided
	if loop.client == nil {
		client, err := NewGeminiClient(ctx, apiKey, model)
		if err != nil {
			return nil, err
		}
		loop.client = client
	}

	return loop, nil
}

// AddTools adds tools to the loop.
// If a tool with the same name already exists, it is replaced.
func (l *AgenticLoop) AddTools(tools ...Tool) {
	for _, tool := range tools {
		// Check if tool already exists in slice - replace if so
		replaced := false
		for i, existing := range l.tools {
			if existing.Name() == tool.Name() {
				l.tools[i] = tool
				replaced = true
				break
			}
		}
		if !replaced {
			l.tools = append(l.tools, tool)
		}
		l.toolMap[tool.Name()] = tool
	}
}

// SetModel changes the model used by the loop.
func (l *AgenticLoop) SetModel(model string) {
	l.client.SetModel(model)
}

// GetModel returns the current model.
func (l *AgenticLoop) GetModel() string {
	return l.client.GetModel()
}

// SetThinkingBudget sets the token budget for extended thinking.
// Set to 0 to disable extended thinking.
func (l *AgenticLoop) SetThinkingBudget(budget int32) {
	l.thinkingBudget = budget
}

// GetThinkingBudget returns the current thinking budget.
func (l *AgenticLoop) GetThinkingBudget() int32 {
	return l.thinkingBudget
}

// SetMaxContextTokens sets the max context token limit.
func (l *AgenticLoop) SetMaxContextTokens(maxTokens int) {
	l.maxContextTokens = maxTokens
}

// GetMaxContextTokens returns the current context token limit.
func (l *AgenticLoop) GetMaxContextTokens() int {
	return l.maxContextTokens
}

// SetResponseTimeout sets the response timeout for each API call.
func (l *AgenticLoop) SetResponseTimeout(timeout time.Duration) {
	l.responseTimeout = timeout
}

// GetResponseTimeout returns the response timeout.
func (l *AgenticLoop) GetResponseTimeout() time.Duration {
	return l.responseTimeout
}

// Close releases resources held by the loop.
func (l *AgenticLoop) Close() error {
	if l.client != nil {
		return l.client.Close()
	}
	return nil
}
