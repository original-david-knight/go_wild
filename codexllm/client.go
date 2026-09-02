package codexllm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Client wraps the `codex` CLI for host-side text generation.
//
// Sandboxing model: callers of this Client run codex directly on the host with
// the caller's own privileges, so codex's built-in sandbox is the primary
// isolation. Generate() passes `--full-auto -s <SandboxMode>` and SandboxMode
// defaults to "read-only" — the safest option for a library consumer.
//
// The pipeline engine in apps/agent_manager/codex_runner.go does NOT use
// this Client. It runs codex inside a bwrap jail and therefore bypasses
// codex's own sandbox with `--dangerously-bypass-approvals-and-sandbox`
// (the outer bwrap provides isolation; codex's internal sandbox is redundant
// and would block legitimate MCP tool calls). See buildCodexPipelineSandboxCommand
// for details.
type Client struct {
	Model string // e.g. "gpt-5.4", "o3" — overrides profile/config default
	// ReasoningEffort is passed as `-c model_reasoning_effort="<effort>"`.
	// Valid values: "low", "medium", "high", "xhigh"; "" passes no flag and
	// leaves the CLI's own default in force.
	ReasoningEffort string
	// Profile is the config.toml profile name (e.g. "research") supplying model
	// and other settings. NOTE: Generate() always passes an explicit `-s` flag,
	// so the profile's sandbox_mode setting is effectively ignored by this
	// Client — SandboxMode below is authoritative. If you want a profile's
	// sandbox_mode to take effect, set SandboxMode to the same value explicitly.
	Profile string
	// SandboxMode is the codex sandbox mode, passed as `-s <mode>`. Valid
	// values: "read-only", "workspace-write", "danger-full-access". Empty
	// defaults to "read-only". This value always overrides any profile's
	// sandbox_mode because Generate() passes `-s` unconditionally.
	SandboxMode string
	// WebSearch, when true, passes `-c tools.web_search=true` so the model
	// has access to codex's native Responses web_search tool. This only
	// enables the tool — it does not assert the model actually used it. A
	// caller that needs web access available but whose prompt may not require
	// a search (e.g. an interactive assistant) should set this without
	// RequireWebSearchUse.
	WebSearch bool
	// RequireWebSearchUse, when true, makes Generate() return an error if no
	// `web_search` activity appeared in the codex event stream. This converts
	// silent hallucination (model answers from pretraining and fabricates
	// URLs) into a hard failure. Only meaningful with WebSearch=true — if the
	// tool wasn't enabled, the model couldn't have used it. Callers whose
	// business logic specifically depends on fresh search results (e.g.
	// deep-research searcher) should set both flags.
	RequireWebSearchUse bool
	Timeout             time.Duration // per-call timeout (0 = no limit, uses parent context only)
	Label               string        // optional label for log lines (e.g. "planner", "synthesizer")
	Dir                 string        // optional working directory for the codex process ("" = inherit)
	Env                 []string      // optional extra KEY=VALUE pairs appended to the inherited environment
}

// Generate runs `codex exec` with the given prompt and optional system prompt,
// returning the final text output. Observed URLs from web_search activity are
// discarded — callers that need them should use GenerateWithObserved.
func (c *Client) Generate(ctx context.Context, prompt, systemPrompt string) (string, error) {
	text, _, err := c.GenerateWithObserved(ctx, prompt, systemPrompt)
	return text, err
}

