package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type GrimCaptureService struct {
	Config Config
	Runner CommandRunner
	Getenv func(string) string
	Logger DebugLogger
	Now    func() time.Time
}

type geometry struct {
	X int
	Y int
	W int
	H int
}

func (g geometry) valid() bool {
	return g.W > 0 && g.H > 0
}

func (g geometry) String() string {
	return fmt.Sprintf("%d,%d %dx%d", g.X, g.Y, g.W, g.H)
}

func (s GrimCaptureService) Capture(ctx context.Context, mode CaptureMode, opts CaptureOptions) (CaptureResult, error) {
	runner := s.Runner
	if runner == nil {
		runner = OSCommandRunner{Getenv: s.Getenv}
	}
	now := s.Now
	if now == nil {
		now = time.Now
	}

	ctx, cancel := context.WithTimeout(ctx, s.Config.CaptureTimeout)
	defer cancel()

	active, activeErr := s.activeWindow(ctx, runner)
	if activeErr != nil {
		s.Logger.Printf("active window metadata unavailable: %v", activeErr)
	}

	var geom geometry
	var selectedRegion string
	switch mode {
	case CaptureModeMonitor:
		g, err := s.focusedMonitorGeometry(ctx, runner)
		if err != nil {
			return CaptureResult{}, fmt.Errorf("focused monitor geometry unavailable: %w", err)
		}
		if !g.valid() {
			return CaptureResult{}, fmt.Errorf("focused monitor geometry unavailable: focused monitor has invalid size")
		}
		geom = g
	case CaptureModeWindow:
		if activeErr != nil {
			return CaptureResult{}, fmt.Errorf("active window geometry unavailable: %w", activeErr)
		}
		if !active.Geometry.valid() {
			return CaptureResult{}, fmt.Errorf("active window geometry unavailable: active window has invalid size")
		}
		geom = active.Geometry
	case CaptureModeRegion:
		out, err := runner.Run(ctx, "slurp")
		if err != nil {
			return CaptureResult{}, fmt.Errorf("select region with slurp: %w", err)
		}
		selectedRegion = strings.TrimSpace(string(out))
		if selectedRegion == "" {
			return CaptureResult{}, fmt.Errorf("slurp returned empty geometry")
		}
	}

	path, cleanup, err := resolveCapturePath(s.Config, s.Getenv, opts, now())
	if err != nil {
		return CaptureResult{}, err
	}
	if selectedRegion != "" {
		if _, err := runner.Run(ctx, "grim", "-g", selectedRegion, path); err != nil {
			_ = cleanup()
			return CaptureResult{}, fmt.Errorf("capture selected region: %w", err)
		}
		w, h := decodeImageSize(path)
		return CaptureResult{
			ImagePath:   path,
			Mode:        mode,
			Width:       w,
			Height:      h,
			ActiveTitle: active.Title,
			ActiveClass: active.Class,
			CapturedAt:  now(),
			Cleanup:     cleanup,
		}, nil
	}

	args := []string{}
	if geom.valid() {
		args = append(args, "-g", geom.String())
	}
	args = append(args, path)
	s.Logger.Printf("capture command: grim %s", strings.Join(args, " "))
	if _, err := runner.Run(ctx, "grim", args...); err != nil {
		_ = cleanup()
		return CaptureResult{}, fmt.Errorf("capture screenshot: %w", err)
	}

	w, h := decodeImageSize(path)
	if w == 0 && geom.valid() {
		w = geom.W
		h = geom.H
	}
	return CaptureResult{
		ImagePath:   path,
		Mode:        mode,
		Width:       w,
		Height:      h,
		ActiveTitle: active.Title,
		ActiveClass: active.Class,
		CapturedAt:  now(),
		Cleanup:     cleanup,
	}, nil
}

func resolveCapturePath(cfg Config, getenv func(string) string, opts CaptureOptions, now time.Time) (string, func() error, error) {
	if strings.TrimSpace(opts.OutputPath) != "" {
		path := expandPath(opts.OutputPath)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", nil, fmt.Errorf("create output directory: %w", err)
		}
		return path, func() error { return nil }, nil
	}

	dir := RuntimeDir(getenv)
	retain := opts.Retain || cfg.RetainDebugCaptures
	if retain {
		dir = cfg.DebugCaptureDir
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, fmt.Errorf("create capture directory: %w", err)
	}
	name := fmt.Sprintf("capture-%s.png", now.UTC().Format("20060102T150405.000000000Z"))
	path := filepath.Join(dir, name)
	if retain {
		return path, func() error { return nil }, nil
	}
	return path, func() error {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}, nil
}

type hyprMonitor struct {
	X       int  `json:"x"`
	Y       int  `json:"y"`
	Width   int  `json:"width"`
	Height  int  `json:"height"`
	Focused bool `json:"focused"`
}

func (s GrimCaptureService) focusedMonitorGeometry(ctx context.Context, runner CommandRunner) (geometry, error) {
	out, err := runner.Run(ctx, "hyprctl", "-j", "monitors")
	if err != nil {
		return geometry{}, err
	}
	return parseFocusedMonitorGeometry(out)
}

func parseFocusedMonitorGeometry(data []byte) (geometry, error) {
	var monitors []hyprMonitor
	if err := json.Unmarshal(data, &monitors); err != nil {
		return geometry{}, fmt.Errorf("parse hyprctl monitors: %w", err)
	}
	for _, m := range monitors {
		if m.Focused {
			g := geometry{X: m.X, Y: m.Y, W: m.Width, H: m.Height}
			if !g.valid() {
				return geometry{}, fmt.Errorf("focused monitor has invalid size")
			}
			return g, nil
		}
	}
	return geometry{}, fmt.Errorf("no focused monitor found")
}

type activeWindow struct {
	Title    string
	Class    string
	Geometry geometry
}

type hyprActiveWindow struct {
	Title string `json:"title"`
	Class string `json:"class"`
	At    []int  `json:"at"`
	Size  []int  `json:"size"`
}

func (s GrimCaptureService) activeWindow(ctx context.Context, runner CommandRunner) (activeWindow, error) {
	out, err := runner.Run(ctx, "hyprctl", "-j", "activewindow")
	if err != nil {
		return activeWindow{}, err
	}
	active, err := parseActiveWindow(out)
	if err != nil {
		return activeWindow{}, err
	}
	return active, nil
}

func parseActiveWindow(data []byte) (activeWindow, error) {
	var raw hyprActiveWindow
	if err := json.Unmarshal(data, &raw); err != nil {
		return activeWindow{}, fmt.Errorf("parse hyprctl activewindow: %w", err)
	}
	var g geometry
	if len(raw.At) >= 2 && len(raw.Size) >= 2 {
		g = geometry{X: raw.At[0], Y: raw.At[1], W: raw.Size[0], H: raw.Size[1]}
	}
	return activeWindow{
		Title:    raw.Title,
		Class:    raw.Class,
		Geometry: g,
	}, nil
}

func decodeImageSize(path string) (int, int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}
