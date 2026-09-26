package gowild_agentic_loop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/genai"
)

// step is one scripted generation: deltas are streamed before the outcome,
// and hook (if set) runs with the call's context first.
type step struct {
	resp   *GenerateResponse
	err    error
	deltas []string
	hook   func(ctx context.Context)
}

// scriptedClient plays its steps in order and records every config it was
// handed, and whether each call carried a deadline.
type scriptedClient struct {
	steps     []step
	calls     int
	configs   []*GenerateContentConfig
	deadlines []bool
	contents  [][]*genai.Content
}

func (s *scriptedClient) next(ctx context.Context, contents []*genai.Content, config *GenerateContentConfig) step {
	s.configs = append(s.configs, config)
	s.contents = append(s.contents, append([]*genai.Content(nil), contents...))
	_, hasDeadline := ctx.Deadline()
	s.deadlines = append(s.deadlines, hasDeadline)
	if s.calls >= len(s.steps) {
		s.calls++
		return step{err: errors.New("script exhausted")}
	}
	st := s.steps[s.calls]
	s.calls++
	if st.hook != nil {
		st.hook(ctx)
	}
	return st
}

func (s *scriptedClient) GenerateContent(ctx context.Context, contents []*genai.Content, config *GenerateContentConfig) (*GenerateResponse, error) {
	st := s.next(ctx, contents, config)
	return st.resp, st.err
}

func (s *scriptedClient) GenerateContentStreaming(ctx context.Context, contents []*genai.Content, config *GenerateContentConfig, sink func(string)) (*GenerateResponse, error) {
	st := s.next(ctx, contents, config)
	for _, d := range st.deltas {
		sink(d)
	}
	return st.resp, st.err
}

func (s *scriptedClient) SetModel(string)  {}
func (s *scriptedClient) GetModel() string { return "scripted" }
func (s *scriptedClient) Close() error     { return nil }

func textResp(text string, prompt int) *GenerateResponse {
	return &GenerateResponse{
		Content:      &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{{Text: text}}},
		FinishReason: "STOP",
		Usage:        &ModelUsage{PromptTokens: prompt, CompletionTokens: 1, TotalTokens: prompt + 1},
	}
}

func callResp(name string, prompt int) *GenerateResponse {
	return &GenerateResponse{
		Content: &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "id-" + name, Name: name, Args: map[string]any{}}},
		}},
		Usage: &ModelUsage{PromptTokens: prompt, CompletionTokens: 2, TotalTokens: prompt + 2},
	}
}

// recordedBackoff replaces the loop's timer: each requested delay is kept and
// the channel fires at once.
type recordedBackoff struct{ delays []time.Duration }

func (r *recordedBackoff) after(d time.Duration) <-chan time.Time {
	r.delays = append(r.delays, d)
	ch := make(chan time.Time, 1)
	ch <- time.Time{}
	return ch
}

