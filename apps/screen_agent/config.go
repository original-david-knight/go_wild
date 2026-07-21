package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

const (
	defaultConfigRelPath        = ".config/screen-agent/config.toml"
	defaultPromptFilename       = "prompt.md"
	defaultDebugCaptureRelPath  = ".local/state/screen-agent/captures"
	defaultCaptureTimeout       = 5 * time.Second
	defaultAgentTimeout         = 45 * time.Second
	defaultTTSTimeout           = 45 * time.Second
	defaultTTSPlaybackTimeout   = 2 * time.Minute
	defaultToneInterval         = 1 * time.Second
	defaultAgentMaxOutputTokens = 4096
	defaultTTSModel             = "gemini-3.1-flash-tts-preview"
	defaultTTSVoice             = "Kore"
	defaultTTSLanguageCode      = "en-US"
)

type Config struct {
	CaptureMode            CaptureMode
	ScreenshotBackend      string
	AgentProvider          string
	AgentModel             string
	AgentPromptPath        string
	AgentBaseURL           string
	AgentMaxOutputTokens   int32
	OpenAIAuthMode         string
	TTSModel               string
	TTSVoice               string
	TTSLanguageCode        string
	TTSPlayerCommand       string
	SpeakWhenUncertain     bool
	MaxSpokenChars         int
	Debug                  bool
	RetainDebugCaptures    bool
	DebugCaptureDir        string
	CaptureTimeout         time.Duration
	AgentTimeout           time.Duration
	TTSTimeout             time.Duration
	TTSPlaybackTimeout     time.Duration
	ThinkingToneEnabled    bool
	ThinkingToneCommand    string
	ThinkingToneInterval   time.Duration
	NoQuestionSoundEnabled bool
	NoQuestionSoundCommand string
	Hotkey                 string
}

type fileConfig struct {
	CaptureMode            string `toml:"capture_mode"`
	ScreenshotBackend      string `toml:"screenshot_backend"`
	AgentProvider          string `toml:"agent_provider"`
	AgentModel             string `toml:"agent_model"`
	AgentPromptPath        string `toml:"agent_prompt_path"`
	AgentBaseURL           string `toml:"agent_base_url"`
	AgentMaxOutputTokens   int32  `toml:"agent_max_output_tokens"`
	OpenAIAuthMode         string `toml:"openai_auth_mode"`
	TTSModel               string `toml:"tts_model"`
	TTSVoice               string `toml:"tts_voice"`
	TTSLanguageCode        string `toml:"tts_language_code"`
	TTSPlayerCommand       string `toml:"tts_player_command"`
	SpeakWhenUncertain     bool   `toml:"speak_when_uncertain"`
	MaxSpokenChars         int    `toml:"max_spoken_chars"`
	Debug                  bool   `toml:"debug"`
	RetainDebugCaptures    bool   `toml:"retain_debug_captures"`
	DebugCaptureDir        string `toml:"debug_capture_dir"`
	CaptureTimeout         string `toml:"capture_timeout"`
	AgentTimeout           string `toml:"agent_timeout"`
	TTSTimeout             string `toml:"tts_timeout"`
	TTSPlaybackTimeout     string `toml:"tts_playback_timeout"`
	ThinkingToneEnabled    bool   `toml:"thinking_tone_enabled"`
	ThinkingToneCommand    string `toml:"thinking_tone_command"`
	ThinkingToneInterval   string `toml:"thinking_tone_interval"`
	NoQuestionSoundEnabled bool   `toml:"no_question_sound_enabled"`
	NoQuestionSoundCommand string `toml:"no_question_sound_command"`
	Hotkey                 string `toml:"hotkey"`
}

type ConfigOverride struct {
	CaptureMode            string
	AgentProvider          string
	AgentModel             string
	AgentMaxOutputTokens   int32
	TTSModel               string
	TTSVoice               string
	TTSLanguageCode        string
	TTSPlayerCommand       string
	TTSPlaybackTimeout     string
	Debug                  *bool
	RetainDebugCaptures    *bool
	ThinkingToneEnabled    *bool
	ThinkingToneCommand    string
	ThinkingToneInterval   string
	NoQuestionSoundEnabled *bool
	NoQuestionSoundCommand string
	Hotkey                 string
}

