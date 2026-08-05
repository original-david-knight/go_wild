package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	data "github.com/original-david-knight/go_wild/agent_data"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"google.golang.org/genai"
)

type mockBrokerLLMClient struct {
	model         string
	setModelCalls int
	lastConfig    *loop.GenerateContentConfig
}

func (m *mockBrokerLLMClient) GenerateContent(_ context.Context, _ []*genai.Content, config *loop.GenerateContentConfig) (*loop.GenerateResponse, error) {
	if config != nil {
		copy := *config
		m.lastConfig = &copy
	}
	return &loop.GenerateResponse{FinishReason: "STOP"}, nil
}

func (m *mockBrokerLLMClient) SetModel(model string) {
	m.setModelCalls++
	m.model = model
}

func (m *mockBrokerLLMClient) GetModel() string { return m.model }

func (m *mockBrokerLLMClient) Close() error { return nil }

func TestBrokerLLMHandler_UsesRequestScopedModelOverride(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)
	agent, err := service.CreateAgent(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}
	agent.ModelProvider = data.LLMProviderOpenAI
	if err := service.UpdateAgent(context.Background(), agent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	client := &mockBrokerLLMClient{model: "default-model"}
	handler := NewBrokerLLMHandler(service)
	handler.newClient = func(_ context.Context, gotAgent *data.Agent, model string) (loop.LLMClient, error) {
		if gotAgent == nil || gotAgent.ID != "agent-1" {
			t.Fatalf("expected agent-1, got %#v", gotAgent)
		}
		if model != "model-x" {
			t.Fatalf("expected model-x passed to client factory, got %q", model)
		}
		return client, nil
	}
	handler.rateLimiter = newLLMRateLimiter(1)

	body, err := json.Marshal(BrokerLLMRequest{
		Contents: []loop.SerializedContent{},
		Model:    "model-x",
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/broker/v1/llm/generate", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, "agent-1"))
	rec := httptest.NewRecorder()

	handler.handleGenerate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if client.setModelCalls != 0 {
		t.Fatalf("expected shared SetModel to remain unused, got %d calls", client.setModelCalls)
	}
	if client.lastConfig == nil {
		t.Fatal("expected GenerateContent to receive config")
	}
	if client.lastConfig.Model != "model-x" {
		t.Fatalf("expected request-scoped model override, got %q", client.lastConfig.Model)
	}
}

func TestBrokerLLMHandler_BadJSON(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)

	handler := NewBrokerLLMHandler(service)
	handler.rateLimiter = newLLMRateLimiter(1)

	req := httptest.NewRequest(http.MethodPost, "/broker/v1/llm/generate", bytes.NewReader([]byte(`{invalid json`)))
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, "agent-1"))
	rec := httptest.NewRecorder()

	handler.handleGenerate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBrokerLLMHandler_MissingAgent(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)

	handler := NewBrokerLLMHandler(service)
	handler.rateLimiter = newLLMRateLimiter(1)

	body, err := json.Marshal(BrokerLLMRequest{
		Contents: []loop.SerializedContent{},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/broker/v1/llm/generate", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, "nonexistent-agent"))
	rec := httptest.NewRecorder()

	handler.handleGenerate(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBrokerLLMHandler_MethodNotAllowed(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)

	handler := NewBrokerLLMHandler(service)
	handler.rateLimiter = newLLMRateLimiter(1)

	req := httptest.NewRequest(http.MethodGet, "/broker/v1/llm/generate", nil)
	req = req.WithContext(context.WithValue(req.Context(), brokerAgentIDKey, "agent-1"))
	rec := httptest.NewRecorder()

	handler.handleGenerate(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBrokerLLMHandler_MissingAgentID(t *testing.T) {
	db := setupManagerTestDB(t)
	service := NewAgentService(db)

	handler := NewBrokerLLMHandler(service)
	handler.rateLimiter = newLLMRateLimiter(1)

	body, err := json.Marshal(BrokerLLMRequest{
		Contents: []loop.SerializedContent{},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	// No brokerAgentIDKey in context
	req := httptest.NewRequest(http.MethodPost, "/broker/v1/llm/generate", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.handleGenerate(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestResolveBrokerRequestedModel_IgnoresMismatchedProviderModel(t *testing.T) {
	agent := &data.Agent{
		ModelProvider: data.LLMProviderOpenAI,
		Model:         "gemini-3-flash-preview",
	}

	got := resolveBrokerRequestedModel(agent, "gemini-3.1-pro-preview")
	if got != loop.DefaultOpenAIModel {
		t.Fatalf("expected %q, got %q", loop.DefaultOpenAIModel, got)
	}
}

func (m *mockBrokerLLMClient) GenerateContentStreaming(ctx context.Context, contents []*genai.Content, config *loop.GenerateContentConfig, sink func(string)) (*loop.GenerateResponse, error) {
	return loop.SingleDeltaFallback(ctx, m, contents, config, sink)
}
