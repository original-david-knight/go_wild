package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"google.golang.org/genai"
)

type VisionAnalyzer struct {
	Config Config
	Client loop.LLMClient
	Prompt string
	Logger DebugLogger
}

func NewAnalyzer(ctx context.Context, cfg Config, logger DebugLogger) (Analyzer, error) {
	if strings.EqualFold(cfg.AgentProvider, "fake") {
		return FakeAnalyzerFromEnv{}, nil
	}
	if err := EnsureDefaultPrompt(cfg.AgentPromptPath); err != nil {
		return nil, err
	}
	prompt, err := os.ReadFile(cfg.AgentPromptPath)
	if err != nil {
		return nil, fmt.Errorf("read prompt %q: %w", cfg.AgentPromptPath, err)
	}
	client, err := loop.NewProviderClient(ctx, loop.ProviderClientConfig{
		Provider:       cfg.AgentProvider,
		Model:          cfg.AgentModel,
		OpenAIAuthMode: cfg.OpenAIAuthMode,
		BaseURL:        cfg.AgentBaseURL,
	})
	if err != nil {
		return nil, err
	}
	return &VisionAnalyzer{
		Config: cfg,
		Client: client,
		Prompt: string(prompt),
		Logger: logger,
	}, nil
}

func (a *VisionAnalyzer) Analyze(ctx context.Context, input AnalyzeInput) (AnalyzeResult, error) {
	intent, err := ParseAssistIntent(string(input.Intent))
	if err != nil {
		return AnalyzeResult{}, err
	}
	tools, err := a.toolsForIntent(intent)
	if err != nil {
		return AnalyzeResult{}, err
	}
	systemInstruction := strings.TrimSpace(a.Prompt)
	if modeInstruction := strings.TrimSpace(intent.systemInstruction()); modeInstruction != "" {
		systemInstruction += "\n\n" + modeInstruction
	}

	imageData, err := os.ReadFile(input.ImagePath)
	if err != nil {
		return AnalyzeResult{}, fmt.Errorf("read screenshot %q: %w", input.ImagePath, err)
	}
	input.Intent = intent
	mimeType := detectImageMIME(input.ImagePath, imageData)
	userText, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return AnalyzeResult{}, fmt.Errorf("encode analyzer metadata: %w", err)
	}

	msg := loop.NewUserMessageWithImage(
		"Analyze the complete screenshot image. Metadata:\n"+string(userText),
		imageData,
		mimeType,
	)
	if intent == AssistIntentFactCheck {
		result, raw, finishReason, sources, err := a.generateAndParse(ctx, msg.Content, systemInstruction, tools, false)
		if err != nil {
			a.Logger.Printf("fact-check analyzer failed: finish_reason=%q chars=%d error=%v", finishReason, len(raw), err)
			return AnalyzeResult{}, err
		}
		a.Logger.Printf("fact-check analyzer response: finish_reason=%q chars=%d attributable_sources=%d", finishReason, len(raw), len(sources))
		return applyIntentEvidencePolicy(intent, result, sources), nil
	}

	result, raw, finishReason, sources, err := a.generateAndParse(ctx, msg.Content, systemInstruction, tools, true)
	if err == nil {
		a.Logger.Printf("analyzer response: finish_reason=%q chars=%d", finishReason, len(raw))
		return applyIntentEvidencePolicy(intent, result, sources), nil
	}
	a.Logger.Printf("structured analyzer attempt failed: finish_reason=%q chars=%d error=%v", finishReason, len(raw), err)

	result, raw, finishReason, sources, retryErr := a.generateAndParse(ctx, msg.Content, systemInstruction, tools, false)
	if retryErr == nil {
		a.Logger.Printf("analyzer fallback response: finish_reason=%q chars=%d", finishReason, len(raw))
		return applyIntentEvidencePolicy(intent, result, sources), nil
	}
	a.Logger.Printf("fallback analyzer attempt failed: finish_reason=%q chars=%d error=%v", finishReason, len(raw), retryErr)
	return AnalyzeResult{}, retryErr
}

func (a *VisionAnalyzer) toolsForIntent(intent AssistIntent) ([]*genai.Tool, error) {
	if intent != AssistIntentFactCheck {
		return nil, nil
	}
	if err := validateIntentProvider(intent, a.Config.AgentProvider); err != nil {
		return nil, err
	}
	return []*genai.Tool{{GoogleSearch: &genai.GoogleSearch{}}}, nil
}

func validateIntentProvider(intent AssistIntent, provider string) error {
	if intent == AssistIntentFactCheck && !strings.EqualFold(strings.TrimSpace(provider), loop.LLMProviderGemini) {
		return fmt.Errorf("fact-check requires agent_provider %q for Google Search grounding", loop.LLMProviderGemini)
	}
	return nil
}

