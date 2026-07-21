package tools

import (
	"context"
	"sync"
	"time"
)

// TelegramMessage represents an incoming Telegram message.
type TelegramMessage struct {
	UpdateID  int64     `json:"update_id"`
	MessageID int64     `json:"message_id"`
	ChatID    int64     `json:"chat_id"`
	ChatType  string    `json:"chat_type"` // "private", "group", "supergroup", "channel"
	ChatTitle string    `json:"chat_title,omitempty"`
	FromID    int64     `json:"from_id"`
	FromName  string    `json:"from_name"`
	Username  string    `json:"username,omitempty"`
	Text      string    `json:"text"`
	Date      time.Time `json:"date"`
	ReplyToID int64     `json:"reply_to_id,omitempty"`
}

// TelegramChat represents a chat the bot has interacted with.
type TelegramChat struct {
	ChatID    int64  `json:"chat_id"`
	ChatType  string `json:"chat_type"`
	Title     string `json:"title,omitempty"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
}

// TelegramTools provides Telegram messaging tools.
type TelegramTools struct {
	token        string
	baseURL      string
	botInfo      *botInfo
	pollInterval time.Duration

	// Message buffer for polling
	mu           sync.Mutex
	messages     []TelegramMessage
	lastUpdateID int64
	knownChats   map[int64]TelegramChat

	// Polling control
	polling    bool
	pollCtx    context.Context
	pollCancel context.CancelFunc

	// Callback for new messages (called outside mutex)
	onNewMessages func([]TelegramMessage)
}

type botInfo struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}
