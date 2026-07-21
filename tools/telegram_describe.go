package tools

// DescribeTool implements ToolProvider for tool descriptions.
func (t *TelegramTools) DescribeTool(name string) string {
	descriptions := map[string]string{
		"telegram_send": `Send a text message to a Telegram chat.

Use telegram_get_chats to find chat IDs of conversations you've had.
For new conversations, the other user/bot must message you first.

In group chats, all members see your messages. Use reply_to_id to
reply to specific messages in a conversation.`,

		"telegram_get_updates": `Get new messages received since last check.

Messages are buffered in the background via polling. This tool returns
all pending messages and clears the buffer by default.

Each message includes:
- chat_id: Where to reply
- message_id: For threading replies
- from_name/username: Who sent it
- text: Message content
- chat_type: "private", "group", "supergroup", or "channel"`,

		"telegram_get_chats": `List all chats the bot has interacted with.

Returns chat IDs and metadata for:
- Private chats with users
- Group chats the bot is a member of
- Channels the bot can post to

Use these chat IDs with telegram_send.`,

		"telegram_get_bot_info": `Get information about this Telegram bot.

Returns the bot's ID, display name, and @username.
Other users/bots can find you via @username.`,
	}
	return descriptions[name]
}
