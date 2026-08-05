package gowild_agentic_loop

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"testing"

	"google.golang.org/genai"
)

// ------------------------------------------------------------ AssembleStream

func chunkText(text string) *genai.GenerateContentResponse {
	return &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{{Text: text}}},
		}},
	}
}

func seqOf(chunks ...*genai.GenerateContentResponse) iter.Seq2[*genai.GenerateContentResponse, error] {
	return func(yield func(*genai.GenerateContentResponse, error) bool) {
		for _, c := range chunks {
			if !yield(c, nil) {
				return
			}
		}
	}
}

func TestAssembleStreamKeepsEveryDeltaInOrder(t *testing.T) {
	deltas := []string{"The ", "quick ", "brown ", "fox."}
	chunks := make([]*genai.GenerateContentResponse, 0, len(deltas)+1)
	for _, d := range deltas {
		chunks = append(chunks, chunkText(d))
	}
	// The closing chunk carries usage and the finish reason, as Gemini's does.
	closing := chunkText("")
	closing.Candidates[0].FinishReason = genai.FinishReasonStop
	closing.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount: 10, CandidatesTokenCount: 4, TotalTokenCount: 14,
	}
	chunks = append(chunks, closing)

	var got []string
	resp, err := AssembleStream(seqOf(chunks...), func(d string) { got = append(got, d) })
	if err != nil {
		t.Fatalf("AssembleStream: %v", err)
	}
	if fmt.Sprint(got) != fmt.Sprint(deltas) {
		t.Errorf("deltas = %v, want %v (order and count intact)", got, deltas)
	}
	if text := responseText(resp); text != "The quick brown fox." {
		t.Errorf("assembled text = %q", text)
	}
	if resp.FinishReason != string(genai.FinishReasonStop) {
		t.Errorf("finish reason = %q", resp.FinishReason)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 14 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestAssembleStreamCarriesFunctionCalls(t *testing.T) {
	call := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{{
				FunctionCall: &genai.FunctionCall{Name: "get_board", Args: map[string]any{}},
			}}},
		}},
	}
	resp, err := AssembleStream(seqOf(chunkText("Let me look. "), call), nil)
	if err != nil {
		t.Fatalf("AssembleStream: %v", err)
	}
	if len(resp.FunctionCalls) != 1 || resp.FunctionCalls[0].Name != "get_board" {
		t.Fatalf("function calls = %+v", resp.FunctionCalls)
	}
	// The assembled content holds text then the call, the shape the history
	// records.
	if len(resp.Content.Parts) != 2 || resp.Content.Parts[1].FunctionCall == nil {
		t.Errorf("assembled parts = %+v", resp.Content.Parts)
	}
}

func TestAssembleStreamReportsAMidStreamError(t *testing.T) {
	seq := func(yield func(*genai.GenerateContentResponse, error) bool) {
		if !yield(chunkText("Half a "), nil) {
			return
		}
		yield(nil, fmt.Errorf("stream cut"))
	}
	if _, err := AssembleStream(seq, nil); err == nil {
		t.Fatal("a mid-stream error was swallowed")
	}
}

// ------------------------------------------------------- SingleDeltaFallback

func TestSingleDeltaFallbackEmitsTheWholeTextOnce(t *testing.T) {
	mock := &mockLLMClient{responses: []*GenerateResponse{{
		Content: genai.NewContentFromText("All at once.", genai.RoleModel),
	}}}
	var got []string
	resp, err := SingleDeltaFallback(context.Background(), mock, nil, nil, func(d string) { got = append(got, d) })
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if len(got) != 1 || got[0] != "All at once." {
		t.Errorf("deltas = %v, want exactly one carrying the whole text", got)
	}
	if responseText(resp) != "All at once." {
		t.Errorf("assembled = %q", responseText(resp))
	}
}

// ------------------------------------------------------------- the loop

// streamScript is a streaming client scripted per turn: each entry's deltas
// are sinked one by one, then its response returned.
type streamScript struct {
	turns []scriptedTurn
	next  int
}

type scriptedTurn struct {
	deltas []string
	calls  []*genai.FunctionCall
}

func (s *streamScript) response(turn scriptedTurn) *GenerateResponse {
	parts := []*genai.Part{}
	if text := strings.Join(turn.deltas, ""); text != "" {
		parts = append(parts, &genai.Part{Text: text})
	}
	for _, c := range turn.calls {
		parts = append(parts, &genai.Part{FunctionCall: c})
	}
	return &GenerateResponse{Content: &genai.Content{Role: string(genai.RoleModel), Parts: parts}}
}

func (s *streamScript) take() scriptedTurn {
	if s.next >= len(s.turns) {
		return scriptedTurn{deltas: []string{"(script exhausted)"}}
	}
	turn := s.turns[s.next]
	s.next++
	return turn
}

