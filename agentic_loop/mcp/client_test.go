package mcp

import (
	"context"
	"encoding/json"
	"testing"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"google.golang.org/genai"
)

func TestConvertMCPSchema_String(t *testing.T) {
	mcpSchema := &MCPSchema{
		Type:        "string",
		Description: "A test string",
	}

	schema := convertMCPSchema(mcpSchema)

	if schema.Type != genai.TypeString {
		t.Errorf("expected TypeString, got %v", schema.Type)
	}
	if schema.Description != "A test string" {
		t.Errorf("expected description 'A test string', got %s", schema.Description)
	}
}

func TestConvertMCPSchema_Integer(t *testing.T) {
	mcpSchema := &MCPSchema{Type: "integer"}
	schema := convertMCPSchema(mcpSchema)

	if schema.Type != genai.TypeInteger {
		t.Errorf("expected TypeInteger, got %v", schema.Type)
	}
}

func TestConvertMCPSchema_Number(t *testing.T) {
	mcpSchema := &MCPSchema{Type: "number"}
	schema := convertMCPSchema(mcpSchema)

	if schema.Type != genai.TypeNumber {
		t.Errorf("expected TypeNumber, got %v", schema.Type)
	}
}

func TestConvertMCPSchema_Boolean(t *testing.T) {
	mcpSchema := &MCPSchema{Type: "boolean"}
	schema := convertMCPSchema(mcpSchema)

	if schema.Type != genai.TypeBoolean {
		t.Errorf("expected TypeBoolean, got %v", schema.Type)
	}
}

func TestConvertMCPSchema_Array(t *testing.T) {
	mcpSchema := &MCPSchema{
		Type:  "array",
		Items: &MCPSchema{Type: "string"},
	}

	schema := convertMCPSchema(mcpSchema)

	if schema.Type != genai.TypeArray {
		t.Errorf("expected TypeArray, got %v", schema.Type)
	}
	if schema.Items == nil {
		t.Fatal("expected non-nil Items")
	}
	if schema.Items.Type != genai.TypeString {
		t.Errorf("expected Items TypeString, got %v", schema.Items.Type)
	}
}

func TestConvertMCPSchema_Object(t *testing.T) {
	mcpSchema := &MCPSchema{
		Type: "object",
		Properties: map[string]*MCPSchema{
			"name": {Type: "string", Description: "The name"},
			"age":  {Type: "integer"},
		},
		Required: []string{"name"},
	}

	schema := convertMCPSchema(mcpSchema)

	if schema.Type != genai.TypeObject {
		t.Errorf("expected TypeObject, got %v", schema.Type)
	}
	if len(schema.Properties) != 2 {
		t.Errorf("expected 2 properties, got %d", len(schema.Properties))
	}
	if schema.Properties["name"].Type != genai.TypeString {
		t.Errorf("expected name TypeString, got %v", schema.Properties["name"].Type)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "name" {
		t.Errorf("expected Required ['name'], got %v", schema.Required)
	}
}

func TestConvertMCPSchema_Enum(t *testing.T) {
	mcpSchema := &MCPSchema{
		Type: "string",
		Enum: []string{"low", "medium", "high"},
	}

	schema := convertMCPSchema(mcpSchema)

	if len(schema.Enum) != 3 {
		t.Errorf("expected 3 enum values, got %d", len(schema.Enum))
	}
}

func TestConvertMCPSchema_Nil(t *testing.T) {
	schema := convertMCPSchema(nil)
	if schema != nil {
		t.Errorf("expected nil for nil input, got %v", schema)
	}
}

func TestBaseClient_NextRequestID(t *testing.T) {
	client := &BaseClient{}

	id1 := client.NextRequestID()
	id2 := client.NextRequestID()
	id3 := client.NextRequestID()

	if id1 != 1 {
		t.Errorf("expected first ID 1, got %d", id1)
	}
	if id2 != 2 {
		t.Errorf("expected second ID 2, got %d", id2)
	}
	if id3 != 3 {
		t.Errorf("expected third ID 3, got %d", id3)
	}
}

func TestJSONRPCError_Error(t *testing.T) {
	err := &JSONRPCError{
		Code:    -32600,
		Message: "Invalid Request",
	}

	expected := "JSON-RPC error -32600: Invalid Request"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestMCPToolWrapper(t *testing.T) {
	// Create a mock wrapper (without actual client)
	wrapper := &MCPToolWrapper{
		name:        "test_mcp_tool",
		description: "A test MCP tool",
		schema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"query": {Type: genai.TypeString},
			},
		},
	}

	if wrapper.Name() != "test_mcp_tool" {
		t.Errorf("expected name 'test_mcp_tool', got %s", wrapper.Name())
	}
	if wrapper.Description() != "A test MCP tool" {
		t.Errorf("expected description 'A test MCP tool', got %s", wrapper.Description())
	}
	if wrapper.InputSchema().Type != genai.TypeObject {
		t.Errorf("expected TypeObject schema, got %v", wrapper.InputSchema().Type)
	}
}

