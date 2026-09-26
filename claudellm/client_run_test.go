package claudellm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// captureLog routes the standard logger into a buffer for the test.
func captureLog(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return buf
}

type syncBuffer struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	onLine func(string)
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	n, err := b.buf.Write(p)
	hook := b.onLine
	b.mu.Unlock()
	if hook != nil {
		hook(string(p))
	}
	return n, err
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

func TestGenerateBuildsEveryFlagAndRunsInDirWithEnv(t *testing.T) {
	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "args.txt")
	envFile := filepath.Join(tmp, "env.txt")
	workDir := t.TempDir()
	stageFakeClaude(t, `#!/bin/sh
: > "$ARGS_CAPTURE_FILE"
for arg in "$@"; do printf '%s\n' "$arg" >> "$ARGS_CAPTURE_FILE"; done
printf '%s\n%s\n' "$(pwd)" "$EXTRA_FLAG" > "$ENV_CAPTURE_FILE"
echo '{"type":"result","result":"final answer"}'
`)
	t.Setenv("ARGS_CAPTURE_FILE", argsFile)
	t.Setenv("ENV_CAPTURE_FILE", envFile)

	c := &Client{
		Model:           "sonnet",
		MCPConfigPath:   " /cfg/mcp.json ",
		StrictMCPConfig: true,
		AllowedTools:    " Read,Grep ",
		Tools:           []string{},
		DisallowedTools: []string{"Bash", "Write"},
		OutputStylePath: " /style.md ",
		Dir:             workDir,
		Env:             []string{"EXTRA_FLAG=on"},
	}
	got, err := c.Generate(context.Background(), "prompt", "  be terse  ")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got != "final answer" {
		t.Errorf("Generate = %q", got)
	}

	want := []string{
		"-p", "--output-format", "stream-json", "--verbose", "--dangerously-skip-permissions",
		"--model", "sonnet",
		"--system-prompt", "be terse",
		"--mcp-config", "/cfg/mcp.json",
		"--strict-mcp-config",
		"--allowedTools", "Read,Grep",
		"--tools", "",
		"--disallowedTools", "Bash,Write",
		"--settings", `{"outputStyle":"/style.md"}`,
	}
	if args := readLines(t, argsFile); strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv = %q\nwant %q", args, want)
	}
	env := readLines(t, envFile)
	wantDir, _ := filepath.EvalSymlinks(workDir)
	gotDir, _ := filepath.EvalSymlinks(env[0])
	if gotDir != wantDir || env[1] != "on" {
		t.Errorf("child saw dir %q and EXTRA_FLAG %q, want %q and on", env[0], env[1], workDir)
	}
}

func TestGenerateNamedToolsAndMinimalArgv(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	stageFakeClaudeCaptureArgs(t, argsFile)

	if _, err := (&Client{Tools: []string{"Read", "Glob"}}).Generate(context.Background(), "p", ""); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := findFlagValue(readLines(t, argsFile), "--tools"); got != "Read,Glob" {
		t.Errorf("--tools = %q", got)
	}

	if _, err := (&Client{}).Generate(context.Background(), "p", "   "); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	want := "-p --output-format stream-json --verbose --dangerously-skip-permissions"
	if got := strings.Join(readLines(t, argsFile), " "); got != want {
		t.Errorf("argv = %q, want only the fixed flags", got)
	}
}

