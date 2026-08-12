package gowild_ytmusic

import "strings"

// nav walks a decoded JSON value: string path elements index maps, int
// elements index slices. It returns false the moment a step misses — wrong
// container type, absent key, index out of range — because InnerTube renderer
// trees vary and callers must branch on presence, not panic.
func nav(v any, path ...any) (any, bool) {
	for _, step := range path {
		switch step := step.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				return nil, false
			}
			v, ok = m[step]
			if !ok {
				return nil, false
			}
		case int:
			s, ok := v.([]any)
			if !ok || step < 0 || step >= len(s) {
				return nil, false
			}
			v = s[step]
		default:
			return nil, false
		}
	}
	return v, true
}

func navString(v any, path ...any) (string, bool) {
	got, ok := nav(v, path...)
	if !ok {
		return "", false
	}
	s, ok := got.(string)
	return s, ok
}

// navInt reads a JSON number. encoding/json decodes every number into
// float64, so that is the type accepted.
func navInt(v any, path ...any) (int, bool) {
	got, ok := nav(v, path...)
	if !ok {
		return 0, false
	}
	f, ok := got.(float64)
	if !ok {
		return 0, false
	}
	return int(f), true
}

func navSlice(v any, path ...any) ([]any, bool) {
	got, ok := nav(v, path...)
	if !ok {
		return nil, false
	}
	s, ok := got.([]any)
	return s, ok
}

func navMap(v any, path ...any) (map[string]any, bool) {
	got, ok := nav(v, path...)
	if !ok {
		return nil, false
	}
	m, ok := got.(map[string]any)
	return m, ok
}

// parseDurationText converts an InnerTube duration display string — "3:25",
// "1:02:11" — to whole seconds. Unreadable text is 0, not an error: a missing
// duration does not make the item itself worthless.
func parseDurationText(text string) int {
	parts := strings.Split(strings.TrimSpace(text), ":")
	if len(parts) == 0 || len(parts) > 3 {
		return 0
	}
	total := 0
	for _, part := range parts {
		if part == "" {
			return 0
		}
		n := 0
		for _, r := range part {
			if r < '0' || r > '9' {
				return 0
			}
			n = n*10 + int(r-'0')
		}
		total = total*60 + n
	}
	return total
}