func DefaultConfig() Config {
	return Config{
		CaptureMode:            CaptureModeWindow,
		ScreenshotBackend:      defaultScreenshotBackend(),
		AgentProvider:          loop.LLMProviderGemini,
		AgentModel:             "gemini-3.5-flash",
		AgentPromptPath:        defaultPromptPath(),
		AgentMaxOutputTokens:   defaultAgentMaxOutputTokens,
		OpenAIAuthMode:         loop.OpenAIAuthModeAPIKey,
		TTSModel:               defaultTTSModel,
		TTSVoice:               defaultTTSVoice,
		TTSLanguageCode:        defaultTTSLanguageCode,
		TTSPlayerCommand:       "auto",
		SpeakWhenUncertain:     false,
		MaxSpokenChars:         300,
		Debug:                  false,
		RetainDebugCaptures:    false,
		DebugCaptureDir:        mustHomePath(defaultDebugCaptureRelPath),
		CaptureTimeout:         defaultCaptureTimeout,
		AgentTimeout:           defaultAgentTimeout,
		TTSTimeout:             defaultTTSTimeout,
		TTSPlaybackTimeout:     defaultTTSPlaybackTimeout,
		ThinkingToneEnabled:    true,
		ThinkingToneCommand:    "auto",
		ThinkingToneInterval:   defaultToneInterval,
		NoQuestionSoundEnabled: true,
		NoQuestionSoundCommand: "auto",
	}
}

func DefaultConfigPath() string {
	return mustHomePath(defaultConfigRelPath)
}

func defaultPromptPath() string {
	if _, file, _, ok := runtime.Caller(0); ok {
		return filepath.Join(filepath.Dir(file), defaultPromptFilename)
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, defaultPromptFilename)
	}
	return defaultPromptFilename
}

