package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestControlServerDispatchesIntentAndUsesPrivateSocket(t *testing.T) {
	dir := socketSafeDir(t)
	getenv := envMap(map[string]string{"XDG_RUNTIME_DIR": dir})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := StartControlServer(ctx, getenv)
	if err != nil {
		t.Fatalf("StartControlServer returned error: %v", err)
	}
	defer server.Close()

	info, err := os.Stat(ControlSocketPath(getenv))
	if err != nil {
		t.Fatalf("stat control socket: %v", err)
	}
	// Windows has no POSIX permission bits; the socket lives in a per-user
	// temp directory there instead.
	if runtime.GOOS != "windows" {
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("control socket permissions = %o, want 600", got)
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- SendAssistIntent(context.Background(), getenv, AssistIntentEnglishSummary)
	}()
	select {
	case dispatch := <-server.Requests():
		if dispatch.Intent != AssistIntentEnglishSummary {
			t.Fatalf("dispatched intent = %q, want %q", dispatch.Intent, AssistIntentEnglishSummary)
		}
		dispatch.respond(nil)
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for dispatched intent")
	}
	if err := <-done; err != nil {
		t.Fatalf("SendAssistIntent returned error: %v", err)
	}
}

func TestControlServerRejectsUnknownIntent(t *testing.T) {
	dir := socketSafeDir(t)
	getenv := envMap(map[string]string{"XDG_RUNTIME_DIR": dir})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := StartControlServer(ctx, getenv)
	if err != nil {
		t.Fatalf("StartControlServer returned error: %v", err)
	}
	defer server.Close()

	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: ControlSocketPath(getenv), Net: "unix"})
	if err != nil {
		t.Fatalf("dial control socket: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if err := json.NewEncoder(conn).Encode(map[string]string{"intent": "invent"}); err != nil {
		t.Fatalf("encode request: %v", err)
	}
	if err := conn.CloseWrite(); err != nil {
		t.Fatalf("close request writer: %v", err)
	}
	var response controlResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.OK || !strings.Contains(response.Error, "unknown assist intent") {
		t.Fatalf("response = %#v, want unknown-intent rejection", response)
	}
}

func TestDecodeControlRequestRequiresExplicitIntent(t *testing.T) {
	for _, raw := range []string{`{}`, `{"intent":null}`, `{"intent":""}`} {
		_, err := decodeControlRequest(strings.NewReader(raw))
		if err == nil || !strings.Contains(err.Error(), "intent is required") {
			t.Fatalf("decodeControlRequest(%s) error = %v", raw, err)
		}
	}
}

func TestControlServerCloseRemovesSocket(t *testing.T) {
	dir := socketSafeDir(t)
	getenv := envMap(map[string]string{"XDG_RUNTIME_DIR": dir})
	server, err := StartControlServer(context.Background(), getenv)
	if err != nil {
		t.Fatalf("StartControlServer returned error: %v", err)
	}
	path := ControlSocketPath(getenv)
	if err := server.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("control socket still exists after close: %v", err)
	}
}

func TestControlServerCanRestartAtSamePath(t *testing.T) {
	dir := socketSafeDir(t)
	getenv := envMap(map[string]string{"XDG_RUNTIME_DIR": dir})
	first, err := StartControlServer(context.Background(), getenv)
	if err != nil {
		t.Fatalf("start first server: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first server: %v", err)
	}
	second, err := StartControlServer(context.Background(), getenv)
	if err != nil {
		t.Fatalf("start replacement server: %v", err)
	}
	defer second.Close()
	if _, err := os.Stat(ControlSocketPath(getenv)); err != nil {
		t.Fatalf("replacement socket unavailable: %v", err)
	}
}

func TestSendAssistIntentRequiresDaemon(t *testing.T) {
	dir := socketSafeDir(t)
	err := SendAssistIntent(context.Background(), envMap(map[string]string{"XDG_RUNTIME_DIR": dir}), AssistIntentFactCheck)
	if err == nil || !strings.Contains(err.Error(), "start it with `screen-agent daemon`") {
		t.Fatalf("error = %v, want daemon startup guidance", err)
	}
}

func TestControlServerReturnsDispatchRejection(t *testing.T) {
	dir := socketSafeDir(t)
	getenv := envMap(map[string]string{"XDG_RUNTIME_DIR": dir})
	server, err := StartControlServer(context.Background(), getenv)
	if err != nil {
		t.Fatalf("StartControlServer returned error: %v", err)
	}
	defer server.Close()

	done := make(chan error, 1)
	go func() {
		done <- SendAssistIntent(context.Background(), getenv, AssistIntentFactCheck)
	}()
	dispatch := <-server.Requests()
	dispatch.respond(errors.New("fact-check is unavailable"))
	err = <-done
	if err == nil || !strings.Contains(err.Error(), "fact-check is unavailable") {
		t.Fatalf("error = %v, want dispatch rejection", err)
	}
}

func TestControlServerRejectsRequestAfterCancellation(t *testing.T) {
	dir := socketSafeDir(t)
	getenv := envMap(map[string]string{"XDG_RUNTIME_DIR": dir})
	ctx, cancel := context.WithCancel(context.Background())
	server, err := StartControlServer(ctx, getenv)
	if err != nil {
		t.Fatalf("StartControlServer returned error: %v", err)
	}
	defer server.Close()
	cancel()

	err = SendAssistIntent(context.Background(), getenv, AssistIntentEnglishSummary)
	if err == nil || !strings.Contains(err.Error(), "daemon is stopping") {
		t.Fatalf("error = %v, want stopping rejection", err)
	}
}