func newScriptedLoop(t *testing.T, client LLMClient, opts ...Option) (*AgenticLoop, *recordedBackoff) {
	t.Helper()
	loop, err := New(context.Background(), "", "", append([]Option{WithLLMClient(client)}, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := &recordedBackoff{}
	loop.after = rec.after
	return loop, rec
}

// collect drains a run into its events.
func collect(loop *AgenticLoop, ctx context.Context) []Event {
	var events []Event
	for ev := range loop.Run(ctx, []Message{NewUserMessage("go")}) {
		events = append(events, ev)
	}
	return events
}

func doneOf(t *testing.T, events []Event) DoneEvent {
	t.Helper()
	for _, ev := range events {
		if d, ok := ev.(DoneEvent); ok {
			return d
		}
	}
	t.Fatalf("no DoneEvent in %v", events)
	return DoneEvent{}
}

// assertDelays checks each delay sits within the ±25% jitter of its base.
func assertDelays(t *testing.T, got []time.Duration, bases []time.Duration) {
	t.Helper()
	if len(got) != len(bases) {
		t.Fatalf("delays = %v, want %d of them (bases %v)", got, len(bases), bases)
	}
	for i, base := range bases {
		lo, hi := time.Duration(float64(base)*0.75), time.Duration(float64(base)*1.25)
		if got[i] < lo || got[i] > hi {
			t.Errorf("delay %d = %v, want within [%v, %v]", i, got[i], lo, hi)
		}
	}
}

func TestGenerateRetriesServerErrorsWithGrowingBackoff(t *testing.T) {
	client := &scriptedClient{steps: []step{
		{err: errors.New("status 503 UNAVAILABLE")},
		{err: errors.New("connection reset by peer")},
		{resp: textResp("recovered", 5)},
	}}
	loop, rec := newScriptedLoop(t, client)

	done, err := loop.RunSync(context.Background(), []Message{NewUserMessage("go")})
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	if done.FinalText != "recovered" || client.calls != 3 {
		t.Errorf("final = %q after %d calls, want recovered after 3", done.FinalText, client.calls)
	}
	assertDelays(t, rec.delays, []time.Duration{time.Second, 2 * time.Second})
}

func TestGenerateGivesUpAfterMaxServerRetries(t *testing.T) {
	client := &scriptedClient{steps: []step{
		{err: errors.New("500 INTERNAL")}, {err: errors.New("500 INTERNAL")},
		{err: errors.New("500 INTERNAL")}, {err: errors.New("500 INTERNAL")},
	}}
	loop, rec := newScriptedLoop(t, client)

	events := collect(loop, context.Background())
	var errEv *ErrorEvent
	for _, ev := range events {
		if e, ok := ev.(ErrorEvent); ok {
			errEv = &e
		}
	}
	if errEv == nil || errEv.Err.Error() != "generation error: failed after retries: 500 INTERNAL" {
		t.Fatalf("error event = %v", errEv)
	}
	if client.calls != 4 {
		t.Errorf("calls = %d, want 1 + 3 retries", client.calls)
	}
	assertDelays(t, rec.delays, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second})

	done := doneOf(t, events)
	if done.StopReason != "error" || done.TurnCount != 1 || len(done.History) != 1 {
		t.Errorf("done = %+v, want stop reason error on turn 1 with the input history", done)
	}
}

func TestGenerateBacksOffPatientlyOnRateLimits(t *testing.T) {
	var steps []step
	for i := 0; i < 7; i++ {
		steps = append(steps, step{err: errors.New("429 RESOURCE_EXHAUSTED")})
	}
	client := &scriptedClient{steps: steps}
	loop, rec := newScriptedLoop(t, client)

	_, err := loop.RunSync(context.Background(), []Message{NewUserMessage("go")})
	if err == nil || err.Error() != "generation error: failed after retries: 429 RESOURCE_EXHAUSTED" {
		t.Fatalf("err = %v", err)
	}
	if client.calls != 7 {
		t.Errorf("calls = %d, want 1 + 6 rate-limit retries", client.calls)
	}
	assertDelays(t, rec.delays, []time.Duration{
		5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 60 * time.Second, 60 * time.Second,
	})
}

func TestGenerateDoesNotRetryPermanentErrors(t *testing.T) {
	client := &scriptedClient{steps: []step{{err: errors.New("invalid argument: bad schema")}}}
	loop, rec := newScriptedLoop(t, client)

	_, err := loop.RunSync(context.Background(), []Message{NewUserMessage("go")})
	if err == nil || err.Error() != "generation error: invalid argument: bad schema" {
		t.Fatalf("err = %v", err)
	}
	if client.calls != 1 || len(rec.delays) != 0 {
		t.Errorf("calls = %d, delays = %v; want one call, no back-off", client.calls, rec.delays)
	}
}

func TestGenerateStopsWhenTheCallerCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &scriptedClient{steps: []step{{
		err:  errors.New("503 UNAVAILABLE"),
		hook: func(context.Context) { cancel() },
	}}}
	loop, rec := newScriptedLoop(t, client)

	_, err := loop.RunSync(ctx, []Message{NewUserMessage("go")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if client.calls != 1 || len(rec.delays) != 0 {
		t.Errorf("calls = %d, delays = %v; want no retry after cancel", client.calls, rec.delays)
	}
}

func TestGenerateStopsWhenCancelledDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &scriptedClient{steps: []step{{err: errors.New("503 UNAVAILABLE")}}}
	loop, _ := newScriptedLoop(t, client)
	var waited []time.Duration
	loop.after = func(d time.Duration) <-chan time.Time {
		waited = append(waited, d)
		cancel()
		return make(chan time.Time) // never fires
	}

	_, err := loop.RunSync(ctx, []Message{NewUserMessage("go")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if client.calls != 1 || len(waited) != 1 {
		t.Errorf("calls = %d, waits = %v; want one call and one interrupted wait", client.calls, waited)
	}
}

func TestResponseTimeoutBoundsEachCall(t *testing.T) {
	client := &scriptedClient{steps: []step{{resp: textResp("ok", 1)}}}
	loop, _ := newScriptedLoop(t, client, WithResponseTimeout(time.Hour))
	if _, err := loop.RunSync(context.Background(), []Message{NewUserMessage("go")}); err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	if len(client.deadlines) != 1 || !client.deadlines[0] {
		t.Errorf("deadlines = %v, want the call bounded by the response timeout", client.deadlines)
	}
}

func TestStreamingRetriesOnlyBeforeAnyDelta(t *testing.T) {
	t.Run("retries a failure that emitted nothing", func(t *testing.T) {
		client := &scriptedClient{steps: []step{
			{err: errors.New("503 UNAVAILABLE")},
			{err: errors.New("503 UNAVAILABLE")},
			{deltas: []string{"fi", "ne"}, resp: textResp("fine", 3)},
		}}
		loop, rec := newScriptedLoop(t, client, WithTokenStreaming())
		events := collect(loop, context.Background())
		var deltas []string
		for _, ev := range events {
			if d, ok := ev.(TextDeltaEvent); ok {
				deltas = append(deltas, d.Text)
			}
		}
		if strings.Join(deltas, "|") != "fi|ne" {
			t.Errorf("deltas = %q, want each delta once", deltas)
		}
		if got := doneOf(t, events).FinalText; got != "fine" {
			t.Errorf("final = %q", got)
		}
		assertDelays(t, rec.delays, []time.Duration{time.Second, 2 * time.Second})
	})

	t.Run("returns a mid-stream failure without replay", func(t *testing.T) {
		client := &scriptedClient{steps: []step{
			{deltas: []string{"partial "}, err: errors.New("503 UNAVAILABLE")},
		}}
		loop, rec := newScriptedLoop(t, client, WithTokenStreaming())
		_, err := loop.RunSync(context.Background(), []Message{NewUserMessage("go")})
		if err == nil || err.Error() != "generation error: 503 UNAVAILABLE" {
			t.Fatalf("err = %v", err)
		}
		if client.calls != 1 || len(rec.delays) != 0 {
			t.Errorf("calls = %d, delays = %v; want no retry", client.calls, rec.delays)
		}
	})

	t.Run("gives up after max retries", func(t *testing.T) {
		client := &scriptedClient{steps: []step{
			{err: errors.New("e1")}, {err: errors.New("e2")}, {err: errors.New("e3")}, {err: errors.New("e4")},
		}}
		loop, rec := newScriptedLoop(t, client, WithTokenStreaming())
		_, err := loop.RunSync(context.Background(), []Message{NewUserMessage("go")})
		if err == nil || err.Error() != "generation error: e4" {
			t.Fatalf("err = %v, want the last attempt's error", err)
		}
		if client.calls != 4 || len(rec.delays) != 3 {
			t.Errorf("calls = %d, delays = %v", client.calls, rec.delays)
		}
	})

	t.Run("stops when cancelled during back-off", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		client := &scriptedClient{steps: []step{{err: errors.New("503")}}}
		loop, _ := newScriptedLoop(t, client, WithTokenStreaming())
		loop.after = func(time.Duration) <-chan time.Time {
			cancel()
			return make(chan time.Time)
		}
		_, err := loop.RunSync(ctx, []Message{NewUserMessage("go")})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	})
}

func TestGenerationErrorKeepsEarlierTurnsText(t *testing.T) {
	client := &scriptedClient{steps: []step{
		{resp: &GenerateResponse{Content: &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{
			{Text: "Looking. "},
			{FunctionCall: &genai.FunctionCall{ID: "e1", Name: "echo", Args: map[string]any{}}},
		}}}},
		{err: errors.New("permission denied")},
	}}
	loop, _ := newScriptedLoop(t, client, WithTools(echoTool()))
	done := doneOf(t, collect(loop, context.Background()))
	if done.StopReason != "error" || done.FinalText != "Looking. " || done.TurnCount != 2 {
		t.Errorf("done = %+v, want the first turn's text kept on an error in turn 2", done)
	}
	// input, model turn, tool result
	if len(done.History) != 3 || done.History[2].Role != RoleTool {
		t.Errorf("history = %+v", done.History)
	}
}

func TestSilentTurnAfterToolsForcesATextOnlyCall(t *testing.T) {
	client := &scriptedClient{steps: []step{
		{resp: callResp("echo", 10)},
		{resp: &GenerateResponse{Content: &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{{Thought: true, Text: ""}}}, Usage: &ModelUsage{PromptTokens: 20, CompletionTokens: 3, TotalTokens: 23}}},
		{resp: textResp("summary", 30)},
	}}
	loop, _ := newScriptedLoop(t, client, WithTools(echoTool()), WithSystemPrompt("sys"))
	loop.SetThinkingBudget(77)

	events := collect(loop, context.Background())
	done := doneOf(t, events)
	if done.FinalText != "summary" || done.StopReason != "STOP" || done.TurnCount != 2 {
		t.Errorf("done = %+v", done)
	}
	if done.Usage != (ModelUsage{PromptTokens: 30, CompletionTokens: 6, TotalTokens: 31}) {
		t.Errorf("usage = %+v, want latest prompt and summed completion", done.Usage)
	}
	if client.calls != 3 {
		t.Fatalf("calls = %d", client.calls)
	}
	forced := client.configs[2]
	if forced.Tools != nil || forced.SystemInstruction != "sys" || forced.ThinkingBudget != 77 {
		t.Errorf("forced config = %+v, want no tools and the same prompt and budget", forced)
	}
	var deltas []string
	for _, ev := range events {
		if d, ok := ev.(TextDeltaEvent); ok {
			deltas = append(deltas, d.Text)
		}
	}
	if strings.Join(deltas, "") != "summary" {
		t.Errorf("deltas = %q", deltas)
	}
}

