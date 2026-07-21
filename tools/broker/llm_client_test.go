package broker

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

func TestNewLLMClient(t *testing.T) {
	c := NewTestClient("/tmp/test.sock", "token")
	lc := NewLLMClient(c, "gemini-3-flash-preview")
	if lc == nil {
		t.Fatal("expected non-nil LLM client")
	}
	if lc.GetModel() != "gemini-3-flash-preview" {
		t.Errorf("expected model 'gemini-3-flash-preview', got %q", lc.GetModel())
	}
}

func TestLLMClient_SetModel(t *testing.T) {
	lc := NewLLMClient(NewTestClient("/tmp/test.sock", "t"), "model-a")
	lc.SetModel("model-b")
	if lc.GetModel() != "model-b" {
		t.Errorf("expected 'model-b', got %q", lc.GetModel())
	}
}

func TestLLMClient_Close(t *testing.T) {
	lc := NewLLMClient(NewTestClient("/tmp/test.sock", "t"), "model")
	if err := lc.Close(); err != nil {
		t.Errorf("unexpected error from Close: %v", err)
	}
}

func TestLLMClient_GenerateContent_Success(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/broker/v1/llm/generate" {
			t.Errorf("expected /broker/v1/llm/generate, got %s", r.URL.Path)
		}

		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if req["model"] != "test-model" {
			t.Errorf("expected model 'test-model', got %v", req["model"])
		}

		w.Write([]byte(`{
			"content": {
				"role": "model",
				"parts": [{"text": "Hello, world!"}]
			},
			"finish_reason": "STOP"
		}`))
	}))

	lc := NewLLMClient(c, "test-model")
	result, err := lc.GenerateContent(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FinishReason != "STOP" {
		t.Errorf("expected STOP, got %q", result.FinishReason)
	}
	if result.Content == nil {
		t.Fatal("expected content")
	}
}

func TestLLMClient_GenerateContent_NilContent(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"finish_reason": "STOP"}`))
	}))

	lc := NewLLMClient(c, "test-model")
	result, err := lc.GenerateContent(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != nil {
		t.Error("expected nil content")
	}
	if result.FinishReason != "STOP" {
		t.Errorf("expected STOP, got %q", result.FinishReason)
	}
}

func TestLLMClient_GenerateContent_BrokerError(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]any{"error": "LLM unavailable"})
	}))

	lc := NewLLMClient(c, "test-model")
	_, err := lc.GenerateContent(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLLMClient_GenerateContent_InvalidResponse(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))

	lc := NewLLMClient(c, "test-model")
	_, err := lc.GenerateContent(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLLMClient_GenerateContent_WithConfig(t *testing.T) {
	var gotBody map[string]any
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"finish_reason": "STOP"}`))
	}))

	temp := float32(0.7)
	lc := NewLLMClient(c, "test-model")
	_, err := lc.GenerateContent(context.Background(), nil, &loop.GenerateContentConfig{
		SystemInstruction: "Be helpful",
		Temperature:       &temp,
		MaxOutputTokens:   1024,
		ThinkingBudget:    512,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["system_instruction"] != "Be helpful" {
		t.Errorf("expected system instruction, got %v", gotBody["system_instruction"])
	}
	if gotBody["max_output_tokens"].(float64) != 1024 {
		t.Errorf("expected max_output_tokens 1024, got %v", gotBody["max_output_tokens"])
	}
}

func TestLLMClient_GenerateContent_ConfigModelOverridesClientModel(t *testing.T) {
	var gotBody map[string]any
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"finish_reason": "STOP"}`))
	}))

	lc := NewLLMClient(c, "default-model")
	_, err := lc.GenerateContent(context.Background(), nil, &loop.GenerateContentConfig{
		Model: "override-model",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["model"] != "override-model" {
		t.Fatalf("expected override model, got %v", gotBody["model"])
	}
}