// GenerateWithObserved runs `codex exec` and returns both the final agent
// message text AND the set of URLs observed in the event stream (currently
// extracted from web_search `open_page` actions via the `action.url` field).
// The observed list is deduplicated and normalized via normalizeURL.
//
// Callers that want to verify the model's response URLs were actually touched
// by a tool (not fabricated) should use this method and cross-check. Note
// that codex's plain `search` actions do NOT expose result URLs in the stream
// — only `open_page` does — so prompts that need verifiable URLs must
// instruct the model to open each candidate page.
func (c *Client) GenerateWithObserved(ctx context.Context, prompt, systemPrompt string) (string, []string, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	bin, err := FindExecutable()
	if err != nil {
		return "", nil, err
	}

	label := c.Label
	if label == "" {
		label = "codexllm"
	}

	fullPrompt := WrapSystemPrompt(systemPrompt, prompt)

	sandboxMode := strings.TrimSpace(c.SandboxMode)
	if sandboxMode == "" {
		sandboxMode = "read-only"
	}

	// codex 0.149 dropped `--full-auto`. A non-interactive run says how
	// approvals are handled explicitly: the sandbox mode through `-s`, and
	// approval prompts off, since nobody is there to answer them. The
	// bypass flag is only shorthand for the same pair and stays off the host
	// path (see TestGenerate_SandboxModeFlag).
	args := []string{
		"exec",
		"--json",
		"-s", sandboxMode,
		"-c", "approval_policy=\"never\"",
		"--skip-git-repo-check",
		"--ephemeral",
	}
	if c.WebSearch {
		args = append(args, "-c", "tools.web_search=true")
	}
	if c.Profile != "" {
		args = append(args, "-p", c.Profile)
	}
	if c.Model != "" {
		args = append(args, "-m", c.Model)
	}
	if c.ReasoningEffort != "" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=\"%s\"", c.ReasoningEffort))
	}
	// The prompt goes in on stdin, not argv: a job prompt carrying a full
	// item feed can run past the OS ARG_MAX, and `codex exec -` reads the
	// prompt from stdin.
	args = append(args, "-")

	log.Printf("[%s] starting codex CLI (profile=%s, model=%s, effort=%s, prompt_len=%d)", label, c.Profile, c.Model, c.ReasoningEffort, len(fullPrompt))
	started := time.Now()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(fullPrompt)
	if c.Dir != "" {
		cmd.Dir = c.Dir
	}
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", nil, fmt.Errorf("%s: failed to create stdout pipe: %w", label, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", nil, fmt.Errorf("%s: failed to create stderr pipe: %w", label, err)
	}
	if err := cmd.Start(); err != nil {
		return "", nil, fmt.Errorf("%s: failed to start codex: %w", label, err)
	}

	// Drain stderr in background.
	var stderrBuf strings.Builder
	var stderrWG sync.WaitGroup
	stderrWG.Go(func() {
		s := bufio.NewScanner(stderr)
		s.Buffer(make([]byte, 0, 64*1024), 256*1024)
		for s.Scan() {
			line := s.Text()
			stderrBuf.WriteString(line)
			stderrBuf.WriteByte('\n')
			log.Printf("[%s] stderr: %s", label, line)
		}
		if err := s.Err(); err != nil {
			log.Printf("[%s] stderr scan error: %v", label, err)
		}
	})

	var result string
	lastEventTime := time.Now()
	eventCount := 0
	webSearchCount := 0
	observedOrder := []string{}
	observedSeen := map[string]struct{}{}
	scanner := bufio.NewScanner(stdout)
	// One stream event carries a whole message, so a line can run to
	// megabytes; a cap the stream exceeds ends this loop early and loses
	// the result.
	scanner.Buffer(make([]byte, 0, 256*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		eventCount++
		lastEventTime = time.Now()
		logCodexStreamEvent(label, line, eventCount)
		text, ok := parseCodexStreamLine(line)
		if ok {
			result = text
		}
		if isCodexWebSearchEvent(line) {
			webSearchCount++
			for _, u := range extractCodexURLs(line) {
				if _, dup := observedSeen[u]; dup {
					continue
				}
				observedSeen[u] = struct{}{}
				observedOrder = append(observedOrder, u)
			}
		}
	}
	scanErr := scanner.Err()
	if scanErr != nil {
		log.Printf("[%s] stdout scan error: %v", label, scanErr)
		// The child may still be writing; drain so it can exit instead of
		// blocking on a full pipe until the timeout.
		_, _ = io.Copy(io.Discard, stdout)
	}

	elapsed := time.Since(started).Round(time.Millisecond)
	waitErr := cmd.Wait()
	stderrWG.Wait()
	if waitErr != nil {
		if ctx.Err() != nil {
			log.Printf("[%s] context canceled after %s (events=%d, last_event=%s ago)", label, elapsed, eventCount, time.Since(lastEventTime).Round(time.Millisecond))
			return "", nil, ctx.Err()
		}
		stderrStr := strings.TrimSpace(stderrBuf.String())
		if stderrStr != "" {
			log.Printf("[%s] stderr output: %s", label, stderrStr)
		}
		return "", nil, fmt.Errorf("%s: codex exited with error after %s: %w", label, elapsed, waitErr)
	}

	if scanErr != nil {
		return "", nil, fmt.Errorf("%s: reading codex output after %s: %w", label, elapsed, scanErr)
	}
	log.Printf("[%s] completed in %s (events=%d, result_len=%d, web_search=%d, observed_urls=%d)", label, elapsed, eventCount, len(result), webSearchCount, len(observedOrder))

	if strings.TrimSpace(result) == "" {
		return "", nil, fmt.Errorf("%s: codex returned empty response after %s", label, elapsed)
	}
	if c.RequireWebSearchUse && webSearchCount == 0 {
		return "", nil, fmt.Errorf("%s: codex produced a response without invoking web_search — refusing to return potentially hallucinated results (check that `tools.web_search` is permitted for profile %q)", label, c.Profile)
	}
	return result, observedOrder, nil
}

// extractCodexURLs pulls tool-observed URLs out of a single codex JSONL event
// line. Only `open_page` actions are treated as attestations: a `search`
// action returns a query but the page has not been loaded, and future action
// types that happen to carry a URL should not become silent attestations
// without a deliberate code change here (this guards against codex upstream
// starting to attach URLs to `find_in_page` or similar shapes in a future
// release). Returns normalized URLs (via NormalizeURL).
func extractCodexURLs(line string) []string {
	var msg codexStreamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return nil
	}
	if msg.Item == nil || msg.Item.Action == nil {
		return nil
	}
	if msg.Item.Action.Type != "open_page" {
		return nil
	}
	u := NormalizeURL(msg.Item.Action.URL)
	if u == "" {
		return nil
	}
	return []string{u}
}

