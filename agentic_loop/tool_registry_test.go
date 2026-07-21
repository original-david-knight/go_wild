package gowild_agentic_loop

import (
	"context"
	"testing"
)

// Test input structs
type GreetInput struct {
	Name string `json:"name" description:"Name to greet"`
}

type MathInput struct {
	A int `json:"a" description:"First number"`
	B int `json:"b" description:"Second number"`
}

// TestToolProvider provides tools for testing
type TestToolProvider struct {
	greeting string
}

// GreetTool greets someone by name.
func (p *TestToolProvider) GreetTool(ctx context.Context, input GreetInput) (*ToolResult, error) {
	return NewSuccessResult(p.greeting + ", " + input.Name + "!"), nil
}

// AddTool adds two numbers.
func (p *TestToolProvider) AddTool(ctx context.Context, input MathInput) (*ToolResult, error) {
	return NewSuccessResult(map[string]any{
		"result": input.A + input.B,
	}), nil
}

// HelperMethod is not a tool because it doesn't end with "Tool".
func (p *TestToolProvider) HelperMethod(ctx context.Context, input GreetInput) (*ToolResult, error) {
	return NewSuccessResult("not a tool"), nil
}

// WrongSignature has wrong signature so should be ignored.
func (p *TestToolProvider) WrongSignatureTool(name string) string {
	return name
}

// DescribeTool implements ToolProvider interface.
func (p *TestToolProvider) DescribeTool(name string) string {
	descriptions := map[string]string{
		"greet": "Greet someone by name",
		"add":   "Add two numbers together",
	}
	return descriptions[name]
}

func TestWrapTools(t *testing.T) {
	provider := &TestToolProvider{greeting: "Hello"}
	tools := WrapTools(provider)

	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	// Check tool names
	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name()] = true
	}

	if !names["greet"] {
		t.Error("missing 'greet' tool")
	}
	if !names["add"] {
		t.Error("missing 'add' tool")
	}
}

func TestWrapTools_Execute(t *testing.T) {
	provider := &TestToolProvider{greeting: "Hello"}
	tools := WrapTools(provider)

	// Find greet tool
	var greetTool Tool
	for _, tool := range tools {
		if tool.Name() == "greet" {
			greetTool = tool
			break
		}
	}

	if greetTool == nil {
		t.Fatal("greet tool not found")
	}

	result, err := greetTool.Execute(context.Background(), map[string]any{
		"name": "World",
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Content != "Hello, World!" {
		t.Errorf("expected 'Hello, World!', got %v", result.Content)
	}
}

func TestWrapTools_Schema(t *testing.T) {
	provider := &TestToolProvider{greeting: "Hello"}
	tools := WrapTools(provider)

	// Find greet tool
	var greetTool Tool
	for _, tool := range tools {
		if tool.Name() == "greet" {
			greetTool = tool
			break
		}
	}

	schema := greetTool.InputSchema()
	if schema == nil {
		t.Fatal("expected non-nil schema")
	}

	nameProp, ok := schema.Properties["name"]
	if !ok {
		t.Fatal("missing 'name' property in schema")
	}
	if nameProp.Description != "Name to greet" {
		t.Errorf("expected description 'Name to greet', got %s", nameProp.Description)
	}
}

func TestWrapToolsWithDescriptions(t *testing.T) {
	provider := &TestToolProvider{greeting: "Hello"}
	tools := WrapToolsWithDescriptions(provider)

	// Find greet tool and check description
	for _, tool := range tools {
		if tool.Name() == "greet" {
			if tool.Description() != "Greet someone by name" {
				t.Errorf("expected description 'Greet someone by name', got %s", tool.Description())
			}
			return
		}
	}
	t.Error("greet tool not found")
}

func TestWrapToolsWithDefinitions(t *testing.T) {
	provider := &TestToolProvider{greeting: "Hi"}
	definitions := []ToolDefinition{
		{Name: "say_hello", Description: "Custom greeting", Method: "GreetTool"},
	}

	tools := WrapToolsWithDefinitions(provider, definitions)

	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	tool := tools[0]
	if tool.Name() != "say_hello" {
		t.Errorf("expected name 'say_hello', got %s", tool.Name())
	}
	if tool.Description() != "Custom greeting" {
		t.Errorf("expected description 'Custom greeting', got %s", tool.Description())
	}

	// Execute it
	result, err := tool.Execute(context.Background(), map[string]any{"name": "Test"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Content != "Hi, Test!" {
		t.Errorf("expected 'Hi, Test!', got %v", result.Content)
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"GetWeather", "get_weather"},
		{"HTTP", "h_t_t_p"},
		{"Simple", "simple"},
		{"CamelCase", "camel_case"},
		{"ABCDef", "a_b_c_def"},
	}

	for _, tt := range tests {
		result := toSnakeCase(tt.input)
		if result != tt.expected {
			t.Errorf("toSnakeCase(%s): expected %s, got %s", tt.input, tt.expected, result)
		}
	}
}

func TestMethodTool_ExecuteWithNestedInput(t *testing.T) {
	provider := &TestToolProvider{greeting: "Hello"}
	tools := WrapTools(provider)

	// Find add tool
	var addTool Tool
	for _, tool := range tools {
		if tool.Name() == "add" {
			addTool = tool
			break
		}
	}

	if addTool == nil {
		t.Fatal("add tool not found")
	}

	result, err := addTool.Execute(context.Background(), map[string]any{
		"a": float64(5), // JSON numbers come as float64
		"b": float64(3),
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	resultMap, ok := result.Content.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result.Content)
	}
	if resultMap["result"] != 8 {
		t.Errorf("expected result 8, got %v", resultMap["result"])
	}
}

func TestWrapTools_PanicOnNonPointer(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for non-pointer provider")
		}
	}()

	provider := TestToolProvider{greeting: "Hello"}
	WrapTools(provider) // Should panic
}