func TestSilentTurnRetryFailureStillFinishes(t *testing.T) {
	client := &scriptedClient{steps: []step{
		{resp: callResp("echo", 10)},
		{resp: &GenerateResponse{FinishReason: "SAFETY"}},
		{err: errors.New("bad request")},
	}}
	loop, _ := newScriptedLoop(t, client, WithTools(echoTool()))
	done := doneOf(t, collect(loop, context.Background()))
	if done.FinalText != "" || done.StopReason != "SAFETY" {
		t.Errorf("done = %+v, want the silent turn's finish reason", done)
	}
}

func TestMaxTurnsAsksForAFinalSummaryWithoutTools(t *testing.T) {
	client := &scriptedClient{steps: []step{
		{resp: callResp("echo", 10)},
		{resp: callResp("echo", 20)},
		{resp: textResp("Did two echoes.", 40)},
	}}
	loop, _ := newScriptedLoop(t, client, WithTools(echoTool()), WithMaxTurns(2))

	done := doneOf(t, collect(loop, context.Background()))
	if done.StopReason != "max_turns" || done.TurnCount != 2 || done.FinalText != "Did two echoes." {
		t.Errorf("done = %+v", done)
	}
	if done.Usage != (ModelUsage{PromptTokens: 40, CompletionTokens: 5, TotalTokens: 41}) {
		t.Errorf("usage = %+v", done.Usage)
	}
	final := client.configs[2]
	if final.Tools != nil {
		t.Errorf("final config carried tools")
	}
	sent := client.contents[2]
	notice := sent[len(sent)-1]
	want := "[System: You have reached the maximum number of tool call turns (2). Do NOT call any more tools. Instead, provide a text response summarizing what you accomplished and what remains to be done.]"
	if notice.Role != string(genai.RoleUser) || notice.Parts[0].Text != want {
		t.Errorf("last content sent = %+v, want the turn-limit notice", notice)
	}
	// input, 2x (call, result), notice, summary
	if len(done.History) != 7 {
		t.Errorf("history has %d messages, want 7", len(done.History))
	}
}