func LoadConfig(path string, getenv func(string) string, override ConfigOverride) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	cfg := DefaultConfig()

	if strings.TrimSpace(path) == "" {
		path = DefaultConfigPath()
	}
	path = expandPath(path)
	if _, err := os.Stat(path); err == nil {
		if err := applyConfigFile(&cfg, path); err != nil {
			return Config{}, err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("stat config %q: %w", path, err)
	}

	if err := applyEnv(&cfg, getenv); err != nil {
		return Config{}, err
	}
	if err := applyOverrides(&cfg, override); err != nil {
		return Config{}, err
	}
	expandConfigPaths(&cfg)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyConfigFile(cfg *Config, path string) error {
	var fc fileConfig
	md, err := toml.DecodeFile(path, &fc)
	if err != nil {
		return fmt.Errorf("parse config %q: %w", path, err)
	}
	if md.IsDefined("capture_mode") {
		cfg.CaptureMode = CaptureMode(strings.ToLower(strings.TrimSpace(fc.CaptureMode)))
	}
	if md.IsDefined("screenshot_backend") {
		cfg.ScreenshotBackend = strings.TrimSpace(fc.ScreenshotBackend)
	}
	if md.IsDefined("agent_provider") {
		cfg.AgentProvider = strings.ToLower(strings.TrimSpace(fc.AgentProvider))
	}
	if md.IsDefined("agent_model") {
		cfg.AgentModel = strings.TrimSpace(fc.AgentModel)
	}
	if md.IsDefined("agent_prompt_path") {
		cfg.AgentPromptPath = strings.TrimSpace(fc.AgentPromptPath)
	}
	if md.IsDefined("agent_base_url") {
		cfg.AgentBaseURL = strings.TrimSpace(fc.AgentBaseURL)
	}
	if md.IsDefined("agent_max_output_tokens") {
		cfg.AgentMaxOutputTokens = fc.AgentMaxOutputTokens
	}
	if md.IsDefined("openai_auth_mode") {
		cfg.OpenAIAuthMode = strings.TrimSpace(fc.OpenAIAuthMode)
	}
	if md.IsDefined("tts_model") {
		cfg.TTSModel = strings.TrimSpace(fc.TTSModel)
	}
	if md.IsDefined("tts_voice") {
		cfg.TTSVoice = strings.TrimSpace(fc.TTSVoice)
	}
	if md.IsDefined("tts_language_code") {
		cfg.TTSLanguageCode = strings.TrimSpace(fc.TTSLanguageCode)
	}
	if md.IsDefined("tts_player_command") {
		cfg.TTSPlayerCommand = strings.TrimSpace(fc.TTSPlayerCommand)
	}
	if md.IsDefined("speak_when_uncertain") {
		cfg.SpeakWhenUncertain = fc.SpeakWhenUncertain
	}
	if md.IsDefined("max_spoken_chars") {
		cfg.MaxSpokenChars = fc.MaxSpokenChars
	}
	if md.IsDefined("debug") {
		cfg.Debug = fc.Debug
	}
	if md.IsDefined("retain_debug_captures") {
		cfg.RetainDebugCaptures = fc.RetainDebugCaptures
	}
	if md.IsDefined("debug_capture_dir") {
		cfg.DebugCaptureDir = strings.TrimSpace(fc.DebugCaptureDir)
	}
	if md.IsDefined("capture_timeout") {
		d, err := time.ParseDuration(strings.TrimSpace(fc.CaptureTimeout))
		if err != nil {
			return fmt.Errorf("capture_timeout: %w", err)
		}
		cfg.CaptureTimeout = d
	}
	if md.IsDefined("agent_timeout") {
		d, err := time.ParseDuration(strings.TrimSpace(fc.AgentTimeout))
		if err != nil {
			return fmt.Errorf("agent_timeout: %w", err)
		}
		cfg.AgentTimeout = d
	}
	if md.IsDefined("tts_timeout") {
		d, err := time.ParseDuration(strings.TrimSpace(fc.TTSTimeout))
		if err != nil {
			return fmt.Errorf("tts_timeout: %w", err)
		}
		cfg.TTSTimeout = d
	}
	if md.IsDefined("tts_playback_timeout") {
		d, err := time.ParseDuration(strings.TrimSpace(fc.TTSPlaybackTimeout))
		if err != nil {
			return fmt.Errorf("tts_playback_timeout: %w", err)
		}
		cfg.TTSPlaybackTimeout = d
	}
	if md.IsDefined("thinking_tone_enabled") {
		cfg.ThinkingToneEnabled = fc.ThinkingToneEnabled
	}
	if md.IsDefined("thinking_tone_command") {
		cfg.ThinkingToneCommand = strings.TrimSpace(fc.ThinkingToneCommand)
	}
	if md.IsDefined("thinking_tone_interval") {
		d, err := time.ParseDuration(strings.TrimSpace(fc.ThinkingToneInterval))
		if err != nil {
			return fmt.Errorf("thinking_tone_interval: %w", err)
		}
		cfg.ThinkingToneInterval = d
	}
	if md.IsDefined("no_question_sound_enabled") {
		cfg.NoQuestionSoundEnabled = fc.NoQuestionSoundEnabled
	}
	if md.IsDefined("no_question_sound_command") {
		cfg.NoQuestionSoundCommand = strings.TrimSpace(fc.NoQuestionSoundCommand)
	}
	if md.IsDefined("hotkey") {
		cfg.Hotkey = strings.TrimSpace(fc.Hotkey)
	}
	return nil
}

func applyEnv(cfg *Config, getenv func(string) string) error {
	envString(getenv, "SCREEN_AGENT_CAPTURE_MODE", func(v string) { cfg.CaptureMode = CaptureMode(strings.ToLower(v)) })
	envString(getenv, "SCREEN_AGENT_SCREENSHOT_BACKEND", func(v string) { cfg.ScreenshotBackend = v })
	envString(getenv, "SCREEN_AGENT_AGENT_PROVIDER", func(v string) { cfg.AgentProvider = strings.ToLower(v) })
	envString(getenv, "SCREEN_AGENT_AGENT_MODEL", func(v string) { cfg.AgentModel = v })
	envString(getenv, "SCREEN_AGENT_AGENT_PROMPT_PATH", func(v string) { cfg.AgentPromptPath = v })
	envString(getenv, "SCREEN_AGENT_AGENT_BASE_URL", func(v string) { cfg.AgentBaseURL = v })
	envString(getenv, "SCREEN_AGENT_OPENAI_AUTH_MODE", func(v string) { cfg.OpenAIAuthMode = v })
	envString(getenv, "SCREEN_AGENT_TTS_MODEL", func(v string) { cfg.TTSModel = v })
	envString(getenv, "SCREEN_AGENT_TTS_VOICE", func(v string) { cfg.TTSVoice = v })
	envString(getenv, "SCREEN_AGENT_TTS_LANGUAGE_CODE", func(v string) { cfg.TTSLanguageCode = v })
	envString(getenv, "SCREEN_AGENT_TTS_PLAYER_COMMAND", func(v string) { cfg.TTSPlayerCommand = v })
	envString(getenv, "SCREEN_AGENT_DEBUG_CAPTURE_DIR", func(v string) { cfg.DebugCaptureDir = v })
	envString(getenv, "SCREEN_AGENT_THINKING_TONE_COMMAND", func(v string) { cfg.ThinkingToneCommand = v })
	envString(getenv, "SCREEN_AGENT_NO_QUESTION_SOUND_COMMAND", func(v string) { cfg.NoQuestionSoundCommand = v })
	envString(getenv, "SCREEN_AGENT_HOTKEY", func(v string) { cfg.Hotkey = v })
	if err := envBool(getenv, "SCREEN_AGENT_SPEAK_WHEN_UNCERTAIN", &cfg.SpeakWhenUncertain); err != nil {
		return err
	}
	if err := envBool(getenv, "SCREEN_AGENT_DEBUG", &cfg.Debug); err != nil {
		return err
	}
	if err := envBool(getenv, "SCREEN_AGENT_RETAIN_DEBUG_CAPTURES", &cfg.RetainDebugCaptures); err != nil {
		return err
	}
	if err := envBool(getenv, "SCREEN_AGENT_THINKING_TONE_ENABLED", &cfg.ThinkingToneEnabled); err != nil {
		return err
	}
	if err := envBool(getenv, "SCREEN_AGENT_NO_QUESTION_SOUND_ENABLED", &cfg.NoQuestionSoundEnabled); err != nil {
		return err
	}
	if err := envInt(getenv, "SCREEN_AGENT_MAX_SPOKEN_CHARS", &cfg.MaxSpokenChars); err != nil {
		return err
	}
	if err := envInt32(getenv, "SCREEN_AGENT_AGENT_MAX_OUTPUT_TOKENS", &cfg.AgentMaxOutputTokens); err != nil {
		return err
	}
	if err := envDuration(getenv, "SCREEN_AGENT_CAPTURE_TIMEOUT", &cfg.CaptureTimeout); err != nil {
		return err
	}
	if err := envDuration(getenv, "SCREEN_AGENT_AGENT_TIMEOUT", &cfg.AgentTimeout); err != nil {
		return err
	}
	if err := envDuration(getenv, "SCREEN_AGENT_TTS_TIMEOUT", &cfg.TTSTimeout); err != nil {
		return err
	}
	if err := envDuration(getenv, "SCREEN_AGENT_TTS_PLAYBACK_TIMEOUT", &cfg.TTSPlaybackTimeout); err != nil {
		return err
	}
	if err := envDuration(getenv, "SCREEN_AGENT_THINKING_TONE_INTERVAL", &cfg.ThinkingToneInterval); err != nil {
		return err
	}
	return nil
}

func applyOverrides(cfg *Config, ov ConfigOverride) error {
	if strings.TrimSpace(ov.CaptureMode) != "" {
		cfg.CaptureMode = CaptureMode(strings.ToLower(strings.TrimSpace(ov.CaptureMode)))
	}
	if strings.TrimSpace(ov.AgentProvider) != "" {
		cfg.AgentProvider = strings.ToLower(strings.TrimSpace(ov.AgentProvider))
	}
	if strings.TrimSpace(ov.AgentModel) != "" {
		cfg.AgentModel = strings.TrimSpace(ov.AgentModel)
	}
	if ov.AgentMaxOutputTokens > 0 {
		cfg.AgentMaxOutputTokens = ov.AgentMaxOutputTokens
	}
	if strings.TrimSpace(ov.TTSModel) != "" {
		cfg.TTSModel = strings.TrimSpace(ov.TTSModel)
	}
	if strings.TrimSpace(ov.TTSVoice) != "" {
		cfg.TTSVoice = strings.TrimSpace(ov.TTSVoice)
	}
	if strings.TrimSpace(ov.TTSLanguageCode) != "" {
		cfg.TTSLanguageCode = strings.TrimSpace(ov.TTSLanguageCode)
	}
	if strings.TrimSpace(ov.TTSPlayerCommand) != "" {
		cfg.TTSPlayerCommand = strings.TrimSpace(ov.TTSPlayerCommand)
	}
	if strings.TrimSpace(ov.TTSPlaybackTimeout) != "" {
		d, err := time.ParseDuration(strings.TrimSpace(ov.TTSPlaybackTimeout))
		if err != nil {
			return fmt.Errorf("tts playback timeout: %w", err)
		}
		cfg.TTSPlaybackTimeout = d
	}
	if ov.Debug != nil {
		cfg.Debug = *ov.Debug
	}
	if ov.RetainDebugCaptures != nil {
		cfg.RetainDebugCaptures = *ov.RetainDebugCaptures
	}
	if ov.ThinkingToneEnabled != nil {
		cfg.ThinkingToneEnabled = *ov.ThinkingToneEnabled
	}
	if strings.TrimSpace(ov.ThinkingToneCommand) != "" {
		cfg.ThinkingToneCommand = strings.TrimSpace(ov.ThinkingToneCommand)
	}
	if strings.TrimSpace(ov.ThinkingToneInterval) != "" {
		d, err := time.ParseDuration(strings.TrimSpace(ov.ThinkingToneInterval))
		if err != nil {
			return fmt.Errorf("thinking tone interval: %w", err)
		}
		cfg.ThinkingToneInterval = d
	}
	if ov.NoQuestionSoundEnabled != nil {
		cfg.NoQuestionSoundEnabled = *ov.NoQuestionSoundEnabled
	}
	if strings.TrimSpace(ov.NoQuestionSoundCommand) != "" {
		cfg.NoQuestionSoundCommand = strings.TrimSpace(ov.NoQuestionSoundCommand)
	}
	if strings.TrimSpace(ov.Hotkey) != "" {
		cfg.Hotkey = strings.TrimSpace(ov.Hotkey)
	}
	return nil
}

func (cfg Config) Validate() error {
	switch cfg.CaptureMode {
	case CaptureModeFull, CaptureModeMonitor, CaptureModeWindow, CaptureModeRegion:
	default:
		return fmt.Errorf("capture_mode must be full, monitor, window, or region, got %q", cfg.CaptureMode)
	}
	if err := validateCaptureSupport(cfg.CaptureMode, strings.TrimSpace(cfg.ScreenshotBackend)); err != nil {
		return err
	}
	if err := validateHotkeyConfig(cfg.Hotkey); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(cfg.AgentProvider)) {
	case loop.LLMProviderOpenAI, loop.LLMProviderGemini, loop.LLMProviderAnthropic, "fake":
	default:
		return fmt.Errorf("agent_provider must be openai, gemini, anthropic, or fake, got %q", cfg.AgentProvider)
	}
	if strings.TrimSpace(cfg.AgentPromptPath) == "" {
		return fmt.Errorf("agent_prompt_path is required")
	}
	if cfg.AgentMaxOutputTokens <= 0 {
		return fmt.Errorf("agent_max_output_tokens must be positive")
	}
	if strings.TrimSpace(cfg.TTSModel) == "" {
		return fmt.Errorf("tts_model is required")
	}
	if strings.TrimSpace(cfg.TTSVoice) == "" {
		return fmt.Errorf("tts_voice is required")
	}
	if strings.TrimSpace(cfg.TTSLanguageCode) == "" {
		return fmt.Errorf("tts_language_code is required")
	}
	if strings.TrimSpace(cfg.TTSPlayerCommand) == "" {
		return fmt.Errorf("tts_player_command is required")
	}
	if cfg.MaxSpokenChars <= 0 {
		return fmt.Errorf("max_spoken_chars must be positive")
	}
	if cfg.CaptureTimeout <= 0 {
		return fmt.Errorf("capture_timeout must be positive")
	}
	if cfg.AgentTimeout <= 0 {
		return fmt.Errorf("agent_timeout must be positive")
	}
	if cfg.TTSTimeout <= 0 {
		return fmt.Errorf("tts_timeout must be positive")
	}
	if cfg.TTSPlaybackTimeout <= 0 {
		return fmt.Errorf("tts_playback_timeout must be positive")
	}
	if cfg.ThinkingToneInterval <= 0 {
		return fmt.Errorf("thinking_tone_interval must be positive")
	}
	if cfg.NoQuestionSoundEnabled && strings.TrimSpace(cfg.NoQuestionSoundCommand) == "" {
		return fmt.Errorf("no_question_sound_command is required when no_question_sound_enabled is true")
	}
	return nil
}

