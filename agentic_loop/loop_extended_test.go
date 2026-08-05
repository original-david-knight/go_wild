package gowild_agentic_loop

import (
	"context"
	"fmt"
	"testing"
	"time"

	"google.golang.org/genai"
)

// mockLLMClient implements LLMClient for testing.
type mockLLMClient struct {
	model     string
	responses []*GenerateResponse
	callCount int
	lastCfg   *GenerateContentConfig
}

func (m *mockLLMClient) GenerateContent(ctx context.Context, contents []*genai.Content, config *GenerateContentConfig) (*GenerateResponse, error) {
	m.lastCfg = config
	if m.callCount < len(m.responses) {
		resp := m.responses[m.callCount]
		m.callCount++
		return resp, nil
	}
	return nil, fmt.Errorf("no more mock responses")
}

func (m *mockLLMClient) SetModel(model string) { m.model = model }
func (m *mockLLMClient) GetModel() string      { return m.model }
func (m *mockLLMClient) Close() error          { return nil }

func TestNew_WithLLMClient(t *testing.T) {
	mock := &mockLLMClient{model: "test-model"}
	loop, err := New(context.Background(), "", "", WithLLMClient(mock))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer loop.Close()

	if loop.GetModel() != "test-model" {
		t.Errorf("expected test-model, got %s", loop.GetModel())
	}
}

func TestNew_WithAllOptions(t *testing.T) {
	mock := &mockLLMClient{model: "m"}
	tool := NewFuncTool("t1", "desc", nil, nil)

	loop, err := New(context.Background(), "", "",
		WithLLMClient(mock),
		WithSystemPrompt("custom"),
		WithMaxTurns(5),
		WithTools(tool),
		WithMaxContextTokens(10000),
		WithResponseTimeout(30*time.Second),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer loop.Close()

	if loop.systemPrompt != "custom" {
		t.Errorf("expected custom prompt, got %s", loop.systemPrompt)
	}
	if loop.maxTurns != 5 {
		t.Errorf("expected maxTurns 5, got %d", loop.maxTurns)
	}
	if len(loop.tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(loop.tools))
	}
	if loop.maxContextTokens != 10000 {
		t.Errorf("expected maxContextTokens 10000, got %d", loop.maxContextTokens)
	}
	if loop.responseTimeout != 30*time.Second {
		t.Errorf("expected responseTimeout 30s, got %v", loop.responseTimeout)
	}
}

func TestSetModel_GetModel(t *testing.T) {
	mock := &mockLLMClient{model: "original"}
	loop, _ := New(context.Background(), "", "", WithLLMClient(mock))
	defer loop.Close()

	loop.SetModel("new-model")
	if loop.GetModel() != "new-model" {
		t.Errorf("expected new-model, got %s", loop.GetModel())
	}
}

func TestThinkingBudget(t *testing.T) {
	mock := &mockLLMClient{model: "m"}
	loop, _ := New(context.Background(), "", "", WithLLMClient(mock))
	defer loop.Close()

	if loop.GetThinkingBudget() != 0 {
		t.Errorf("expected 0 default thinking budget, got %d", loop.GetThinkingBudget())
	}

	loop.SetThinkingBudget(8192)
	if loop.GetThinkingBudget() != 8192 {
		t.Errorf("expected 8192, got %d", loop.GetThinkingBudget())
	}
}

func TestMaxContextTokens(t *testing.T) {
	mock := &mockLLMClient{model: "m"}
	loop, _ := New(context.Background(), "", "", WithLLMClient(mock))
	defer loop.Close()

	if loop.GetMaxContextTokens() != 0 {
		t.Errorf("expected 0 default, got %d", loop.GetMaxContextTokens())
	}

	loop.SetMaxContextTokens(32000)
	if loop.GetMaxContextTokens() != 32000 {
		t.Errorf("expected 32000, got %d", loop.GetMaxContextTokens())
	}
}

func TestResponseTimeout(t *testing.T) {
	mock := &mockLLMClient{model: "m"}
	loop, _ := New(context.Background(), "", "", WithLLMClient(mock))
	defer loop.Close()

	if loop.GetResponseTimeout() != 0 {
		t.Errorf("expected 0 default, got %v", loop.GetResponseTimeout())
	}

	loop.SetResponseTimeout(60 * time.Second)
	if loop.GetResponseTimeout() != 60*time.Second {
		t.Errorf("expected 60s, got %v", loop.GetResponseTimeout())
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		err       error
		retryable bool
	}{
		{nil, false},
		{fmt.Errorf("500 INTERNAL server error"), true},
		{fmt.Errorf("502 BAD_GATEWAY"), true},
		{fmt.Errorf("503 UNAVAILABLE"), true},
		{fmt.Errorf("429 RESOURCE_EXHAUSTED"), true},
		{fmt.Errorf("connection reset by peer"), true},
		{fmt.Errorf("connection refused"), true},
		{fmt.Errorf("context deadline exceeded"), true},
		{fmt.Errorf("timeout waiting for response"), true},
		{fmt.Errorf("invalid argument"), false},
		{fmt.Errorf("not found"), false},
		{fmt.Errorf("permission denied"), false},
	}

	for _, tc := range tests {
		got := isRetryableError(tc.err)
		errStr := "<nil>"
		if tc.err != nil {
			errStr = tc.err.Error()
		}
		if got != tc.retryable {
			t.Errorf("isRetryableError(%q) = %v, want %v", errStr, got, tc.retryable)
		}
	}
}

