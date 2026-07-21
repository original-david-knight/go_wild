package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
)

func RunDaemon(ctx context.Context, cfg Config, getenv func(string) string, out, errOut io.Writer) error {
	if getenv == nil {
		getenv = os.Getenv
	}
	if out == nil {
		out = io.Discard
	}
	if errOut == nil {
		errOut = io.Discard
	}

	runtimeDir := RuntimeDir(getenv)
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	daemonCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	control, err := StartControlServer(daemonCtx, getenv)
	if err != nil {
		return err
	}
	defer control.Close()
	pidPath := PIDFilePath(getenv)
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer os.Remove(pidPath)

	app := NewApp(cfg, getenv, out, errOut)

	hotkeys, stopHotkeys, err := StartHotkeyListener(cfg, DebugLogger{Enabled: cfg.Debug, Out: errOut})
	if err != nil {
		return err
	}
	defer stopHotkeys()

	description := daemonSignalDescription()
	if strings.TrimSpace(cfg.Hotkey) != "" {
		description += fmt.Sprintf("; global hotkey %q", cfg.Hotkey)
	}
	app.Logger.Printf("daemon %s and commands; pid file: %s; socket: %s", description, pidPath, ControlSocketPath(getenv))

	signals := make(chan os.Signal, 8)
	signal.Notify(signals, daemonSignals()...)
	defer signal.Stop(signals)

	jobs := &hotkeyJobController{
		Logger: app.Logger,
		Err:    errOut,
	}

	startAssistJob := func(intent AssistIntent) {
		jobs.Start(daemonCtx, func(jobCtx context.Context) error {
			return app.HandleIntent(jobCtx, intent)
		})
	}
	shutdown := func() {
		cancel()
		_ = control.Close()
		jobs.CancelAndWait()
	}

	for {
		select {
		case <-ctx.Done():
			shutdown()
			return nil
		case sig := <-signals:
			switch {
			case isHotkeySignal(sig):
				startAssistJob(AssistIntentAuto)
			case isShutdownSignal(sig):
				shutdown()
				return nil
			}
		case <-hotkeys:
			startAssistJob(AssistIntentAuto)
		case dispatch := <-control.Requests():
			if err := validateIntentProvider(dispatch.Intent, cfg.AgentProvider); err != nil {
				dispatch.respond(err)
				continue
			}
			if daemonCtx.Err() != nil {
				dispatch.respond(fmt.Errorf("screen-agent daemon is stopping"))
				continue
			}
			startAssistJob(dispatch.Intent)
			dispatch.respond(nil)
		case err := <-control.Errors():
			shutdown()
			return fmt.Errorf("control socket: %w", err)
		}
	}
}

func PIDFilePath(getenv func(string) string) string {
	return filepath.Join(RuntimeDir(getenv), "screen-agent.pid")
}

type hotkeyJobController struct {
	Logger DebugLogger
	Err    io.Writer

	mu           sync.Mutex
	wg           sync.WaitGroup
	activeCancel context.CancelFunc
	activeID     uint64
}

func (c *hotkeyJobController) Start(parent context.Context, run func(context.Context) error) {
	var previousCancel context.CancelFunc

	c.mu.Lock()
	if c.activeCancel != nil {
		previousCancel = c.activeCancel
		c.Logger.Printf("hotkey received while processing; canceling previous analysis")
	}
	jobCtx, cancel := context.WithCancel(parent)
	c.activeID++
	jobID := c.activeID
	c.activeCancel = cancel
	c.wg.Add(1)
	c.mu.Unlock()

	if previousCancel != nil {
		previousCancel()
	}

	go func() {
		defer c.wg.Done()
		if err := run(jobCtx); err != nil {
			if jobCtx.Err() != nil {
				c.Logger.Printf("abandoned hotkey job stopped: %v", err)
			} else if c.Err != nil {
				fmt.Fprintf(c.Err, "screen-agent: %v\n", err)
			}
		}

		c.mu.Lock()
		if c.activeID == jobID {
			c.activeCancel = nil
		}
		c.mu.Unlock()
	}()
}

func (c *hotkeyJobController) CancelAndWait() {
	c.mu.Lock()
	cancel := c.activeCancel
	c.activeCancel = nil
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	c.wg.Wait()
}
