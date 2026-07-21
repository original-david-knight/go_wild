package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunDaemonWritesAndRemovesPIDFile(t *testing.T) {
	dir := socketSafeDir(t)
	cfg := DefaultConfig()
	cfg.AgentProvider = "fake"
	cfg.AgentPromptPath = filepath.Join(dir, "prompt.md")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunDaemon(ctx, cfg, envMap(map[string]string{"XDG_RUNTIME_DIR": dir}), &bytes.Buffer{}, &bytes.Buffer{})
	}()

	pidPath := PIDFilePath(envMap(map[string]string{"XDG_RUNTIME_DIR": dir}))
	socketPath := ControlSocketPath(envMap(map[string]string{"XDG_RUNTIME_DIR": dir}))
	waitForFile(t, pidPath)
	waitForFile(t, socketPath)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunDaemon returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("RunDaemon did not exit after context cancellation")
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file still exists after daemon exit: %v", err)
	}
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("control socket still exists after daemon exit: %v", err)
	}
}

func TestRunDaemonRejectsFactCheckWithoutGemini(t *testing.T) {
	dir := socketSafeDir(t)
	getenv := envMap(map[string]string{"XDG_RUNTIME_DIR": dir})
	cfg := DefaultConfig()
	cfg.AgentProvider = "fake"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunDaemon(ctx, cfg, getenv, &bytes.Buffer{}, &bytes.Buffer{})
	}()
	waitForFile(t, ControlSocketPath(getenv))

	err := SendAssistIntent(context.Background(), getenv, AssistIntentFactCheck)
	if err == nil || !strings.Contains(err.Error(), `requires agent_provider "gemini"`) {
		t.Fatalf("error = %v, want Gemini requirement", err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunDaemon returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("RunDaemon did not stop")
	}
}

func TestHotkeyJobControllerReplacesActiveJob(t *testing.T) {
	var errOut bytes.Buffer
	controller := &hotkeyJobController{Err: &errOut}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()

	firstStarted := make(chan struct{})
	firstCanceled := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})
	var calls int32

	run := func(ctx context.Context) error {
		call := atomic.AddInt32(&calls, 1)
		switch call {
		case 1:
			close(firstStarted)
			<-ctx.Done()
			close(firstCanceled)
			return ctx.Err()
		case 2:
			close(secondStarted)
			select {
			case <-releaseSecond:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		default:
			t.Errorf("unexpected call %d", call)
			return nil
		}
	}

	controller.Start(parent, run)
	waitForClosed(t, firstStarted)
	controller.Start(parent, run)
	waitForClosed(t, firstCanceled)
	waitForClosed(t, secondStarted)

	close(releaseSecond)
	controller.wg.Wait()
	if got := errOut.String(); got != "" {
		t.Fatalf("expected abandoned job error to be suppressed, got %q", got)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func waitForClosed(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for channel")
	}
}
