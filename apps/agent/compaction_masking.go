package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/genai"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// =============================================================================
// Observation Masking (Preferred Method)
// =============================================================================
// Observation masking preserves the agent's reasoning trajectory while replacing
// verbose tool outputs with brief placeholders. This outperforms LLM summarization
// by avoiding "trajectory elongation" - where summarization smooths over failure
// details causing the agent to repeat unproductive actions.

// MaskingResult holds the result of observation masking.
type MaskingResult struct {
	MaskedHistory []loop.Message
	MaskedCount   int // Number of tool outputs that were masked
	KeptFullCount int // Number of tool outputs kept in full
}

// maskObservations replaces older tool outputs with placeholders while
// preserving the full reasoning trajectory (user messages, model reasoning, tool calls).
//
// Strategy:
// - Keep ALL user messages intact (they contain instructions and context)
// - Keep ALL model messages intact (they contain reasoning and tool calls)
// - Keep the last `keepRecentOutputs` tool result messages in full
// - Replace older tool results with a one-line placeholder
func maskObservations(history []loop.Message, keepRecentOutputs int) *MaskingResult {
	if keepRecentOutputs < 0 {
		keepRecentOutputs = 3 // Default
	}

	// First pass: identify tool result messages (RoleTool)
	var toolIndices []int
	for i, msg := range history {
		if msg.Role == loop.RoleTool {
			toolIndices = append(toolIndices, i)
		}
	}

	// Determine which tool outputs to mask (all except the last N)
	maskCount := len(toolIndices) - keepRecentOutputs
	if maskCount < 0 {
		maskCount = 0
	}

	// Create set of indices to mask
	indicesToMask := make(map[int]bool)
	for i := 0; i < maskCount; i++ {
		indicesToMask[toolIndices[i]] = true
	}

	// Second pass: build new history with masked tool outputs
	maskedHistory := make([]loop.Message, len(history))
	maskedCount := 0

	for i, msg := range history {
		if indicesToMask[i] {
			// Mask this tool output
			maskedHistory[i] = createMaskedToolMessage(msg)
			maskedCount++
		} else {
			// Keep as-is
			maskedHistory[i] = msg
		}
	}

	return &MaskingResult{
		MaskedHistory: maskedHistory,
		MaskedCount:   maskedCount,
		KeptFullCount: len(toolIndices) - maskedCount,
	}
}

// createMaskedToolMessage creates a placeholder message for a tool output.
// The placeholder preserves the tool name and a brief summary of the output.
func createMaskedToolMessage(msg loop.Message) loop.Message {
	if msg.Content == nil || len(msg.Content.Parts) == 0 {
		return msg
	}

	// Find the FunctionResponse in the message
	var maskedParts []*genai.Part
	for _, part := range msg.Content.Parts {
		if part.FunctionResponse != nil {
			// Create masked response
			toolName := part.FunctionResponse.Name
			originalResponse := part.FunctionResponse.Response
			summary := createOutputSummary(toolName, originalResponse)

			maskedParts = append(maskedParts, &genai.Part{
				FunctionResponse: &genai.FunctionResponse{
					ID:       part.FunctionResponse.ID,
					Name:     toolName,
					Response: map[string]any{"_masked": summary},
				},
			})
		} else {
			// Keep other parts as-is
			maskedParts = append(maskedParts, part)
		}
	}

	return loop.Message{
		Role: msg.Role,
		Content: &genai.Content{
			Role:  msg.Content.Role,
			Parts: maskedParts,
		},
	}
}

// createOutputSummary creates a brief one-line summary of a tool output.
// Examples:
//   - "read_file: 500 lines of Go code"
//   - "run_python: executed successfully, returned dict with 3 keys"
//   - "web_search: 10 results for 'AI agents'"
//   - "run_shell: command completed, 15 lines of output"
//   - "http_request: GET 200 OK, JSON response with 5 fields"
func createOutputSummary(toolName string, response map[string]any) string {
	if response == nil {
		return fmt.Sprintf("[%s: no output]", toolName)
	}

	// Check for errors first
	if errVal, ok := response["error"]; ok {
		errStr := fmt.Sprintf("%v", errVal)
		if len(errStr) > 100 {
			errStr = errStr[:100] + "..."
		}
		return fmt.Sprintf("[%s: ERROR - %s]", toolName, errStr)
	}

	// Tool-specific summaries
	switch toolName {
	case "read_file":
		return summarizeReadFile(response)
	case "list_files":
		return summarizeListFiles(response)
	case "run_python":
		return summarizeRunPython(response)
	case "run_shell":
		return summarizeRunShell(response)
	case "web_search":
		return summarizeWebSearch(response)
	case "http_request":
		return summarizeHTTPRequest(response)
	case "read_webpage":
		return summarizeReadWebpage(response)
	default:
		return summarizeGeneric(toolName, response)
	}
}

func summarizeReadFile(resp map[string]any) string {
	content, _ := resp["content"].(string)
	lines := strings.Count(content, "\n") + 1
	path, _ := resp["path"].(string)
	if path == "" {
		path, _ = resp["file_path"].(string)
	}
	// Guess file type from extension
	fileType := "text"
	if strings.HasSuffix(path, ".go") {
		fileType = "Go code"
	} else if strings.HasSuffix(path, ".py") {
		fileType = "Python code"
	} else if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".ts") {
		fileType = "JavaScript/TypeScript"
	} else if strings.HasSuffix(path, ".md") {
		fileType = "Markdown"
	} else if strings.HasSuffix(path, ".json") {
		fileType = "JSON"
	} else if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		fileType = "YAML"
	}
	return fmt.Sprintf("[read_file: %d lines of %s from %s]", lines, fileType, shortenPath(path))
}

