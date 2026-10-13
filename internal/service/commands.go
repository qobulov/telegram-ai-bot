package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/agent"
)

// outcome is what handling a message produced: the text to send back, and
// for a model answer, the ID of its stored row.
type outcome struct {
	text     string
	answerID int64
}

func say(text string) outcome { return outcome{text: text} }

// process handles one accepted update from start to finish. It uses its own
// context rather than the polling one, so that a shutdown lets the turn in
// progress finish instead of cutting it off.
func (s *Service) process(in incoming) {
	ctx, cancel := context.WithTimeout(context.Background(), s.opts.TurnTimeout)
	defer cancel()

	if notice, limited := s.rateLimited(in); limited {
		if notice != "" {
			s.reply(in, notice)
		}
		return
	}
	out := s.respond(ctx, in)
	if out.text == "" {
		return
	}
	sentID := s.reply(in, out.text)
	if out.answerID != 0 && sentID != 0 {
		s.recordSent(out.answerID, sentID)
	}
}

// rateLimited applies the per-user limit and, in a group, the per-group
// limit. It returns the notice to send, which is empty when the sender was
// already told: answering every message of a flood would itself be a flood.
func (s *Service) rateLimited(in incoming) (notice string, limited bool) {
	now := time.Now()
	switch s.userLimiter.check(in.scope.UserID, now) {
	case deniedFirst:
		return textRateLimited, true
	case deniedAgain:
		return "", true
	}
	if !in.group {
		return "", false
	}
	switch s.groupLimiter.check(in.scope.ChatID, now) {
	case deniedFirst:
		return textGroupRateLimited, true
	case deniedAgain:
		return "", true
	}
	return "", false
}

// respond decides what to send back. An empty outcome sends nothing.
func (s *Service) respond(ctx context.Context, in incoming) outcome {
	content, _ := in.msg.Content()
	quoted := quotedText(in.msg.ReplyToMessage)
	switch {
	case strings.HasPrefix(in.text, "/"):
		return say(s.runCommand(ctx, in))
	case in.text != "" && utf8.RuneCountInString(in.text) > s.opts.MaxInputChars:
		return say(fmt.Sprintf(textTooLong, s.opts.MaxInputChars))
	case in.text != "":
		return s.ask(ctx, in)
	case !in.group && in.msg.Text == "", in.group && content == "":
		return say(textTextOnly) // a photo, sticker, document or other non-text message
	case in.group && quoted != "":
		return s.ask(ctx, in) // "@bot" as a reply: the question is the quoted message
	case in.group:
		return say(textMentionHint)
	default:
		return outcome{} // whitespace only
	}
}

func (s *Service) ask(ctx context.Context, in incoming) outcome {
	started := time.Now()
	answer, err := s.opts.Replier.Reply(ctx, agent.Request{
		Scope:            in.scope,
		UpdateID:         in.updateID,
		MessageID:        in.msg.MessageID,
		ReplyToMessageID: repliedID(in),
		Text:             withQuote(in.text, in.msg.ReplyToMessage, s.opts.Bot.ID),
		Group:            in.group,
	})
	log := s.opts.Logger.With("user_id", in.scope.UserID, "chat_id", in.scope.ChatID,
		"update_id", in.updateID, "duration", time.Since(started).String())

	switch {
	case err == nil:
		log.Info("turn completed")
		return outcome{text: s.fitted(in, answer.Text), answerID: answer.ID}
	case errors.Is(err, agent.ErrDuplicate):
		log.Info("duplicate update skipped")
		return outcome{}
	case errors.Is(err, context.DeadlineExceeded):
		log.Warn("turn timed out")
		return say(textTimeout)
	default:
		// The error is logged in full but never shown: it can carry provider
		// or database details the user must not see.
		log.Error("turn failed", "error", err)
		return say(textError)
	}
}

func repliedID(in incoming) int64 {
	if in.msg.ReplyToMessage == nil {
		return 0
	}
	return in.msg.ReplyToMessage.MessageID
}

// fitted shortens an answer for a group so the bot posts a single message
// there. Private answers are left whole and split by the sender if needed.
func (s *Service) fitted(in incoming, answer string) string {
	if !in.group {
		return answer
	}
	return truncate(answer, s.opts.MaxGroupReplyChars)
}

// recordSent stores which Telegram message carries the answer. It is
// bookkeeping only: if it fails, the answer was still delivered.
func (s *Service) recordSent(answerID, sentID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if err := s.opts.Recorder.SetSentMessageID(ctx, answerID, sentID); err != nil {
		s.opts.Logger.Warn("could not record sent message ID", "error", err)
	}
}

func (s *Service) runCommand(ctx context.Context, in incoming) string {
	command := commandName(in.text)
	userID := in.scope.UserID
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
		return s.reset(ctx, in)
	default:
		return textUnknown
	}
}

// reset deletes the sender's conversation in the chat the command was sent
// in, and nothing else: not their other chats, and not other members' history.
func (s *Service) reset(ctx context.Context, in incoming) string {
	if err := s.opts.Resetter.Reset(ctx, in.scope); err != nil {
		s.opts.Logger.Error("reset failed", "user_id", in.scope.UserID, "chat_id", in.scope.ChatID, "error", err)
		return textError
	}
	s.opts.Logger.Info("conversation reset", "user_id", in.scope.UserID, "chat_id", in.scope.ChatID)
	return textResetDone
}

// commandName extracts the command from text such as "/Help@my_bot extra",
// giving "/help".
func commandName(text string) string {
	first, _, _ := strings.Cut(text, " ")
	name, _, _ := strings.Cut(first, "@")
	return strings.ToLower(name)
}
