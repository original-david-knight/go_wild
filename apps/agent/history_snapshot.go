package main

import (
	"context"
	"errors"
	"strings"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

func loadHistorySnapshot(ctx context.Context) ([]loop.Message, error) {
	if globalBrokerClient == nil && globalAgentService == nil {
		return nil, nil
	}

	// Load snapshot messages
	var (
		payload string
		err     error
	)
	if globalBrokerClient != nil {
		result, err := globalBrokerClient.CallTool(ctx, "get_history_snapshot", map[string]any{})
		if err != nil {
			return nil, err
		}
		payload, _ = result["payload"].(string)
	} else {
		snapshot, err := globalAgentService.GetHistorySnapshot(ctx)
		if err != nil {
			return nil, err
		}
		if snapshot != nil {
			payload = snapshot.Payload
		}
	}

	var history []loop.Message
	if payload != "" {
		history, err = loop.DeserializeHistory([]byte(payload))
		if err != nil {
			return nil, err
		}
	}

	// Load separate summary and prepend if present
	var summaryContent string
	if globalBrokerClient != nil {
		summaryResult, err := globalBrokerClient.CallTool(ctx, "get_history_summary", map[string]any{})
		if err != nil {
			if len(history) == 0 {
				return nil, nil
			}
			return history, nil
		}
		summaryContent, _ = summaryResult["content"].(string)
	} else {
		summary, err := globalAgentService.GetHistorySummary(ctx)
		if err == nil && summary != nil {
			summaryContent = summary.Content
		}
	}
	if summaryContent != "" {
		summaryMsg := loop.NewUserMessage("<summary>\n" + summaryContent + "\n</summary>")
		history = append([]loop.Message{summaryMsg}, history...)
	}

	if len(history) == 0 {
		return nil, nil
	}
	return history, nil
}

func saveHistorySnapshot(ctx context.Context, history []loop.Message) error {
	if globalBrokerClient == nil && globalAgentService == nil {
		return nil
	}
	if len(history) == 0 {
		var errs []error
		if err := deleteHistorySummary(ctx); err != nil {
			errs = append(errs, err)
		}
		if globalBrokerClient != nil {
			if _, err := globalBrokerClient.CallTool(ctx, "delete_history_snapshot", map[string]any{}); err != nil {
				errs = append(errs, err)
			}
		} else if err := globalAgentService.DeleteHistorySnapshot(ctx); err != nil {
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	}

	// If the first message is a summary, extract and save separately
	toSave := history
	if isSummaryMessage(history[0]) {
		summaryText := extractSummaryContent(history[0])
		if summaryText != "" {
			if globalBrokerClient != nil {
				_, err := globalBrokerClient.CallTool(ctx, "save_history_summary", map[string]any{
					"content": summaryText,
				})
				if err != nil {
					return err
				}
			} else if err := globalAgentService.SaveHistorySummary(ctx, summaryText); err != nil {
				return err
			}
		}
		toSave = history[1:]
	}

	if len(toSave) == 0 {
		// Only a summary, no snapshot messages to save
		if globalBrokerClient != nil {
			_, err := globalBrokerClient.CallTool(ctx, "delete_history_snapshot", map[string]any{})
			return err
		}
		return globalAgentService.DeleteHistorySnapshot(ctx)
	}

	payload, err := loop.SerializeHistory(toSave)
	if err != nil {
		return err
	}

	if globalBrokerClient != nil {
		_, err = globalBrokerClient.CallTool(ctx, "save_history_snapshot", map[string]any{
			"payload": string(payload),
		})
		return err
	}
	return globalAgentService.SaveHistorySnapshot(ctx, string(payload))
}

func deleteHistorySummary(ctx context.Context) error {
	if globalBrokerClient == nil && globalAgentService == nil {
		return nil
	}
	if globalBrokerClient != nil {
		_, err := globalBrokerClient.CallTool(ctx, "delete_history_summary", map[string]any{})
		return err
	}
	return globalAgentService.DeleteHistorySummary(ctx)
}

// isSummaryMessage checks if a message is a compaction summary.
func isSummaryMessage(msg loop.Message) bool {
	text := loop.ExtractText(msg.Content)
	return strings.HasPrefix(strings.TrimSpace(text), "<summary>")
}

// extractSummaryContent extracts the text between <summary> tags.
func extractSummaryContent(msg loop.Message) string {
	text := loop.ExtractText(msg.Content)
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "<summary>")
	text = strings.TrimSuffix(text, "</summary>")
	return strings.TrimSpace(text)
}