func (a *VisionAnalyzer) generateAndParse(ctx context.Context, content *genai.Content, systemInstruction string, tools []*genai.Tool, strictSchema bool) (AnalyzeResult, string, string, []string, error) {
	temp := float32(0)
	cfg := &loop.GenerateContentConfig{
		SystemInstruction: systemInstruction,
		Tools:             tools,
		Temperature:       &temp,
		MaxOutputTokens:   a.Config.AgentMaxOutputTokens,
	}
	if strictSchema {
		cfg.ResponseMIMEType = "application/json"
		cfg.ResponseSchema = analyzeResultSchema()
	} else if len(tools) == 0 {
		cfg.ResponseMIMEType = "application/json"
	}
	resp, err := a.Client.GenerateContent(ctx, []*genai.Content{content}, cfg)
	if err != nil {
		return AnalyzeResult{}, "", "", nil, err
	}
	raw := ""
	finishReason := ""
	if resp != nil {
		raw = loop.ExtractText(resp.Content)
		finishReason = resp.FinishReason
	}
	result, err := ParseAnalyzeResult(raw)
	if err != nil {
		if strings.TrimSpace(raw) == "" {
			return AnalyzeResult{}, raw, finishReason, nil, fmt.Errorf("empty analyzer response; finish_reason=%q", finishReason)
		}
		return AnalyzeResult{}, raw, finishReason, nil, err
	}
	sources := responseGroundingSources(resp, result.SpokenAnswer)
	if len(tools) > 0 && resp != nil && resp.GroundingMetadata != nil {
		a.Logger.Printf("grounding metadata: chunks=%d supports=%d attributable_sources=%d", len(resp.GroundingMetadata.GroundingChunks), len(resp.GroundingMetadata.GroundingSupports), len(sources))
	}
	return result, raw, finishReason, sources, nil
}

func responseGroundingSources(resp *loop.GenerateResponse, spokenAnswer string) []string {
	if resp == nil || resp.Content == nil || resp.GroundingMetadata == nil {
		return nil
	}
	bodyWords := normalizedWords(factCheckAnswerBody(spokenAnswer))
	if len(bodyWords) == 0 {
		return nil
	}
	answerSpans := jsonStringFieldSpansByPart(resp.Content, "spoken_answer", spokenAnswer)
	if len(answerSpans) == 0 {
		return nil
	}
	chunks := resp.GroundingMetadata.GroundingChunks
	seen := make(map[string]struct{})
	supportedWords := make(map[string]struct{})
	var sources []string
	for _, support := range resp.GroundingMetadata.GroundingSupports {
		if support == nil || support.Segment == nil {
			continue
		}
		segmentText, ok := spokenAnswerGroundingSegment(resp.Content, support.Segment, answerSpans)
		if !ok {
			continue
		}
		segmentWords := normalizedWords(segmentText)
		if len(segmentWords) == 0 {
			continue
		}
		attributed := false
		for _, index := range support.GroundingChunkIndices {
			if index < 0 || int(index) >= len(chunks) {
				continue
			}
			chunk := chunks[index]
			if chunk == nil || chunk.Web == nil {
				continue
			}
			uri := strings.TrimSpace(chunk.Web.URI)
			if uri == "" {
				continue
			}
			attributed = true
			if _, ok := seen[uri]; ok {
				continue
			}
			seen[uri] = struct{}{}
			if len(sources) < 5 {
				sources = append(sources, uri)
			}
		}
		if attributed {
			for _, word := range segmentWords {
				supportedWords[word] = struct{}{}
			}
		}
	}
	bodyWordSet := make(map[string]struct{})
	for _, word := range bodyWords {
		bodyWordSet[word] = struct{}{}
	}
	matched := 0
	for word := range bodyWordSet {
		if _, ok := supportedWords[word]; ok {
			matched++
		}
	}
	const minimumWords = 3
	if len(bodyWordSet) < minimumWords {
		return nil
	}
	if matched < minimumWords || float64(matched)/float64(len(bodyWordSet)) < 0.6 {
		return nil
	}
	return sources
}

type byteSpan struct {
	start int
	end   int
}

func jsonStringFieldSpansByPart(content *genai.Content, key, value string) map[int][]byteSpan {
	spans := make(map[int][]byteSpan)
	if content == nil {
		return spans
	}
	for partIndex, part := range content.Parts {
		if part == nil || part.Thought || part.Text == "" {
			continue
		}
		if partSpans := jsonStringFieldSpans(part.Text, key, value); len(partSpans) > 0 {
			spans[partIndex] = partSpans
		}
	}
	return spans
}

