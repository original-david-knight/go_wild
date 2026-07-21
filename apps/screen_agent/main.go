package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"strings"
)

func main() {
	LoadGitRootEnv()
	LoadUserConfigEnv()
	os.Exit(realMain(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func realMain(args []string, getenv func(string) string, out, errOut io.Writer) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return runDaemonCommand(args, getenv, out, errOut)
	}
	command := args[0]
	if command == "-h" || command == "--help" || command == "help" {
		usage(out)
		return 0
	}

	if command == "daemon" {
		return runDaemonCommand(args[1:], getenv, out, errOut)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch command {
	case "assist", "fact-check", "summarize":
		if len(args) > 1 {
			printErr(errOut, "unexpected %s arguments: %s", command, joinArgs(args[1:]))
			return 2
		}
		intent := AssistIntentAuto
		switch command {
		case "fact-check":
			intent = AssistIntentFactCheck
		case "summarize":
			intent = AssistIntentEnglishSummary
		}
		if err := SendAssistIntent(ctx, getenv, intent); err != nil {
			printErr(errOut, "%v", err)
			return 1
		}
		return 0
	case "check":
		cfg, _, ok := loadCommandConfig("check", args[1:], getenv, errOut, func(fs *flag.FlagSet) {})
		if !ok {
			return 2
		}
		result := RunCheck(cfg, getenv, OSCommandRunner{Getenv: getenv}, out)
		if len(result.Errors) > 0 {
			return 1
		}
		return 0
	case "capture":
		var output string
		cfg, rest, ok := loadCommandConfig("capture", args[1:], getenv, errOut, func(fs *flag.FlagSet) {
			fs.StringVar(&output, "output", "", "output path for screenshot")
		})
		if !ok {
			return 2
		}
		if len(rest) > 0 {
			printErr(errOut, "unexpected capture arguments: %s", joinArgs(rest))
			return 2
		}
		app := NewApp(cfg, getenv, out, errOut)
		capture, err := app.Capture(ctx, output, output == "")
		if err != nil {
			printErr(errOut, "%v", err)
			return 1
		}
		fmt.Fprintln(out, capture.ImagePath)
		return 0
	case "analyze", "replay":
		var imagePath string
		var intentName string
		cfg, rest, ok := loadCommandConfig(command, args[1:], getenv, errOut, func(fs *flag.FlagSet) {
			fs.StringVar(&imagePath, "image", "", "image path to analyze")
			fs.StringVar(&intentName, "intent", "auto", "analysis intent: auto, fact-check, or english-summary")
		})
		if !ok {
			return 2
		}
		if imagePath == "" && len(rest) > 0 {
			imagePath = rest[0]
			rest = rest[1:]
		}
		if imagePath == "" {
			printErr(errOut, "analyze requires --image PATH")
			return 2
		}
		if len(rest) > 0 {
			printErr(errOut, "unexpected analyze arguments: %s", joinArgs(rest))
			return 2
		}
		intent, err := ParseAssistIntent(intentName)
		if err != nil {
			printErr(errOut, "%v", err)
			return 2
		}
		app := NewApp(cfg, getenv, out, errOut)
		if err := app.AnalyzeImageCommandWithIntent(ctx, imagePath, intent); err != nil {
			printErr(errOut, "%v", err)
			return 1
		}
		return 0
	case "speak":
		cfg, rest, ok := loadCommandConfig("speak", args[1:], getenv, errOut, func(fs *flag.FlagSet) {})
		if !ok {
			return 2
		}
		text := joinArgs(rest)
		if text == "" {
			printErr(errOut, "speak requires text")
			return 2
		}
		text = sanitizeSpeech(text, cfg.MaxSpokenChars)
		app := NewApp(cfg, getenv, out, errOut)
		if err := app.Speak(ctx, text); err != nil {
			printErr(errOut, "%v", err)
			return 1
		}
		return 0
	default:
		printErr(errOut, "unknown command %q", command)
		usage(errOut)
		return 2
	}
}

func runDaemonCommand(args []string, getenv func(string) string, out, errOut io.Writer) int {
	cfg, rest, ok := loadCommandConfig("daemon", args, getenv, errOut, func(fs *flag.FlagSet) {})
	if !ok {
		return 2
	}
	if len(rest) > 0 {
		printErr(errOut, "unexpected daemon arguments: %s", joinArgs(rest))
		return 2
	}
	if err := RunDaemon(context.Background(), cfg, getenv, out, errOut); err != nil {
		printErr(errOut, "%v", err)
		return 1
	}
	return 0
}

func loadCommandConfig(name string, args []string, getenv func(string) string, errOut io.Writer, addFlags func(*flag.FlagSet)) (Config, []string, bool) {
	fs := flag.NewFlagSet("screen-agent "+name, flag.ContinueOnError)
	fs.SetOutput(errOut)
	var configPath string
	var captureMode string
	var agentProvider string
	var agentModel string
	var agentMaxOutputTokens int
	var ttsModel string
	var ttsVoice string
	var ttsLanguageCode string
	var ttsPlayerCommand string
	var ttsPlaybackTimeout string
	var toneCommand string
	var toneInterval string
	var noQuestionSoundCommand string
	var hotkey string
	var debug bool
	var noTone bool
	var noQuestionSound bool
	var retain bool
	fs.StringVar(&configPath, "config", "", "config file path")
	fs.StringVar(&captureMode, "capture-mode", "", "capture mode")
	fs.StringVar(&agentProvider, "agent-provider", "", "agent provider")
	fs.StringVar(&agentModel, "agent-model", "", "agent model")
	fs.IntVar(&agentMaxOutputTokens, "agent-max-output-tokens", 0, "maximum model output tokens for analyzer JSON")
	fs.StringVar(&ttsModel, "tts-model", "", "Gemini TTS model")
	fs.StringVar(&ttsVoice, "tts-voice", "", "Gemini TTS prebuilt voice")
	fs.StringVar(&ttsLanguageCode, "tts-language-code", "", "Gemini TTS language code")
	fs.StringVar(&ttsPlayerCommand, "tts-player-command", "", "audio player for Gemini TTS WAV output")
	fs.StringVar(&ttsPlaybackTimeout, "tts-playback-timeout", "", "maximum time allowed for local TTS audio playback")
	fs.StringVar(&toneCommand, "tone-command", "", "thinking tone command")
	fs.StringVar(&toneInterval, "tone-interval", "", "thinking tone interval")
	fs.StringVar(&noQuestionSoundCommand, "no-question-sound-command", "", "sound command when no answerable screen content is found")
	fs.StringVar(&hotkey, "hotkey", "", "global hotkey for auto assist (Windows daemon only, e.g. ctrl+alt+a)")
	fs.BoolVar(&debug, "debug", false, "enable debug logging")
	fs.BoolVar(&noTone, "no-tone", false, "disable thinking tone")
	fs.BoolVar(&noQuestionSound, "no-question-sound", false, "disable sound when no answerable screen content is found")
	fs.BoolVar(&retain, "retain-debug-captures", false, "retain captures in debug capture directory")
	if addFlags != nil {
		addFlags(fs)
	}
	if err := fs.Parse(args); err != nil {
		return Config{}, nil, false
	}

	ov := ConfigOverride{
		CaptureMode:            captureMode,
		AgentProvider:          agentProvider,
		AgentModel:             agentModel,
		TTSModel:               ttsModel,
		TTSVoice:               ttsVoice,
		TTSLanguageCode:        ttsLanguageCode,
		TTSPlayerCommand:       ttsPlayerCommand,
		TTSPlaybackTimeout:     ttsPlaybackTimeout,
		ThinkingToneCommand:    toneCommand,
		ThinkingToneInterval:   toneInterval,
		NoQuestionSoundCommand: noQuestionSoundCommand,
		Hotkey:                 hotkey,
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "agent-max-output-tokens":
			ov.AgentMaxOutputTokens = int32(agentMaxOutputTokens)
		case "debug":
			ov.Debug = &debug
		case "retain-debug-captures":
			ov.RetainDebugCaptures = &retain
		case "no-tone":
			enabled := !noTone
			ov.ThinkingToneEnabled = &enabled
		case "no-question-sound":
			enabled := !noQuestionSound
			ov.NoQuestionSoundEnabled = &enabled
		}
	})

	cfg, err := LoadConfig(configPath, getenv, ov)
	if err != nil {
		printErr(errOut, "%v", err)
		return Config{}, nil, false
	}
	return cfg, fs.Args(), true
}

func envMap(values map[string]string) func(string) string {
	return func(key string) string {
		if v, ok := values[key]; ok {
			return v
		}
		return os.Getenv(key)
	}
}

func splitArgsForTest(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts, err := splitCommandLine(raw)
	if err != nil {
		return strings.Fields(raw)
	}
	return parts
}
