package gowild_agentic_loop

import (
	"context"

	"google.golang.org/genai"
)

// Tool defines the interface for agentic tools.
type Tool interface {
	// Name returns the unique name of the tool.
	Name() string

	// Description returns a description of what the tool does.
	Description() string

	// InputSchema returns the JSON Schema for the tool's input parameters.
	InputSchema() *genai.Schema

	// Execute runs the tool with the given input.
	Execute(ctx context.Context, input map[string]any) (*ToolResult, error)
}

// FuncTool is a simple function-based tool implementation.
type FuncTool struct {
	name        string
	description string
	schema      *genai.Schema
	fn          func(ctx context.Context, input map[string]any) (*ToolResult, error)
}

// NewFuncTool creates a new function-based tool.
func NewFuncTool(
	name string,
	description string,
	schema *genai.Schema,
	fn func(ctx context.Context, input map[string]any) (*ToolResult, error),
) *FuncTool {
	return &FuncTool{
		name:        name,
		description: description,
		schema:      schema,
		fn:          fn,
	}
}

func (t *FuncTool) Name() string               { return t.name }
func (t *FuncTool) Description() string        { return t.description }
func (t *FuncTool) InputSchema() *genai.Schema { return t.schema }

func (t *FuncTool) Execute(ctx context.Context, input map[string]any) (*ToolResult, error) {
	return t.fn(ctx, input)
}

// toFunctionDeclaration converts a Tool to a Gemini FunctionDeclaration.
func toFunctionDeclaration(tool Tool) *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:        tool.Name(),
		Description: tool.Description(),
		Parameters:  tool.InputSchema(),
	}
}

// toGeminiTools converts a slice of Tools to Gemini Tool format.
func toGeminiTools(tools []Tool) []*genai.Tool {
	if len(tools) == 0 {
		return nil
	}

	declarations := make([]*genai.FunctionDeclaration, len(tools))
	for i, tool := range tools {
		declarations[i] = toFunctionDeclaration(tool)
	}

	return []*genai.Tool{
		{FunctionDeclarations: declarations},
	}
}