func jsonStringFieldSpans(text, key, value string) []byteSpan {
	var spans []byteSpan
	for i := 0; i < len(text); {
		if text[i] != '"' {
			i++
			continue
		}
		keyEnd, ok := jsonStringEnd(text, i)
		if !ok {
			return spans
		}
		var decodedKey string
		if err := json.Unmarshal([]byte(text[i:keyEnd]), &decodedKey); err != nil || decodedKey != key {
			i = keyEnd
			continue
		}

		valueStart := skipJSONSpace(text, keyEnd)
		if valueStart >= len(text) || text[valueStart] != ':' {
			i = keyEnd
			continue
		}
		valueStart = skipJSONSpace(text, valueStart+1)
		if valueStart >= len(text) || text[valueStart] != '"' {
			i = keyEnd
			continue
		}
		valueEnd, ok := jsonStringEnd(text, valueStart)
		if !ok {
			return spans
		}
		var decodedValue string
		if err := json.Unmarshal([]byte(text[valueStart:valueEnd]), &decodedValue); err == nil && decodedValue == value {
			spans = append(spans, byteSpan{start: valueStart + 1, end: valueEnd - 1})
		}
		i = valueEnd
	}
	return spans
}

func jsonStringEnd(text string, start int) (int, bool) {
	if start < 0 || start >= len(text) || text[start] != '"' {
		return 0, false
	}
	for i := start + 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case '"':
			return i + 1, true
		}
	}
	return 0, false
}

func skipJSONSpace(text string, start int) int {
	for start < len(text) {
		switch text[start] {
		case ' ', '\t', '\r', '\n':
			start++
		default:
			return start
		}
	}
	return start
}

func spokenAnswerGroundingSegment(content *genai.Content, segment *genai.Segment, answerSpans map[int][]byteSpan) (string, bool) {
	if content == nil || segment == nil {
		return "", false
	}
	partIndex := int(segment.PartIndex)
	if partIndex < 0 || partIndex >= len(content.Parts) {
		return "", false
	}
	part := content.Parts[partIndex]
	if part == nil || part.Thought {
		return "", false
	}
	spans := answerSpans[partIndex]
	if len(spans) == 0 {
		return "", false
	}

	start := int(segment.StartIndex)
	end := int(segment.EndIndex)
	if end > start && start >= 0 && end <= len(part.Text) {
		extracted := part.Text[start:end]
		if segment.Text != "" && strings.TrimSpace(segment.Text) != strings.TrimSpace(extracted) {
			return "", false
		}
		if !containedByAnySpan(byteSpan{start: start, end: end}, spans) {
			return "", false
		}
		if segment.Text != "" {
			return segment.Text, true
		}
		return extracted, true
	}

	// Older responses can omit offsets. Accept their text only when it has one
	// unambiguous occurrence and that occurrence is inside spoken_answer.
	if segment.Text == "" {
		return "", false
	}
	occurrences := 0
	insideAnswer := false
	for offset := 0; offset <= len(part.Text)-len(segment.Text); {
		relative := strings.Index(part.Text[offset:], segment.Text)
		if relative < 0 {
			break
		}
		start = offset + relative
		end = start + len(segment.Text)
		occurrences++
		if containedByAnySpan(byteSpan{start: start, end: end}, spans) {
			insideAnswer = true
		}
		offset = end
	}
	if occurrences != 1 || !insideAnswer {
		return "", false
	}
	return segment.Text, true
}

func containedByAnySpan(candidate byteSpan, spans []byteSpan) bool {
	for _, span := range spans {
		if candidate.start >= span.start && candidate.end <= span.end {
			return true
		}
	}
	return false
}

func factCheckAnswerBody(answer string) string {
	answer = strings.TrimSpace(answer)
	prefix := factCheckVerdictPrefix(answer)
	if prefix == "" {
		return answer
	}
	return strings.TrimSpace(strings.TrimPrefix(answer, prefix))
}

func normalizedWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func applyIntentEvidencePolicy(intent AssistIntent, result AnalyzeResult, sources []string) AnalyzeResult {
	if intent != AssistIntentFactCheck {
		return result
	}
	result.Sources = sources
	if !result.QuestionFound {
		return result
	}
	prefix := factCheckVerdictPrefix(result.SpokenAnswer)
	if prefix == "Could not verify:" {
		result.Confidence = ConfidenceLow
		result.SpokenAnswer = "Could not verify: grounded evidence did not support a reliable verdict."
		return result
	}
	if prefix == "" || result.QuestionCount != 1 || len(sources) == 0 {
		return unverifiedFactCheckResult(result, sources)
	}
	// Grounding URIs can be Google redirect URLs, so this fast path cannot
	// reliably establish source independence. Never present it as high confidence.
	if result.Confidence == ConfidenceHigh {
		result.Confidence = ConfidenceMedium
	}
	return result
}