func TestIsRateLimitError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("429 RESOURCE_EXHAUSTED"), true},
		{fmt.Errorf("status 429: too many requests"), true},
		{fmt.Errorf("RESOURCE_EXHAUSTED: please slow down"), true},
		{fmt.Errorf("rate limiter cancelled: context deadline exceeded"), true},
		{fmt.Errorf("502 BAD_GATEWAY"), false},
		{fmt.Errorf("500 INTERNAL"), false},
		{fmt.Errorf("connection reset"), false},
	}
	for _, tc := range tests {
		if got := isRateLimitError(tc.err); got != tc.want {
			t.Errorf("isRateLimitError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestAddJitter(t *testing.T) {
	base := 10 * time.Second
	for i := 0; i < 100; i++ {
		jittered := addJitter(base)
		// Should be within ±25% (7.5s to 12.5s)
		if jittered < 7*time.Second || jittered > 13*time.Second {
			t.Fatalf("addJitter(%v) = %v, outside expected range", base, jittered)
		}
	}
}

func TestWithCompaction(t *testing.T) {
	loop := &AgenticLoop{
		tools:   []Tool{},
		toolMap: make(map[string]Tool),
	}

	called := false
	fn := func(history []Message, promptTokens int) ([]Message, error) {
		called = true
		return history, nil
	}

	WithCompaction(5000, fn)(loop)

	if loop.compactTokens != 5000 {
		t.Errorf("expected compactTokens 5000, got %d", loop.compactTokens)
	}
	if loop.compactFunc == nil {
		t.Error("expected non-nil compactFunc")
	}

	// Verify the function works
	loop.compactFunc(nil, 0)
	if !called {
		t.Error("compact function was not called")
	}
}

func TestAddTools_ReplacesExisting(t *testing.T) {
	loop := &AgenticLoop{
		tools:   []Tool{},
		toolMap: make(map[string]Tool),
	}

	tool1 := NewFuncTool("test", "original", nil, nil)
	tool2 := NewFuncTool("test", "replacement", nil, nil)

	loop.AddTools(tool1)
	if len(loop.tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(loop.tools))
	}

	loop.AddTools(tool2)
	if len(loop.tools) != 1 {
		t.Errorf("expected 1 tool after replacement, got %d", len(loop.tools))
	}
	if loop.tools[0].Description() != "replacement" {
		t.Error("tool should have been replaced")
	}
}

func TestRunSync_SimpleTextResponse(t *testing.T) {
	mock := &mockLLMClient{
		model: "test",
		responses: []*GenerateResponse{
			{
				Content: &genai.Content{
					Role: "model",
					Parts: []*genai.Part{
						{Text: "Hello, world!"},
					},
				},
				FinishReason: "STOP",
				Usage:        &ModelUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			},
		},
	}

	loop, _ := New(context.Background(), "", "", WithLLMClient(mock))
	defer loop.Close()

	done, err := loop.RunSync(context.Background(), []Message{NewUserMessage("hi")})
	if err != nil {
		t.Fatalf("RunSync failed: %v", err)
	}
	if done == nil {
		t.Fatal("expected non-nil done event")
	}
	if done.FinalText != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", done.FinalText)
	}
	if done.TurnCount != 1 {
		t.Errorf("expected 1 turn, got %d", done.TurnCount)
	}
	if done.Usage.PromptTokens != 10 {
		t.Errorf("expected 10 prompt tokens, got %d", done.Usage.PromptTokens)
	}
}

func TestRunSync_WithToolCall(t *testing.T) {
	tool := NewFuncTool("greet", "greeting tool", nil,
		func(ctx context.Context, input map[string]any) (*ToolResult, error) {
			return NewSuccessResult("Hello!"), nil
		},
	)

	mock := &mockLLMClient{
		model: "test",
		responses: []*GenerateResponse{
			// First response: tool call
			{
				Content: &genai.Content{
					Role: "model",
					Parts: []*genai.Part{
						{FunctionCall: &genai.FunctionCall{Name: "greet", Args: map[string]any{}}},
					},
				},
				Usage: &ModelUsage{PromptTokens: 10, CompletionTokens: 5},
			},
			// Second response: text after tool result
			{
				Content: &genai.Content{
					Role: "model",
					Parts: []*genai.Part{
						{Text: "The greeting is: Hello!"},
					},
				},
				FinishReason: "STOP",
				Usage:        &ModelUsage{PromptTokens: 20, CompletionTokens: 10},
			},
		},
	}

	loop, _ := New(context.Background(), "", "",
		WithLLMClient(mock),
		WithTools(tool),
	)
	defer loop.Close()

	done, err := loop.RunSync(context.Background(), []Message{NewUserMessage("greet me")})
	if err != nil {
		t.Fatalf("RunSync failed: %v", err)
	}
	if done.FinalText != "The greeting is: Hello!" {
		t.Errorf("expected greeting text, got %q", done.FinalText)
	}
	if done.TurnCount != 2 {
		t.Errorf("expected 2 turns, got %d", done.TurnCount)
	}
}

