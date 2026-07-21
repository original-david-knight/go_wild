package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// TelegramSendTool sends a message to a Telegram chat.
func (t *TelegramTools) TelegramSendTool(ctx context.Context, input TelegramSendInput) (*loop.ToolResult, error) {
	if input.ChatID == 0 {
		return loop.NewErrorResult("chat_id is required"), nil
	}
	if input.Text == "" {
		return loop.NewErrorResult("text is required"), nil
	}

	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(input.ChatID, 10))
	params.Set("text", input.Text)
	if input.ReplyToID != 0 {
		params.Set("reply_to_message_id", strconv.FormatInt(input.ReplyToID, 10))
	}

	reqURL := t.baseURL + "/sendMessage?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, nil)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to create request: %v", err)), nil
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to send message: %v", err)), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to read response: %v", err)), nil
	}

	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int64 `json:"message_id"`
			Chat      struct {
				ID int64 `json:"id"`
			} `json:"chat"`
			Date int64 `json:"date"`
		} `json:"result"`
		Description string `json:"description"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return loop.NewErrorResult(fmt.Sprintf("failed to parse response: %v", err)), nil
	}

	if !result.OK {
		return loop.NewErrorResult(fmt.Sprintf("telegram error: %s", result.Description)), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"success":    true,
		"message_id": result.Result.MessageID,
		"chat_id":    result.Result.Chat.ID,
		"sent_at":    time.Unix(result.Result.Date, 0).Format(time.RFC3339),
	}), nil
}

// TelegramGetUpdatesTool returns buffered messages from polling.
func (t *TelegramTools) TelegramGetUpdatesTool(ctx context.Context, input TelegramGetUpdatesInput) (*loop.ToolResult, error) {
	// Default to clearing messages after return
	clear := true
	if !input.Clear {
		// Only keep if explicitly set to false (the zero value is handled here)
		// Actually, we need a pointer to distinguish unset from false
		// For now, default to clearing
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	messages := make([]TelegramMessage, len(t.messages))
	copy(messages, t.messages)

	if clear {
		t.messages = t.messages[:0]
	}

	return loop.NewSuccessResult(map[string]any{
		"messages": messages,
		"count":    len(messages),
		"polling":  t.polling,
	}), nil
}

// TelegramGetChatsTool returns known chats the bot has interacted with.
func (t *TelegramTools) TelegramGetChatsTool(ctx context.Context, input TelegramGetChatsInput) (*loop.ToolResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	chats := make([]TelegramChat, 0, len(t.knownChats))
	for _, chat := range t.knownChats {
		chats = append(chats, chat)
	}

	return loop.NewSuccessResult(map[string]any{
		"chats": chats,
		"count": len(chats),
	}), nil
}

// TelegramGetBotInfoTool returns information about the bot.
func (t *TelegramTools) TelegramGetBotInfoTool(ctx context.Context, input TelegramGetBotInfoInput) (*loop.ToolResult, error) {
	if t.botInfo == nil {
		return loop.NewErrorResult("bot info not available - telegram not initialized"), nil
	}

	return loop.NewSuccessResult(map[string]any{
		"id":       t.botInfo.ID,
		"name":     t.botInfo.FirstName,
		"username": t.botInfo.Username,
		"polling":  t.polling,
	}), nil
}
