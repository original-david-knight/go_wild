package main

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/genai"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
	broker "github.com/original-david-knight/go_wild/tools/broker"
)

type summaryFallbackResult struct {
	History          []loop.Message
	SummarizedCount  int
	EstimatedTokens  int
	SummaryGenerated bool
}

func summarizeOlderHistoryFallback(ctx context.Context, history []loop.Message, model string) (*summaryFallbackResult, error) {
	if len(history) == 0 {
		return &summaryFallbackResult{History: history}, nil
	}

	older, newer := splitHistoryByTokenFraction(history, 2.0/3.0)
	if len(older) == 0 || len(newer) == 0 {
		return &summaryFallbackResult{History: history}, nil
	}

	formatted := formatHistoryForCompaction(older)
	prompt := buildCompactionPrompt(formatted)

	var llmClient loop.LLMClient
	if globalBrokerClient != nil {
		llmClient = broker.NewLLMClient(globalBrokerClient, model)
	} else {
		client, err := newConfiguredLLMClient(ctx, model)
		if err != nil {
			return nil, fmt.Errorf("failed to create direct %s client: %w", configuredLLMProvider(), err)
		}
		defer client.Close()
		llmClient = client
	}
	temp := float32(0.2)
	resp, err := llmClient.GenerateContent(ctx, []*genai.Content{
		genai.NewContentFromText(prompt, genai.RoleUser),
	}, &loop.GenerateContentConfig{
		Temperature:     &temp,
		MaxOutputTokens: 4096,
	})
	if err != nil {
		return nil, err
	}

	summaryText := loop.ExtractText(resp.Content)
	if summaryText == "" {
		return nil, fmt.Errorf("empty summary from model")
	}

	summaryMsg := loop.NewUserMessage("<summary>\n" + summaryText + "\n</summary>")
	newHistory := append([]loop.Message{summaryMsg}, newer...)

	return &summaryFallbackResult{
		History:          newHistory,
		SummarizedCount:  len(older),
		EstimatedTokens:  estimateHistorySize(newHistory) / 4,
		SummaryGenerated: true,
	}, nil
}

func splitHistoryByTokenFraction(history []loop.Message, fraction float64) ([]loop.Message, []loop.Message) {
	if len(history) == 0 || fraction <= 0 {
		return nil, history
	}

	totalTokens := estimateHistorySize(history) / 4
	if totalTokens <= 0 {
		// Fallback: split by message count.
		cut := int(float64(len(history)) * fraction)
		if cut < 1 {
			cut = 1
		}
		if cut >= len(history) {
			cut = len(history) - 1
		}
		return history[:cut], history[cut:]
	}

	target := int(float64(totalTokens) * fraction)
	if target < 1 {
		target = 1
	}

	cumulative := 0
	cut := len(history)
	for i, msg := range history {
		cumulative += estimateMessageSize(msg) / 4
		if cumulative >= target {
			cut = i + 1
			break
		}
	}

	if cut < 1 {
		cut = 1
	}
	if cut >= len(history) {
		cut = len(history) - 1
	}

	return history[:cut], history[cut:]
}

// formatHistoryForCompaction converts messages to text format for the prompt.
// Format: "USER [1]: ...\nASSISTANT [2]: ...\nTOOL RESULT [3]: ..."
// Truncates long tool results to 500 chars.
func formatHistoryForCompaction(messages []loop.Message) string {
	var sb strings.Builder

	for i, msg := range messages {
		msgNum := i + 1
		roleLabel := getRoleLabel(msg.Role)
		text := extractMessageText(msg)

		// Truncate very long messages (especially tool results)
		const maxLen = 500
		if len(text) > maxLen {
			text = text[:maxLen] + "... [truncated]"
		}

		sb.WriteString(fmt.Sprintf("%s [%d]: %s\n\n", roleLabel, msgNum, text))
	}

	return sb.String()
}

// getRoleLabel converts a MessageRole to a display label.
func getRoleLabel(role loop.MessageRole) string {
	switch role {
	case loop.RoleUser:
		return "USER"
	case loop.RoleModel:
		return "ASSISTANT"
	case loop.RoleTool:
		return "TOOL RESULT"
	default:
		return "UNKNOWN"
	}
}

// extractMessageText extracts text content from a Message.
func extractMessageText(msg loop.Message) string {
	if msg.Content == nil {
		return ""
	}

	var parts []string
	for _, part := range msg.Content.Parts {
		if part.Thought {
			continue
		}

		if part.Text != "" {
			parts = append(parts, part.Text)
		}

		if fc := part.FunctionCall; fc != nil {
			parts = append(parts, fmt.Sprintf("[Called tool: %s]", fc.Name))
		}

		if fr := part.FunctionResponse; fr != nil {
			if fr.Response != nil {
				parts = append(parts, fmt.Sprintf("[Tool %s result: %v]", fr.Name, summarizeToolResponse(fr.Response)))
			} else {
				parts = append(parts, fmt.Sprintf("[Tool %s completed]", fr.Name))
			}
		}

		if part.InlineData != nil {
			parts = append(parts, "[Image attached]")
		}
	}

	return strings.Join(parts, " ")
}

// summarizeToolResponse creates a brief summary of a tool response.
func summarizeToolResponse(resp map[string]any) string {
	if result, ok := resp["result"]; ok {
		s := fmt.Sprintf("%v", result)
		if len(s) > 200 {
			return s[:200] + "..."
		}
		return s
	}

	if errStr, ok := resp["error"]; ok {
		return fmt.Sprintf("error: %v", errStr)
	}

	keys := make([]string, 0, len(resp))
	for k := range resp {
		keys = append(keys, k)
	}
	return fmt.Sprintf("{%s}", strings.Join(keys, ", "))
}

// buildCompactionPrompt creates the prompt for Gemini to summarize the history.
func buildCompactionPrompt(formattedHistory string) string {
	return fmt.Sprintf(`You are summarizing a conversation history for context compaction. Create a concise summary that preserves:

1. Key facts and decisions made
2. Important context the assistant needs to continue the conversation
3. Any ongoing tasks or incomplete work
4. Technical details that might be referenced later
5. User preferences or constraints mentioned

Write in a factual, chronological style. Preserve enough detail that the assistant can continue naturally without losing context. More recent items in the history are more important - retain more detail for those.

Do NOT include:
- Redundant information
- Casual conversation that doesn't affect context
- Detailed tool outputs (just note what was done)

CONVERSATION HISTORY TO SUMMARIZE:

%s

SUMMARY:`, formattedHistory)
}
