package gowild_ytmusic

import (
	"encoding/json"
	"testing"
)

// tree decodes a JSON document the way browse responses arrive, so nav tests
// exercise the exact runtime types (map[string]any, []any, float64).
func tree(t *testing.T, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("test JSON: %v", err)
	}
	return v
}

func TestNav(t *testing.T) {
	v := tree(t, `{"contents": {"tabs": [{"title": "Library", "count": 42}, {"title": "Other"}]}}`)

	got, ok := nav(v, "contents", "tabs", 1, "title")
	if !ok || got != "Other" {
		t.Fatalf("nav deep path = %v, %v; want Other, true", got, ok)
	}
	if got, ok := nav(v); !ok || got == nil {
		t.Fatalf("nav empty path should return the value itself, got %v, %v", got, ok)
	}

	misses := [][]any{
		{"missing"},                                // absent key
		{"contents", "tabs", 2},                    // index out of range
		{"contents", "tabs", -1},                   // negative index
		{"contents", 0},                            // int step into a map
		{"contents", "tabs", "title"},              // string step into a slice
		{"contents", "tabs", 0, "title", "deeper"}, // step into a scalar
		{"contents", 1.5},                          // unsupported path element type
	}
	for _, path := range misses {
		if got, ok := nav(v, path...); ok {
			t.Errorf("nav(%v) = %v, true; want miss", path, got)
		}
	}
}

func TestNavTypedWrappers(t *testing.T) {
	v := tree(t, `{"title": "Mix", "count": 42, "items": [1, 2], "obj": {"k": "v"}}`)

	if s, ok := navString(v, "title"); !ok || s != "Mix" {
		t.Errorf("navString = %q, %v; want Mix, true", s, ok)
	}
	if _, ok := navString(v, "count"); ok {
		t.Error("navString on a number should miss")
	}
	if _, ok := navString(v, "absent"); ok {
		t.Error("navString on an absent key should miss")
	}

	if n, ok := navInt(v, "count"); !ok || n != 42 {
		t.Errorf("navInt = %d, %v; want 42, true", n, ok)
	}
	if _, ok := navInt(v, "title"); ok {
		t.Error("navInt on a string should miss")
	}

	if s, ok := navSlice(v, "items"); !ok || len(s) != 2 {
		t.Errorf("navSlice = %v, %v; want 2 elements, true", s, ok)
	}
	if _, ok := navSlice(v, "obj"); ok {
		t.Error("navSlice on a map should miss")
	}

	if m, ok := navMap(v, "obj"); !ok || m["k"] != "v" {
		t.Errorf("navMap = %v, %v; want {k: v}, true", m, ok)
	}
	if _, ok := navMap(v, "items"); ok {
		t.Error("navMap on a slice should miss")
	}
}

func TestParseDurationText(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"3:25", 205},
		{"1:02:11", 3731},
		{"0:45", 45},
		{"45", 45},
		{" 3:25 ", 205},
		{"10:00:00", 36000},
		{"", 0},
		{"abc", 0},
		{"3:xx", 0},
		{"3:", 0},
		{":25", 0},
		{"-3:25", 0},
		{"1:2:3:4", 0},
	}
	for _, c := range cases {
		if got := parseDurationText(c.in); got != c.want {
			t.Errorf("parseDurationText(%q) = %d; want %d", c.in, got, c.want)
		}
	}
}
