// Package mcp provides MCP (Model Context Protocol) client implementations.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"google.golang.org/genai"
)

// Client defines the interface for MCP clients.
type Client interface {
	// Initialize performs the MCP handshake.
	Initialize(ctx context.Context) error

	// ListTools retrieves available tools from the MCP server.
	ListTools(ctx context.Context) ([]loop.Tool, error)

	// CallTool invokes a tool on the MCP server.
	CallTool(ctx context.Context, name string, args map[string]any) (*loop.ToolResult, error)

	// Close closes the connection to the MCP server.
	Close() error
}

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *JSONRPCError) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// MCPInitializeParams represents MCP initialize parameters.
type MCPInitializeParams struct {
	ProtocolVersion string            `json:"protocolVersion"`
	Capabilities    MCPCapabilities   `json:"capabilities"`
	ClientInfo      MCPImplementation `json:"clientInfo"`
}

// MCPCapabilities represents MCP client capabilities.
type MCPCapabilities struct {
	Roots    *MCPRootsCapability `json:"roots,omitempty"`
	Sampling *struct{}           `json:"sampling,omitempty"`
}

// MCPRootsCapability represents roots capability.
type MCPRootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// MCPImplementation represents client/server info.
type MCPImplementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// MCPInitializeResult represents MCP initialize response.
type MCPInitializeResult struct {
	ProtocolVersion string            `json:"protocolVersion"`
	Capabilities    MCPCapabilities   `json:"capabilities"`
	ServerInfo      MCPImplementation `json:"serverInfo"`
}

// MCPToolsListResult represents MCP tools/list response.
type MCPToolsListResult struct {
	Tools []MCPTool `json:"tools"`
}

// MCPTool represents an MCP tool definition.
type MCPTool struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	InputSchema *MCPSchema `json:"inputSchema"`
}

// MCPSchema represents a JSON Schema in MCP format.
type MCPSchema struct {
	Type        string                `json:"type"`
	Properties  map[string]*MCPSchema `json:"properties,omitempty"`
	Required    []string              `json:"required,omitempty"`
	Items       *MCPSchema            `json:"items,omitempty"`
	Enum        []string              `json:"enum,omitempty"`
	Description string                `json:"description,omitempty"`
}

// MCPCallToolParams represents MCP tools/call parameters.
type MCPCallToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// MCPCallToolResult represents MCP tools/call response.
type MCPCallToolResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// MCPContent represents content in MCP responses.
type MCPContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
}

// BaseClient provides common functionality for MCP clients.
type BaseClient struct {
	requestID  atomic.Int64
	serverInfo *MCPImplementation
}

// NextRequestID returns the next request ID.
func (c *BaseClient) NextRequestID() int64 {
	return c.requestID.Add(1)
}

// ServerInfo returns the server information from initialization.
func (c *BaseClient) ServerInfo() *MCPImplementation {
	return c.serverInfo
}

// MCPToolWrapper wraps an MCP tool to implement the Tool interface.
type MCPToolWrapper struct {
	client      Client
	name        string
	description string
	schema      *genai.Schema
}

func (t *MCPToolWrapper) Name() string               { return t.name }
func (t *MCPToolWrapper) Description() string        { return t.description }
func (t *MCPToolWrapper) InputSchema() *genai.Schema { return t.schema }

func (t *MCPToolWrapper) Execute(ctx context.Context, input map[string]any) (*loop.ToolResult, error) {
	return t.client.CallTool(ctx, t.name, input)
}

// WrapMCPTools converts MCP tools to the standard Tool interface.
func WrapMCPTools(client Client, mcpTools []MCPTool) []loop.Tool {
	tools := make([]loop.Tool, len(mcpTools))
	for i, mt := range mcpTools {
		tools[i] = &MCPToolWrapper{
			client:      client,
			name:        mt.Name,
			description: mt.Description,
			schema:      convertMCPSchema(mt.InputSchema),
		}
	}
	return tools
}

// convertMCPSchema converts an MCP schema to a Gemini schema.
func convertMCPSchema(s *MCPSchema) *genai.Schema {
	if s == nil {
		return nil
	}

	schema := &genai.Schema{
		Description: s.Description,
	}

	switch s.Type {
	case "string":
		schema.Type = genai.TypeString
	case "integer":
		schema.Type = genai.TypeInteger
	case "number":
		schema.Type = genai.TypeNumber
	case "boolean":
		schema.Type = genai.TypeBoolean
	case "array":
		schema.Type = genai.TypeArray
		schema.Items = convertMCPSchema(s.Items)
	case "object":
		schema.Type = genai.TypeObject
		if s.Properties != nil {
			schema.Properties = make(map[string]*genai.Schema)
			for name, prop := range s.Properties {
				schema.Properties[name] = convertMCPSchema(prop)
			}
		}
		schema.Required = s.Required
	}

	if len(s.Enum) > 0 {
		schema.Enum = s.Enum
	}

	return schema
}