func expandConfigPaths(cfg *Config) {
	cfg.AgentPromptPath = expandPath(cfg.AgentPromptPath)
	cfg.DebugCaptureDir = expandPath(cfg.DebugCaptureDir)
}

func EnsureDefaultPrompt(path string) error {
	path = expandPath(path)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat prompt %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create prompt directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(defaultPrompt), 0o600); err != nil {
		return fmt.Errorf("write default prompt %q: %w", path, err)
	}
	return nil
}

func RuntimeDir(getenv func(string) string) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	if v := strings.TrimSpace(getenv("XDG_RUNTIME_DIR")); v != "" {
		return filepath.Join(v, "screen-agent")
	}
	return runtimeDirFallback()
}

func envString(getenv func(string) string, key string, set func(string)) {
	if v := strings.TrimSpace(getenv(key)); v != "" {
		set(v)
	}
}

func envBool(getenv func(string) string, key string, target *bool) error {
	if v := strings.TrimSpace(getenv(key)); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		*target = b
	}
	return nil
}

func envInt(getenv func(string) string, key string, target *int) error {
	if v := strings.TrimSpace(getenv(key)); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		*target = n
	}
	return nil
}

func envInt32(getenv func(string) string, key string, target *int32) error {
	if v := strings.TrimSpace(getenv(key)); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		*target = int32(n)
	}
	return nil
}

