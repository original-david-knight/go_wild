package dockermgr

import (
	"testing"
)

func TestContainerName(t *testing.T) {
	tests := []struct {
		agentID  string
		expected string
	}{
		{"jake", "gowild-agent-jake"},
		{"alice", "gowild-agent-alice"},
		{"test-agent-1", "gowild-agent-test-agent-1"},
	}
	for _, tc := range tests {
		got := ContainerName(tc.agentID)
		if got != tc.expected {
			t.Errorf("ContainerName(%q) = %q, want %q", tc.agentID, got, tc.expected)
		}
	}
}

func TestVolumeName(t *testing.T) {
	tests := []struct {
		agentID  string
		expected string
	}{
		{"jake", "gowild-agent-jake-data"},
		{"alice", "gowild-agent-alice-data"},
	}
	for _, tc := range tests {
		got := VolumeName(tc.agentID)
		if got != tc.expected {
			t.Errorf("VolumeName(%q) = %q, want %q", tc.agentID, got, tc.expected)
		}
	}
}

func TestParseMemoryBytes(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		// Gigabytes
		{"1g", 1 * 1024 * 1024 * 1024},
		{"2g", 2 * 1024 * 1024 * 1024},
		{"4g", 4 * 1024 * 1024 * 1024},

		// Megabytes
		{"512m", 512 * 1024 * 1024},
		{"256m", 256 * 1024 * 1024},

		// Kilobytes
		{"1024k", 1024 * 1024},

		// Default (empty string -> 2g)
		{"", 2 * 1024 * 1024 * 1024},

		// Invalid -> default 2g
		{"invalid", 2 * 1024 * 1024 * 1024},

		// With whitespace
		{"  1g  ", 1 * 1024 * 1024 * 1024},

		// Case insensitive (already lowercased)
		{"1G", 1 * 1024 * 1024 * 1024},

		// Plain number (no suffix, treated as bytes)
		{"1073741824", 1073741824},
	}
	for _, tc := range tests {
		got := ParseMemoryBytes(tc.input)
		if got != tc.expected {
			t.Errorf("ParseMemoryBytes(%q) = %d, want %d", tc.input, got, tc.expected)
		}
	}
}

func TestParseCPUs(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"1", 1e9},
		{"2", 2e9},
		{"0.5", 5e8},
		{"4", 4e9},

		// Default (empty string -> 2 CPUs)
		{"", 2e9},

		// Invalid -> default 2 CPUs
		{"invalid", 2e9},

		// With whitespace
		{"  1  ", 1e9},
	}
	for _, tc := range tests {
		got := ParseCPUs(tc.input)
		if got != tc.expected {
			t.Errorf("ParseCPUs(%q) = %d, want %d", tc.input, got, tc.expected)
		}
	}
}

func TestShouldMountHostHome(t *testing.T) {
	t.Setenv("GOWILD_ALLOW_HOME_MOUNT", "")
	if shouldMountHostHome() {
		t.Fatalf("expected false when env var is empty")
	}

	for _, v := range []string{"1", "true", "TRUE", "yes", "on"} {
		t.Setenv("GOWILD_ALLOW_HOME_MOUNT", v)
		if !shouldMountHostHome() {
			t.Fatalf("expected true for value %q", v)
		}
	}

	for _, v := range []string{"0", "false", "no", "off", "random"} {
		t.Setenv("GOWILD_ALLOW_HOME_MOUNT", v)
		if shouldMountHostHome() {
			t.Fatalf("expected false for value %q", v)
		}
	}
}
