package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/agent"
	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/telegram"
	"telegram-ai-bot/internal/tools"
)

// outcome is what handling a message produced: the text to send back, and
// for a model answer, the ID of its stored row.
type outcome struct {
	text     string
	answerID int64
	// drawings are pictures made during the turn, sent with the text.
	drawings []tools.Drawing
}

func say(text string) outcome { return outcome{text: text} }

// process handles one accepted update from start to finish. It uses its own
// context rather than the polling one, so that a shutdown lets the turn in
// progress finish instead of cutting it off.
func (s *Service) process(in incoming) {
	ctx, cancel := context.WithTimeout(context.Background(), s.opts.TurnTimeout)
	defer cancel()

	// A muted group is ignored, except for the commands that control muting.
	// This is checked here, in queue order, so that a message sent right
	// after /unmute is answered and one sent right after /mute is not.
	if s.silenced(in) {
		return
	}
	if in.fromBot && !s.mayAnswerBot(in) {
		return
	}
	if notice, limited := s.rateLimited(in); limited {
		if notice != "" && !in.fromBot {
			s.reply(in, notice)
		}
		return
	}
	in.described = s.describe(ctx, in.msg, in.text)
	out := s.respond(ctx, in)
	if sentID := s.deliver(in, out); out.answerID != 0 && sentID != 0 {
		s.recordSent(out.answerID, sentID)
	}
	s.logExchange(in, out)
}

// deliver sends the outcome and returns the ID of the message that carries
// the answer. A picture goes first, with the answer as its caption when it
// fits; a longer answer follows as its own message.
func (s *Service) deliver(in incoming, out outcome) int64 {
	if len(out.drawings) == 0 {
		if out.text == "" {
			return 0
		}
		return s.reply(in, out.text)
	}
	caption := out.text
	if utf8.RuneCountInString(caption) > telegram.CaptionLimit {
		caption = ""
	}
	var answerID int64
	for i, drawing := range out.drawings {
		text := ""
		if i == 0 {
			text = caption
		}
		if id := s.replyPhoto(in, drawing, text); answerID == 0 {
			answerID = id
		}
	}
	if caption == "" && out.text != "" {
		answerID = s.reply(in, out.text)
	}
	return answerID
}

func (s *Service) replyPhoto(in incoming, drawing tools.Drawing, caption string) int64 {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	sentID, err := s.opts.Sender.SendPhoto(ctx, telegram.Photo{
		ChatID:           in.msg.Chat.ID,
		ReplyToMessageID: in.replyTarget(),
		Data:             drawing.Data,
		Caption:          caption,
	})
	if err != nil {
		s.opts.Logger.Error("send photo failed", "chat_id", in.msg.Chat.ID, "error", err)
		return 0
	}
	return sentID
}

// logExchange adds an addressed group message, and the bot's answer to it,
// to the day's log, after the answer: logged before, the question would
// also appear in its own context.
func (s *Service) logExchange(in incoming, out outcome) {
	if !in.group || strings.HasPrefix(in.text, "/") {
		return
	}
	s.ambient.addMessage(in.msg, s.label(in.msg.From), in.described)
	if out.answerID != 0 {
		answer := out.text
		if len(out.drawings) > 0 {
			answer = "<photo> " + answer
		}
		s.ambient.add(in.scope.ChatID, 0, s.opts.Bot.ID, botAuthor, answer)
	}
}

// botWindow is the period BotRepliesPer10Min counts over.
const botWindow = 10 * time.Minute

// mayAnswerBot takes one of the chat's replies to bots. Past the limit, or
// when the count cannot be read, the bot stays silent: two bots answering
// each other must not run on.
func (s *Service) mayAnswerBot(in incoming) bool {
	if s.opts.BotReplies == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	ok, err := s.opts.BotReplies.ReserveBotReply(ctx, in.scope.ChatID, in.userID, time.Now().Add(-botWindow), s.opts.BotRepliesPer10Min)
	if err != nil {
		s.opts.Logger.Warn("bot reply count failed; staying quiet", "chat_id", in.scope.ChatID, "error", err)
		return false
	}
	if !ok {
		s.opts.Logger.Info("bot conversation limit reached", "chat_id", in.scope.ChatID, "bot_id", in.userID)
	}
	return ok
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
	// The group limit does not apply to /mute and /unmute: the off switch
	// has to work precisely when the group is flooding the bot.
	if !in.group || isMuteCommand(in.text) {
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
	case in.text != "" || s.hasMedia(in):
		return s.ask(ctx, in)
	case hasNoText(in):
		return say(textTextOnly) // a document, video, location or other kind the bot does not read
	default:
		return outcome{} // whitespace only
	}
}

// hasMedia reports a photo, sticker, GIF or voice message the bot can be
// shown, on the message itself or on the one it replies to.
func (s *Service) hasMedia(in incoming) bool {
	_, own := s.mediaOf(in.msg)
	_, replied := s.mediaOf(in.msg.ReplyToMessage)
	return own || (replied && in.group)
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
	sink := &tools.Sink{}
	ctx = tools.WithTurn(ctx, tools.Caller{UserID: in.userID, ChatID: in.scope.ChatID, Owner: s.isOwner(in.msg.From)}, sink)
	out := s.answerOf(ctx, in, started)
	// A picture drawn before a later failure is still worth sending.
	out.drawings = sink.Take()
	return out
}

func (s *Service) answerOf(ctx context.Context, in incoming, started time.Time) outcome {
	answer, err := s.opts.Replier.Reply(ctx, s.request(ctx, in))
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
	case ai.IsOverloaded(err):
		log.Warn("model provider overloaded or over its limits", "error", err)
		return say(textOverloaded)
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
func (s *Service) request(ctx context.Context, in incoming) agent.Request {
	req := agent.Request{
		Scope:            in.scope,
		UserID:           in.userID,
		UpdateID:         in.updateID,
		MessageID:        in.msg.MessageID,
		ReplyToMessageID: repliedID(in),
		Text:             in.text,
		Group:            in.group,
		Attachments:      s.attachments(ctx, in),
	}
	if _, ok := s.mediaOf(in.msg); ok {
		req.Text = in.described
	}
	if !in.group && s.isOwner(in.msg.From) {
		req.Context = ownerNotice
	}
	if in.group {
		req.AuthorName = s.label(in.msg.From)
		req.Text = req.AuthorName + ": " + oneLine(req.Text)
		req.Context = s.ambient.block(in.scope.ChatID, time.Now())
	}
	req.Text = withQuote(req.Text, s.quoted(ctx, in.msg.ReplyToMessage), s.opts.Bot.ID)
	return req
}

// oneLine folds a group message into a single line without square brackets.
// A group's history is shared and replayed to every member, and each line in
// it starts with the name of who said it. Left as written, a message could
// contain a line break followed by "Ann: ..." or a marker, and so put words
// in someone else's mouth for everyone who talks to the bot afterwards.
func oneLine(text string) string {
	return strings.Join(strings.Fields(bracketless.Replace(text)), " ")
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
	// What was said before or during the mute should not resurface after it.
	s.ambient.clear(in.scope.ChatID)
	if muted {
		return textMuted
	}
	return textUnmuted
}

// refuseUnlessManager returns the refusal to send when the sender may not
// manage the bot in this chat, and "" when they may. In a private chat the
// user manages their own conversation. In a group it takes one of the bot's
// administrators or an administrator of that group, as Telegram reports it.
func (s *Service) refuseUnlessManager(ctx context.Context, in incoming) string {
	if !in.group || s.opts.IsAdmin(in.userID) || s.isOwner(in.msg.From) {
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
