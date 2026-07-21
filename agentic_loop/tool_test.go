package gowild_agentic_loop

import (
	"context"
	"testing"

	"google.golang.org/genai"
)

func TestFuncTool(t *testing.T) {
	tool := NewFuncTool(
		"test_tool",
		"A test tool",
		&genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"input": {Type: genai.TypeString},
			},
		},
		func(ctx context.Context, input map[string]any) (*ToolResult, error) {
			return NewSuccessResult(input["input"]), nil
		},
	)

	if tool.Name() != "test_tool" {
		t.Errorf("expected name 'test_tool', got %s", tool.Name())
	}
	if tool.Description() != "A test tool" {
		t.Errorf("expected description 'A test tool', got %s", tool.Description())
	}

	schema := tool.InputSchema()
	if schema.Type != genai.TypeObject {
		t.Errorf("expected TypeObject schema, got %v", schema.Type)
	}

	result, err := tool.Execute(context.Background(), map[string]any{"input": "hello"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Content != "hello" {
		t.Errorf("expected result 'hello', got %v", result.Content)
	}
}

func TestToFunctionDeclaration(t *testing.T) {
	tool := NewFuncTool(
		"my_func",
		"Does something useful",
		&genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"param": {Type: genai.TypeString, Description: "A parameter"},
			},
			Required: []string{"param"},
		},
		func(ctx context.Context, input map[string]any) (*ToolResult, error) {
			return NewSuccessResult("ok"), nil
		},
	)

	decl := toFunctionDeclaration(tool)

	if decl.Name != "my_func" {
		t.Errorf("expected name 'my_func', got %s", decl.Name)
	}
	if decl.Description != "Does something useful" {
		t.Errorf("expected description 'Does something useful', got %s", decl.Description)
	}
	if decl.Parameters.Type != genai.TypeObject {
		t.Errorf("expected TypeObject parameters, got %v", decl.Parameters.Type)
	}
}

func TestToGeminiTools(t *testing.T) {
	tools := []Tool{
		NewFuncTool("tool1", "desc1", &genai.Schema{Type: genai.TypeObject}, nil),
		NewFuncTool("tool2", "desc2", &genai.Schema{Type: genai.TypeObject}, nil),
	}

	geminiTools := toGeminiTools(tools)

	if len(geminiTools) != 1 {
		t.Fatalf("expected 1 genai.Tool, got %d", len(geminiTools))
	}
	if len(geminiTools[0].FunctionDeclarations) != 2 {
		t.Errorf("expected 2 function declarations, got %d", len(geminiTools[0].FunctionDeclarations))
	}
}

func TestToGeminiTools_Empty(t *testing.T) {
	geminiTools := toGeminiTools(nil)
	if geminiTools != nil {
		t.Errorf("expected nil for empty tools, got %v", geminiTools)
	}

	geminiTools = toGeminiTools([]Tool{})
	if geminiTools != nil {
		t.Errorf("expected nil for empty tools, got %v", geminiTools)
	}
}
