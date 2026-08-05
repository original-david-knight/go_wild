package main

import (
	"context"
	"testing"

	"google.golang.org/genai"
	"github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

type mockLLMClient struct {
	model string
}

func (m *mockLLMClient) GenerateContent(ctx context.Context, contents []*genai.Content, config *loop.GenerateContentConfig) (*loop.GenerateResponse, error) {
	return &loop.GenerateResponse{}, nil
}

func (m *mockLLMClient) SetModel(model string) {
	m.model = model
}

func (m *mockLLMClient) GetModel() string {
	return m.model
}

func (m *mockLLMClient) Close() error {
	return nil
}

func newTestAgentLoop(t *testing.T, initialModel string) *loop.AgenticLoop {
	t.Helper()
	client := &mockLLMClient{model: initialModel}
	agent, err := loop.New(context.Background(), "", initialModel, loop.WithLLMClient(client))
	if err != nil {
		t.Fatalf("loop.New failed: %v", err)
	}
	return agent
}

func TestHandleSmartCommandToggleOnOff(t *testing.T) {
	models := modelPair{base: "base-model", smart: "smart-model"}
	agent := newTestAgentLoop(t, models.base)
	agent.SetThinkingBudget(0)

	smart := false
	ctx := commandContext{agent: agent, smartMode: &smart, models: models}

	// toggle on
	res := handleSmartCommand(data.CommandMessage{}, ctx)
	if res != cmdContinue {
		t.Fatalf("expected cmdContinue, got %v", res)
	}
	if !smart {
		t.Fatalf("expected smart mode enabled")
	}
	if agent.GetModel() != models.smart {
		t.Fatalf("expected model %q, got %q", models.smart, agent.GetModel())
	}
	if agent.GetThinkingBudget() != smartThinkingBudget {
		t.Fatalf("expected thinking budget %d, got %d", smartThinkingBudget, agent.GetThinkingBudget())
	}

	// toggle off
	res = handleSmartCommand(data.CommandMessage{}, ctx)
	if res != cmdContinue {
		t.Fatalf("expected cmdContinue, got %v", res)
	}
	if smart {
		t.Fatalf("expected smart mode disabled")
	}
	if agent.GetModel() != models.base {
		t.Fatalf("expected model %q, got %q", models.base, agent.GetModel())
	}
	if agent.GetThinkingBudget() != normalThinkingBudget {
		t.Fatalf("expected thinking budget %d, got %d", normalThinkingBudget, agent.GetThinkingBudget())
	}
}

func TestHandleSmartCommandExplicitDisable(t *testing.T) {
	models := modelPair{base: "base-model", smart: "smart-model"}
	agent := newTestAgentLoop(t, models.smart)
	agent.SetThinkingBudget(smartThinkingBudget)

	smart := true
	ctx := commandContext{agent: agent, smartMode: &smart, models: models}

	cm := data.CommandMessage{Args: map[string]any{"enabled": "off"}}
	res := handleSmartCommand(cm, ctx)
	if res != cmdContinue {
		t.Fatalf("expected cmdContinue, got %v", res)
	}
	if smart {
		t.Fatalf("expected smart mode disabled")
	}
	if agent.GetModel() != models.base {
		t.Fatalf("expected model %q, got %q", models.base, agent.GetModel())
	}
}

func TestHandleSmartCommandStatusDoesNotToggle(t *testing.T) {
	models := modelPair{base: "base-model", smart: "smart-model"}
	agent := newTestAgentLoop(t, models.base)
	agent.SetThinkingBudget(0)

	smart := false
	ctx := commandContext{agent: agent, smartMode: &smart, models: models}

	cm := data.CommandMessage{Args: map[string]any{"status": true}}
	res := handleSmartCommand(cm, ctx)
	if res != cmdContinue {
		t.Fatalf("expected cmdContinue, got %v", res)
	}
	if smart {
		t.Fatalf("expected smart mode unchanged (false)")
	}
	if agent.GetModel() != models.base {
		t.Fatalf("expected model %q, got %q", models.base, agent.GetModel())
	}
}

func TestHandleSmartCommandDoesNotEnableWithoutSmartModel(t *testing.T) {
	models := modelPair{base: "base-model", smart: ""}
	agent := newTestAgentLoop(t, models.base)
	agent.SetThinkingBudget(0)

	smart := false
	ctx := commandContext{agent: agent, smartMode: &smart, models: models}

	res := handleSmartCommand(data.CommandMessage{}, ctx)
	if res != cmdContinue {
		t.Fatalf("expected cmdContinue, got %v", res)
	}
	if smart {
		t.Fatalf("expected smart mode to remain disabled")
	}
	if agent.GetModel() != models.base {
		t.Fatalf("expected model %q, got %q", models.base, agent.GetModel())
	}
	if agent.GetThinkingBudget() != 0 {
		t.Fatalf("expected thinking budget 0, got %d", agent.GetThinkingBudget())
	}
}

func (m *mockLLMClient) GenerateContentStreaming(ctx context.Context, contents []*genai.Content, config *loop.GenerateContentConfig, sink func(string)) (*loop.GenerateResponse, error) {
	return loop.SingleDeltaFallback(ctx, m, contents, config, sink)
}
