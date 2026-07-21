package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigUsesActiveWindowCapture(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.CaptureMode != CaptureModeWindow {
		t.Fatalf("default capture mode = %q, want window", cfg.CaptureMode)
	}
	if cfg.AgentProvider != "gemini" {
		t.Fatalf("default provider = %q, want gemini", cfg.AgentProvider)
	}
	if cfg.AgentModel != "gemini-3.5-flash" {
		t.Fatalf("default model = %q, want gemini-3.5-flash", cfg.AgentModel)
	}
	if cfg.AgentMaxOutputTokens != 4096 {
		t.Fatalf("default max output tokens = %d, want 4096", cfg.AgentMaxOutputTokens)
	}
	if filepath.Base(cfg.AgentPromptPath) != "prompt.md" {
		t.Fatalf("default prompt path = %q, want prompt.md in app directory", cfg.AgentPromptPath)
	}
	if cfg.TTSModel != "gemini-3.1-flash-tts-preview" {
		t.Fatalf("default TTS model = %q, want gemini-3.1-flash-tts-preview", cfg.TTSModel)
	}
	if cfg.TTSVoice != "Kore" {
		t.Fatalf("default TTS voice = %q, want Kore", cfg.TTSVoice)
	}
	if cfg.TTSTimeout.String() != "45s" {
		t.Fatalf("default TTS timeout = %s, want 45s", cfg.TTSTimeout)
	}
	if cfg.TTSPlaybackTimeout.String() != "2m0s" {
		t.Fatalf("default TTS playback timeout = %s, want 2m0s", cfg.TTSPlaybackTimeout)
	}
	if !cfg.NoQuestionSoundEnabled {
		t.Fatalf("default no-question sound should be enabled")
	}
	if cfg.NoQuestionSoundCommand != "auto" {
		t.Fatalf("default no-question sound command = %q, want auto", cfg.NoQuestionSoundCommand)
	}
}

func TestLoadConfig_FileEnvOverridePrecedence(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
capture_mode = "monitor"
agent_provider = "gemini"
tts_model = "old-model"
tts_voice = "Aoede"
max_spoken_chars = 100
thinking_tone_enabled = false
`), 0o600); err != nil {
		t.Fatal(err)
	}

	debug := true
	cfg, err := LoadConfig(cfgPath, envMap(map[string]string{
		"SCREEN_AGENT_CAPTURE_MODE":              "window",
		"SCREEN_AGENT_MAX_SPOKEN_CHARS":          "120",
		"SCREEN_AGENT_AGENT_MAX_OUTPUT_TOKENS":   "2048",
		"SCREEN_AGENT_AGENT_PROMPT_PATH":         filepath.Join(dir, "prompt.md"),
		"SCREEN_AGENT_TTS_MODEL":                 "gemini-3.1-flash-tts-preview",
		"SCREEN_AGENT_TTS_VOICE":                 "Kore",
		"SCREEN_AGENT_TTS_PLAYBACK_TIMEOUT":      "90s",
		"SCREEN_AGENT_NO_QUESTION_SOUND_ENABLED": "false",
	}), ConfigOverride{
		CaptureMode: "full",
		Debug:       &debug,
	})
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.CaptureMode != CaptureModeFull {
		t.Fatalf("capture mode = %q, want full", cfg.CaptureMode)
	}
	if cfg.AgentProvider != "gemini" {
		t.Fatalf("agent provider = %q, want gemini", cfg.AgentProvider)
	}
	if cfg.MaxSpokenChars != 120 {
		t.Fatalf("max chars = %d, want 120", cfg.MaxSpokenChars)
	}
	if cfg.AgentMaxOutputTokens != 2048 {
		t.Fatalf("max output tokens = %d, want 2048", cfg.AgentMaxOutputTokens)
	}
	if cfg.TTSModel != "gemini-3.1-flash-tts-preview" {
		t.Fatalf("TTS model = %q, want gemini-3.1-flash-tts-preview", cfg.TTSModel)
	}
	if cfg.TTSVoice != "Kore" {
		t.Fatalf("TTS voice = %q, want Kore", cfg.TTSVoice)
	}
	if cfg.TTSPlaybackTimeout.String() != "1m30s" {
		t.Fatalf("TTS playback timeout = %s, want 1m30s", cfg.TTSPlaybackTimeout)
	}
	if !cfg.Debug {
		t.Fatalf("debug override was not applied")
	}
	if cfg.ThinkingToneEnabled {
		t.Fatalf("thinking_tone_enabled should remain false from config")
	}
	if cfg.NoQuestionSoundEnabled {
		t.Fatalf("no_question_sound_enabled should be false from env")
	}
}

func TestParseCommandSpecQuotes(t *testing.T) {
	spec, err := ParseCommandSpec(`pw-play "--target=my sink"`)
	if err != nil {
		t.Fatalf("ParseCommandSpec returned error: %v", err)
	}
	if spec.Name != "pw-play" || len(spec.Args) != 1 || spec.Args[0] != "--target=my sink" {
		t.Fatalf("unexpected spec: %#v", spec)
	}
}
