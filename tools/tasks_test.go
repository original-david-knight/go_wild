package tools

import (
	"testing"
)

func TestFormatIntervalForDisplay(t *testing.T) {
	tests := []struct {
		minutes  int
		expected string
	}{
		{0, "0m"},
		{30, "30m"},
		{59, "59m"},
		{60, "1h"},
		{90, "1h30m"},
		{120, "2h"},
		{1440, "1d"},
		{1500, "1d1h"},
		{1530, "1d1h30m"},
		{2880, "2d"},
		{2940, "2d1h"},
		{2970, "2d1h30m"},
	}

	for _, tc := range tests {
		got := FormatIntervalForDisplay(tc.minutes)
		if got != tc.expected {
			t.Errorf("FormatIntervalForDisplay(%d) = %q, want %q", tc.minutes, got, tc.expected)
		}
	}
}

func TestDetectImageMIME(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"photo.png", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"photo.gif", "image/gif"},
		{"photo.webp", "image/webp"},
		{"diagram.svg", "image/svg+xml"},
		{"PHOTO.PNG", "image/png"},
	}

	for _, tc := range tests {
		got := detectImageMIME(tc.path, nil)
		if got != tc.expected {
			t.Errorf("detectImageMIME(%q) = %q, want %q", tc.path, got, tc.expected)
		}
	}
}
