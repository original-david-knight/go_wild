package claudellm

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseStreamLineResult(t *testing.T) {
	line := `{"type":"result","result":"Hello, world!"}`
	text, ok := parseStreamLine(line)
	if !ok {
		t.Fatal("expected ok=true for result message")
	}
	if text != "Hello, world!" {
		t.Fatalf("expected %q, got %q", "Hello, world!", text)
	}
}

func TestParseStreamLineAssistant(t *testing.T) {
	line := `{"type":"assistant","content":[{"type":"text","text":"Generated output"}]}`
	text, ok := parseStreamLine(line)
	if !ok {
		t.Fatal("expected ok=true for assistant message")
	}
	if text != "Generated output" {
		t.Fatalf("expected %q, got %q", "Generated output", text)
	}
}

func TestParseStreamLineDeltaIgnored(t *testing.T) {
	line := `{"type":"content_block_delta","delta":{"type":"text_delta","text":"partial"}}`
	_, ok := parseStreamLine(line)
	if ok {
		t.Fatal("expected ok=false for delta messages")
	}
}

func TestParseStreamLineInvalidJSON(t *testing.T) {
	_, ok := parseStreamLine("not json")
	if ok {
		t.Fatal("expected ok=false for invalid JSON")
	}
}

func TestParseStreamLineEmptyResult(t *testing.T) {
	line := `{"type":"result","result":""}`
	_, ok := parseStreamLine(line)
	if ok {
		t.Fatal("expected ok=false for empty result")
	}
}

func TestParseStreamLineAssistantNoText(t *testing.T) {
	line := `{"type":"assistant","content":[{"type":"tool_use","text":""}]}`
	_, ok := parseStreamLine(line)
	if ok {
		t.Fatal("expected ok=false for non-text content block")
	}
}

func TestFindExecutableMissing(t *testing.T) {
	// This test verifies FindExecutable returns an error when the binary isn't found.
	// It will pass if claude is not in PATH, skip if it is.
	_, err := FindExecutable()
	if err == nil {
		t.Skip("claude binary found in PATH, skipping missing-binary test")
	}
	if err.Error() != "claude executable not found in PATH (tried: claude, claude-code)" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// stageFakeClaude writes a shell script named "claude" to a temp dir and
// prepends that dir to PATH so FindExecutable resolves it.
func stageFakeClaude(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-claude test uses a POSIX shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// stageFakeClaudeCaptureArgs stages a fake claude that writes all argv entries
// (one per line) to the file at ARGS_CAPTURE_FILE and emits a minimal valid
// stream. Used to assert which CLI flags Generate() constructs.
func stageFakeClaudeCaptureArgs(t *testing.T, argsFile string) {
	t.Helper()
	stageFakeClaude(t, `#!/bin/sh
: > "$ARGS_CAPTURE_FILE"
for arg in "$@"; do
  printf '%s\n' "$arg" >> "$ARGS_CAPTURE_FILE"
done
echo '{"type":"result","result":"ok"}'
`)
	t.Setenv("ARGS_CAPTURE_FILE", argsFile)
}

// findFlagValue returns the value following flag in args, or "" if absent.
// e.g. findFlagValue(["-p","x","--model","opus"], "--model") == "opus".
func findFlagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// TestGenerate_EffortFlag verifies that Effort is passed as `--effort <effort>`
// and that an empty Effort passes no such flag, leaving the CLI's own default
// in force.
func TestGenerate_EffortFlag(t *testing.T) {
	cases := []struct {
		name   string
		effort string
		want   string // value after --effort; "" = flag absent
	}{
		{name: "empty passes no flag", effort: "", want: ""},
		{name: "xhigh", effort: "xhigh", want: "xhigh"},
		{name: "max", effort: "max", want: "max"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			argsFile := filepath.Join(t.TempDir(), "args.txt")
			stageFakeClaudeCaptureArgs(t, argsFile)

			c := &Client{Label: "test", Model: "opus", Effort: tc.effort}
			if _, err := c.Generate(context.Background(), "p", ""); err != nil {
				t.Fatalf("Generate() error = %v", err)
			}
			raw, err := os.ReadFile(argsFile)
			if err != nil {
				t.Fatalf("read args file: %v", err)
			}
			args := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
			if got := findFlagValue(args, "--effort"); got != tc.want {
				t.Fatalf("--effort flag = %q, want %q (argv=%v)", got, tc.want, args)
			}
			if tc.want == "" {
				for _, a := range args {
					if a == "--effort" {
						t.Fatalf("did not expect --effort in argv with empty Effort: %v", args)
					}
				}
			}
			// The model flag is unaffected either way.
			if got := findFlagValue(args, "--model"); got != "opus" {
				t.Fatalf("--model flag = %q, want %q (argv=%v)", got, "opus", args)
			}
		})
	}
}
