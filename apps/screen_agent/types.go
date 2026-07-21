package main

import (
	"context"
	"time"
)

type CaptureMode string

const (
	CaptureModeFull    CaptureMode = "full"
	CaptureModeMonitor CaptureMode = "monitor"
	CaptureModeWindow  CaptureMode = "window"
	CaptureModeRegion  CaptureMode = "region"
)

type Confidence string

const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

type CaptureResult struct {
	ImagePath   string       `json:"image_path"`
	Mode        CaptureMode  `json:"capture_mode"`
	Width       int          `json:"width"`
	Height      int          `json:"height"`
	ActiveTitle string       `json:"active_title,omitempty"`
	ActiveClass string       `json:"active_class,omitempty"`
	CapturedAt  time.Time    `json:"captured_at"`
	Cleanup     func() error `json:"-"`
}

type AnalyzeInput struct {
	ImagePath    string       `json:"image_path"`
	Intent       AssistIntent `json:"intent,omitempty"`
	CaptureMode  string       `json:"capture_mode"`
	CapturedAt   time.Time    `json:"captured_at"`
	ActiveTitle  string       `json:"active_title,omitempty"`
	ActiveClass  string       `json:"active_class,omitempty"`
	ScreenWidth  int          `json:"screen_width,omitempty"`
	ScreenHeight int          `json:"screen_height,omitempty"`
}

type AnalyzeResult struct {
	QuestionFound bool       `json:"question_found"`
	QuestionCount int        `json:"question_count"`
	Confidence    Confidence `json:"confidence"`
	SpokenAnswer  string     `json:"spoken_answer"`
	DebugSummary  string     `json:"debug_summary,omitempty"`
	Sources       []string   `json:"sources,omitempty"`
}

type CaptureService interface {
	Capture(ctx context.Context, mode CaptureMode, opts CaptureOptions) (CaptureResult, error)
}

type CaptureOptions struct {
	OutputPath string
	Retain     bool
}

type Analyzer interface {
	Analyze(ctx context.Context, input AnalyzeInput) (AnalyzeResult, error)
}

type Speaker interface {
	Speak(ctx context.Context, text string) error
}
