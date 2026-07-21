package broker

import (
	"context"

	"github.com/original-david-knight/go_wild/tools"
	loop "github.com/original-david-knight/go_wild/agentic_loop"
)

// TelegramTools proxies Telegram operations through the broker API.
type TelegramTools struct {
	client *Client
}

// NewTelegramTools creates broker-backed telegram tools.
func NewTelegramTools(client *Client) *TelegramTools {
	return &TelegramTools{client: client}
}

func (t *TelegramTools) TelegramSendTool(ctx context.Context, input tools.TelegramSendInput) (*loop.ToolResult, error) {
	result, err := t.client.Post(ctx, "/broker/v1/telegram/send", input)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *TelegramTools) TelegramGetUpdatesTool(ctx context.Context, input tools.TelegramGetUpdatesInput) (*loop.ToolResult, error) {
	path := "/broker/v1/telegram/updates"
	if input.Clear {
		path += "?clear=true"
	}
	result, err := t.client.Get(ctx, path)
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *TelegramTools) TelegramGetChatsTool(ctx context.Context, input tools.TelegramGetChatsInput) (*loop.ToolResult, error) {
	result, err := t.client.Get(ctx, "/broker/v1/telegram/chats")
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

func (t *TelegramTools) TelegramGetBotInfoTool(ctx context.Context, input tools.TelegramGetBotInfoInput) (*loop.ToolResult, error) {
	result, err := t.client.Get(ctx, "/broker/v1/telegram/bot_info")
	if err != nil {
		return loop.NewErrorResult(err.Error()), nil
	}
	return loop.NewSuccessResult(result), nil
}

// DescribeTool implements ToolProvider.
func (t *TelegramTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"telegram_send":         "Send a message to a Telegram chat",
		"telegram_get_updates":  "Get new messages from Telegram",
		"telegram_get_chats":    "List known Telegram chats",
		"telegram_get_bot_info": "Get your Telegram bot information",
	}
	return descriptions[name]
}
