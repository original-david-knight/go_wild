package tools

// TelegramSendInput is the input for sending a message.
type TelegramSendInput struct {
	ChatID    int64  `json:"chat_id" description:"The chat ID to send to. Use telegram_get_chats to find chat IDs." required:"true"`
	Text      string `json:"text" description:"The message text to send" required:"true"`
	ReplyToID int64  `json:"reply_to_id,omitempty" description:"Message ID to reply to (optional)"`
}

// TelegramGetUpdatesInput is the input for getting updates.
type TelegramGetUpdatesInput struct {
	Clear bool `json:"clear,omitempty" description:"If true, clear messages after returning them (default: true)"`
}

// TelegramGetChatsInput is the input for getting known chats.
type TelegramGetChatsInput struct{}

// TelegramGetBotInfoInput is the input for getting bot info.
type TelegramGetBotInfoInput struct{}