// --- mockClient implements Client for testing ---

type mockClient struct {
	callToolResult *loop.ToolResult
	callToolErr    error
}

func (m *mockClient) Initialize(ctx context.Context) error               { return nil }
func (m *mockClient) ListTools(ctx context.Context) ([]loop.Tool, error) { return nil, nil }
func (m *mockClient) CallTool(ctx context.Context, name string, args map[string]any) (*loop.ToolResult, error) {
	return m.callToolResult, m.callToolErr
}
func (m *mockClient) Close() error { return nil }

// --- MCPToolWrapper.Execute ---

func TestMCPToolWrapper_Execute_Success(t *testing.T) {
	mc := &mockClient{callToolResult: loop.NewSuccessResult("tool output")}
	wrapper := &MCPToolWrapper{
		client: mc,
		name:   "test_tool",
	}

	result, err := wrapper.Execute(context.Background(), map[string]any{"query": "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestMCPToolWrapper_Execute_Error(t *testing.T) {
	mc := &mockClient{callToolResult: loop.NewErrorResult("tool failed")}
	wrapper := &MCPToolWrapper{
		client: mc,
		name:   "test_tool",
	}

	result, err := wrapper.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected failure")
	}
}

// --- WrapMCPTools ---

func TestWrapMCPTools_Empty(t *testing.T) {
	mc := &mockClient{}
	tools := WrapMCPTools(mc, nil)
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestWrapMCPTools(t *testing.T) {
	mc := &mockClient{}
	mcpTools := []MCPTool{
		{
			Name:        "tool_a",
			Description: "Tool A",
			InputSchema: &MCPSchema{
				Type: "object",
				Properties: map[string]*MCPSchema{
					"query": {Type: "string"},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        "tool_b",
			Description: "Tool B",
			InputSchema: &MCPSchema{Type: "object"},
		},
	}

	tools := WrapMCPTools(mc, mcpTools)
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	if tools[0].Name() != "tool_a" {
		t.Errorf("expected name 'tool_a', got %q", tools[0].Name())
	}
	if tools[0].Description() != "Tool A" {
		t.Errorf("expected description 'Tool A', got %q", tools[0].Description())
	}
	schema := tools[0].InputSchema()
	if schema == nil {
		t.Fatal("expected non-nil schema")
	}
	if schema.Type != genai.TypeObject {
		t.Errorf("expected TypeObject, got %v", schema.Type)
	}
	if schema.Properties["query"] == nil {
		t.Error("expected 'query' property in schema")
	}

	if tools[1].Name() != "tool_b" {
		t.Errorf("expected name 'tool_b', got %q", tools[1].Name())
	}
}

func TestWrapMCPTools_NilSchema(t *testing.T) {
	mc := &mockClient{}
	mcpTools := []MCPTool{
		{Name: "no_schema_tool", Description: "No schema"},
	}

	tools := WrapMCPTools(mc, mcpTools)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].InputSchema() != nil {
		t.Error("expected nil schema")
	}
}

// --- BaseClient.ServerInfo ---

func TestBaseClient_ServerInfo_Nil(t *testing.T) {
	bc := &BaseClient{}
	if bc.ServerInfo() != nil {
		t.Error("expected nil server info")
	}
}

func TestBaseClient_ServerInfo(t *testing.T) {
	bc := &BaseClient{
		serverInfo: &MCPImplementation{Name: "test-server", Version: "1.0"},
	}
	info := bc.ServerInfo()
	if info.Name != "test-server" {
		t.Errorf("expected 'test-server', got %q", info.Name)
	}
	if info.Version != "1.0" {
		t.Errorf("expected '1.0', got %q", info.Version)
	}
}

// --- JSON-RPC serialization ---

func TestJSONRPCRequest_Serialization(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
		Params:  map[string]any{"cursor": "abc"},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded JSONRPCRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc '2.0', got %q", decoded.JSONRPC)
	}
	if decoded.ID != 1 {
		t.Errorf("expected id 1, got %d", decoded.ID)
	}
	if decoded.Method != "tools/list" {
		t.Errorf("expected method 'tools/list', got %q", decoded.Method)
	}
}

func TestJSONRPCRequest_NoParams(t *testing.T) {
	req := JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "initialize"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	// Params should be omitted when nil
	var raw map[string]any
	json.Unmarshal(data, &raw)
	if _, exists := raw["params"]; exists {
		t.Error("expected params to be omitted")
	}
}

func TestJSONRPCResponse_Success(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`
	var resp JSONRPCResponse
	if err := json.Unmarshal([]byte(input), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp.ID != 1 {
		t.Errorf("expected id 1, got %d", resp.ID)
	}
	if resp.Error != nil {
		t.Error("expected nil error")
	}
	if resp.Result == nil {
		t.Error("expected non-nil result")
	}
}

func TestJSONRPCResponse_Error(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found"}}`
	var resp JSONRPCResponse
	if err := json.Unmarshal([]byte(input), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected non-nil error")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected code -32601, got %d", resp.Error.Code)
	}
	if resp.Error.Message != "Method not found" {
		t.Errorf("expected 'Method not found', got %q", resp.Error.Message)
	}
}

// --- MCP types serialization ---

func TestMCPInitializeParams_Serialization(t *testing.T) {
	params := MCPInitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities: MCPCapabilities{
			Roots: &MCPRootsCapability{ListChanged: true},
		},
		ClientInfo: MCPImplementation{Name: "test", Version: "1.0"},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded MCPInitializeParams
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded.ProtocolVersion != "2024-11-05" {
		t.Errorf("expected version '2024-11-05', got %q", decoded.ProtocolVersion)
	}
	if decoded.ClientInfo.Name != "test" {
		t.Errorf("expected name 'test', got %q", decoded.ClientInfo.Name)
	}
	if decoded.Capabilities.Roots == nil || !decoded.Capabilities.Roots.ListChanged {
		t.Error("expected roots.listChanged to be true")
	}
}

func TestMCPCallToolParams_Serialization(t *testing.T) {
	params := MCPCallToolParams{
		Name:      "search",
		Arguments: map[string]any{"query": "test", "limit": float64(10)},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded MCPCallToolParams
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded.Name != "search" {
		t.Errorf("expected name 'search', got %q", decoded.Name)
	}
	if decoded.Arguments["query"] != "test" {
		t.Errorf("expected query 'test', got %v", decoded.Arguments["query"])
	}
}

func TestMCPCallToolResult_Success(t *testing.T) {
	input := `{"content":[{"type":"text","text":"result text"}]}`
	var result MCPCallToolResult
	if err := json.Unmarshal([]byte(input), &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if result.IsError {
		t.Error("expected isError false")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result.Content))
	}
	if result.Content[0].Type != "text" {
		t.Errorf("expected type 'text', got %q", result.Content[0].Type)
	}
	if result.Content[0].Text != "result text" {
		t.Errorf("expected text 'result text', got %q", result.Content[0].Text)
	}
}

func TestMCPCallToolResult_Error(t *testing.T) {
	input := `{"content":[{"type":"text","text":"tool error"}],"isError":true}`
	var result MCPCallToolResult
	if err := json.Unmarshal([]byte(input), &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !result.IsError {
		t.Error("expected isError true")
	}
}

func TestMCPCallToolResult_MultipleContent(t *testing.T) {
	input := `{"content":[{"type":"text","text":"hello"},{"type":"image","mimeType":"image/png","data":"base64data"}]}`
	var result MCPCallToolResult
	if err := json.Unmarshal([]byte(input), &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(result.Content) != 2 {
		t.Fatalf("expected 2 content items, got %d", len(result.Content))
	}
	if result.Content[1].Type != "image" {
		t.Errorf("expected type 'image', got %q", result.Content[1].Type)
	}
	if result.Content[1].MimeType != "image/png" {
		t.Errorf("expected mimeType 'image/png', got %q", result.Content[1].MimeType)
	}
}

func TestMCPToolsListResult_Serialization(t *testing.T) {
	input := `{"tools":[{"name":"search","description":"Search tool","inputSchema":{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}}]}`
	var result MCPToolsListResult
	if err := json.Unmarshal([]byte(input), &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	if result.Tools[0].Name != "search" {
		t.Errorf("expected name 'search', got %q", result.Tools[0].Name)
	}
	if result.Tools[0].InputSchema == nil {
		t.Fatal("expected non-nil input schema")
	}
	if result.Tools[0].InputSchema.Type != "object" {
		t.Errorf("expected type 'object', got %q", result.Tools[0].InputSchema.Type)
	}
}

func TestMCPInitializeResult_Serialization(t *testing.T) {
	input := `{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"test-server","version":"0.1.0"}}`
	var result MCPInitializeResult
	if err := json.Unmarshal([]byte(input), &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if result.ProtocolVersion != "2024-11-05" {
		t.Errorf("expected version '2024-11-05', got %q", result.ProtocolVersion)
	}
	if result.ServerInfo.Name != "test-server" {
		t.Errorf("expected name 'test-server', got %q", result.ServerInfo.Name)
	}
}
