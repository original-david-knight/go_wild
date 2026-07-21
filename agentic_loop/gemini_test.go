package gowild_agentic_loop

import (
	"testing"

	"google.golang.org/genai"
)

func TestExtractText(t *testing.T) {
	content := &genai.Content{
		Parts: []*genai.Part{
			{Text: "Hello, "},
			{Text: "world!"},
		},
	}

	text := ExtractText(content)
	if text != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %s", text)
	}
}

func TestExtractText_Nil(t *testing.T) {
	text := ExtractText(nil)
	if text != "" {
		t.Errorf("expected empty string for nil content, got %s", text)
	}
}

func TestExtractText_NoParts(t *testing.T) {
	content := &genai.Content{
		Parts: []*genai.Part{},
	}

	text := ExtractText(content)
	if text != "" {
		t.Errorf("expected empty string for no parts, got %s", text)
	}
}

func TestExtractText_NonTextParts(t *testing.T) {
	content := &genai.Content{
		Parts: []*genai.Part{
			{Text: "text"},
			{FunctionCall: &genai.FunctionCall{Name: "test"}}, // Non-text part
		},
	}

	text := ExtractText(content)
	if text != "text" {
		t.Errorf("expected 'text', got %s", text)
	}
}

func TestExtractText_WithThoughts(t *testing.T) {
	content := &genai.Content{
		Parts: []*genai.Part{
			{Text: "Internal reasoning...", Thought: true}, // Should be filtered
			{Text: "Hello!"}, // Should be included
		},
	}

	text := ExtractText(content)
	if text != "Hello!" {
		t.Errorf("expected 'Hello!', got %s", text)
	}
}

func TestParseGenerateResponse_Nil(t *testing.T) {
	result := parseGenerateResponse(nil)
	if result == nil {
		t.Fatal("expected non-nil result for nil input")
	}
	if result.Content != nil {
		t.Errorf("expected nil content, got %v", result.Content)
	}
}

func TestParseGenerateResponse_Empty(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{},
	}

	result := parseGenerateResponse(resp)
	if result.Content != nil {
		t.Errorf("expected nil content for empty candidates, got %v", result.Content)
	}
}

func TestParseGenerateResponse_NilCandidate(t *testing.T) {
	resp := &genai.GenerateContentResponse{Candidates: []*genai.Candidate{nil}}
	result := parseGenerateResponse(resp)
	if result == nil || result.Content != nil || result.GroundingMetadata != nil {
		t.Fatalf("unexpected result for nil candidate: %#v", result)
	}
}

func TestParseGenerateResponse_WithContent(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Role: "model",
					Parts: []*genai.Part{
						{Text: "Hello!"},
					},
				},
				FinishReason: genai.FinishReasonStop,
			},
		},
	}

	result := parseGenerateResponse(resp)

	if result.Content == nil {
		t.Fatal("expected non-nil content")
	}
	if result.FinishReason != "STOP" {
		t.Errorf("expected finish reason 'STOP', got %s", result.FinishReason)
	}
}

func TestParseGenerateResponse_WithFunctionCalls(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Role: "model",
					Parts: []*genai.Part{
						{
							FunctionCall: &genai.FunctionCall{
								Name: "get_weather",
								Args: map[string]any{"location": "SF"},
							},
						},
					},
				},
			},
		},
	}

	result := parseGenerateResponse(resp)

	if len(result.FunctionCalls) != 1 {
		t.Fatalf("expected 1 function call, got %d", len(result.FunctionCalls))
	}
	if result.FunctionCalls[0].Name != "get_weather" {
		t.Errorf("expected function name 'get_weather', got %s", result.FunctionCalls[0].Name)
	}
}

func TestParseGenerateResponse_PreservesGroundingMetadata(t *testing.T) {
	grounding := &genai.GroundingMetadata{GroundingChunks: []*genai.GroundingChunk{{
		Web: &genai.GroundingChunkWeb{URI: "https://example.test/source"},
	}}}
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{GroundingMetadata: grounding}},
	}

	result := parseGenerateResponse(resp)
	if result.GroundingMetadata != grounding {
		t.Fatalf("grounding metadata was not preserved: %#v", result.GroundingMetadata)
	}
}

func TestParseGenerateResponse_WithUsage(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{}},
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     100,
			CandidatesTokenCount: 50,
			TotalTokenCount:      150,
		},
	}

	result := parseGenerateResponse(resp)

	if result.Usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if result.Usage.PromptTokens != 100 {
		t.Errorf("expected prompt tokens 100, got %d", result.Usage.PromptTokens)
	}
	if result.Usage.CompletionTokens != 50 {
		t.Errorf("expected completion tokens 50, got %d", result.Usage.CompletionTokens)
	}
	if result.Usage.TotalTokens != 150 {
		t.Errorf("expected total tokens 150, got %d", result.Usage.TotalTokens)
	}
}
