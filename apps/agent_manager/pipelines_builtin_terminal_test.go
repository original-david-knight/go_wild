package main

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	data "github.com/original-david-knight/go_wild/agent_data"
)

func TestBuiltinTerminalHubReplayAndBroadcast(t *testing.T) {
	hub := newBuiltinTerminalHub()
	hub.PublishText("first line\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.ServeWS(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial failed: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var status WSMessage
	if err := conn.ReadJSON(&status); err != nil {
		t.Fatalf("reading initial status failed: %v", err)
	}
	if status.Type != "status" || status.Status != "running" {
		t.Fatalf("unexpected initial status message: %#v", status)
	}

	var replay WSMessage
	if err := conn.ReadJSON(&replay); err != nil {
		t.Fatalf("reading replay message failed: %v", err)
	}
	replayText, err := base64.StdEncoding.DecodeString(replay.Data)
	if err != nil {
		t.Fatalf("decoding replay payload failed: %v", err)
	}
	if got := string(replayText); got != "first line\n" {
		t.Fatalf("unexpected replay payload %q", got)
	}

	hub.PublishText("second line\n")

	var live WSMessage
	if err := conn.ReadJSON(&live); err != nil {
		t.Fatalf("reading live message failed: %v", err)
	}
	liveText, err := base64.StdEncoding.DecodeString(live.Data)
	if err != nil {
		t.Fatalf("decoding live payload failed: %v", err)
	}
	if got := string(liveText); got != "second line\n" {
		t.Fatalf("unexpected live payload %q", got)
	}
}

func TestExecuteBuiltinStepPublishesBuiltinTerminalIO(t *testing.T) {
	ctx := context.Background()
	db := setupManagerTestDB(t)
	svc := NewAgentService(db)
	engine := NewPipelineEngine(db, svc)

	const builtinMethod = "builtin_test_terminal_io"
	builtinPipelineMethodHandlers[builtinMethod] = func(_ context.Context, _ *PipelineEngine, _ *data.PipelineRun, _ PipelineStep, params map[string]any) (map[string]any, error) {
		return map[string]any{
			"echo": params["foo"],
			"ok":   true,
		}, nil
	}
	defer delete(builtinPipelineMethodHandlers, builtinMethod)

	pipeline := Pipeline{
		ID:   "builtin_terminal_test",
		Name: "Builtin Terminal Test",
		Steps: []PipelineStep{
			{
				Runner:     pipelineStepRunnerBuiltin,
				OnMethod:   "seed",
				NextMethod: builtinMethod,
			},
		},
	}
	engine.upsertPipelineInMemory(pipeline)

	run := &data.PipelineRun{
		ID:         uuid.New().String(),
		PipelineID: pipeline.ID,
		Status:     "running",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := data.NewAgentService(db, "system").CreatePipelineRun(ctx, run); err != nil {
		t.Fatalf("CreatePipelineRun failed: %v", err)
	}

	ok := engine.executeBuiltinStep(ctx, data.NewAgentService(db, "system"), run, pipeline.Steps[0], 0, nil, map[string]any{"foo": "bar"})
	if !ok {
		t.Fatal("executeBuiltinStep returned false")
	}

	messages := engine.getBuiltinTerminalHub().Snapshot()
	if len(messages) < 2 {
		t.Fatalf("expected at least 2 builtin terminal messages, got %d", len(messages))
	}

	requestPayload, err := base64.StdEncoding.DecodeString(messages[0].Data)
	if err != nil {
		t.Fatalf("decoding request payload failed: %v", err)
	}
	requestText := string(requestPayload)
	if !strings.Contains(requestText, `"event": "request"`) {
		t.Fatalf("expected request event in payload, got %s", requestText)
	}
	if !strings.Contains(requestText, `"foo": "bar"`) {
		t.Fatalf("expected params in request payload, got %s", requestText)
	}

	resultPayload, err := base64.StdEncoding.DecodeString(messages[1].Data)
	if err != nil {
		t.Fatalf("decoding result payload failed: %v", err)
	}
	resultText := string(resultPayload)
	if !strings.Contains(resultText, `"event": "result"`) {
		t.Fatalf("expected result event in payload, got %s", resultText)
	}
	if !strings.Contains(resultText, `"ok": true`) {
		t.Fatalf("expected result body in payload, got %s", resultText)
	}
	if !strings.Contains(resultText, `"status": "succeeded"`) {
		t.Fatalf("expected succeeded status in payload, got %s", resultText)
	}
}