func TestRunSync_ContextCancelled(t *testing.T) {
	mock := &mockLLMClient{
		model: "test",
		responses: []*GenerateResponse{
			{
				Content: &genai.Content{
					Role:  "model",
					Parts: []*genai.Part{{Text: "start"}},
				},
				Usage: &ModelUsage{PromptTokens: 10},
			},
		},
	}

	loop, _ := New(context.Background(), "", "", WithLLMClient(mock))
	defer loop.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := loop.RunSync(ctx, []Message{NewUserMessage("hi")})
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestRunSync_ContextLimit(t *testing.T) {
	mock := &mockLLMClient{
		model: "test",
		responses: []*GenerateResponse{
			{
				Content: &genai.Content{
					Role:  "model",
					Parts: []*genai.Part{{Text: "response"}},
				},
				Usage: &ModelUsage{PromptTokens: 9500, TotalTokens: 10000},
			},
		},
	}

	loop, _ := New(context.Background(), "", "",
		WithLLMClient(mock),
		WithMaxContextTokens(10000), // 90% threshold = 9000
	)
	defer loop.Close()

	done, err := loop.RunSync(context.Background(), []Message{NewUserMessage("hi")})
	if err != nil {
		t.Fatalf("RunSync failed: %v", err)
	}
	if done.StopReason != "context_limit" {
		t.Errorf("expected stop reason 'context_limit', got %q", done.StopReason)
	}
}

func TestPrompt(t *testing.T) {
	mock := &mockLLMClient{
		model: "test",
		responses: []*GenerateResponse{
			{
				Content: &genai.Content{
					Role:  "model",
					Parts: []*genai.Part{{Text: "response"}},
				},
				FinishReason: "STOP",
			},
		},
	}

	loop, _ := New(context.Background(), "", "", WithLLMClient(mock))
	defer loop.Close()

	events, err := loop.Prompt(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	var gotText bool
	var gotDone bool
	for event := range events {
		switch event.(type) {
		case TextDeltaEvent:
			gotText = true
		case DoneEvent:
			gotDone = true
		}
	}

	if !gotText {
		t.Error("expected TextDeltaEvent")
	}
	if !gotDone {
		t.Error("expected DoneEvent")
	}
}

func TestContentsToMessages(t *testing.T) {
	contents := []*genai.Content{
		{Role: "user", Parts: []*genai.Part{{Text: "hi"}}},
		{Role: "model", Parts: []*genai.Part{{Text: "hello"}}},
		{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{Name: "tool", Response: map[string]any{}}}}},
		nil, // Should be skipped
	}

	messages := contentsToMessages(contents)
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages (nil skipped), got %d", len(messages))
	}
	if messages[0].Role != RoleUser {
		t.Errorf("expected RoleUser, got %s", messages[0].Role)
	}
	if messages[1].Role != RoleModel {
		t.Errorf("expected RoleModel, got %s", messages[1].Role)
	}
	if messages[2].Role != RoleTool {
		t.Errorf("expected RoleTool, got %s", messages[2].Role)
	}
}

func TestBuildToolResultContent(t *testing.T) {
	result := NewSuccessResult("done")
	content := buildToolResultContent("my_tool", "call_123", result)

	if content == nil {
		t.Fatal("expected non-nil content")
	}
	if len(content.Parts) == 0 {
		t.Fatal("expected at least one part")
	}
	if content.Parts[0].FunctionResponse == nil {
		t.Fatal("expected FunctionResponse part")
	}
	if content.Parts[0].FunctionResponse.Name != "my_tool" {
		t.Errorf("expected name 'my_tool', got %s", content.Parts[0].FunctionResponse.Name)
	}
	if content.Parts[0].FunctionResponse.ID != "call_123" {
		t.Errorf("expected ID 'call_123', got %s", content.Parts[0].FunctionResponse.ID)
	}
}

func TestWithLLMClient(t *testing.T) {
	loop := &AgenticLoop{
		tools:   []Tool{},
		toolMap: make(map[string]Tool),
	}

	mock := &mockLLMClient{model: "custom"}
	WithLLMClient(mock)(loop)

	if loop.client != mock {
		t.Error("expected custom LLM client to be set")
	}
}

func (m *mockLLMClient) GenerateContentStreaming(ctx context.Context, contents []*genai.Content, config *GenerateContentConfig, sink func(string)) (*GenerateResponse, error) {
	return SingleDeltaFallback(ctx, m, contents, config, sink)
}
