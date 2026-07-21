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
)

// Start begins background polling for messages.
func (t *TelegramTools) Start(ctx context.Context) error {
	// Get bot info first
	info, err := t.getMe(ctx)
	if err != nil {
		return fmt.Errorf("failed to get bot info: %w", err)
	}
	t.botInfo = info

	// Start polling
	t.pollCtx, t.pollCancel = context.WithCancel(ctx)
	t.polling = true
	go t.pollLoop()

	return nil
}

// Stop stops background polling.
func (t *TelegramTools) Stop() {
	if t.pollCancel != nil {
		t.pollCancel()
	}
	t.polling = false
}

// GetBotUsername returns the bot's username.
func (t *TelegramTools) GetBotUsername() string {
	if t.botInfo != nil {
		return t.botInfo.Username
	}
	return ""
}

// SetOnNewMessages sets a callback invoked when new messages arrive.
// The callback is called asynchronously (in a goroutine) to avoid holding the mutex.
func (t *TelegramTools) SetOnNewMessages(fn func([]TelegramMessage)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onNewMessages = fn
}

// HasPendingMessages returns true if there are unread messages.
func (t *TelegramTools) HasPendingMessages() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.messages) > 0
}

// PendingMessageCount returns the number of pending messages.
func (t *TelegramTools) PendingMessageCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.messages)
}

// pollLoop continuously polls for new messages.
func (t *TelegramTools) pollLoop() {
	ticker := time.NewTicker(t.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.pollCtx.Done():
			return
		case <-ticker.C:
			t.fetchUpdates(t.pollCtx)
		}
	}
}

// fetchUpdates fetches new messages from Telegram.
func (t *TelegramTools) fetchUpdates(ctx context.Context) {
	t.mu.Lock()
	offset := t.lastUpdateID + 1
	t.mu.Unlock()

	params := url.Values{}
	params.Set("offset", strconv.FormatInt(offset, 10))
	params.Set("timeout", "1") // Short timeout for polling
	params.Set("allowed_updates", `["message"]`)

	reqURL := t.baseURL + "/getUpdates?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var result struct {
		OK     bool `json:"ok"`
		Result []struct {
			UpdateID int64 `json:"update_id"`
			Message  *struct {
				MessageID int64 `json:"message_id"`
				From      *struct {
					ID        int64  `json:"id"`
					FirstName string `json:"first_name"`
					LastName  string `json:"last_name"`
					Username  string `json:"username"`
				} `json:"from"`
				Chat *struct {
					ID        int64  `json:"id"`
					Type      string `json:"type"`
					Title     string `json:"title"`
					Username  string `json:"username"`
					FirstName string `json:"first_name"`
					LastName  string `json:"last_name"`
				} `json:"chat"`
				Date           int64  `json:"date"`
				Text           string `json:"text"`
				ReplyToMessage *struct {
					MessageID int64 `json:"message_id"`
				} `json:"reply_to_message"`
			} `json:"message"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil || !result.OK {
		return
	}

	t.mu.Lock()

	var newMessages []TelegramMessage

	for _, update := range result.Result {
		if update.Message == nil || update.Message.Text == "" {
			// Skip non-text messages for now
			if update.UpdateID > t.lastUpdateID {
				t.lastUpdateID = update.UpdateID
			}
			continue
		}

		msg := update.Message
		tm := TelegramMessage{
			UpdateID:  update.UpdateID,
			MessageID: msg.MessageID,
			ChatID:    msg.Chat.ID,
			ChatType:  msg.Chat.Type,
			ChatTitle: msg.Chat.Title,
			Text:      msg.Text,
			Date:      time.Unix(msg.Date, 0),
		}

		if msg.From != nil {
			tm.FromID = msg.From.ID
			tm.FromName = msg.From.FirstName
			if msg.From.LastName != "" {
				tm.FromName += " " + msg.From.LastName
			}
			tm.Username = msg.From.Username
		}

		if msg.ReplyToMessage != nil {
			tm.ReplyToID = msg.ReplyToMessage.MessageID
		}

		t.messages = append(t.messages, tm)
		newMessages = append(newMessages, tm)

		// Track known chats
		chat := TelegramChat{
			ChatID:   msg.Chat.ID,
			ChatType: msg.Chat.Type,
			Title:    msg.Chat.Title,
			Username: msg.Chat.Username,
		}
		if msg.Chat.FirstName != "" {
			chat.FirstName = msg.Chat.FirstName
			chat.LastName = msg.Chat.LastName
		}
		t.knownChats[msg.Chat.ID] = chat

		if update.UpdateID > t.lastUpdateID {
			t.lastUpdateID = update.UpdateID
		}
	}

	cb := t.onNewMessages
	t.mu.Unlock()

	// Call callback outside mutex to avoid deadlocks
	if cb != nil && len(newMessages) > 0 {
		go cb(newMessages)
	}
}
