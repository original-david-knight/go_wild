package deepresearch

import "testing"

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "already valid JSON",
			input: `{"queries": []}`,
			want:  `{"queries": []}`,
		},
		{
			name:  "json followed by prose",
			input: "{\"results\": []}\nSearch completed with no additional notes.",
			want:  `{"results": []}`,
		},
		{
			name:  "markdown fences",
			input: "```json\n{\"queries\": []}\n```",
			want:  `{"queries": []}`,
		},
		{
			name:  "prose before JSON",
			input: "Research shows the following results:\n{\"queries\": [{\"query\": \"test\"}]}",
			want:  `{"queries": [{"query": "test"}]}`,
		},
		{
			name:  "prose before and after JSON",
			input: "No problem, here is the JSON:\n{\"complete\": true}\nLet me know if you need more.",
			want:  `{"complete": true}`,
		},
		{
			name:  "JSON array",
			input: `[{"url": "https://example.com"}]`,
			want:  `[{"url": "https://example.com"}]`,
		},
		{
			name:  "json array followed by prose",
			input: "[{\"url\": \"https://example.com\"}]\nSearch summary follows.",
			want:  `[{"url": "https://example.com"}]`,
		},
		{
			name:  "prose with array",
			input: "Here are the results:\n[{\"url\": \"https://example.com\"}]\nDone.",
			want:  `[{"url": "https://example.com"}]`,
		},
		{
			name:  "pure prose no JSON",
			input: "No results found for this query.",
			want:  "No results found for this query.",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace wrapping",
			input: "  \n  {\"ok\": true}  \n  ",
			want:  `{"ok": true}`,
		},
		{
			// Exercises the else-branch of stripMarkdownFences: some models
			// emit a plain ``` fence without the "json" language tag.
			name:  "plain code fence without json tag",
			input: "```\n{\"ok\": true}\n```",
			want:  `{"ok": true}`,
		},
		{
			// Truncated JSON — first '{' can't be decoded, and there's no
			// second object to fall back to. Expected behavior: return the
			// fence-stripped string so the caller's json.Unmarshal surfaces
			// a meaningful parse error for diagnostics.
			name:  "truncated JSON falls through to raw",
			input: `{"incomplete": `,
			want:  `{"incomplete":`,
		},
		{
			// Invalid JSON at first '{', valid JSON later — tests that the
			// brace-scan loop continues past a candidate that fails to decode.
			name:  "invalid first object then valid object",
			input: "noise {not json here\nthen real: {\"ok\": true}",
			want:  `{"ok": true}`,
		},
		{
			// Leading quoted string is valid JSON but not an object/array —
			// must not be returned (we only want {...} or [...]).
			name:  "quoted string then valid object",
			input: "\"preamble quote\"\n{\"ok\": true}",
			want:  `{"ok": true}`,
		},
		{
			// Codex occasionally wraps with ```json and trails prose after
			// the closing fence — the stripped form still has trailing text
			// but extractLeadingJSONValue decodes only the first value.
			name:  "fence with trailing prose after close",
			input: "```json\n{\"ok\": true}\n```\nThat's the result.",
			want:  `{"ok": true}`,
		},
		{
			// Regression pin for a known limitation flagged in review: if
			// the model echoes an example/schema JSON block before the real
			// answer, "first valid JSON wins" returns the echo. Callers must
			// rely on the prompt-suffix instruction to avoid this, or
			// shape-validate the decoded struct afterwards.
			name:  "echoed schema before real answer returns echo (known limitation)",
			input: "Example schema:\n{\"schema\": \"example\"}\n\nMy answer:\n{\"real\": true}",
			want:  `{"schema": "example"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			if got != tt.want {
				t.Errorf("extractJSON(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.want)
			}
		})
	}
}