func factCheckVerdictPrefix(text string) string {
	text = strings.TrimSpace(text)
	for _, prefix := range []string{"Supported:", "Contradicted:", "Misleading:", "Could not verify:"} {
		if strings.HasPrefix(text, prefix) {
			return prefix
		}
	}
	return ""
}

func unverifiedFactCheckResult(result AnalyzeResult, sources []string) AnalyzeResult {
	result.Confidence = ConfidenceLow
	result.SpokenAnswer = "Could not verify: the grounded response did not contain enough usable evidence."
	result.DebugSummary = "Fact-check result withheld because its verdict or grounding support was incomplete."
	result.QuestionCount = 1
	result.Sources = sources
	return result
}

func (a *VisionAnalyzer) Close() error {
	if a == nil || a.Client == nil {
		return nil
	}
	return a.Client.Close()
}

type FakeAnalyzerFromEnv struct{}

func (FakeAnalyzerFromEnv) Analyze(_ context.Context, _ AnalyzeInput) (AnalyzeResult, error) {
	raw := strings.TrimSpace(os.Getenv("SCREEN_AGENT_FAKE_ANALYZER_JSON"))
	if raw == "" {
		return AnalyzeResult{
			QuestionFound: false,
			QuestionCount: 0,
			Confidence:    ConfidenceLow,
		}, nil
	}
	return ParseAnalyzeResult(raw)
}

func ParseAnalyzeResult(raw string) (AnalyzeResult, error) {
	raw = strings.TrimSpace(stripCodeFence(raw))
	if raw == "" {
		return AnalyzeResult{}, fmt.Errorf("empty analyzer response")
	}
	var result AnalyzeResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		extracted := extractJSONObject(raw)
		if extracted == "" {
			return AnalyzeResult{}, fmt.Errorf("parse analyzer JSON: %w", err)
		}
		if err2 := json.Unmarshal([]byte(extracted), &result); err2 != nil {
			return AnalyzeResult{}, fmt.Errorf("parse analyzer JSON: %w", err)
		}
	}
	if err := result.Validate(); err != nil {
		return AnalyzeResult{}, err
	}
	return result, nil
}

func (r AnalyzeResult) Validate() error {
	if r.QuestionCount < 0 {
		return fmt.Errorf("question_count must not be negative")
	}
	switch r.Confidence {
	case ConfidenceLow, ConfidenceMedium, ConfidenceHigh:
	default:
		return fmt.Errorf("confidence must be low, medium, or high, got %q", r.Confidence)
	}
	if !r.QuestionFound {
		if r.QuestionCount != 0 {
			return fmt.Errorf("question_count must be zero when question_found is false")
		}
		if strings.TrimSpace(r.SpokenAnswer) != "" {
			return fmt.Errorf("spoken_answer must be empty when question_found is false")
		}
	}
	if r.QuestionFound {
		if r.QuestionCount == 0 {
			return fmt.Errorf("question_count must be positive when question_found is true")
		}
		if strings.TrimSpace(r.SpokenAnswer) == "" {
			return fmt.Errorf("spoken_answer must not be empty when question_found is true")
		}
	}
	return nil
}

func detectImageMIME(path string, data []byte) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	}
	mime := http.DetectContentType(data)
	if strings.HasPrefix(mime, "image/") {
		return mime
	}
	return "image/png"
}

func analyzeResultSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"question_found": {
				Type:        genai.TypeBoolean,
				Description: "True when the screenshot contains visible content that merits a concise spoken answer or useful assistance.",
			},
			"question_count": {
				Type:        genai.TypeInteger,
				Description: "Number of distinct visible prompts, questions, or tasks being answered. Use 1 for one general screen-assist response.",
			},
			"confidence": {
				Type:        genai.TypeString,
				Enum:        []string{"low", "medium", "high"},
				Description: "Confidence in the detection and concise response.",
			},
			"spoken_answer": {
				Type:        genai.TypeString,
				Description: "Short response safe to read aloud. Leave empty when question_found is false.",
			},
			"debug_summary": {
				Type:        genai.TypeString,
				Description: "Short private debugging summary without chain-of-thought.",
			},
		},
		Required: []string{"question_found", "question_count", "confidence", "spoken_answer"},
	}
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return s
	}
	if strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		return strings.Join(lines[1:len(lines)-1], "\n")
	}
	return s
}

func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}
