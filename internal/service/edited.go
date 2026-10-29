package service

import (
	"context"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// An edited group message is guarded as a new one is: a member who edits a
// swear into a message, or swaps its picture for a nude one, after the
// guards let it through, is caught all the same. Nothing else is done with
// it: the model never answers an edit, the reply quotas lose nothing, and
// the log keeps the first version.

// SwearRecord tells whether the swear guard already caught a message, so
// that editing it is not taken for a second swear.
type SwearRecord interface {
	SwornIn(ctx context.Context, chatID, messageID int64) (bool, error)
}

// guardEdited runs the picture, link and spam guards and the swear guard on
// an edited group message, as acceptGroup runs them on a new one.
func (s *Service) guardEdited(update telegram.Update) {
	msg := update.EditedMessage
	isGroup := msg.Chat.Type == telegram.ChatTypeGroup || msg.Chat.Type == telegram.ChatTypeSupergroup
	if !isGroup || msg.From == nil || !s.acceptsSender(msg) {
		return
	}
	msg = withoutTopicReply(msg)
	in := incoming{
		updateID: update.UpdateID, msg: msg, userID: msg.From.ID, fromBot: msg.From.IsBot,
		scope: storage.Scope{ChatID: msg.Chat.ID}, group: true,
	}
	if s.protected(in) || s.sworeAlready(msg) {
		return
	}
	s.guardedSwearing(msg, update.UpdateID)
}

// sworeAlready reports an edit of a message whose swearing the guard caught
// already: the warning or mute it got stands, and a typo fixed is no new
// strike. When the record cannot be read, the edit is left alone.
func (s *Service) sworeAlready(msg *telegram.Message) bool {
	text, _ := msg.Content()
	if s.opts.Sworn == nil || !isSwearing(text) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	caught, err := s.opts.Sworn.SwornIn(ctx, msg.Chat.ID, msg.MessageID)
	if err != nil {
		s.opts.Logger.Warn("swear guard: could not tell whether an edited message was caught; leaving it", "chat_id", msg.Chat.ID, "error", err)
		return true
	}
	return caught
}
