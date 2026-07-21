// Package main demonstrates basic usage of the gowild_agentic_loop package.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
	"google.golang.org/genai"
)

// WeatherInput defines the input schema for the get_weather tool.
type WeatherInput struct {
	Location string `json:"location" description:"The city and state, e.g. San Francisco, CA"`
	Unit     string `json:"unit,omitempty" description:"Temperature unit" enum:"celsius,fahrenheit"`
}

// CalculatorInput defines the input schema for the calculate tool.
type CalculatorInput struct {
	Expression string `json:"expression" description:"The mathematical expression to evaluate"`
}

// MyTools provides a collection of tools.
type MyTools struct{}

// GetWeatherTool retrieves weather for a location.
// Methods ending in "Tool" with the correct signature are auto-discovered.
func (t *MyTools) GetWeatherTool(ctx context.Context, input WeatherInput) (*loop.ToolResult, error) {
	// In a real implementation, you would call a weather API
	unit := input.Unit
	if unit == "" {
		unit = "fahrenheit"
	}

	temp := 72
	if unit == "celsius" {
		temp = 22
	}

	return loop.NewSuccessResult(map[string]any{
		"location":    input.Location,
		"temperature": temp,
		"unit":        unit,
		"condition":   "Sunny",
	}), nil
}

// CalculateTool evaluates a mathematical expression.
func (t *MyTools) CalculateTool(ctx context.Context, input CalculatorInput) (*loop.ToolResult, error) {
	// Simplified - in real implementation, use a proper expression parser
	return loop.NewSuccessResult(map[string]any{
		"expression": input.Expression,
		"result":     "42", // Placeholder
		"note":       "This is a demo - real implementation would evaluate the expression",
	}), nil
}

// DescribeTool implements ToolProvider to provide tool descriptions.
func (t *MyTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"get_weather": "Get the current weather for a specified location. Use this when the user asks about weather conditions.",
		"calculate":   "Evaluate a mathematical expression. Use this for calculations.",
	}
	return descriptions[name]
}

func main() {
	ctx := context.Background()

	// Create the agentic loop
	agent, err := loop.New(ctx, "", "", // Uses GEMINI_API_KEY env var and default model
		loop.WithSystemPrompt("You are a helpful assistant with access to weather and calculation tools."),
		loop.WithMaxTurns(5),
	)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer agent.Close()

	// Add tools using struct-based discovery
	myTools := &MyTools{}
	agent.AddTools(loop.WrapToolsWithDescriptions(myTools)...)

	// Alternative: Add a functional tool directly
	agent.AddTools(loop.NewFuncTool(
		"get_time",
		"Get the current time",
		&genai.Schema{
			Type:       genai.TypeObject,
			Properties: map[string]*genai.Schema{},
		},
		func(ctx context.Context, input map[string]any) (*loop.ToolResult, error) {
			return loop.NewSuccessResult("The current time is 3:42 PM"), nil
		},
	))

	// Get user prompt
	prompt := "What's the weather like in San Francisco?"
	if len(os.Args) > 1 {
		prompt = os.Args[1]
	}

	fmt.Printf("User: %s\n\n", prompt)

	// Run the agent and stream events
	events := agent.Run(ctx, []loop.Message{loop.NewUserMessage(prompt)})

	fmt.Print("Assistant: ")
	for event := range events {
		switch e := event.(type) {
		case loop.TextDeltaEvent:
			fmt.Print(e.Text)

		case loop.ToolCallEvent:
			fmt.Printf("\n[Calling tool: %s with %v]\n", e.Name, e.Input)

		case loop.ToolResultEvent:
			fmt.Printf("[Tool %s returned: %v]\n", e.Name, e.Result.Content)

		case loop.DoneEvent:
			fmt.Printf("\n\n--- Done (turns: %d, tokens: %d) ---\n",
				e.TurnCount, e.Usage.TotalTokens)

		case loop.ErrorEvent:
			fmt.Printf("\nError: %v\n", e.Err)
		}
	}
}
