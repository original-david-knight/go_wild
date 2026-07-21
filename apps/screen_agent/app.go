package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type App struct {
	Config Config
	Runner CommandRunner
	Getenv func(string) string
	Out    io.Writer
	Err    io.Writer
	Logger DebugLogger
}

func NewApp(cfg Config, getenv func(string) string, out, errOut io.Writer) *App {
	if getenv == nil {
		getenv = os.Getenv
	}
	if out == nil {
		out = io.Discard
	}
	if errOut == nil {
		errOut = io.Discard
	}
	logger := DebugLogger{Enabled: cfg.Debug, Out: errOut}
	return &App{
		Config: cfg,
		Runner: OSCommandRunner{Getenv: getenv},
		Getenv: getenv,
		Out:    out,
		Err:    errOut,
		Logger: logger,
	}
}

func (a *App) Capture(ctx context.Context, output string, retain bool) (CaptureResult, error) {
	svc := NewCaptureService(a.Config, a.Runner, a.Getenv, a.Logger)
	return svc.Capture(ctx, a.Config.CaptureMode, CaptureOptions{
		OutputPath: output,
		Retain:     retain,
	})
}

func (a *App) Analyze(ctx context.Context, input AnalyzeInput) (AnalyzeResult, error) {
	intent, err := ParseAssistIntent(string(input.Intent))
	if err != nil {
		return AnalyzeResult{}, err
	}
	if err := validateIntentProvider(intent, a.Config.AgentProvider); err != nil {
		return AnalyzeResult{}, err
	}
	input.Intent = intent
	ctx, cancel := context.WithTimeout(ctx, a.Config.AgentTimeout)
	defer cancel()
	analyzer, err := NewAnalyzer(ctx, a.Config, a.Logger)
	if err != nil {
		return AnalyzeResult{}, err
	}
	result, err := analyzer.Analyze(ctx, input)
	if closer, ok := analyzer.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
	return result, err
}

func (a *App) Speak(ctx context.Context, text string) error {
	speaker, err := NewGeminiSpeaker(ctx, a.Config, a.Runner, a.Getenv, a.Logger)
	if err != nil {
		return err
	}
	return speaker.Speak(ctx, text)
}

func (a *App) HandleHotkey(ctx context.Context) error {
	return a.HandleIntent(ctx, AssistIntentAuto)
}

func (a *App) HandleIntent(ctx context.Context, intent AssistIntent) error {
	intent, err := ParseAssistIntent(string(intent))
	if err != nil {
		return err
	}
	if err := validateIntentProvider(intent, a.Config.AgentProvider); err != nil {
		return err
	}
	a.Logger.Printf("assist event received: intent=%s", intent)
	capture, err := a.Capture(ctx, "", a.Config.RetainDebugCaptures)
	if err != nil {
		return err
	}
	defer func() {
		if capture.Cleanup != nil {
			if err := capture.Cleanup(); err != nil {
				a.Logger.Printf("cleanup failed: %v", err)
			}
		}
	}()
	a.Logger.Printf("capture saved: %s", capture.ImagePath)

	stopTone := ToneService{Config: a.Config, Runner: a.Runner, Logger: a.Logger}.Start(ctx)
	toneActive := true
	stopThinkingTone := func() {
		if !toneActive {
			return
		}
		stopTone()
		toneActive = false
	}
	defer stopThinkingTone()

	result, err := a.Analyze(ctx, AnalyzeInput{
		ImagePath:    capture.ImagePath,
		Intent:       intent,
		CaptureMode:  string(capture.Mode),
		CapturedAt:   capture.CapturedAt,
		ActiveTitle:  capture.ActiveTitle,
		ActiveClass:  capture.ActiveClass,
		ScreenWidth:  capture.Width,
		ScreenHeight: capture.Height,
	})
	if err != nil {
		return err
	}
	a.Logger.Printf("analysis result: found=%v count=%d confidence=%s", result.QuestionFound, result.QuestionCount, result.Confidence)

	text, ok, reason := SpokenTextForIntent(result, a.Config, intent)
	if !ok {
		a.Logger.Printf("speech suppressed: %s", reason)
		if !result.QuestionFound {
			stopThinkingTone()
			ToneService{Config: a.Config, Runner: a.Runner, Logger: a.Logger}.PlayNoQuestion(ctx)
		}
		return nil
	}

	speaker, err := NewGeminiSpeaker(ctx, a.Config, a.Runner, a.Getenv, a.Logger)
	if err != nil {
		return err
	}
	prepared, err := speaker.Prepare(ctx, text)
	if err != nil {
		return err
	}
	if prepared.Cleanup != nil {
		defer prepared.Cleanup()
	}
	stopThinkingTone()
	if prepared.Path == "" {
		return nil
	}
	return speaker.Play(ctx, prepared.Path)
}

