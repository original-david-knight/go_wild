package main

import (
	"bytes"
	"context"
	"testing"
)

func TestTriggerCommandRemoved(t *testing.T) {
	var out, errOut bytes.Buffer
	code := realMain([]string{"trigger"}, envMap(map[string]string{}), &out, &errOut)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !bytes.Contains(errOut.Bytes(), []byte(`unknown command "trigger"`)) {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestAssistCommandsDispatchToDaemon(t *testing.T) {
	dir := socketSafeDir(t)
	getenv := envMap(map[string]string{"XDG_RUNTIME_DIR": dir})
	server, err := StartControlServer(context.Background(), getenv)
	if err != nil {
		t.Fatalf("StartControlServer returned error: %v", err)
	}
	defer server.Close()

	tests := []struct {
		command string
		intent  AssistIntent
	}{
		{command: "assist", intent: AssistIntentAuto},
		{command: "fact-check", intent: AssistIntentFactCheck},
		{command: "summarize", intent: AssistIntentEnglishSummary},
	}
	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			var out, errOut bytes.Buffer
			done := make(chan int, 1)
			go func() {
				done <- realMain([]string{tc.command}, getenv, &out, &errOut)
			}()
			dispatch := <-server.Requests()
			if dispatch.Intent != tc.intent {
				t.Fatalf("dispatched intent = %q, want %q", dispatch.Intent, tc.intent)
			}
			dispatch.respond(nil)
			if code := <-done; code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
			}
		})
	}
}

func TestAssistCommandsRejectArguments(t *testing.T) {
	var out, errOut bytes.Buffer
	code := realMain([]string{"summarize", "extra"}, envMap(map[string]string{}), &out, &errOut)
	if code != 2 || !bytes.Contains(errOut.Bytes(), []byte("unexpected summarize arguments")) {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
}
