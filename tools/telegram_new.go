package tools

import "time"

// NewTelegramTools creates a new TelegramTools instance.
func NewTelegramTools(token string) *TelegramTools {
	t := &TelegramTools{
		token:        token,
		baseURL:      "https://api.telegram.org/bot" + token,
		pollInterval: 5 * time.Second,
		messages:     make([]TelegramMessage, 0),
		knownChats:   make(map[int64]TelegramChat),
	}
	return t
}