// NormalizeURL trims whitespace + trailing slashes and strips any `#fragment`,
// so that minor spelling differences between a model-produced URL and the one
// codex's web_search tool reported via `open_page` don't trigger a false
// fabrication error. It does NOT lowercase the path or strip the query: both
// can be semantically significant for cross-checking (e.g. pagination params,
// case-sensitive paths on some hosts, query-parameter-as-id patterns like
// `?v=<youtube-id>` or `?market=<slug>` where dropping the query would swap
// the resource).
//
// Exported so callers that build their own attested-URL sets (e.g. the
// deep-research searcher) use exactly the same normalization; keeping two
// copies in lockstep via comments has failed in the past.
func NormalizeURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if i := strings.Index(u, "#"); i >= 0 {
		u = u[:i]
	}
	for len(u) > 1 && strings.HasSuffix(u, "/") {
		u = strings.TrimSuffix(u, "/")
	}
	return u
}

// isCodexWebSearchEvent reports whether the given JSONL line describes a
// native Responses web_search tool invocation. Used to verify that a
// RequireWebSearchUse=true run actually performed a search rather than
// answering from pretraining.
//
// The detector is intentionally lenient: the codex CLI has emitted multiple
// shapes for search activity across versions (`item.type=="web_search"`,
// `web_search_call`, bare `web_search_begin`/`web_search_end` events on the
// envelope, etc.). A false negative here would reopen the exact silent
// hallucination path this code exists to prevent, so we accept any event
// family whose name begins with `web_search`.
func isCodexWebSearchEvent(line string) bool {
	var msg codexStreamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return false
	}
	if strings.HasPrefix(msg.Type, "web_search") {
		return true
	}
	if msg.Item == nil {
		return false
	}
	if msg.Type != "item.started" && msg.Type != "item.completed" {
		return false
	}
	return strings.HasPrefix(msg.Item.Type, "web_search")
}

// logCodexStreamEvent logs interesting stream events for visibility.
func logCodexStreamEvent(label, line string, eventNum int) {
	var msg codexStreamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return
	}
	switch msg.Type {
	case "thread.started":
		log.Printf("[%s] event #%d: thread started (id=%s)", label, eventNum, msg.ThreadID)
	case "turn.started":
		log.Printf("[%s] event #%d: turn started", label, eventNum)
	case "item.completed":
		if msg.Item != nil {
			log.Printf("[%s] event #%d: item completed (type=%s, text_len=%d)", label, eventNum, msg.Item.Type, len(msg.Item.Text))
		}
	case "turn.completed":
		if msg.Usage != nil {
			log.Printf("[%s] event #%d: turn completed (input=%d, output=%d)", label, eventNum, msg.Usage.InputTokens, msg.Usage.OutputTokens)
		}
	case "error":
		log.Printf("[%s] event #%d: ERROR: %s", label, eventNum, msg.Message)
	case "turn.failed":
		errMsg := ""
		if msg.Error != nil {
			errMsg = msg.Error.Message
		}
		log.Printf("[%s] event #%d: turn FAILED: %s", label, eventNum, errMsg)
	default:
		if eventNum <= 3 || eventNum%50 == 0 {
			log.Printf("[%s] event #%d: type=%s", label, eventNum, msg.Type)
		}
	}
}

// parseCodexStreamLine extracts text from a codex JSONL line.
// Returns the text and true if this line contained a completed agent message.
func parseCodexStreamLine(line string) (string, bool) {
	var msg codexStreamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return "", false
	}

	// item.completed with type:"agent_message" has the final text
	if msg.Type == "item.completed" && msg.Item != nil && msg.Item.Type == "agent_message" && msg.Item.Text != "" {
		return msg.Item.Text, true
	}

	return "", false
}

type codexStreamMessage struct {
	Type     string            `json:"type"`
	ThreadID string            `json:"thread_id,omitempty"`
	Message  string            `json:"message,omitempty"`
	Item     *codexItem        `json:"item,omitempty"`
	Usage    *codexUsage       `json:"usage,omitempty"`
	Error    *codexErrorDetail `json:"error,omitempty"`
}

type codexItem struct {
	ID     string           `json:"id"`
	Type   string           `json:"type"`
	Text   string           `json:"text,omitempty"`
	Action *codexItemAction `json:"action,omitempty"`
}

// codexItemAction is the nested `action` object on a `web_search` item. Codex
// emits multiple action types (`search`, `open_page`, `find_in_page`, `other`)
// — only `open_page` carries a URL we can cross-check, but we decode the
// envelope uniformly.
type codexItemAction struct {
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
}

type codexUsage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens"`
	OutputTokens      int `json:"output_tokens"`
}

type codexErrorDetail struct {
	Message string `json:"message"`
}