func (s *streamScript) GenerateContent(context.Context, []*genai.Content, *GenerateContentConfig) (*GenerateResponse, error) {
	return s.response(s.take()), nil
}

func (s *streamScript) GenerateContentStreaming(_ context.Context, _ []*genai.Content, _ *GenerateContentConfig, sink func(string)) (*GenerateResponse, error) {
	turn := s.take()
	for _, d := range turn.deltas {
		if sink != nil {
			sink(d)
		}
	}
	return s.response(turn), nil
}

func (s *streamScript) SetModel(string)  {}
func (s *streamScript) GetModel() string { return "stream-script" }
func (s *streamScript) Close() error     { return nil }

func echoTool() Tool {
	return NewFuncTool("echo", "echoes", nil,
		func(ctx context.Context, input map[string]any) (*ToolResult, error) {
			return NewSuccessResult("echoed"), nil
		})
}

func script() []scriptedTurn {
	return []scriptedTurn{
		{deltas: []string{"Let me ", "check. "},
			calls: []*genai.FunctionCall{{Name: "echo", Args: map[string]any{}}}},
		{deltas: []string{"The board ", "is ", "quiet."}},
	}
}

// TestStreamedRunEmitsDeltasAroundTheToolCall is the rung's ordering claim:
// deltas, then the tool pair, then the continuation's deltas — and the
// concatenation of every delta equals the final text.
func TestStreamedRunEmitsDeltasAroundTheToolCall(t *testing.T) {
	loop, err := New(context.Background(), "", "stream-script",
		WithLLMClient(&streamScript{turns: script()}),
		WithTools(echoTool()),
		WithTokenStreaming(),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var sequence []string
	var deltas []string
	var done *DoneEvent
	for event := range loop.Run(context.Background(), []Message{NewUserMessage("hello")}) {
		switch e := event.(type) {
		case TextDeltaEvent:
			sequence = append(sequence, "delta")
			deltas = append(deltas, e.Text)
		case ToolCallEvent:
			sequence = append(sequence, "tool_call:"+e.Name)
		case ToolResultEvent:
			sequence = append(sequence, "tool_result:"+e.Name)
		case DoneEvent:
			done = &e
		}
	}
	if done == nil {
		t.Fatal("no DoneEvent")
	}
	want := []string{
		"delta", "delta", "tool_call:echo", "tool_result:echo",
		"delta", "delta", "delta",
	}
	if fmt.Sprint(sequence) != fmt.Sprint(want) {
		t.Errorf("event order = %v, want %v", sequence, want)
	}
	if joined := strings.Join(deltas, ""); joined != done.FinalText {
		t.Errorf("delta concatenation %q != final text %q", joined, done.FinalText)
	}
}

// TestStreamedHistoryEqualsTheBlockingPaths: the same scripted exchange run
// both ways serializes to byte-identical history.
func TestStreamedHistoryEqualsTheBlockingPaths(t *testing.T) {
	run := func(streaming bool) []byte {
		opts := []Option{WithLLMClient(&streamScript{turns: script()}), WithTools(echoTool())}
		if streaming {
			opts = append(opts, WithTokenStreaming())
		}
		loop, err := New(context.Background(), "", "stream-script", opts...)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		done, err := loop.RunSync(context.Background(), []Message{NewUserMessage("hello")})
		if err != nil {
			t.Fatalf("RunSync: %v", err)
		}
		raw, err := SerializeHistory(done.History)
		if err != nil {
			t.Fatalf("SerializeHistory: %v", err)
		}
		return raw
	}
	streamed, blocking := run(true), run(false)
	if string(streamed) != string(blocking) {
		t.Errorf("streamed history differs from the blocking path's:\n%s\nvs\n%s", streamed, blocking)
	}
}

// TestBlockingRunStillEmitsOneDeltaPerTurn pins the compatibility claim: with
// streaming off, nothing about the event stream changes.
func TestBlockingRunStillEmitsOneDeltaPerTurn(t *testing.T) {
	loop, err := New(context.Background(), "", "stream-script",
		WithLLMClient(&streamScript{turns: script()}), WithTools(echoTool()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	count := 0
	var done *DoneEvent
	for event := range loop.Run(context.Background(), []Message{NewUserMessage("hello")}) {
		switch e := event.(type) {
		case TextDeltaEvent:
			count++
		case DoneEvent:
			done = &e
		}
	}
	if count != 2 {
		t.Errorf("blocking run emitted %d deltas, want one per turn (2)", count)
	}
	if done == nil || done.FinalText != "Let me check. The board is quiet." {
		t.Errorf("final = %+v", done)
	}
}
