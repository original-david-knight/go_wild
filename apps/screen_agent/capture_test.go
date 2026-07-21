package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	commands         [][]string
	look             map[string]string
	invalidActive    bool
	noFocusedMonitor bool
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	cmd := append([]string{name}, args...)
	r.commands = append(r.commands, cmd)
	switch name {
	case "hyprctl":
		if reflect.DeepEqual(args, []string{"-j", "monitors"}) {
			if r.noFocusedMonitor {
				return []byte(`[]`), nil
			}
			return []byte(`[{"x":10,"y":20,"width":800,"height":600,"focused":true}]`), nil
		}
		if reflect.DeepEqual(args, []string{"-j", "activewindow"}) {
			if r.invalidActive {
				return []byte(`{}`), nil
			}
			return []byte(`{"title":"Practice","class":"browser","at":[1,2],"size":[300,200]}`), nil
		}
	case "grim":
		if len(args) == 0 {
			return nil, nil
		}
		path := args[len(args)-1]
		if err := writeTestPNG(path, 2, 3); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (r *fakeRunner) LookPath(name string) (string, error) {
	if r.look != nil {
		if path, ok := r.look[name]; ok {
			return path, nil
		}
	}
	return "/usr/bin/" + name, nil
}

func TestParseFocusedMonitorGeometry(t *testing.T) {
	g, err := parseFocusedMonitorGeometry([]byte(`[{"x":0,"y":0,"width":1,"height":1},{"x":10,"y":20,"width":800,"height":600,"focused":true}]`))
	if err != nil {
		t.Fatalf("parseFocusedMonitorGeometry returned error: %v", err)
	}
	if g.String() != "10,20 800x600" {
		t.Fatalf("geometry = %s", g.String())
	}
}

func TestParseActiveWindow(t *testing.T) {
	active, err := parseActiveWindow([]byte(`{"title":"Practice","class":"browser","at":[4,5],"size":[640,480]}`))
	if err != nil {
		t.Fatalf("parseActiveWindow returned error: %v", err)
	}
	if active.Title != "Practice" || active.Class != "browser" || active.Geometry.String() != "4,5 640x480" {
		t.Fatalf("unexpected active window: %#v", active)
	}
}

func TestCaptureWindowUsesActiveWindowGeometry(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{}
	cfg := DefaultConfig()
	cfg.AgentPromptPath = filepath.Join(dir, "prompt.md")
	cfg.DebugCaptureDir = filepath.Join(dir, "captures")
	cfg.CaptureTimeout = time.Second
	svc := GrimCaptureService{
		Config: cfg,
		Runner: runner,
		Getenv: envMap(map[string]string{"XDG_RUNTIME_DIR": dir}),
		Now:    func() time.Time { return time.Date(2026, 7, 9, 1, 2, 3, 0, time.UTC) },
	}
	result, err := svc.Capture(context.Background(), CaptureModeWindow, CaptureOptions{Retain: true})
	if err != nil {
		t.Fatalf("Capture returned error: %v", err)
	}
	if result.ActiveTitle != "Practice" || result.ActiveClass != "browser" {
		t.Fatalf("active window metadata = %q/%q", result.ActiveTitle, result.ActiveClass)
	}
	want := []string{"grim", "-g", "1,2 300x200", result.ImagePath}
	if !reflect.DeepEqual(runner.commands[len(runner.commands)-1], want) {
		t.Fatalf("grim command = %#v, want %#v", runner.commands[len(runner.commands)-1], want)
	}
}

func TestCaptureFullSendsWholeScreenshot(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{}
	cfg := DefaultConfig()
	cfg.AgentPromptPath = filepath.Join(dir, "prompt.md")
	cfg.DebugCaptureDir = filepath.Join(dir, "captures")
	cfg.CaptureTimeout = time.Second
	svc := GrimCaptureService{
		Config: cfg,
		Runner: runner,
		Getenv: envMap(map[string]string{"XDG_RUNTIME_DIR": dir}),
		Now:    func() time.Time { return time.Date(2026, 7, 9, 1, 2, 3, 0, time.UTC) },
	}
	result, err := svc.Capture(context.Background(), CaptureModeFull, CaptureOptions{Retain: true})
	if err != nil {
		t.Fatalf("Capture returned error: %v", err)
	}
	if result.Width != 2 || result.Height != 3 {
		t.Fatalf("size = %dx%d, want 2x3", result.Width, result.Height)
	}
	want := []string{"grim", result.ImagePath}
	if !reflect.DeepEqual(runner.commands[len(runner.commands)-1], want) {
		t.Fatalf("grim command = %#v, want %#v", runner.commands[len(runner.commands)-1], want)
	}
}

func TestCaptureWindowFailsClosedWithoutGeometry(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{invalidActive: true}
	cfg := DefaultConfig()
	cfg.DebugCaptureDir = filepath.Join(dir, "captures")
	svc := GrimCaptureService{Config: cfg, Runner: runner, Getenv: envMap(map[string]string{"XDG_RUNTIME_DIR": dir})}

	_, err := svc.Capture(context.Background(), CaptureModeWindow, CaptureOptions{Retain: true})
	if err == nil || !strings.Contains(err.Error(), "active window geometry unavailable") {
		t.Fatalf("error = %v, want fail-closed geometry error", err)
	}
	for _, command := range runner.commands {
		if len(command) > 0 && command[0] == "grim" {
			t.Fatalf("unexpected full-screen capture after geometry failure: %#v", command)
		}
	}
}

func TestCaptureMonitorFailsClosedWithoutFocusedMonitor(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{noFocusedMonitor: true}
	cfg := DefaultConfig()
	cfg.DebugCaptureDir = filepath.Join(dir, "captures")
	svc := GrimCaptureService{Config: cfg, Runner: runner, Getenv: envMap(map[string]string{"XDG_RUNTIME_DIR": dir})}

	_, err := svc.Capture(context.Background(), CaptureModeMonitor, CaptureOptions{Retain: true})
	if err == nil || !strings.Contains(err.Error(), "focused monitor geometry unavailable") {
		t.Fatalf("error = %v, want fail-closed geometry error", err)
	}
	for _, command := range runner.commands {
		if len(command) > 0 && command[0] == "grim" {
			t.Fatalf("unexpected full-screen capture after geometry failure: %#v", command)
		}
	}
}

func writeTestPNG(path string, width, height int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}