func TestMaxTurnsFinalCallFailureKeepsTheRun(t *testing.T) {
	client := &scriptedClient{steps: []step{
		{resp: callResp("echo", 10)},
		{err: errors.New("bad request")},
	}}
	loop, _ := newScriptedLoop(t, client, WithTools(echoTool()), WithMaxTurns(1))
	done := doneOf(t, collect(loop, context.Background()))
	if done.StopReason != "max_turns" || done.FinalText != "" || done.TurnCount != 1 {
		t.Errorf("done = %+v", done)
	}
}

func TestMaxTurnsWithoutToolCallsMakesNoFinalCall(t *testing.T) {
	client := &scriptedClient{}
	loop, _ := newScriptedLoop(t, client, WithMaxTurns(0))
	done := doneOf(t, collect(loop, context.Background()))
	if done.StopReason != "max_turns" || done.TurnCount != 0 || client.calls != 0 {
		t.Errorf("done = %+v after %d calls", done, client.calls)
	}
}

func TestCompactionReplacesHistoryOnceOverThreshold(t *testing.T) {
	big := strings.Repeat("x", 400) // ~100+ tokens once wrapped
	bigTool := NewFuncTool("big", "returns a lot", nil,
		func(context.Context, map[string]any) (*ToolResult, error) { return NewSuccessResult(big), nil })

	var gotTokens []int
	var gotLen []int
	compact := func(history []Message, tokens int) ([]Message, error) {
		gotTokens = append(gotTokens, tokens)
		gotLen = append(gotLen, len(history))
		return []Message{NewUserMessage("summary of earlier work")}, nil
	}
	client := &scriptedClient{steps: []step{
		{resp: callResp("big", 50)},
		{resp: textResp("done", 5)},
	}}
	loop, _ := newScriptedLoop(t, client, WithTools(bigTool), WithCompaction(100, compact))

	events := collect(loop, context.Background())
	var compaction *CompactionEvent
	for _, ev := range events {
		if c, ok := ev.(CompactionEvent); ok {
			compaction = &c
		}
	}
	// 50 prompt tokens + len(`{"result":"x…x"}`)/4 = 50 + 413/4
	wantTokens := 153
	if len(gotTokens) != 1 || gotTokens[0] != wantTokens || gotLen[0] != 3 {
		t.Fatalf("compact called with tokens %v, lens %v; want one call with %d tokens and 3 messages", gotTokens, gotLen, wantTokens)
	}
	if compaction == nil || *compaction != (CompactionEvent{PromptTokensBefore: wantTokens, MessagesCompacted: 2}) {
		t.Errorf("compaction event = %+v", compaction)
	}
	sent := client.contents[1]
	if len(sent) != 1 || sent[0].Parts[0].Text != "summary of earlier work" {
		t.Errorf("second call saw %d contents, want only the compacted summary", len(sent))
	}
}

