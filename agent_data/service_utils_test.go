package data

import "testing"

func TestCapitalizeFirst(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"", ""},
		{"hello", "Hello"},
		{"Hello", "Hello"},
		{"a", "A"},
		{"ABC", "ABC"},
	}
	for _, tc := range tests {
		got := capitalizeFirst(tc.input)
		if got != tc.expected {
			t.Errorf("capitalizeFirst(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestToLower(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"", ""},
		{"hello", "hello"},
		{"HELLO", "hello"},
		{"HeLLo WoRLd", "hello world"},
	}
	for _, tc := range tests {
		got := toLower(tc.input)
		if got != tc.expected {
			t.Errorf("toLower(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		s, substr string
		expected  bool
	}{
		{"hello world", "world", true},
		{"hello world", "xyz", false},
		{"hello", "", true},
		{"", "a", false},
		{"abc", "abcd", false},
	}
	for _, tc := range tests {
		got := contains(tc.s, tc.substr)
		if got != tc.expected {
			t.Errorf("contains(%q, %q) = %v, want %v", tc.s, tc.substr, got, tc.expected)
		}
	}
}