func summarizeListFiles(resp map[string]any) string {
	if files, ok := resp["files"].([]any); ok {
		return fmt.Sprintf("[list_files: %d files/directories]", len(files))
	}
	if files, ok := resp["files"].([]string); ok {
		return fmt.Sprintf("[list_files: %d files/directories]", len(files))
	}
	return "[list_files: completed]"
}

func summarizeRunPython(resp map[string]any) string {
	stdout, _ := resp["stdout"].(string)
	stderr, _ := resp["stderr"].(string)
	exitCode, _ := resp["exit_code"].(float64)

	lines := strings.Count(stdout, "\n")
	if exitCode != 0 {
		errPreview := stderr
		if len(errPreview) > 50 {
			errPreview = errPreview[:50] + "..."
		}
		return fmt.Sprintf("[run_python: exit %d, error: %s]", int(exitCode), errPreview)
	}
	if lines > 0 {
		return fmt.Sprintf("[run_python: success, %d lines of output]", lines)
	}
	return "[run_python: success, no output]"
}

// estimateTokenSavings estimates tokens saved by masking
// (rough estimate: 4 chars per token).
func estimateTokenSavings(original, masked []loop.Message) int {
	originalSize := estimateHistorySize(original)
	maskedSize := estimateHistorySize(masked)
	return (originalSize - maskedSize) / 4
}

func estimateHistorySize(history []loop.Message) int {
	total := 0
	for _, msg := range history {
		total += estimateMessageSize(msg)
	}
	return total
}

func estimateMessageSize(msg loop.Message) int {
	if msg.Content == nil {
		return 0
	}
	total := 0
	for _, part := range msg.Content.Parts {
		if part.Text != "" {
			total += len(part.Text)
		}
		if part.FunctionResponse != nil {
			// Estimate JSON size of response.
			if data, err := json.Marshal(part.FunctionResponse.Response); err == nil {
				total += len(data)
			}
		}
		if part.FunctionCall != nil {
			if data, err := json.Marshal(part.FunctionCall.Args); err == nil {
				total += len(data)
			}
		}
	}
	return total
}

func summarizeRunShell(resp map[string]any) string {
	stdout, _ := resp["stdout"].(string)
	stderr, _ := resp["stderr"].(string)
	exitCode, _ := resp["exit_code"].(float64)

	lines := strings.Count(stdout, "\n")
	if exitCode != 0 {
		errPreview := stderr
		if len(errPreview) > 50 {
			errPreview = errPreview[:50] + "..."
		}
		return fmt.Sprintf("[run_shell: exit %d, error: %s]", int(exitCode), errPreview)
	}
	if lines > 0 {
		return fmt.Sprintf("[run_shell: success, %d lines of output]", lines)
	}
	return "[run_shell: success]"
}

func summarizeWebSearch(resp map[string]any) string {
	if results, ok := resp["results"].([]any); ok {
		query, _ := resp["query"].(string)
		if query != "" && len(query) > 30 {
			query = query[:30] + "..."
		}
		return fmt.Sprintf("[web_search: %d results for '%s']", len(results), query)
	}
	return "[web_search: completed]"
}

func summarizeHTTPRequest(resp map[string]any) string {
	status, _ := resp["status_code"].(float64)
	method, _ := resp["method"].(string)
	if method == "" {
		method = "GET"
	}

	// Check "json" key (JSON responses) or "body" key (non-JSON responses)
	for _, key := range []string{"json", "body"} {
		payload, ok := resp[key]
		if !ok {
			continue
		}
		switch v := payload.(type) {
		case map[string]any:
			return fmt.Sprintf("[http_request: %s %d, JSON with %d fields]", method, int(status), len(v))
		case []any:
			return fmt.Sprintf("[http_request: %s %d, JSON array with %d items]", method, int(status), len(v))
		}
	}
	return fmt.Sprintf("[http_request: %s %d]", method, int(status))
}

func summarizeReadWebpage(resp map[string]any) string {
	content, _ := resp["content"].(string)
	url, _ := resp["url"].(string)
	chars := len(content)
	return fmt.Sprintf("[read_webpage: %d chars from %s]", chars, shortenURL(url))
}

func summarizeGeneric(toolName string, resp map[string]any) string {
	// Try to give a useful summary based on common response patterns
	if result, ok := resp["result"]; ok {
		switch v := result.(type) {
		case string:
			if len(v) > 50 {
				return fmt.Sprintf("[%s: %s...]", toolName, v[:50])
			}
			return fmt.Sprintf("[%s: %s]", toolName, v)
		case bool:
			return fmt.Sprintf("[%s: %v]", toolName, v)
		case float64:
			return fmt.Sprintf("[%s: %v]", toolName, v)
		case map[string]any:
			return fmt.Sprintf("[%s: object with %d fields]", toolName, len(v))
		case []any:
			return fmt.Sprintf("[%s: array with %d items]", toolName, len(v))
		}
	}

	// Fallback: count keys
	return fmt.Sprintf("[%s: response with %d fields]", toolName, len(resp))
}

// shortenPath shortens a file path for display
func shortenPath(path string) string {
	if len(path) <= 40 {
		return path
	}
	// Keep filename and last directory
	parts := strings.Split(path, "/")
	if len(parts) <= 2 {
		return path
	}
	return ".../" + strings.Join(parts[len(parts)-2:], "/")
}

// shortenURL shortens a URL for display
func shortenURL(url string) string {
	if len(url) <= 50 {
		return url
	}
	// Remove protocol and truncate
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	if len(url) > 47 {
		return url[:47] + "..."
	}
	return url
}
