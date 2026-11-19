package service

import (
	"fmt"

	"telegram-ai-bot/internal/telegram"
)

// pressUpdate is a press of data, by from, on a message showing text in
// chat.
func pressUpdate(id int, from *telegram.User, chat telegram.Chat, text, data string) telegram.Update {
	nextUpdateID++
	return telegram.Update{UpdateID: nextUpdateID, CallbackQuery: &telegram.CallbackQuery{
		ID: fmt.Sprint(id), From: from, Data: data,
		Message: &telegram.Message{MessageID: 80, Chat: chat, Text: text},
	}}
}
