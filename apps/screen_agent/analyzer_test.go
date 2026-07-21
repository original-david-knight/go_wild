package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"google.golang.org/genai"
)

type recordingAnalyzerClient struct {
	configs  []*loop.GenerateContentConfig
	response *loop.GenerateResponse
	model    string
}

func (c *recordingAnalyzerClient) GenerateContent(_ context.Context, _ []*genai.Content, cfg *loop.GenerateContentConfig) (*loop.GenerateResponse, error) {
	c.configs = append(c.configs, cfg)
	return c.response, nil
}

func (c *recordingAnalyzerClient) SetModel(model string) { c.model = model }
func (c *recordingAnalyzerClient) GetModel() string      { return c.model }
func (c *recordingAnalyzerClient) Close() error          { return nil }

func TestParseAnalyzeResult_CodeFence(t *testing.T) {
	result, err := ParseAnalyzeResult("```json\n{\"question_found\":true,\"question_count\":1,\"confidence\":\"medium\",\"spoken_answer\":\"The answer is B.\"}\n```")
	if err != nil {
		t.Fatalf("ParseAnalyzeResult returned error: %v", err)
	}
	if !result.QuestionFound || result.QuestionCount != 1 || result.SpokenAnswer != "The answer is B." {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestParseAnalyzeResultRejectsContradictoryNoQuestion(t *testing.T) {
	_, err := ParseAnalyzeResult(`{"question_found":false,"question_count":0,"confidence":"low","spoken_answer":"The answer is C."}`)
	if err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestParseAnalyzeResultRejectsInconsistentPresenceFields(t *testing.T) {
	tests := []string{
		`{"question_found":false,"question_count":1,"confidence":"low","spoken_answer":""}`,
		`{"question_found":true,"question_count":1,"confidence":"high","spoken_answer":""}`,
	}
	for _, raw := range tests {
		if _, err := ParseAnalyzeResult(raw); err == nil {
			t.Fatalf("ParseAnalyzeResult(%s) unexpectedly succeeded", raw)
		}
	}
}

func TestVisionAnalyzerEnglishSummaryAddsIntentWithoutSearch(t *testing.T) {
	client := &recordingAnalyzerClient{response: analyzerResponse(
		`{"question_found":true,"question_count":1,"confidence":"high","spoken_answer":"The document announces a July launch."}`,
		nil,
	)}
	analyzer := VisionAnalyzer{
		Config: DefaultConfig(),
		Client: client,
		Prompt: defaultPrompt,
	}
	result, err := analyzer.Analyze(context.Background(), AnalyzeInput{
		ImagePath: testAnalyzerImage(t),
		Intent:    AssistIntentEnglishSummary,
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if result.SpokenAnswer != "The document announces a July launch." {
		t.Fatalf("spoken answer = %q", result.SpokenAnswer)
	}
	if len(client.configs) != 1 {
		t.Fatalf("GenerateContent calls = %d, want 1", len(client.configs))
	}
	cfg := client.configs[0]
	if !strings.Contains(cfg.SystemInstruction, "ENGLISH-SUMMARY mode") || !strings.Contains(cfg.SystemInstruction, "regardless of the language") {
		t.Fatalf("missing English-summary instruction: %s", cfg.SystemInstruction)
	}
	if len(cfg.Tools) != 0 {
		t.Fatalf("English summary tools = %#v, want none", cfg.Tools)
	}
}

func TestVisionAnalyzerFactCheckUsesGoogleSearchGrounding(t *testing.T) {
	grounding := &genai.GroundingMetadata{
		GroundingChunks: []*genai.GroundingChunk{{
			Web: &genai.GroundingChunkWeb{URI: "https://example.test/source", Title: "Source"},
		}},
		GroundingSupports: []*genai.GroundingSupport{{
			Segment:               &genai.Segment{Text: "the visible date is one year too early"},
			GroundingChunkIndices: []int32{0},
		}},
	}
	client := &recordingAnalyzerClient{response: analyzerResponse(
		`{"question_found":true,"question_count":1,"confidence":"high","spoken_answer":"Contradicted: the visible date is one year too early."}`,
		grounding,
	)}
	analyzer := VisionAnalyzer{
		Config: DefaultConfig(),
		Client: client,
		Prompt: defaultPrompt,
	}
	result, err := analyzer.Analyze(context.Background(), AnalyzeInput{
		ImagePath: testAnalyzerImage(t),
		Intent:    AssistIntentFactCheck,
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if !strings.HasPrefix(result.SpokenAnswer, "Contradicted:") {
		t.Fatalf("spoken answer = %q", result.SpokenAnswer)
	}
	if result.Confidence != ConfidenceMedium {
		t.Fatalf("confidence = %q, want medium with only one source host", result.Confidence)
	}
	if len(result.Sources) != 1 || result.Sources[0] != "https://example.test/source" {
		t.Fatalf("grounding sources = %#v", result.Sources)
	}
	cfg := client.configs[0]
	if len(cfg.Tools) != 1 || cfg.Tools[0] == nil || cfg.Tools[0].GoogleSearch == nil {
		t.Fatalf("fact-check tools = %#v, want Google Search", cfg.Tools)
	}
	if cfg.ResponseSchema != nil || cfg.ResponseMIMEType != "" {
		t.Fatalf("fact-check should use one unstructured grounded call: %#v", cfg)
	}
	if !strings.Contains(cfg.SystemInstruction, "FACT-CHECK mode") {
		t.Fatalf("missing fact-check instruction: %s", cfg.SystemInstruction)
	}
	if !strings.Contains(cfg.SystemInstruction, "Do not repeat or quote the visible claim") {
		t.Fatalf("missing grounded-reason instruction: %s", cfg.SystemInstruction)
	}
}

func TestVisionAnalyzerFactCheckWithholdsUngroundedVerdict(t *testing.T) {
	client := &recordingAnalyzerClient{response: analyzerResponse(
		`{"question_found":true,"question_count":1,"confidence":"high","spoken_answer":"Supported: this is true."}`,
		nil,
	)}
	analyzer := VisionAnalyzer{
		Config: DefaultConfig(),
		Client: client,
		Prompt: defaultPrompt,
	}
	result, err := analyzer.Analyze(context.Background(), AnalyzeInput{
		ImagePath: testAnalyzerImage(t),
		Intent:    AssistIntentFactCheck,
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if result.Confidence != ConfidenceLow || !strings.HasPrefix(result.SpokenAnswer, "Could not verify:") {
		t.Fatalf("ungrounded result was not withheld: %#v", result)
	}
}

func TestVisionAnalyzerFactCheckRejectsUnattributedSearchChunk(t *testing.T) {
	grounding := &genai.GroundingMetadata{GroundingChunks: []*genai.GroundingChunk{{
		Web: &genai.GroundingChunkWeb{URI: "https://example.test/unattributed"},
	}}}
	client := &recordingAnalyzerClient{response: analyzerResponse(
		`{"question_found":true,"question_count":1,"confidence":"high","spoken_answer":"Supported: this is true."}`,
		grounding,
	)}
	analyzer := VisionAnalyzer{Config: DefaultConfig(), Client: client, Prompt: defaultPrompt}
	result, err := analyzer.Analyze(context.Background(), AnalyzeInput{
		ImagePath: testAnalyzerImage(t),
		Intent:    AssistIntentFactCheck,
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if !strings.HasPrefix(result.SpokenAnswer, "Could not verify:") || len(result.Sources) != 0 {
		t.Fatalf("unattributed search result was accepted: %#v", result)
	}
	if len(client.configs) != 1 {
		t.Fatalf("fact-check calls = %d, want one call without retry", len(client.configs))
	}
}

func TestVisionAnalyzerFactCheckRejectsInvalidVerdictPrefix(t *testing.T) {
	grounding := &genai.GroundingMetadata{
		GroundingChunks: []*genai.GroundingChunk{{
			Web: &genai.GroundingChunkWeb{URI: "https://example.test/source"},
		}},
		GroundingSupports: []*genai.GroundingSupport{{
			Segment:               &genai.Segment{Text: "the claim looks right"},
			GroundingChunkIndices: []int32{0},
		}},
	}
	client := &recordingAnalyzerClient{response: analyzerResponse(
		`{"question_found":true,"question_count":1,"confidence":"high","spoken_answer":"Probably true: the claim looks right."}`,
		grounding,
	)}
	analyzer := VisionAnalyzer{Config: DefaultConfig(), Client: client, Prompt: defaultPrompt}
	result, err := analyzer.Analyze(context.Background(), AnalyzeInput{
		ImagePath: testAnalyzerImage(t),
		Intent:    AssistIntentFactCheck,
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if !strings.HasPrefix(result.SpokenAnswer, "Could not verify:") || result.Confidence != ConfidenceLow {
		t.Fatalf("invalid verdict prefix was accepted: %#v", result)
	}
}

func TestVisionAnalyzerFactCheckReplacesUngroundedCouldNotVerifyText(t *testing.T) {
	client := &recordingAnalyzerClient{response: analyzerResponse(
		`{"question_found":true,"question_count":1,"confidence":"low","spoken_answer":"Could not verify: no sources were found, but the claim is definitely false."}`,
		nil,
	)}
	analyzer := VisionAnalyzer{Config: DefaultConfig(), Client: client, Prompt: defaultPrompt}
	result, err := analyzer.Analyze(context.Background(), AnalyzeInput{
		ImagePath: testAnalyzerImage(t),
		Intent:    AssistIntentFactCheck,
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	want := "Could not verify: grounded evidence did not support a reliable verdict."
	if result.SpokenAnswer != want {
		t.Fatalf("spoken answer = %q, want fixed safe uncertainty", result.SpokenAnswer)
	}
}

func TestVisionAnalyzerFactCheckRequiresSubstantiveGroundingCoverage(t *testing.T) {
	grounding := &genai.GroundingMetadata{
		GroundingChunks: []*genai.GroundingChunk{{
			Web: &genai.GroundingChunkWeb{URI: "https://example.test/nasa"},
		}},
		GroundingSupports: []*genai.GroundingSupport{{
			Segment:               &genai.Segment{Text: "NASA exists"},
			GroundingChunkIndices: []int32{0},
		}},
	}
	client := &recordingAnalyzerClient{response: analyzerResponse(
		`{"question_found":true,"question_count":1,"confidence":"high","spoken_answer":"Supported: NASA exists, therefore the moon is made of cheese."}`,
		grounding,
	)}
	analyzer := VisionAnalyzer{Config: DefaultConfig(), Client: client, Prompt: defaultPrompt}
	result, err := analyzer.Analyze(context.Background(), AnalyzeInput{
		ImagePath: testAnalyzerImage(t),
		Intent:    AssistIntentFactCheck,
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if !strings.HasPrefix(result.SpokenAnswer, "Could not verify:") || len(result.Sources) != 0 {
		t.Fatalf("partial grounding unlocked full verdict: %#v", result)
	}
}

func TestVisionAnalyzerFactCheckAcceptsOffsetOnlyGroundingInSpokenAnswer(t *testing.T) {
	raw := `{"question_found":true,"question_count":1,"confidence":"high","spoken_answer":"Contradicted: the visible date is one year too early.","debug_summary":"Claim checked."}`
	segmentText := "the visible date is one year too early"
	start := strings.Index(raw, segmentText)
	if start < 0 {
		t.Fatal("test segment missing from response")
	}
	grounding := &genai.GroundingMetadata{
		GroundingChunks: []*genai.GroundingChunk{{
			Web: &genai.GroundingChunkWeb{URI: "https://example.test/date"},
		}},
		GroundingSupports: []*genai.GroundingSupport{{
			Segment: &genai.Segment{
				PartIndex:  0,
				StartIndex: int32(start),
				EndIndex:   int32(start + len(segmentText)),
			},
			GroundingChunkIndices: []int32{0},
		}},
	}
	client := &recordingAnalyzerClient{response: analyzerResponse(raw, grounding)}
	analyzer := VisionAnalyzer{Config: DefaultConfig(), Client: client, Prompt: defaultPrompt}
	result, err := analyzer.Analyze(context.Background(), AnalyzeInput{
		ImagePath: testAnalyzerImage(t),
		Intent:    AssistIntentFactCheck,
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if len(result.Sources) != 1 || result.Sources[0] != "https://example.test/date" {
		t.Fatalf("offset-only grounding sources = %#v", result.Sources)
	}
}

func TestVisionAnalyzerFactCheckRejectsGroundingForAnotherJSONField(t *testing.T) {
	segmentText := "the visible date is one year too early"
	raw := `{"question_found":true,"question_count":1,"confidence":"high","spoken_answer":"Contradicted: the visible date is one year too early.","debug_summary":"the visible date is one year too early"}`
	start := strings.LastIndex(raw, segmentText)
	if start < 0 {
		t.Fatal("test segment missing from response")
	}
	grounding := &genai.GroundingMetadata{
		GroundingChunks: []*genai.GroundingChunk{{
			Web: &genai.GroundingChunkWeb{URI: "https://example.test/wrong-field"},
		}},
		GroundingSupports: []*genai.GroundingSupport{{
			Segment: &genai.Segment{
				PartIndex:  0,
				StartIndex: int32(start),
				EndIndex:   int32(start + len(segmentText)),
				Text:       segmentText,
			},
			GroundingChunkIndices: []int32{0},
		}},
	}
	client := &recordingAnalyzerClient{response: analyzerResponse(raw, grounding)}
	analyzer := VisionAnalyzer{Config: DefaultConfig(), Client: client, Prompt: defaultPrompt}
	result, err := analyzer.Analyze(context.Background(), AnalyzeInput{
		ImagePath: testAnalyzerImage(t),
		Intent:    AssistIntentFactCheck,
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if !strings.HasPrefix(result.SpokenAnswer, "Could not verify:") || len(result.Sources) != 0 {
		t.Fatalf("grounding from debug_summary was accepted: %#v", result)
	}
}

func TestVisionAnalyzerFactCheckRejectsSegmentTextThatDisagreesWithOffsets(t *testing.T) {
	raw := `{"question_found":true,"question_count":1,"confidence":"high","spoken_answer":"Contradicted: the law does not prohibit solar panels in homes.","debug_summary":"Claim checked."}`
	actualText := "the law does not prohibit solar panels in homes"
	start := strings.Index(raw, actualText)
	if start < 0 {
		t.Fatal("test segment missing from response")
	}
	grounding := &genai.GroundingMetadata{
		GroundingChunks: []*genai.GroundingChunk{{
			Web: &genai.GroundingChunkWeb{URI: "https://example.test/inconsistent"},
		}},
		GroundingSupports: []*genai.GroundingSupport{{
			Segment: &genai.Segment{
				PartIndex:  0,
				StartIndex: int32(start),
				EndIndex:   int32(start + len(actualText)),
				Text:       "the law does prohibit solar panels in homes",
			},
			GroundingChunkIndices: []int32{0},
		}},
	}
	client := &recordingAnalyzerClient{response: analyzerResponse(raw, grounding)}
	analyzer := VisionAnalyzer{Config: DefaultConfig(), Client: client, Prompt: defaultPrompt}
	result, err := analyzer.Analyze(context.Background(), AnalyzeInput{
		ImagePath: testAnalyzerImage(t),
		Intent:    AssistIntentFactCheck,
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if !strings.HasPrefix(result.SpokenAnswer, "Could not verify:") || len(result.Sources) != 0 {
		t.Fatalf("inconsistent grounding segment was accepted: %#v", result)
	}
}

func TestVisionAnalyzerFactCheckRejectsUngroundedProvider(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AgentProvider = loop.LLMProviderOpenAI
	analyzer := VisionAnalyzer{Config: cfg, Client: &recordingAnalyzerClient{}, Prompt: defaultPrompt}
	_, err := analyzer.Analyze(context.Background(), AnalyzeInput{Intent: AssistIntentFactCheck})
	if err == nil || !strings.Contains(err.Error(), `requires agent_provider "gemini"`) {
		t.Fatalf("error = %v, want Gemini requirement", err)
	}
}

func TestAppFactCheckRejectsFakeProviderBeforeAnalyzer(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AgentProvider = "fake"
	app := NewApp(cfg, nil, nil, nil)
	_, err := app.Analyze(context.Background(), AnalyzeInput{Intent: AssistIntentFactCheck})
	if err == nil || !strings.Contains(err.Error(), `requires agent_provider "gemini"`) {
		t.Fatalf("error = %v, want Gemini requirement", err)
	}
}

func analyzerResponse(raw string, grounding *genai.GroundingMetadata) *loop.GenerateResponse {
	return &loop.GenerateResponse{
		Content:           genai.NewContentFromText(raw, genai.RoleModel),
		GroundingMetadata: grounding,
		FinishReason:      "STOP",
	}
}

func testAnalyzerImage(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "screen.png")
	if err := os.WriteFile(path, []byte("not-a-real-png"), 0o600); err != nil {
		t.Fatalf("write analyzer image: %v", err)
	}
	return path
}