func envDuration(getenv func(string) string, key string, target *time.Duration) error {
	if v := strings.TrimSpace(getenv(key)); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		*target = d
	}
	return nil
}

func expandPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return os.ExpandEnv(path)
}

func mustHomePath(rel string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("~", rel)
	}
	return filepath.Join(home, rel)
}

const defaultPrompt = `You are the vision analyzer for a local screen assistant.

The user has intentionally triggered this assistant on their own desktop where AI assistance is allowed.

Your task:

- Inspect the screenshot image carefully.
- Decide whether it contains a visible question, request, error, dialog, document, UI state, or other screen content where concise spoken assistance would be useful.
- If useful assistance is possible, answer directly in text suitable for text-to-speech.
- If there is no clear request, question, actionable state, or useful screen context, return no answer.

Answer style:

- Prefer the direct answer, summary, or next step over explanation.
- Do not include chain-of-thought, hidden reasoning, long derivations, markdown, tables, code fences, or JSON inside spoken_answer.
- Keep spoken_answer short enough to read aloud.
- For multiple-choice questions, answer like: "The answer is C."
- For math, answer like: "The derivative is 2 x plus 3."
- For errors or dialogs, say the likely issue and the next practical action.
- For UI state, say what appears selected, blocked, missing, or most relevant.
- For documents or pages, give a concise summary only when it is likely to help the user act on what is visible.
- If multiple questions, prompts, errors, or tasks are visible, identify each briefly before its answer. Use visible labels, question numbers, or short screen-position references.
- If the screenshot has no visible labels, create short references such as "top item", "middle item", "bottom item", "left item", or "right item".
- If the image is unclear or the answer is uncertain, use confidence low.
- Some questions have multiple parts or allow multiple selections. Make sure to give all answers.

Dropdown and blank handling:

- A single question or form prompt can contain multiple dropdowns, select boxes, blanks, or answer fields. Inspect the full sentence and count every visible answer slot.
- If only one dropdown is open, nearby dropdowns in the same prompt often use the same options.
- Empty dropdowns often look like a blank rectangle with a small down arrow or caret. Do not ignore them just because no option text is selected.
- If one dropdown already shows a selected value, still look for additional dropdowns or blanks in the same prompt.
- If a dropdown menu is open, its visible options belong to the active dropdown. Still inspect nearby closed dropdowns in the same sentence.
- For a prompt with multiple dropdowns or blanks, answer each slot separately in reading order. Name the slots as first dropdown, second dropdown, first blank, second blank, and so on.
- question_count is the number of distinct visible prompts, questions, or tasks being answered, not the number of dropdowns or blanks.

Return JSON only. Do not wrap it in markdown. Use exactly this shape:

{
  "question_found": true,
  "question_count": 1,
  "confidence": "high",
  "spoken_answer": "The next step is to reconnect the account.",
  "debug_summary": "Actionable account dialog detected."
}

Field rules:

- question_found: true when visible screen content merits a concise spoken answer or useful assistance.
- question_count: number of distinct visible prompts, questions, or tasks being answered, or 0 when none are visible. Use 1 for a single general screen-assist response.
- confidence: one of "low", "medium", or "high".
- spoken_answer: concise response to speak aloud, or "" when question_found is false. For multiple items, include a brief identifier for each item before its answer. For one item with multiple dropdowns or blanks, include a brief identifier for each answer slot.
- debug_summary: short non-secret summary for debugging. Do not include detailed reasoning.
`
