package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseHotkey(t *testing.T) {
	tests := []struct {
		raw       string
		modifiers uint32
		vk        uint32
	}{
		{raw: "ctrl+alt+a", modifiers: modControl | modAlt, vk: 'A'},
		{raw: "Ctrl + Shift + F9", modifiers: modControl | modShift, vk: 0x78},
		{raw: "win+space", modifiers: modWin, vk: 0x20},
		{raw: "f12", modifiers: 0, vk: 0x7B},
		{raw: "control+shift+9", modifiers: modControl | modShift, vk: '9'},
	}
	for _, tc := range tests {
		spec, err := parseHotkey(tc.raw)
		if err != nil {
			t.Fatalf("parseHotkey(%q) returned error: %v", tc.raw, err)
		}
		if spec.Modifiers != tc.modifiers || spec.VirtualKey != tc.vk {
			t.Fatalf("parseHotkey(%q) = %#v, want modifiers=%#x vk=%#x", tc.raw, spec, tc.modifiers, tc.vk)
		}
	}
}

func TestParseHotkeyRejectsInvalidCombos(t *testing.T) {
	for _, raw := range []string{"", "ctrl+alt", "a", "ctrl+", "ctrl+a+b", "ctrl+foo", "f25"} {
		if _, err := parseHotkey(raw); err == nil {
			t.Fatalf("parseHotkey(%q) should fail", raw)
		}
	}
}

func TestValidateCaptureSupportOnWindows(t *testing.T) {
	if err := validateCaptureSupport(CaptureModeWindow, "windows"); err != nil {
		t.Fatalf("windows backend rejected: %v", err)
	}
	if err := validateCaptureSupport(CaptureModeWindow, "grim"); err == nil {
		t.Fatalf("grim backend should be rejected on Windows")
	}
	if err := validateCaptureSupport(CaptureModeRegion, "windows"); err == nil {
		t.Fatalf("region capture should be rejected on Windows")
	}
}

func TestLoadConfigValidatesHotkeyOnWindows(t *testing.T) {
	_, err := LoadConfig("", envMap(map[string]string{"SCREEN_AGENT_HOTKEY": "ctrl+alt+a"}), ConfigOverride{})
	if err != nil {
		t.Fatalf("valid hotkey rejected: %v", err)
	}
	_, err = LoadConfig("", envMap(map[string]string{"SCREEN_AGENT_HOTKEY": "banana"}), ConfigOverride{})
	if err == nil || !strings.Contains(err.Error(), "hotkey") {
		t.Fatalf("error = %v, want hotkey validation failure", err)
	}
}

func TestWindowsSystemSoundCommandsAreDistinct(t *testing.T) {
	runner := &toneRunner{}
	tone, toneOK := autoToneCommand(runner)
	noQuestion, noQuestionOK := autoNoQuestionSoundCommand(runner)
	if !toneOK || !noQuestionOK {
		t.Skipf("system sounds unavailable: tone=%v noQuestion=%v", toneOK, noQuestionOK)
	}
	if tone.Name != noQuestion.Name {
		t.Fatalf("players differ: %q vs %q", tone.Name, noQuestion.Name)
	}
	if strings.Join(tone.Args, " ") == strings.Join(noQuestion.Args, " ") {
		t.Fatalf("no-question sound should differ from thinking tone: %#v", tone)
	}
}

func TestWindowsCaptureFullWritesPNG(t *testing.T) {
	if _, err := virtualScreenRect(); err != nil {
		t.Skipf("no interactive desktop session: %v", err)
	}
	dir := t.TempDir()
	cfg := DefaultConfig()
	svc := WindowsCaptureService{
		Config: cfg,
		Getenv: envMap(map[string]string{"XDG_RUNTIME_DIR": dir}),
		Now:    func() time.Time { return time.Date(2026, 7, 11, 1, 2, 3, 0, time.UTC) },
	}
	out := filepath.Join(dir, "shot.png")
	result, err := svc.Capture(context.Background(), CaptureModeFull, CaptureOptions{OutputPath: out})
	if err != nil {
		t.Fatalf("Capture returned error: %v", err)
	}
	if result.Width <= 0 || result.Height <= 0 {
		t.Fatalf("capture size = %dx%d", result.Width, result.Height)
	}
	w, h := decodeImageSize(out)
	if w != result.Width || h != result.Height {
		t.Fatalf("PNG size %dx%d does not match result %dx%d", w, h, result.Width, result.Height)
	}
}
