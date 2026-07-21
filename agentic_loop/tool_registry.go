package gowild_agentic_loop

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"google.golang.org/genai"
)

// toolMetadata holds metadata for a tool method.
type toolMetadata struct {
	Name        string
	Description string
	InputType   reflect.Type
}

// methodTool wraps a struct method as a Tool.
type methodTool struct {
	metadata  toolMetadata
	method    reflect.Value
	receiver  reflect.Value
	inputType reflect.Type
	schema    *genai.Schema
}

func (t *methodTool) Name() string               { return t.metadata.Name }
func (t *methodTool) Description() string        { return t.metadata.Description }
func (t *methodTool) InputSchema() *genai.Schema { return t.schema }

func (t *methodTool) Execute(ctx context.Context, input map[string]any) (*ToolResult, error) {
	// Create new instance of input struct
	inputVal := reflect.New(t.inputType)
	if err := mapToStruct(input, inputVal.Interface()); err != nil {
		return nil, fmt.Errorf("failed to parse input: %w", err)
	}

	// Call the method: func(ctx, input) (*ToolResult, error)
	results := t.method.Call([]reflect.Value{
		t.receiver,
		reflect.ValueOf(ctx),
		inputVal.Elem(),
	})

	// Handle return values
	var toolResult *ToolResult
	var err error

	if !results[0].IsNil() {
		toolResult = results[0].Interface().(*ToolResult)
	}
	if !results[1].IsNil() {
		err = results[1].Interface().(error)
	}

	return toolResult, err
}

// WrapTools discovers and wraps all registered tool methods on a provider instance.
// Returns a slice of Tools that can be added to an AgenticLoop.
func WrapTools(provider any) []Tool {
	providerVal := reflect.ValueOf(provider)
	providerType := providerVal.Type()

	// Check if provider is a pointer
	if providerType.Kind() != reflect.Ptr {
		panic("WrapTools requires a pointer to struct")
	}

	var tools []Tool

	// Look for methods that match the tool signature
	for i := 0; i < providerType.NumMethod(); i++ {
		method := providerType.Method(i)

		// Check method signature: func(ctx, input) (*ToolResult, error)
		if !isToolMethod(method.Type) {
			continue
		}

		// Extract metadata from method name or tags
		metadata := extractMethodMetadata(method)
		if metadata.Name == "" {
			continue // Not a tool method
		}

		// Get input type (second argument after receiver)
		inputType := method.Type.In(2)

		// Generate schema from input type
		schema := schemaFromType(inputType)

		tools = append(tools, &methodTool{
			metadata:  metadata,
			method:    method.Func,
			receiver:  providerVal,
			inputType: inputType,
			schema:    schema,
		})
	}

	return tools
}

// isToolMethod checks if a method has the correct signature for a tool.
// Expected: func(receiver, context.Context, InputStruct) (*ToolResult, error)
func isToolMethod(t reflect.Type) bool {
	if t.Kind() != reflect.Func {
		return false
	}

	// Check number of inputs (receiver, ctx, input)
	if t.NumIn() != 3 {
		return false
	}

	// Check context.Context as second argument
	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()
	if !t.In(1).Implements(ctxType) {
		return false
	}

	// Check number of outputs (*ToolResult, error)
	if t.NumOut() != 2 {
		return false
	}

	// Check first output is *ToolResult
	if t.Out(0) != reflect.TypeOf((*ToolResult)(nil)) {
		return false
	}

	// Check second output is error
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if !t.Out(1).Implements(errorType) {
		return false
	}

	return true
}

// extractMethodMetadata extracts tool metadata from a method.
// By convention, methods ending with "Tool" are treated as tools.
// The name is derived from the method name (e.g., GetWeatherTool -> get_weather).
func extractMethodMetadata(method reflect.Method) toolMetadata {
	name := method.Name

	// Only process methods ending with "Tool"
	if !strings.HasSuffix(name, "Tool") {
		return toolMetadata{}
	}

	// Remove "Tool" suffix and convert to snake_case
	name = strings.TrimSuffix(name, "Tool")
	name = toSnakeCase(name)

	return toolMetadata{
		Name:        name,
		Description: "", // Will be overridden by DescribeTool
	}
}

// toSnakeCase converts PascalCase to snake_case.
func toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

// ToolProvider is an optional interface that tool providers can implement
// to provide descriptions for their tools.
type ToolProvider interface {
	// DescribeTool returns the description for a tool by name.
	// Return empty string if no description is available.
	DescribeTool(name string) string
}

// WrapToolsWithDescriptions is like WrapTools but also calls DescribeTool
// if the provider implements ToolProvider.
func WrapToolsWithDescriptions(provider any) []Tool {
	tools := WrapTools(provider)

	// Check if provider implements ToolProvider
	if tp, ok := provider.(ToolProvider); ok {
		for _, tool := range tools {
			if mt, ok := tool.(*methodTool); ok {
				desc := tp.DescribeTool(mt.metadata.Name)
				if desc != "" {
					mt.metadata.Description = desc
				}
			}
		}
	}

	return tools
}

// DefineTools is a helper to create tools with explicit definitions.
// This is useful when you want full control over tool metadata.
type ToolDefinition struct {
	Name        string
	Description string
	Method      string // Method name on the provider
}

// WrapToolsWithDefinitions wraps tools using explicit definitions.
func WrapToolsWithDefinitions(provider any, definitions []ToolDefinition) []Tool {
	providerVal := reflect.ValueOf(provider)
	providerType := providerVal.Type()

	if providerType.Kind() != reflect.Ptr {
		panic("WrapToolsWithDefinitions requires a pointer to struct")
	}

	var tools []Tool

	for _, def := range definitions {
		method, ok := providerType.MethodByName(def.Method)
		if !ok {
			panic(fmt.Sprintf("method %s not found on provider", def.Method))
		}

		if !isToolMethod(method.Type) {
			panic(fmt.Sprintf("method %s does not have valid tool signature", def.Method))
		}

		inputType := method.Type.In(2)
		schema := schemaFromType(inputType)

		tools = append(tools, &methodTool{
			metadata: toolMetadata{
				Name:        def.Name,
				Description: def.Description,
				InputType:   inputType,
			},
			method:    method.Func,
			receiver:  providerVal,
			inputType: inputType,
			schema:    schema,
		})
	}

	return tools
}
