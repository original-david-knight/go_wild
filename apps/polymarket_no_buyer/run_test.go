package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// parseEvents splits newline-delimited JSON log output into decoded objects.
func parseEvents(t *testing.T, out string) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("non-JSON log line %q: %v", line, err)
		}
		events = append(events, obj)
	}
	return events
}

func eventsNamed(events []map[string]any, name string) []map[string]any {
	var out []map[string]any
	for _, e := range events {
		if e["event"] == name {
			out = append(out, e)
		}
	}
	return out
}

// seqRunID returns a deterministic, monotonically increasing run-ID generator.
func seqRunID() func() string {
	var mu sync.Mutex
	n := 0
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		n++
		return fmt.Sprintf("run_%06d", n)
	}
}

func newTestApp(cfg *Config) *App {
	return &App{
		cfg:      cfg,
		clients:  nil, // no trading at this rung; runOnce does not touch clients
		newRunID: seqRunID(),
		now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}
}

func TestRunMode(t *testing.T) {
	t.Run("once runs exactly once and exits", func(t *testing.T) {
		cfg, err := LoadConfig([]string{"--once"}, emptyEnv)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		var buf bytes.Buffer
		app := newTestApp(cfg)
		if err := app.Run(context.Background(), &buf); err != nil {
			t.Fatalf("Run: %v", err)
		}
		starts := eventsNamed(parseEvents(t, buf.String()), "run_start")
		if len(starts) != 1 {
			t.Fatalf("run_start count = %d, want 1", len(starts))
		}
		if starts[0]["mode"] != "once" || starts[0]["dry_run"] != false {
			t.Errorf("run_start mode/dry_run = %v/%v, want once/false", starts[0]["mode"], starts[0]["dry_run"])
		}
		if len(eventsNamed(parseEvents(t, buf.String()), "run_done")) != 1 {
			t.Errorf("expected one run_done event")
		}
	})
}

func TestSchedule(t *testing.T) {
	cfg, err := LoadConfig([]string{"--schedule", "--interval", "5ms"}, emptyEnv)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Mode != ModeSchedule {
		t.Fatalf("mode = %q, want schedule", cfg.Mode)
	}

	var buf bytes.Buffer
	app := newTestApp(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after enough wall-clock for several ticks (immediate + ~5/interval).
	go func() {
		time.Sleep(40 * time.Millisecond)
		cancel()
	}()
	if err := app.Run(ctx, &buf); err != nil {
		t.Fatalf("Run(schedule): %v", err)
	}

	starts := eventsNamed(parseEvents(t, buf.String()), "run_start")
	if len(starts) < 3 {
		t.Fatalf("run_start count = %d, want >= 3 (loop should tick on its interval)", len(starts))
	}

	ids := make([]string, 0, len(starts))
	seen := map[string]bool{}
	for _, s := range starts {
		id, _ := s["run_id"].(string)
		if seen[id] {
			t.Fatalf("duplicate run_id %q across ticks", id)
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("run_ids are not monotonically increasing: %v", ids)
	}
}

func TestDryRun(t *testing.T) {
	cfg, err := LoadConfig([]string{"--once", "--dry-run"}, emptyEnv)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.DryRun {
		t.Fatalf("dry_run = false, want true")
	}
	var buf bytes.Buffer
	app := newTestApp(cfg)
	if err := app.Run(context.Background(), &buf); err != nil {
		t.Fatalf("Run: %v", err)
	}
	starts := eventsNamed(parseEvents(t, buf.String()), "run_start")
	if len(starts) != 1 || starts[0]["dry_run"] != true {
		t.Fatalf("expected one run_start with dry_run=true, got %v", starts)
	}
}

func TestFlagPrecedence(t *testing.T) {
	env := mapEnv(map[string]string{
		envMode:     "schedule",
		envInterval: "2s",
		envDryRun:   "true",
	})

	// Env-only: mode/interval/dry_run come from env.
	cfg, err := LoadConfig(nil, env)
	if err != nil {
		t.Fatalf("LoadConfig(env): %v", err)
	}
	if cfg.Mode != ModeSchedule || humanDuration(cfg.Interval) != "2s" || !cfg.DryRun {
		t.Errorf("env config = mode %q interval %s dry_run %v, want schedule/2s/true", cfg.Mode, humanDuration(cfg.Interval), cfg.DryRun)
	}

	// Flag overrides env: --interval 5s wins over env 2s.
	cfg2, err := LoadConfig([]string{"--interval", "5s"}, env)
	if err != nil {
		t.Fatalf("LoadConfig(flag): %v", err)
	}
	if humanDuration(cfg2.Interval) != "5s" {
		t.Errorf("interval = %s, want 5s (flag over env)", humanDuration(cfg2.Interval))
	}

	// Contradictory flags fail loudly.
	if _, err := LoadConfig([]string{"--once", "--schedule"}, emptyEnv); err == nil {
		t.Errorf("expected error for --once --schedule")
	}
}