func TestGenerateTakesTheLastTextAndLogsEvents(t *testing.T) {
	logs := captureLog(t)
	stageFakeClaude(t, `#!/bin/sh
cat > /dev/null
echo '{"type":"system"}'
echo ''
echo 'not json'
echo '{"type":"assistant","content":[{"type":"tool_use"},{"type":"text","text":"draft"}]}'
echo '{"type":"content_block_delta","delta":{"type":"text_delta","text":"x"}}'
echo '{"type":"error","error":"transient"}'
echo '{"type":"result","result":"final"}'
`)
	got, err := (&Client{Label: "planner", Model: "opus", Effort: "high"}).Generate(context.Background(), "abc", "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got != "final" {
		t.Errorf("Generate = %q, want the result event's text", got)
	}
	out := logs.String()
	for _, want := range []string{
		"[planner] starting claude CLI (model=opus, effort=high, prompt_len=3)\n",
		"[planner] event #1: system message\n",
		"[planner] event #3: assistant (blocks=[tool_use text])\n",
		`[planner] event #5: ERROR: {"type":"error","error":"transient"}` + "\n",
		"[planner] event #6: result (len=5)\n",
		"[planner] completed in ",
		"(events=6, result_len=5)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q\nlog:\n%s", want, out)
		}
	}
	if strings.Contains(out, "event #2:") {
		t.Errorf("a non-JSON line was logged as an event:\n%s", out)
	}
	if strings.Contains(out, "event #4:") {
		t.Errorf("an unremarkable event past the third was logged:\n%s", out)
	}
}

func TestLogStreamEventSamplesUnknownTypes(t *testing.T) {
	logs := captureLog(t)
	for n := 1; n <= 100; n++ {
		logStreamEvent("l", `{"type":"ping"}`, n)
	}
	want := "[l] event #1: type=ping\n[l] event #2: type=ping\n[l] event #3: type=ping\n[l] event #50: type=ping\n[l] event #100: type=ping\n"
	if got := logs.String(); got != want {
		t.Errorf("log =\n%s\nwant\n%s", got, want)
	}
}

func TestGenerateReportsTheExitAndStderr(t *testing.T) {
	logs := captureLog(t)
	stageFakeClaude(t, `#!/bin/sh
echo 'auth failed' >&2
echo '{"type":"result","result":"partial"}'
exit 3
`)
	_, err := (&Client{Label: "job"}).Generate(context.Background(), "p", "")
	if err == nil || !strings.HasPrefix(err.Error(), "job: claude exited with error after ") || !strings.HasSuffix(err.Error(), ": exit status 3") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(logs.String(), "[job] stderr output: auth failed\n") {
		t.Errorf("stderr not logged:\n%s", logs.String())
	}
}

// The child's stderr is still unread when it exits, so the reported output
// must wait for the reader instead of racing it.
func TestGenerateReportsAllStderrOfAFailedRun(t *testing.T) {
	logs := captureLog(t)
	done := filepath.Join(t.TempDir(), "done")
	t.Setenv("DONE_FILE", done)
	stageFakeClaude(t, `#!/bin/sh
i=1
while [ $i -le 2000 ]; do echo "line $i" >&2; i=$((i+1)); done
: > "$DONE_FILE"
exit 3
`)
	logs.onLine = func(line string) {
		// Hold the reader after its first line until the child has written
		// everything and is exiting, as a slow log sink would.
		if line != "[job] stderr: line 1\n" {
			return
		}
		for {
			if _, err := os.Stat(done); err == nil {
				return
			}
			runtime.Gosched()
		}
	}
	_, err := (&Client{Label: "job"}).Generate(context.Background(), "p", "")
	if err == nil || !strings.HasSuffix(err.Error(), ": exit status 3") {
		t.Fatalf("err = %v", err)
	}
	var want strings.Builder
	want.WriteString("[job] stderr output: line 1")
	for i := 2; i <= 2000; i++ {
		fmt.Fprintf(&want, "\nline %d", i)
	}
	want.WriteString("\n")
	if out := logs.String(); !strings.Contains(out, want.String()) {
		_, reported, _ := strings.Cut(out, "[job] stderr output: ")
		reported, _, _ = strings.Cut(reported, "\n[job]")
		lines := strings.Split(strings.TrimRight(reported, "\n"), "\n")
		t.Errorf("stderr output holds %d lines, the last %q; want 2000 lines", len(lines), lines[len(lines)-1])
	}
}