func TestCompactionFailureOrBelowThresholdKeepsHistory(t *testing.T) {
	for _, tc := range []struct {
		name      string
		threshold int
		fn        CompactFunc
		wantCalls int
	}{
		{"compactor error", 1, func([]Message, int) ([]Message, error) { return nil, errors.New("nope") }, 1},
		{"compactor returns nothing", 1, func([]Message, int) ([]Message, error) { return nil, nil }, 1},
		{"under threshold", 1_000_000, func([]Message, int) ([]Message, error) { return nil, nil }, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			fn := func(h []Message, n int) ([]Message, error) { calls++; return tc.fn(h, n) }
			client := &scriptedClient{steps: []step{{resp: callResp("echo", 10)}, {resp: textResp("ok", 5)}}}
			loop, _ := newScriptedLoop(t, client, WithTools(echoTool()), WithCompaction(tc.threshold, fn))
			events := collect(loop, context.Background())
			for _, ev := range events {
				if _, ok := ev.(CompactionEvent); ok {
					t.Errorf("unexpected CompactionEvent")
				}
			}
			if calls != tc.wantCalls {
				t.Errorf("compactor calls = %d, want %d", calls, tc.wantCalls)
			}
			if n := len(client.contents[1]); n != 3 {
				t.Errorf("second call saw %d contents, want the full 3", n)
			}
		})
	}
}

func TestToolExecutionErrorBecomesAnErrorResult(t *testing.T) {
	failing := NewFuncTool("fail", "fails", nil,
		func(context.Context, map[string]any) (*ToolResult, error) { return nil, fmt.Errorf("disk full") })
	client := &scriptedClient{steps: []step{{resp: callResp("fail", 1)}, {resp: textResp("sorry", 2)}}}
	loop, _ := newScriptedLoop(t, client, WithTools(failing))

	var result *ToolResult
	for _, ev := range collect(loop, context.Background()) {
		if r, ok := ev.(ToolResultEvent); ok {
			result = r.Result
		}
	}
	if result == nil || result.Success || result.Error != "tool execution error: disk full" {
		t.Errorf("tool result = %+v", result)
	}
}

func TestEstimateToolResultTokens(t *testing.T) {
	if got := estimateToolResultTokens(nil); got != 0 {
		t.Errorf("nil = %d", got)
	}
	// {"result":"abcdefgh"} is 21 bytes: 5 tokens at four bytes each.
	if got := estimateToolResultTokens(NewSuccessResult("abcdefgh")); got != 5 {
		t.Errorf("estimate = %d, want 5", got)
	}
}
