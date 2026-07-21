package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// StdioClient implements MCP over stdio (subprocess communication).
type StdioClient struct {
	BaseClient
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr io.ReadCloser

	mu          sync.Mutex
	initialized bool
}

// StdioClientOptions configures how the MCP server process is started.
type StdioClientOptions struct {
	Env map[string]string
	Dir string
}

// NewStdioClient creates a new MCP client that communicates via stdio.
// The command and args specify the MCP server process to spawn.
func NewStdioClient(ctx context.Context, command string, args ...string) (*StdioClient, error) {
	return NewStdioClientWithOptions(ctx, command, args, StdioClientOptions{})
}

// NewStdioClientWithOptions creates a new MCP client with process options.
func NewStdioClientWithOptions(ctx context.Context, command string, args []string, opts StdioClientOptions) (*StdioClient, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	if len(opts.Env) > 0 {
		cmd.Env = mergeEnv(os.Environ(), opts.Env)
	}
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		return nil, fmt.Errorf("failed to start MCP server: %w", err)
	}

	return &StdioClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		stderr: stderr,
	}, nil
}

func mergeEnv(base []string, override map[string]string) []string {
	envMap := make(map[string]string, len(base)+len(override))
	for _, entry := range base {
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	for k, v := range override {
		envMap[k] = v
	}
	keys := make([]string, 0, len(envMap))
	for k := range envMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, k := range keys {
		result = append(result, k+"="+envMap[k])
	}
	return result
}

// Initialize performs the MCP handshake with the server.
func (c *StdioClient) Initialize(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized {
		return nil
	}

	// Send initialize request
	params := MCPInitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities: MCPCapabilities{
			Roots: &MCPRootsCapability{ListChanged: true},
		},
		ClientInfo: MCPImplementation{
			Name:    "gowild_agentic_loop",
			Version: "1.0.0",
		},
	}

	result, err := c.call(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	var initResult MCPInitializeResult
	if err := json.Unmarshal(result, &initResult); err != nil {
		return fmt.Errorf("failed to parse initialize result: %w", err)
	}

	c.serverInfo = &initResult.ServerInfo

	// Send initialized notification
	if err := c.notify("notifications/initialized", nil); err != nil {
		return fmt.Errorf("initialized notification failed: %w", err)
	}

	c.initialized = true
	return nil
}

// ListTools retrieves available tools from the MCP server.
func (c *StdioClient) ListTools(ctx context.Context) ([]loop.Tool, error) {
	if !c.initialized {
		if err := c.Initialize(ctx); err != nil {
			return nil, err
		}
	}

	tools, err := c.ListMCPTools(ctx)
	if err != nil {
		return nil, err
	}

	return WrapMCPTools(c, tools), nil
}

// ListMCPTools retrieves raw MCP tool definitions from the server.
func (c *StdioClient) ListMCPTools(ctx context.Context) ([]MCPTool, error) {
	if !c.initialized {
		if err := c.Initialize(ctx); err != nil {
			return nil, err
		}
	}

	result, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("tools/list failed: %w", err)
	}

	var toolsResult MCPToolsListResult
	if err := json.Unmarshal(result, &toolsResult); err != nil {
		return nil, fmt.Errorf("failed to parse tools list: %w", err)
	}

	return toolsResult.Tools, nil
}

// CallTool invokes a tool on the MCP server.
func (c *StdioClient) CallTool(ctx context.Context, name string, args map[string]any) (*loop.ToolResult, error) {
	if !c.initialized {
		if err := c.Initialize(ctx); err != nil {
			return nil, err
		}
	}

	params := MCPCallToolParams{
		Name:      name,
		Arguments: args,
	}

	result, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("tool call failed: %v", err)), nil
	}

	var callResult MCPCallToolResult
	if err := json.Unmarshal(result, &callResult); err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to parse tool result: %v", err)), nil
	}

	if callResult.IsError {
		// Extract error text from content
		for _, content := range callResult.Content {
			if content.Type == "text" {
				return loop.NewErrorResult(content.Text), nil
			}
		}
		return loop.NewErrorResult("unknown error"), nil
	}

	// Extract result from content
	resultMap := make(map[string]any)
	for i, content := range callResult.Content {
		switch content.Type {
		case "text":
			if len(callResult.Content) == 1 {
				return loop.NewSuccessResult(content.Text), nil
			}
			resultMap[fmt.Sprintf("text_%d", i)] = content.Text
		case "image":
			resultMap[fmt.Sprintf("image_%d", i)] = map[string]any{
				"mimeType": content.MimeType,
				"data":     content.Data,
			}
		}
	}

	return loop.NewSuccessResult(resultMap), nil
}

// Close terminates the MCP server process.
func (c *StdioClient) Close() error {
	c.stdin.Close()

	// Give the process a chance to exit gracefully
	done := make(chan error, 1)
	go func() {
		done <- c.cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	default:
		// Process didn't exit, kill it
		c.cmd.Process.Kill()
		return <-done
	}
}

// call sends a JSON-RPC request and waits for the response.
func (c *StdioClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.NextRequestID(),
		Method:  method,
		Params:  params,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Write request
	if _, err := c.stdin.Write(append(reqBytes, '\n')); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Read response
	respBytes, err := c.stdout.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var resp JSONRPCResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.Error != nil {
		return nil, resp.Error
	}

	return resp.Result, nil
}

// notify sends a JSON-RPC notification (no response expected).
func (c *StdioClient) notify(method string, params any) error {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	if _, err := c.stdin.Write(append(reqBytes, '\n')); err != nil {
		return fmt.Errorf("failed to write notification: %w", err)
	}

	return nil
}
