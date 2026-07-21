//go:build !windows

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func defaultScreenshotBackend() string {
	return "grim"
}

func validateCaptureSupport(mode CaptureMode, backend string) error {
	if backend != "grim" {
		return fmt.Errorf("screenshot_backend must be %q on this platform, got %q", "grim", backend)
	}
	return nil
}

func validateHotkeyConfig(hotkey string) error {
	if strings.TrimSpace(hotkey) != "" {
		return fmt.Errorf("hotkey is only supported on Windows; bind a key in your compositor instead")
	}
	return nil
}

func runtimeDirFallback() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("screen-agent-%d", os.Getuid()))
}

func NewCaptureService(cfg Config, runner CommandRunner, getenv func(string) string, logger DebugLogger) CaptureService {
	return GrimCaptureService{Config: cfg, Runner: runner, Getenv: getenv, Logger: logger}
}

func checkCaptureDependencies(cfg Config, runner CommandRunner, result *CheckResult, out io.Writer) {
	checkCommand(runner, "grim", result, out)
	checkCommand(runner, "hyprctl", result, out)
	if cfg.CaptureMode == CaptureModeRegion {
		checkCommand(runner, "slurp", result, out)
	}
}

func autoAudioPlayerSpecs() []CommandSpec {
	return []CommandSpec{
		{Name: "pw-play"},
		{Name: "paplay"},
		{Name: "aplay"},
		{Name: "ffplay", Args: []string{"-nodisp", "-autoexit", "-loglevel", "error"}},
	}
}

func autoToneCommand(runner CommandRunner) (CommandSpec, bool) {
	sounds := []string{
		"/usr/share/sounds/freedesktop/stereo/bell.oga",
		"/usr/share/sounds/freedesktop/stereo/message.oga",
		"/usr/share/sounds/freedesktop/stereo/complete.oga",
	}
	if spec, ok := freedesktopSoundCommand(runner, sounds); ok {
		return spec, true
	}
	if _, err := runner.LookPath("canberra-gtk-play"); err == nil {
		return CommandSpec{Name: "canberra-gtk-play", Args: []string{"-i", "bell"}}, true
	}
	return CommandSpec{}, false
}

func autoNoQuestionSoundCommand(runner CommandRunner) (CommandSpec, bool) {
	sounds := []string{
		"/usr/share/sounds/freedesktop/stereo/dialog-information.oga",
		"/usr/share/sounds/freedesktop/stereo/dialog-warning.oga",
		"/usr/share/sounds/freedesktop/stereo/complete.oga",
	}
	if spec, ok := freedesktopSoundCommand(runner, sounds); ok {
		return spec, true
	}
	if _, err := runner.LookPath("canberra-gtk-play"); err == nil {
		return CommandSpec{Name: "canberra-gtk-play", Args: []string{"-i", "dialog-information"}}, true
	}
	return CommandSpec{}, false
}

func freedesktopSoundCommand(runner CommandRunner, sounds []string) (CommandSpec, bool) {
	for _, player := range []string{"pw-play", "paplay"} {
		if _, err := runner.LookPath(player); err != nil {
			continue
		}
		for _, sound := range sounds {
			if _, err := os.Stat(sound); err == nil {
				return CommandSpec{Name: player, Args: []string{sound}}, true
			}
		}
	}
	return CommandSpec{}, false
}
