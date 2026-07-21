package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type CheckResult struct {
	Errors   []string
	Warnings []string
}

func RunCheck(cfg Config, getenv func(string) string, runner CommandRunner, out io.Writer) CheckResult {
	if getenv == nil {
		getenv = os.Getenv
	}
	if runner == nil {
		runner = OSCommandRunner{}
	}
	var result CheckResult
	checkCaptureDependencies(cfg, runner, &result, out)
	if spec, ok, err := ResolveAudioPlayerCommand(cfg.TTSPlayerCommand, runner); err != nil {
		result.Errors = append(result.Errors, err.Error())
		fmt.Fprintf(out, "missing TTS audio player: %v\n", err)
	} else if ok {
		fmt.Fprintf(out, "ok TTS audio player: %s\n", formatCommandSpec(spec))
	} else {
		result.Errors = append(result.Errors, "no usable TTS audio player command found")
		fmt.Fprintln(out, "missing TTS audio player")
	}

	if err := EnsureDefaultPrompt(cfg.AgentPromptPath); err != nil {
		result.Errors = append(result.Errors, "prompt: "+err.Error())
	} else {
		fmt.Fprintf(out, "ok prompt: %s\n", cfg.AgentPromptPath)
	}
	checkProviderEnv(cfg, getenv, &result, out)
	if !strings.EqualFold(strings.TrimSpace(cfg.AgentProvider), "gemini") {
		checkGeminiEnv(getenv, "Gemini TTS credentials", &result, out)
	}
	checkWritableDir(RuntimeDir(getenv), "runtime dir", &result, out)
	if cfg.RetainDebugCaptures {
		checkWritableDir(cfg.DebugCaptureDir, "debug capture dir", &result, out)
	}
	if cfg.ThinkingToneEnabled {
		if spec, ok, err := ResolveToneCommand(cfg.ThinkingToneCommand, runner); err != nil {
			result.Warnings = append(result.Warnings, err.Error())
			fmt.Fprintf(out, "warn thinking tone: %v\n", err)
		} else if ok {
			fmt.Fprintf(out, "ok thinking tone: %s\n", formatCommandSpec(spec))
		} else {
			result.Warnings = append(result.Warnings, "no usable thinking tone command found")
			fmt.Fprintln(out, "warn thinking tone: no usable auto command found")
		}
	}
	if cfg.NoQuestionSoundEnabled {
		if spec, ok, err := ResolveNoQuestionSoundCommand(cfg.NoQuestionSoundCommand, runner); err != nil {
			result.Warnings = append(result.Warnings, err.Error())
			fmt.Fprintf(out, "warn no-question sound: %v\n", err)
		} else if ok {
			fmt.Fprintf(out, "ok no-question sound: %s\n", formatCommandSpec(spec))
		} else {
			result.Warnings = append(result.Warnings, "no usable no-question sound command found")
			fmt.Fprintln(out, "warn no-question sound: no usable auto command found")
		}
	}
	return result
}

func formatCommandSpec(spec CommandSpec) string {
	if len(spec.Args) == 0 {
		return spec.Name
	}
	return spec.Name + " " + strings.Join(spec.Args, " ")
}

func checkCommand(runner CommandRunner, name string, result *CheckResult, out io.Writer) {
	path, err := runner.LookPath(name)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("%s not found on PATH", name))
		fmt.Fprintf(out, "missing %s\n", name)
		return
	}
	fmt.Fprintf(out, "ok %s: %s\n", name, path)
}

func checkProviderEnv(cfg Config, getenv func(string) string, result *CheckResult, out io.Writer) {
	switch strings.ToLower(strings.TrimSpace(cfg.AgentProvider)) {
	case "fake":
		fmt.Fprintln(out, "ok agent provider: fake")
	case "openai":
		if strings.EqualFold(cfg.OpenAIAuthMode, "codex_oauth") {
			if strings.TrimSpace(getenv("OPENAI_API_KEY")) != "" || fileExists(codexAuthPath(getenv)) {
				fmt.Fprintln(out, "ok OpenAI credentials")
				return
			}
			result.Errors = append(result.Errors, "OPENAI_API_KEY is not set and Codex auth file was not found")
			fmt.Fprintln(out, "missing OPENAI_API_KEY or Codex auth file")
			return
		}
		checkEnv(getenv, "OPENAI_API_KEY", result, out)
	case "gemini":
		checkGeminiEnv(getenv, "Gemini credentials", result, out)
	case "anthropic":
		checkEnv(getenv, "ANTHROPIC_API_KEY", result, out)
	}
}

func checkGeminiEnv(getenv func(string) string, label string, result *CheckResult, out io.Writer) {
	_, keyName := geminiAPIKey(getenv)
	if keyName == "" {
		result.Errors = append(result.Errors, "GEMINI_API_KEY or GOOGLE_API_KEY is not set")
		fmt.Fprintln(out, "missing GEMINI_API_KEY or GOOGLE_API_KEY")
		return
	}
	fmt.Fprintf(out, "ok %s: %s\n", label, keyName)
}

func checkEnv(getenv func(string) string, key string, result *CheckResult, out io.Writer) {
	if strings.TrimSpace(getenv(key)) == "" {
		result.Errors = append(result.Errors, key+" is not set")
		fmt.Fprintf(out, "missing %s\n", key)
		return
	}
	fmt.Fprintf(out, "ok %s\n", key)
}

func checkWritableDir(path, label string, result *CheckResult, out io.Writer) {
	if err := os.MkdirAll(path, 0o700); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("%s %q is not writable: %v", label, path, err))
		fmt.Fprintf(out, "missing %s: %s\n", label, path)
		return
	}
	f, err := os.CreateTemp(path, ".check-*")
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("%s %q is not writable: %v", label, path, err))
		fmt.Fprintf(out, "missing %s: %s\n", label, path)
		return
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	fmt.Fprintf(out, "ok %s: %s\n", label, path)
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func codexAuthPath(getenv func(string) string) string {
	if v := strings.TrimSpace(getenv("OPENAI_CODEX_AUTH_FILE")); v != "" {
		return expandPath(v)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "auth.json")
}
