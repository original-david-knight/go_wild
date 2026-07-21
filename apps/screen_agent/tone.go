package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type ToneService struct {
	Config Config
	Runner CommandRunner
	Logger DebugLogger
}

func (s ToneService) Start(ctx context.Context) func() {
	if !s.Config.ThinkingToneEnabled {
		return func() {}
	}
	runner := s.Runner
	if runner == nil {
		runner = OSCommandRunner{}
	}
	spec, ok, err := ResolveToneCommand(s.Config.ThinkingToneCommand, runner)
	if err != nil {
		s.Logger.Printf("thinking tone disabled: %v", err)
		return func() {}
	}
	if !ok {
		s.Logger.Printf("thinking tone disabled: no usable auto tone command found")
		return func() {}
	}

	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(s.Config.ThinkingToneInterval)
		defer ticker.Stop()
		s.play(ctx, runner, spec)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.play(ctx, runner, spec)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func (s ToneService) play(ctx context.Context, runner CommandRunner, spec CommandSpec) {
	toneCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := runner.Run(toneCtx, spec.Name, spec.Args...); err != nil {
		if ctx.Err() != nil || toneCtx.Err() == context.Canceled {
			return
		}
		s.Logger.Printf("thinking tone failed: %v", err)
	}
}

func (s ToneService) PlayNoQuestion(ctx context.Context) {
	if !s.Config.NoQuestionSoundEnabled {
		return
	}
	runner := s.Runner
	if runner == nil {
		runner = OSCommandRunner{}
	}
	spec, ok, err := ResolveNoQuestionSoundCommand(s.Config.NoQuestionSoundCommand, runner)
	if err != nil {
		s.Logger.Printf("no-question sound disabled: %v", err)
		return
	}
	if !ok {
		s.Logger.Printf("no-question sound disabled: no usable auto sound command found")
		return
	}
	soundCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := runner.Run(soundCtx, spec.Name, spec.Args...); err != nil {
		if ctx.Err() != nil || soundCtx.Err() == context.Canceled {
			return
		}
		s.Logger.Printf("no-question sound failed: %v", err)
	}
}

func ResolveToneCommand(raw string, runner CommandRunner) (CommandSpec, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "none") || strings.EqualFold(raw, "off") {
		return CommandSpec{}, false, nil
	}
	if !strings.EqualFold(raw, "auto") {
		spec, err := ParseCommandSpec(raw)
		if err != nil {
			return CommandSpec{}, false, fmt.Errorf("thinking_tone_command: %w", err)
		}
		if _, err := runner.LookPath(spec.Name); err != nil {
			return CommandSpec{}, false, fmt.Errorf("thinking tone command %q not found", spec.Name)
		}
		return spec, true, nil
	}

	spec, ok := autoToneCommand(runner)
	return spec, ok, nil
}

func ResolveNoQuestionSoundCommand(raw string, runner CommandRunner) (CommandSpec, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "none") || strings.EqualFold(raw, "off") {
		return CommandSpec{}, false, nil
	}
	if runner == nil {
		runner = OSCommandRunner{}
	}
	if !strings.EqualFold(raw, "auto") {
		spec, err := ParseCommandSpec(raw)
		if err != nil {
			return CommandSpec{}, false, fmt.Errorf("no_question_sound_command: %w", err)
		}
		if _, err := runner.LookPath(spec.Name); err != nil {
			return CommandSpec{}, false, fmt.Errorf("no-question sound command %q not found", spec.Name)
		}
		return spec, true, nil
	}

	spec, ok := autoNoQuestionSoundCommand(runner)
	return spec, ok, nil
}
