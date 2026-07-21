package main

import (
	"errors"
	"sync"
	"testing"
)

type fakeLineReader struct {
	mu        sync.Mutex
	callCount int
	callCh    chan struct{}
	resultCh  chan fakeReadResult
}

type fakeReadResult struct {
	line string
	err  error
}

func newFakeLineReader() *fakeLineReader {
	return &fakeLineReader{
		callCh:   make(chan struct{}, 4),
		resultCh: make(chan fakeReadResult, 4),
	}
}

func (r *fakeLineReader) Readline() (string, error) {
	r.mu.Lock()
	r.callCount++
	r.mu.Unlock()
	r.callCh <- struct{}{}

	result := <-r.resultCh
	return result.line, result.err
}

func (r *fakeLineReader) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.callCount
}

func TestAsyncInputReaderStartIsSingleFlight(t *testing.T) {
	t.Parallel()

	reader := newFakeLineReader()
	inputCh := make(chan inputResult, 2)

	var promptMu sync.Mutex
	promptCount := 0
	async := newAsyncInputReader(reader, func() {
		promptMu.Lock()
		promptCount++
		promptMu.Unlock()
	}, inputCh)

	async.Start()
	async.Start()

	<-reader.callCh

	if got := reader.Calls(); got != 1 {
		t.Fatalf("expected one active readline call, got %d", got)
	}

	promptMu.Lock()
	if promptCount != 1 {
		t.Fatalf("expected prompt to be emitted once, got %d", promptCount)
	}
	promptMu.Unlock()

	reader.resultCh <- fakeReadResult{line: "  ping  "}

	result := <-inputCh
	if result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	if result.line != "ping" {
		t.Fatalf("expected trimmed line %q, got %q", "ping", result.line)
	}

	async.Start()
	<-reader.callCh
	if got := reader.Calls(); got != 2 {
		t.Fatalf("expected readline to restart after completion, got %d calls", got)
	}

	reader.resultCh <- fakeReadResult{err: errors.New("boom")}
	result = <-inputCh
	if result.err == nil || result.err.Error() != "boom" {
		t.Fatalf("expected propagated error, got %v", result.err)
	}
}