func TestGenerateRejectsAnEmptyResult(t *testing.T) {
	captureLog(t)
	stageFakeClaude(t, `#!/bin/sh
echo '{"type":"system"}'
echo '{"type":"result","result":""}'
`)
	_, err := (&Client{}).Generate(context.Background(), "p", "")
	if err == nil || !strings.HasPrefix(err.Error(), "claudellm: claude returned empty response after ") {
		t.Fatalf("err = %v, want the default label and the empty-response error", err)
	}
}

func TestGenerateFailsOnAnOverlongLine(t *testing.T) {
	captureLog(t)
	// One line past the 16 MiB scanner cap, then a clean exit.
	stageFakeClaude(t, `#!/bin/sh
head -c 17000000 /dev/zero | tr '\0' x
echo
`)
	_, err := (&Client{Label: "big"}).Generate(context.Background(), "p", "")
	if err == nil || !strings.HasPrefix(err.Error(), "big: reading claude output after ") || !strings.HasSuffix(err.Error(), "bufio.Scanner: token too long") {
		t.Fatalf("err = %v", err)
	}
}

func TestGenerateReturnsTheContextErrorWhenCancelled(t *testing.T) {
	logs := captureLog(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Cancel once the first event has been logged; the child then blocks
	// until it is killed.
	logs.onLine = func(line string) {
		if strings.Contains(line, "event #1:") {
			cancel()
		}
	}
	stageFakeClaude(t, `#!/bin/sh
echo '{"type":"system"}'
exec sleep 60
`)
	_, err := (&Client{Label: "cx", Timeout: 1 << 40}).Generate(ctx, "p", "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if !strings.Contains(logs.String(), "[cx] context canceled after ") {
		t.Errorf("cancel not logged:\n%s", logs.String())
	}
}

func TestGenerateFailsToStartWithACancelledContext(t *testing.T) {
	captureLog(t)
	stageFakeClaude(t, "#!/bin/sh\necho '{\"type\":\"result\",\"result\":\"x\"}'\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (&Client{Label: "st"}).Generate(ctx, "p", "")
	if err == nil || err.Error() != "st: failed to start claude: context canceled" {
		t.Fatalf("err = %v", err)
	}
}

func TestGenerateWithoutTheCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := (&Client{}).Generate(context.Background(), "p", "")
	if err == nil || err.Error() != "claude executable not found in PATH (tried: claude, claude-code)" {
		t.Fatalf("err = %v", err)
	}
}

func TestFindExecutableFallsBackToClaudeCode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude-code")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if got, err := FindExecutable(); err != nil || got != path {
		t.Errorf("FindExecutable = %q, %v; want %q", got, err, path)
	}
}

func TestFindBwrapExecutable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if _, err := FindBwrapExecutable(); err == nil || err.Error() != "bwrap executable not found; claude sandboxing requires bubblewrap" {
		t.Errorf("missing bwrap err = %v", err)
	}
	path := filepath.Join(dir, "bwrap")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := FindBwrapExecutable(); err != nil || got != path {
		t.Errorf("FindBwrapExecutable = %q, %v; want %q", got, err, path)
	}
}

func TestResearchOutputStylePathIsStableAndReadable(t *testing.T) {
	first := ResearchOutputStylePath()
	if second := ResearchOutputStylePath(); second != first {
		t.Errorf("second call = %q, want the same path %q", second, first)
	}
	raw, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("read style: %v", err)
	}
	if !strings.HasPrefix(string(raw), "---\nname: Research Agent\n") {
		t.Errorf("style file starts %q", string(raw[:min(len(raw), 40)]))
	}
}

func TestWriteResearchOutputStyleFailsWithoutATempDir(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	path, cleanup, err := WriteResearchOutputStyle()
	if err == nil || path != "" || cleanup != nil {
		t.Errorf("WriteResearchOutputStyle = %q, %v, %v; want an error", path, cleanup != nil, err)
	}
}