func (a *App) AnalyzeImageCommand(ctx context.Context, imagePath string) error {
	return a.AnalyzeImageCommandWithIntent(ctx, imagePath, AssistIntentAuto)
}

func (a *App) AnalyzeImageCommandWithIntent(ctx context.Context, imagePath string, intent AssistIntent) error {
	imagePath = expandPath(imagePath)
	result, err := a.Analyze(ctx, AnalyzeInput{
		ImagePath:   imagePath,
		Intent:      intent,
		CaptureMode: "file",
	})
	if err != nil {
		return err
	}
	enc := json.NewEncoder(a.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func (a *App) SpeakResult(ctx context.Context, result AnalyzeResult) error {
	text, ok, reason := SpokenText(result, a.Config)
	if !ok {
		a.Logger.Printf("speech suppressed: %s", reason)
		return nil
	}
	return a.Speak(ctx, text)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage: screen-agent [command] [flags]")
	fmt.Fprintln(w, "Run without a command to start the hotkey listener.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  daemon        Listen for key-combo signals and assist commands")
	fmt.Fprintln(w, "  assist        Trigger auto assist on the running daemon")
	fmt.Fprintln(w, "  fact-check    Fact-check the main visible claim using grounded web search")
	fmt.Fprintln(w, "  summarize     Summarize visible content in English, in any source language")
	fmt.Fprintln(w, "  check         Verify dependencies and config")
	fmt.Fprintln(w, "  capture       Capture a screenshot and print the output path")
	fmt.Fprintln(w, "  analyze       Analyze an existing image file")
	fmt.Fprintln(w, "  speak         Speak text through Gemini TTS")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Common flags:")
	fmt.Fprintln(w, "  --config PATH")
	fmt.Fprintln(w, "  --debug")
	fmt.Fprintln(w, "  --capture-mode full|monitor|window|region")
	fmt.Fprintln(w, "  --intent auto|fact-check|english-summary  (analyze/replay only)")
	fmt.Fprintln(w, "  --agent-provider openai|gemini|anthropic|fake")
	fmt.Fprintln(w, "  --agent-model MODEL")
	fmt.Fprintln(w, "  --agent-max-output-tokens N")
	fmt.Fprintln(w, "  --tts-model MODEL")
	fmt.Fprintln(w, "  --tts-voice VOICE")
	fmt.Fprintln(w, "  --tts-language-code CODE")
	fmt.Fprintln(w, "  --tts-player-command COMMAND")
	fmt.Fprintln(w, "  --tts-playback-timeout DURATION")
	fmt.Fprintln(w, "  --no-question-sound-command COMMAND")
	fmt.Fprintln(w, "  --hotkey COMBO  (Windows daemon only, e.g. ctrl+alt+a)")
	fmt.Fprintln(w, "  --no-tone")
	fmt.Fprintln(w, "  --no-question-sound")
}

func printErr(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, "screen-agent: "+format+"\n", args...)
}

func joinArgs(args []string) string {
	return strings.TrimSpace(strings.Join(args, " "))
}
