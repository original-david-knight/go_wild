package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// newRunID returns a collision-resistant run identifier that is also
// monotonically increasing across successive calls. A fixed-width, zero-padded
// UnixNano prefix guarantees lexicographic ordering matches time order, while a
// random suffix guarantees distinctness even for calls within the same
// nanosecond.
func newRunID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("run_%020d", time.Now().UTC().UnixNano())
	}
	return fmt.Sprintf("run_%020d_%s", time.Now().UTC().UnixNano(), hex.EncodeToString(buf[:]))
}

// Logger records logical run events. In its default (structured) mode it writes
// one newline-delimited JSON object per event — used by tests and any machine
// consumer. In human mode it renders concise, readable text instead: only the
// actions the app takes, plus skipped markets when verbose is set.
type Logger struct {
	mu      sync.Mutex
	w       io.Writer
	runID   string
	now     func() time.Time
	human   bool
	verbose bool
}

// NewLogger creates a Logger that writes newline-delimited JSON events to w.
func NewLogger(w io.Writer, runID string) *Logger {
	return &Logger{w: w, runID: runID, now: func() time.Time { return time.Now().UTC() }}
}

// NewHumanLogger creates a Logger that renders concise human-readable text to w.
// When verbose is false only actions are printed; when true, skipped markets and
// their reasons are printed too.
func NewHumanLogger(w io.Writer, runID string, verbose bool) *Logger {
	return &Logger{w: w, runID: runID, now: func() time.Time { return time.Now().UTC() }, human: true, verbose: verbose}
}

// RunID returns the run identifier this logger stamps onto every event.
func (l *Logger) RunID() string { return l.runID }

// Event records a single logical event. In human mode it renders readable text
// (skips non-action events unless verbose); in structured mode it emits JSON.
func (l *Logger) Event(event string, fields map[string]any) {
	if l.human {
		line, show := renderHuman(l.runID, event, fields, l.verbose)
		if !show {
			return
		}
		l.mu.Lock()
		defer l.mu.Unlock()
		fmt.Fprintf(l.w, "%s\n", line)
		return
	}

	obj := make(map[string]any, len(fields)+3)
	for k, v := range fields {
		obj[k] = v
	}
	obj["run_id"] = l.runID
	obj["event"] = event
	obj["ts"] = l.now().Format(time.RFC3339Nano)

	data, err := json.Marshal(obj)
	if err != nil {
		// Marshalling should never fail for these maps; degrade to a minimal line.
		data = []byte(fmt.Sprintf(`{"run_id":%q,"event":%q,"log_error":%q}`, l.runID, event, err.Error()))
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, "%s\n", data)
}

// runInitFields builds the run-init event payload: run ID, mode, dry-run
// status, and the resolved effective config values.
func runInitFields(cfg *Config) map[string]any {
	return cfg.fields()
}
