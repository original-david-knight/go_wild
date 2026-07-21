package tools

import (
	"testing"
)

func TestToPythonLiteral_String(t *testing.T) {
	tests := []struct {
		input    any
		expected string
	}{
		{"hello", `"hello"`},
		{"it's a \"test\"", `"it's a \"test\""`},
		{"line1\nline2", `"line1\nline2"`},
		{"tab\there", `"tab\there"`},
		{"back\\slash", `"back\\slash"`},
		{"", `""`},
	}

	for _, tc := range tests {
		got, err := toPythonLiteral(tc.input)
		if err != nil {
			t.Errorf("toPythonLiteral(%q) error: %v", tc.input, err)
			continue
		}
		if got != tc.expected {
			t.Errorf("toPythonLiteral(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestToPythonLiteral_Bool(t *testing.T) {
	got, _ := toPythonLiteral(true)
	if got != "True" {
		t.Errorf("expected True, got %q", got)
	}

	got, _ = toPythonLiteral(false)
	if got != "False" {
		t.Errorf("expected False, got %q", got)
	}
}

func TestToPythonLiteral_Numbers(t *testing.T) {
	tests := []struct {
		input    any
		expected string
	}{
		{42, "42"},
		{int64(100), "100"},
		{3.14, "3.14"},
		{float32(2.5), "2.5"},
	}

	for _, tc := range tests {
		got, err := toPythonLiteral(tc.input)
		if err != nil {
			t.Errorf("toPythonLiteral(%v) error: %v", tc.input, err)
			continue
		}
		if got != tc.expected {
			t.Errorf("toPythonLiteral(%v) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestToPythonLiteral_Nil(t *testing.T) {
	got, err := toPythonLiteral(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "None" {
		t.Errorf("expected None, got %q", got)
	}
}

func TestToPythonLiteral_Slice(t *testing.T) {
	input := []any{"a", "b", "c"}
	got, err := toPythonLiteral(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != `["a", "b", "c"]` {
		t.Errorf("got %q", got)
	}
}

func TestToPythonLiteral_EmptySlice(t *testing.T) {
	got, err := toPythonLiteral([]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "[]" {
		t.Errorf("expected [], got %q", got)
	}
}

func TestToPythonLiteral_NestedSlice(t *testing.T) {
	input := []any{[]any{1, 2}, []any{3, 4}}
	got, err := toPythonLiteral(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "[[1, 2], [3, 4]]" {
		t.Errorf("got %q", got)
	}
}

func TestToPythonLiteral_Map(t *testing.T) {
	// Single-key map for deterministic output
	input := map[string]any{"key": "value"}
	got, err := toPythonLiteral(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != `{"key": "value"}` {
		t.Errorf("got %q", got)
	}
}

func TestToPythonLiteral_MapMixed(t *testing.T) {
	input := map[string]any{"name": "test"}
	got, err := toPythonLiteral(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != `{"name": "test"}` {
		t.Errorf("got %q", got)
	}
}

func TestToAnySlice(t *testing.T) {
	input := []string{"a", "b", "c"}
	got := toAnySlice(input)
	if len(got) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(got))
	}
	for i, v := range got {
		if v.(string) != input[i] {
			t.Errorf("element %d: expected %q, got %q", i, input[i], v)
		}
	}
}

func TestToAnySlice_Empty(t *testing.T) {
	got := toAnySlice([]string{})
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %d elements", len(got))
	}
}

func TestNewTaskTools_NilService(t *testing.T) {
	tools := NewTaskTools(nil)
	if tools != nil {
		t.Error("expected nil for nil service")
	}
}

func TestNewContentTools(t *testing.T) {
	ct := NewContentTools(nil)
	if ct == nil {
		t.Error("expected non-nil ContentTools")
	}
}

