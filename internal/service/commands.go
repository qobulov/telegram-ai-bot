package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/agent"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// process handles one accepted update from start to finish. It uses its own
// context rather than the polling one, so that a shutdown lets the turn in
// progress finish instead of cutting it off.
func (s *Service) process(update telegram.Update) {
	ctx, cancel := context.WithTimeout(context.Background(), s.opts.TurnTimeout)
	defer cancel()

	msg := update.Message
	switch s.limiter.check(msg.From.ID, time.Now()) {
	case deniedFirst:
		s.reply(msg.Chat.ID, textRateLimited)
		return
	case deniedAgain:
		return
	}

	if answer := s.answer(ctx, update); answer != "" {
		s.reply(msg.Chat.ID, answer)
	}
}

// answer returns the text to send back, or "" when nothing should be sent.
func (s *Service) answer(ctx context.Context, update telegram.Update) string {
	msg := update.Message
	text := strings.TrimSpace(msg.Text)
	switch {
	case msg.Text == "":
		return textTextOnly // a photo, sticker, document or other non-text message
	case text == "":
		return ""
	case strings.HasPrefix(text, "/"):
		return s.runCommand(ctx, msg.From.ID, text)
	case utf8.RuneCountInString(text) > s.opts.MaxInputChars:
		return fmt.Sprintf(textTooLong, s.opts.MaxInputChars)
	default:
		return s.ask(ctx, update, text)
	}
}

func (s *Service) ask(ctx context.Context, update telegram.Update, text string) string {
	userID := update.Message.From.ID
	started := time.Now()
	msg := update.Message
	answer, err := s.opts.Replier.Reply(ctx, agent.Request{
		Scope:     storage.Scope{ChatID: msg.Chat.ID, UserID: userID},
		UpdateID:  update.UpdateID,
		MessageID: msg.MessageID,
		Text:      text,
	})
	log := s.opts.Logger.With("user_id", userID, "update_id", update.UpdateID, "duration", time.Since(started).String())

	switch {
	case err == nil:
		log.Info("turn completed")
		return answer.Text
	case errors.Is(err, agent.ErrDuplicate):
		log.Info("duplicate update skipped")
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		log.Warn("turn timed out")
		return textTimeout
	default:
		// The error is logged in full but never shown: it can carry provider
		// or database details the user must not see.
		log.Error("turn failed", "error", err)
		return textError
	}
}

func (s *Service) runCommand(ctx context.Context, userID int64, text string) string {
	command := commandName(text)
	if s.opts.AdminCommands[command] && !s.opts.IsAdmin(userID) {
		s.opts.Logger.Warn("restricted command refused", "user_id", userID, "command", command)
		return textForbidden
	}
	switch command {
	case "/start":
		return textWelcome
	case "/help":
		return helpText
	case "/about":
		return textAbout
	case "/model":
		return fmt.Sprintf(textModel, s.opts.Model)
	case "/reset":
		return s.reset(ctx, userID)
	default:
		return textUnknown
	}
}

func (s *Service) reset(ctx context.Context, userID int64) string {
	if err := s.opts.Resetter.Reset(ctx, storage.Scope{ChatID: userID, UserID: userID}); err != nil {
		s.opts.Logger.Error("reset failed", "user_id", userID, "error", err)
		return textError
	}
	s.opts.Logger.Info("conversation reset", "user_id", userID)
	return textResetDone
}

// commandName extracts the command from text such as "/Help@my_bot extra",
// giving "/help".
func commandName(text string) string {
	first, _, _ := strings.Cut(text, " ")
	name, _, _ := strings.Cut(first, "@")
	return strings.ToLower(name)
}
