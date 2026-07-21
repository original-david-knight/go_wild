package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
)

// Server implements the MCP stdio server protocol.
type Server struct {
	brokerURL       string
	token           string
	executionMethod string               // A2A method name for policy enforcement
	disabledTools   map[string]struct{}   // tool names to exclude from tools/list
	requestID       atomic.Int64

	// Dynamic tools fetched from the broker on first tools/list.
	dynamicOnce  sync.Once
	dynamicTools []mcpTool
	mcpRoutes    map[string]mcpRoute // tool name → MCP routing info
}

// mcpRoute stores routing metadata for a dynamically discovered MCP tool.
type mcpRoute struct {
	ServerID string
	ToolName string
}

// JSON-RPC types (matching MCP protocol).

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type callToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type toolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type initializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    serverCapabilities `json:"capabilities"`
	ServerInfo      serverInfo         `json:"serverInfo"`
}

type serverCapabilities struct {
	Tools *struct{} `json:"tools,omitempty"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type toolsListResult struct {
	Tools []mcpTool `json:"tools"`
}

type mcpTool struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	InputSchema *mcpSchema `json:"inputSchema"`
}

type mcpSchema struct {
	Type        string                `json:"type"`
	Properties  map[string]*mcpSchema `json:"properties,omitempty"`
	Required    []string              `json:"required,omitempty"`
	Items       *mcpSchema            `json:"items,omitempty"`
	Enum        []string              `json:"enum,omitempty"`
	Description string                `json:"description,omitempty"`
}

// Run starts the stdio JSON-RPC loop.
func (s *Server) Run() error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			log.Printf("Failed to parse JSON-RPC request: %v", err)
			continue
		}

		resp := s.handleRequest(req)
		if resp == nil {
			// Notification — no response needed
			continue
		}

		out, err := json.Marshal(resp)
		if err != nil {
			log.Printf("Failed to marshal response: %v", err)
			continue
		}
		fmt.Fprintf(os.Stdout, "%s\n", out)
	}

	return scanner.Err()
}

func (s *Server) handleRequest(req jsonRPCRequest) *jsonRPCResponse {
	switch req.Method {
	case "initialize":
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: initializeResult{
				ProtocolVersion: "2024-11-05",
				Capabilities: serverCapabilities{
					Tools: &struct{}{},
				},
				ServerInfo: serverInfo{
					Name:    "gowild-mcp-broker",
					Version: "1.0.0",
				},
			},
		}

	case "notifications/initialized":
		return nil // No response for notifications

	case "tools/list":
		tools := s.allToolsWithDynamic()
		if len(s.disabledTools) > 0 {
			filtered := make([]mcpTool, 0, len(tools))
			for _, t := range tools {
				if _, disabled := s.disabledTools[t.Name]; !disabled {
					filtered = append(filtered, t)
				}
			}
			tools = filtered
		}
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: toolsListResult{
				Tools: tools,
			},
		}

	case "tools/call":
		return s.handleToolCall(req)

	default:
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &rpcError{
				Code:    -32601,
				Message: fmt.Sprintf("method not found: %s", req.Method),
			},
		}
	}
}

func (s *Server) handleToolCall(req jsonRPCRequest) *jsonRPCResponse {
	var params callToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &rpcError{
				Code:    -32602,
				Message: fmt.Sprintf("invalid params: %v", err),
			},
		}
	}

	result, err := s.callTool(params.Name, params.Arguments)
	if err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: toolResult{
				Content: []toolContent{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
				IsError: true,
			},
		}
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		resultJSON = []byte(fmt.Sprintf("%v", result))
	}

	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: toolResult{
			Content: []toolContent{{Type: "text", Text: string(resultJSON)}},
		},
	}
}

// callTool routes a tool call, checking dynamic MCP routes first.
func (s *Server) callTool(toolName string, args map[string]any) (any, error) {
	// Ensure dynamic tools have been fetched.
	s.loadDynamicTools()

	// Check if this is a dynamically discovered MCP tool.
	if route, ok := s.mcpRoutes[toolName]; ok {
		return callMCPTool(s.brokerURL, s.token, s.executionMethod, route.ServerID, route.ToolName, args)
	}

	// Fall through to standard broker routing.
	return callBrokerTool(s.brokerURL, s.token, s.executionMethod, toolName, args)
}

// allToolsWithDynamic returns the static tools merged with dynamically
// discovered tools from the broker.
func (s *Server) allToolsWithDynamic() []mcpTool {
	s.loadDynamicTools()

	static := allTools()
	if len(s.dynamicTools) == 0 {
		return static
	}

	// Build a set of static tool names to skip duplicates.
	seen := make(map[string]struct{}, len(static))
	for _, t := range static {
		seen[t.Name] = struct{}{}
	}

	merged := make([]mcpTool, len(static), len(static)+len(s.dynamicTools))
	copy(merged, static)

	for _, t := range s.dynamicTools {
		if _, exists := seen[t.Name]; exists {
			continue
		}
		seen[t.Name] = struct{}{}
		merged = append(merged, t)
	}

	return merged
}

func (s *Server) loadDynamicTools() {
	s.dynamicOnce.Do(func() {
		tools, routes, err := fetchDynamicTools(s.brokerURL, s.token)
		if err != nil {
			log.Printf("Failed to fetch dynamic tools from broker: %v", err)
			s.mcpRoutes = make(map[string]mcpRoute)
			return
		}
		s.dynamicTools = tools
		s.mcpRoutes = routes
		if len(tools) > 0 {
			log.Printf("Loaded %d dynamic tools from broker", len(tools))
		}
	})
}
