package main

import (
	"context"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
)

type toneRunner struct {
	mu    sync.Mutex
	count int
}

func (r *toneRunner) Run(_ context.Context, _ string, _ ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.count++
	return nil, nil
}

func (r *toneRunner) LookPath(name string) (string, error) {
	return "/usr/bin/" + name, nil
}

func (r *toneRunner) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

func TestToneServiceRepeatsUntilStopped(t *testing.T) {
	runner := &toneRunner{}
	cfg := DefaultConfig()
	cfg.ThinkingToneCommand = "tone"
	cfg.ThinkingToneInterval = 20 * time.Millisecond
	stop := ToneService{Config: cfg, Runner: runner}.Start(context.Background())
	time.Sleep(75 * time.Millisecond)
	stop()
	if got := runner.Count(); got < 3 {
		t.Fatalf("tone count = %d, want at least 3", got)
	}
}

func TestResolveNoQuestionSoundCommandUsesDistinctAutoSound(t *testing.T) {
	spec, ok, err := ResolveNoQuestionSoundCommand("auto", &toneRunner{})
	if err != nil {
		t.Fatalf("ResolveNoQuestionSoundCommand returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected auto no-question sound command")
	}
	if runtime.GOOS == "windows" {
		if spec.Name != "ffplay" && spec.Name != "powershell" {
			t.Fatalf("unexpected spec: %#v", spec)
		}
		toneSpec, toneOK := autoToneCommand(&toneRunner{})
		if toneOK && reflect.DeepEqual(spec, toneSpec) {
			t.Fatalf("no-question sound should not reuse the thinking tone: %#v", spec)
		}
		return
	}
	switch spec.Name {
	case "pw-play", "paplay":
		if len(spec.Args) != 1 || spec.Args[0] == "/usr/share/sounds/freedesktop/stereo/bell.oga" {
			t.Fatalf("no-question sound should not reuse thinking bell: %#v", spec)
		}
	case "canberra-gtk-play":
		if len(spec.Args) != 2 || spec.Args[0] != "-i" || spec.Args[1] != "dialog-information" {
			t.Fatalf("unexpected canberra spec: %#v", spec)
		}
	default:
		t.Fatalf("unexpected spec: %#v", spec)
	}
}
