package claudellm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Client wraps the `claude` CLI for text generation.
type Client struct {
	Model           string        // e.g. "opus", "sonnet"
	Effort          string        // low, medium, high, xhigh, max; "" passes no flag
	MCPConfigPath   string        // optional path to MCP server config JSON
	AllowedTools    string        // optional comma-separated list of allowed tools
	Tools           []string      // nil = default built-ins, empty = disable built-ins
	StrictMCPConfig bool          // when true, ignore all MCP configs except MCPConfigPath
	DisallowedTools []string      // optional list of disallowed tools
	OutputStylePath string        // optional path to a .md output style file (strips default SE prompt)
	Timeout         time.Duration // per-call timeout (0 = no limit, uses parent context only)
	Label           string        // optional label for log lines (e.g. "planner", "synthesizer")
	Dir             string        // optional working directory for the claude process ("" = inherit)
	Env             []string      // optional extra KEY=VALUE pairs appended to the inherited environment
}

// Generate runs `claude -p` with the given prompt and optional system prompt,
// returning the final text output.
func (c *Client) Generate(ctx context.Context, prompt, systemPrompt string) (string, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	bin, err := FindExecutable()
	if err != nil {
		return "", err
	}

	label := c.Label
	if label == "" {
		label = "claudellm"
	}

	args := []string{"-p", prompt, "--output-format", "stream-json", "--verbose", "--dangerously-skip-permissions"}
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	if c.Effort != "" {
		args = append(args, "--effort", c.Effort)
	}
	if strings.TrimSpace(systemPrompt) != "" {
		args = append(args, "--system-prompt", strings.TrimSpace(systemPrompt))
	}
	if strings.TrimSpace(c.MCPConfigPath) != "" {
		args = append(args, "--mcp-config", strings.TrimSpace(c.MCPConfigPath))
	}
	if c.StrictMCPConfig {
		args = append(args, "--strict-mcp-config")
	}
	if strings.TrimSpace(c.AllowedTools) != "" {
		args = append(args, "--allowedTools", strings.TrimSpace(c.AllowedTools))
	}
	if c.Tools != nil {
		toolsArg := ""
		if len(c.Tools) > 0 {
			toolsArg = strings.Join(c.Tools, ",")
		}
		args = append(args, "--tools", toolsArg)
	}
	if len(c.DisallowedTools) > 0 {
		args = append(args, "--disallowedTools", strings.Join(c.DisallowedTools, ","))
	}
	if strings.TrimSpace(c.OutputStylePath) != "" {
		args = append(args, "--settings", fmt.Sprintf(`{"outputStyle":"%s"}`, strings.TrimSpace(c.OutputStylePath)))
	}

	log.Printf("[%s] starting claude CLI (model=%s, effort=%s, prompt_len=%d)", label, c.Model, c.Effort, len(prompt))
	started := time.Now()

	cmd := exec.CommandContext(ctx, bin, args...)
	if c.Dir != "" {
		cmd.Dir = c.Dir
	}
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("%s: failed to create stdout pipe: %w", label, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("%s: failed to create stderr pipe: %w", label, err)
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("%s: failed to start claude: %w", label, err)
	}

	// Drain stderr in background.
	var stderrBuf strings.Builder
	go func() {
		s := bufio.NewScanner(stderr)
		s.Buffer(make([]byte, 0, 64*1024), 256*1024)
		for s.Scan() {
			line := s.Text()
			stderrBuf.WriteString(line)
			stderrBuf.WriteByte('\n')
			log.Printf("[%s] stderr: %s", label, line)
		}
	}()

	var result string
	lastEventTime := time.Now()
	eventCount := 0
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		eventCount++
		lastEventTime = time.Now()
		logStreamEvent(label, line, eventCount)
		text, ok := parseStreamLine(line)
		if ok {
			result = text
		}
	}

	elapsed := time.Since(started).Round(time.Millisecond)
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			log.Printf("[%s] context canceled after %s (events=%d, last_event=%s ago)", label, elapsed, eventCount, time.Since(lastEventTime).Round(time.Millisecond))
			return "", ctx.Err()
		}
		stderrStr := strings.TrimSpace(stderrBuf.String())
		if stderrStr != "" {
			log.Printf("[%s] stderr output: %s", label, stderrStr)
		}
		return "", fmt.Errorf("%s: claude exited with error after %s: %w", label, elapsed, err)
	}

	log.Printf("[%s] completed in %s (events=%d, result_len=%d)", label, elapsed, eventCount, len(result))

	if strings.TrimSpace(result) == "" {
		return "", fmt.Errorf("%s: claude returned empty response after %s", label, elapsed)
	}
	return result, nil
}

// logStreamEvent logs interesting stream events for visibility.
func logStreamEvent(label, line string, eventNum int) {
	var msg streamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return
	}
	switch msg.Type {
	case "system":
		log.Printf("[%s] event #%d: system message", label, eventNum)
	case "assistant":
		var blockTypes []string
		for _, b := range msg.Content {
			blockTypes = append(blockTypes, b.Type)
		}
		log.Printf("[%s] event #%d: assistant (blocks=%v)", label, eventNum, blockTypes)
	case "result":
		log.Printf("[%s] event #%d: result (len=%d)", label, eventNum, len(msg.Result))
	case "error":
		log.Printf("[%s] event #%d: ERROR: %.500s", label, eventNum, line)
	default:
		// Log every 50th event or unknown types to avoid flooding.
		if eventNum <= 3 || eventNum%50 == 0 {
			log.Printf("[%s] event #%d: type=%s", label, eventNum, msg.Type)
		}
	}
}

// parseStreamLine extracts text from a stream-json NDJSON line.
// Returns the accumulated text and true if this line contained text content.
func parseStreamLine(line string) (string, bool) {
	var msg streamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return "", false
	}

	// type:"result" has the final text in .result
	if msg.Type == "result" && msg.Result != "" {
		return msg.Result, true
	}

	// type:"assistant" with content blocks containing text
	if msg.Type == "assistant" {
		for _, block := range msg.Content {
			if block.Type == "text" && block.Text != "" {
				return block.Text, true
			}
		}
	}

	// content_block_delta with text delta
	if msg.Type == "content_block_delta" && msg.Delta.Type == "text_delta" && msg.Delta.Text != "" {
		// We don't accumulate deltas — we only use the final result message.
		return "", false
	}

	return "", false
}

type streamMessage struct {
	Type    string         `json:"type"`
	Result  string         `json:"result,omitempty"`
	Content []contentBlock `json:"content,omitempty"`
	Delta   deltaBlock     `json:"delta,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type deltaBlock struct {
	Type string `json:"type,omitempty"`
	Text string `json:"text,omitempty"`
}
