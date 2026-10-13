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

	// The group may have been muted while this update waited in the queue.
	if in.group && s.isMuted(in.scope.ChatID) && !isMuteCommand(in.text) {
		return
	}
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
	switch s.userLimiter.check(in.userID, now) {
	case deniedFirst:
		return textRateLimited, true
	case deniedAgain:
		return "", true
	}
	if !in.group {
		return "", false
	}
	groupDecision := s.groupLimiter.check(in.scope.ChatID, now)
	if groupDecision == allowed {
		return "", false
	}
	// The group as a whole is over its limit. That is not this member's
	// doing, so it must not use up their own allowance.
	s.userLimiter.refund(in.userID)
	if groupDecision == deniedFirst {
		return textGroupRateLimited, true
	}
	return "", true
}

// respond decides what to send back. An empty outcome sends nothing.
func (s *Service) respond(ctx context.Context, in incoming) outcome {
	switch {
	case strings.HasPrefix(in.text, "/"):
		return say(s.runCommand(ctx, in))
	case utf8.RuneCountInString(in.text) > s.opts.MaxInputChars:
		return say(fmt.Sprintf(textTooLong, s.opts.MaxInputChars))
	case in.text != "":
		return s.ask(ctx, in)
	case hasNoText(in):
		return say(textTextOnly) // a photo, sticker, document or other non-text message
	default:
		return outcome{} // whitespace only
	}
}

// hasNoText reports a message with nothing to read. A caption counts as text
// only in groups, where it is how a photo can address the bot.
func hasNoText(in incoming) bool {
	if in.group {
		content, _ := in.msg.Content()
		return content == ""
	}
	return in.msg.Text == ""
}

func (s *Service) ask(ctx context.Context, in incoming) outcome {
	started := time.Now()
	answer, err := s.opts.Replier.Reply(ctx, s.request(in))
	log := s.opts.Logger.With("user_id", in.userID, "chat_id", in.scope.ChatID,
		"update_id", in.updateID, "duration", time.Since(started).String())

	switch {
	case err == nil:
		log.Info("turn completed")
		return outcome{text: s.fitted(in, answer.Text), answerID: answer.ID}
	case errors.Is(err, agent.ErrSilent):
		log.Info("the model chose not to reply")
		return outcome{}
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

// request builds what the agent needs. In a group the stored text names its
// author ("Bek: ..."), because the conversation is shared, and the recent
// chatter is passed along as context for this turn only.
func (s *Service) request(in incoming) agent.Request {
	req := agent.Request{
		Scope:            in.scope,
		UserID:           in.userID,
		UpdateID:         in.updateID,
		MessageID:        in.msg.MessageID,
		ReplyToMessageID: repliedID(in),
		Text:             in.text,
		Group:            in.group,
	}
	if in.group {
		req.AuthorName = authorLabel(in.msg.From.FirstName)
		req.Text = req.AuthorName + ": " + in.text
		req.Context = s.ambient.block(in.scope.ChatID)
	}
	req.Text = withQuote(req.Text, in.msg.ReplyToMessage, s.opts.Bot.ID)
	return req
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
	return truncateUnits(answer, s.opts.MaxGroupReplyChars)
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
	if s.opts.AdminCommands[command] && !s.opts.IsAdmin(in.userID) {
		s.opts.Logger.Warn("restricted command refused", "user_id", in.userID, "command", command)
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
	case "/mute":
		return s.setMuted(ctx, in, true)
	case "/unmute":
		return s.setMuted(ctx, in, false)
	default:
		return textUnknown
	}
}

// reset deletes the conversation of the chat the command was sent in. In a
// private chat that is the sender's own. In a group it is the one all
// members share, so only the group's administrators may do it.
func (s *Service) reset(ctx context.Context, in incoming) string {
	if refusal := s.refuseUnlessManager(ctx, in); refusal != "" {
		return refusal
	}
	if err := s.opts.Resetter.Reset(ctx, in.scope); err != nil {
		s.opts.Logger.Error("reset failed", "user_id", in.userID, "chat_id", in.scope.ChatID, "error", err)
		return textError
	}
	s.ambient.clear(in.scope.ChatID)
	s.opts.Logger.Info("conversation reset", "user_id", in.userID, "chat_id", in.scope.ChatID)
	return textResetDone
}

// setMuted turns the bot off or on in a group. The setting is stored first
// and applied only if that worked, so memory never disagrees with what a
// restart would load.
func (s *Service) setMuted(ctx context.Context, in incoming, muted bool) string {
	if !in.group {
		return textGroupsOnly
	}
	if refusal := s.refuseUnlessManager(ctx, in); refusal != "" {
		return refusal
	}
	if err := s.opts.Muter.SetMuted(ctx, in.scope.ChatID, muted, in.userID); err != nil {
		s.opts.Logger.Error("mute change failed", "user_id", in.userID, "chat_id", in.scope.ChatID, "error", err)
		return textError
	}
	s.markMuted(in.scope.ChatID, muted)
	s.opts.Logger.Info("mute changed", "user_id", in.userID, "chat_id", in.scope.ChatID, "muted", muted)
	if !muted {
		return textUnmuted
	}
	// What was said before the mute should not resurface after it.
	s.ambient.clear(in.scope.ChatID)
	return textMuted
}

// refuseUnlessManager returns the refusal to send when the sender may not
// manage the bot in this chat, and "" when they may. In a private chat the
// user manages their own conversation. In a group it takes one of the bot's
// administrators or an administrator of that group, as Telegram reports it.
func (s *Service) refuseUnlessManager(ctx context.Context, in incoming) string {
	if !in.group || s.opts.IsAdmin(in.userID) {
		return ""
	}
	isAdmin, err := s.opts.Members.IsGroupAdmin(ctx, in.scope.ChatID, in.userID)
	if err != nil {
		s.opts.Logger.Error("group admin check failed", "user_id", in.userID, "chat_id", in.scope.ChatID, "error", err)
		return textError
	}
	if !isAdmin {
		return textAdminsOnly
	}
	return ""
}

// commandName extracts the command from text such as "/Help@my_bot extra",
// giving "/help".
func commandName(text string) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	name, _, _ := strings.Cut(words[0], "@")
	return strings.ToLower(name)
}
