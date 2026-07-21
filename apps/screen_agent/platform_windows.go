package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func defaultScreenshotBackend() string {
	return "windows"
}

func validateCaptureSupport(mode CaptureMode, backend string) error {
	if backend != "windows" {
		return fmt.Errorf("screenshot_backend must be %q on Windows, got %q", "windows", backend)
	}
	if mode == CaptureModeRegion {
		return fmt.Errorf("capture_mode region is not supported on Windows; use full, monitor, or window")
	}
	return nil
}

func validateHotkeyConfig(hotkey string) error {
	if strings.TrimSpace(hotkey) == "" {
		return nil
	}
	if _, err := parseHotkey(hotkey); err != nil {
		return fmt.Errorf("hotkey: %w", err)
	}
	return nil
}

func runtimeDirFallback() string {
	// os.TempDir is already per-user on Windows and keeps the control socket
	// path under the 108-byte AF_UNIX limit.
	return filepath.Join(os.TempDir(), "screen-agent")
}

func NewCaptureService(cfg Config, runner CommandRunner, getenv func(string) string, logger DebugLogger) CaptureService {
	return WindowsCaptureService{Config: cfg, Getenv: getenv, Logger: logger}
}

func checkCaptureDependencies(cfg Config, runner CommandRunner, result *CheckResult, out io.Writer) {
	fmt.Fprintln(out, "ok screenshot backend: windows (native GDI capture)")
	if strings.TrimSpace(cfg.Hotkey) == "" {
		result.Warnings = append(result.Warnings, "no hotkey configured; trigger the daemon with `screen-agent assist`, `fact-check`, or `summarize`")
		fmt.Fprintln(out, "warn hotkey: not configured; daemon is triggered via `screen-agent assist`")
	} else {
		fmt.Fprintf(out, "ok hotkey: %s\n", cfg.Hotkey)
	}
}

func autoAudioPlayerSpecs() []CommandSpec {
	return []CommandSpec{
		{Name: "ffplay", Args: []string{"-nodisp", "-autoexit", "-loglevel", "error"}},
		{Name: "powershell", Args: []string{"-NoProfile", "-NonInteractive", "-Command", "(New-Object Media.SoundPlayer '{file}').PlaySync()"}},
	}
}

func autoToneCommand(runner CommandRunner) (CommandSpec, bool) {
	return windowsSystemSoundCommand(runner, []string{
		"Windows Navigation Start.wav",
		"Windows Background.wav",
		"Speech On.wav",
	})
}

func autoNoQuestionSoundCommand(runner CommandRunner) (CommandSpec, bool) {
	return windowsSystemSoundCommand(runner, []string{
		"Windows Ding.wav",
		"Windows Exclamation.wav",
		"chord.wav",
	})
}

func windowsSystemSoundCommand(runner CommandRunner, sounds []string) (CommandSpec, bool) {
	root := strings.TrimSpace(os.Getenv("SystemRoot"))
	if root == "" {
		return CommandSpec{}, false
	}
	var player CommandSpec
	found := false
	for _, spec := range autoAudioPlayerSpecs() {
		if _, err := runner.LookPath(spec.Name); err == nil {
			player = spec
			found = true
			break
		}
	}
	if !found {
		return CommandSpec{}, false
	}
	for _, name := range sounds {
		path := filepath.Join(root, "Media", name)
		if _, err := os.Stat(path); err == nil {
			return CommandSpec{Name: player.Name, Args: commandArgsWithFile(player.Args, path)}, true
		}
	}
	return CommandSpec{}, false
}
