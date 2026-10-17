package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/agent"
	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
	"telegram-ai-bot/internal/tools"
)

// outcome is what handling a message produced: the text to send back, and
// for a model answer, the ID of its stored row.
type outcome struct {
	text     string
	answerID int64
	// reaction is the emoji the model answered with instead of text. It is
	// set on the message being answered; no message is sent for it.
	reaction string
	// drawings are pictures made during the turn, sent with the text.
	drawings []tools.Drawing
	// duplicate marks an update that was already answered: Telegram sent it
	// again after a slow turn. The exchange is in the log already.
	duplicate bool
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
		if notice != "" {
			s.reply(in, notice)
		}
		s.passOver(in)
		return
	}
	// An admin's yes to the bot's question is handled here, in code, before
	// it could be taken for applause or run into the group's quota.
	if s.confirmedModeration(ctx, in) {
		return
	}
	// So is an owner's order not to answer a member.
	if s.obeyedIgnoreOrder(ctx, in) {
		return
	}
	// A member's order not to answer someone is not theirs to give. It is not
	// even logged: from the log the model would repeat it to that member.
	if s.isOthersIgnoreOrder(in) {
		return
	}
	rules := s.loadRules(ctx, in)
	// An ignored member gets nothing, not even a reaction, and takes none of
	// the group's quota. The managing commands still work for them if they
	// may use them, as they do over the quota. The flood guard above comes
	// first, so that a flood of theirs cannot ask Telegram, or the rules,
	// without end.
	if s.ignored(in, rules) && !s.isManagerCommand(in) {
		s.passOver(in)
		return
	}
	// Applause is answered with a reaction, which takes nothing from the
	// group's quota of answers.
	if s.reactedToApplause(in) {
		return
	}
	reservation, allowed := s.mayAnswerInGroup(in)
	if !allowed {
		s.passOver(in)
		return
	}
	in.groupReply = reservation
	if in.group {
		// The group's log needs it even when no answer follows. A private
		// message is described in ask, within the daily limit.
		in.described = s.describe(ctx, in.msg, in.text)
	}
	out := s.respond(ctx, in, rules)
	if sentID := s.deliver(in, out); out.answerID != 0 && sentID != 0 {
		s.recordSent(out.answerID, sentID)
	}
	s.logExchange(in, out)
}

// deliver sends the outcome and returns the ID of the message that carries
// the answer. A picture goes first, with the answer as its caption when it
// fits; a longer answer follows as its own message. A reaction is set on the
// message being answered and is not a message, so alone it returns 0.
func (s *Service) deliver(in incoming, out outcome) int64 {
	if out.reaction != "" {
		s.react(in, out.reaction)
	}
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
// to the group's log, after the answer: logged before, the question would
// also appear in its own context.
func (s *Service) logExchange(in incoming, out outcome) {
	if !in.group || out.duplicate || strings.HasPrefix(in.text, "/") {
		return
	}
	s.logFrom(in.msg, in.described)
	if out.answerID != 0 {
		answer := out.text
		if out.reaction != "" {
			answer = "<reacted " + out.reaction + ">"
		}
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

// rateLimited applies the per-user limit, a flood guard kept in memory. It
// returns the notice to send, which is empty when the sender was already
// told: answering every message of a flood would itself be a flood. A group
// is never sent a notice; there the bot just stays quiet.
func (s *Service) rateLimited(in incoming) (notice string, limited bool) {
	switch s.userLimiter.check(in.userID, time.Now()) {
	case deniedFirst:
		if in.group {
			return "", true
		}
		return textRateLimited, true
	case deniedAgain:
		return "", true
	}
	return "", false
}

// overQuotaReaction is set on a message the quota leaves unanswered, so
// that the bot is seen to have read it: silence alone looked as if it were
// broken.
const overQuotaReaction = "👀"

// mayAnswerInGroup takes one of the group's answers from the quota kept in
// the database, and reports whether there was one. Past either limit the
// bot answers with a reaction only, without a notice: a notice is one more
// message in a group that already has too many. When the count cannot be
// read it stays silent, like mayAnswerBot.
//
// Owners are not counted, other bots have their own quota, and the
// commands that manage the bot always pass for those who may use them: they
// have to work precisely when the group is flooding the bot. So does a
// group admin's message, past the quota, that replies to another member's:
// that is how moderation and ignoring are asked for. A message with nothing
// the bot can read is not counted either: respond sends no answer to it.
//
// It also returns the answer taken, to give back if the turn answers
// nothing; 0 when none was taken.
func (s *Service) mayAnswerInGroup(in incoming) (reservation int64, allowed bool) {
	if !in.group || in.fromBot || s.opts.GroupReplies == nil || s.isOwner(in.msg.From) || !s.hasContent(in) || s.isManagerCommand(in) {
		return 0, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	id, ok, err := s.opts.GroupReplies.ReserveGroupReply(ctx, storage.ReplyQuota{
		ChatID:    in.scope.ChatID,
		UserID:    in.userID,
		PerMinute: s.opts.GroupRatePerMinute,
		PerMember: s.opts.MemberRepliesPer10Min,
	})
	if err != nil {
		s.opts.Logger.Warn("group reply count failed; staying quiet", "chat_id", in.scope.ChatID, "error", err)
		return 0, false
	}
	switch {
	case ok:
		return id, true
	case s.isAdminActingOnAMember(in):
		s.opts.Logger.Info("group reply limit reached, but an admin replied to a member", "chat_id", in.scope.ChatID, "user_id", in.userID)
		return 0, true
	}
	s.opts.Logger.Info("group reply limit reached", "chat_id", in.scope.ChatID, "user_id", in.userID)
	s.react(in, overQuotaReaction)
	return 0, false
}

// managerCommands manage the bot in a group, and are for its admins.
var managerCommands = map[string]bool{"/mute": true, "/unmute": true, "/ignore": true, "/unignore": true, "/reset": true}

// isManagerCommand reports one of managerCommands from someone who may use
// it. Anyone else's is counted like any other message: it only gets a
// refusal, and refusals sent without limit would flood the group themselves.
func (s *Service) isManagerCommand(in incoming) bool {
	if !managerCommands[commandName(in.text)] {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	return s.refuseUnlessManager(ctx, in) == ""
}

// isAdminActingOnAMember reports a group admin's message that replies to
// another member's, as asking to moderate or ignore them does. Telegram is
// asked only then, past the quota.
func (s *Service) isAdminActingOnAMember(in incoming) bool {
	replied := in.msg.ReplyToMessage
	if replied == nil || replied.From == nil || replied.From.ID == s.opts.Bot.ID || replied.From.ID == in.userID {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	isAdmin, err := s.opts.Members.IsGroupAdmin(ctx, in.scope.ChatID, in.userID)
	if err != nil {
		s.opts.Logger.Warn("group admin check failed", "user_id", in.userID, "chat_id", in.scope.ChatID, "error", err)
	}
	return isAdmin && err == nil
}

// passOver logs a group message the bot will not answer, as unaddressed
// chatter is logged, so that its next answer knows what was said while it
// was quiet. Commands are left out, as logExchange leaves them out.
func (s *Service) passOver(in incoming) {
	if in.group && !strings.HasPrefix(in.text, "/") {
		s.remember(in.msg)
	}
}

// loadRules reads the standing rules for this turn, once: a group's own and
// those that hold everywhere, such as how an owner wants to be addressed. In
// a private chat only an owner has any. When they cannot be read the turn
// goes on without them: answering an ignored member once is better than
// going quiet for the whole group.
func (s *Service) loadRules(ctx context.Context, in incoming) []storage.GroupRule {
	if s.opts.Rules == nil || (!in.group && !s.isOwner(in.msg.From)) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, logTimeout)
	defer cancel()
	rules, err := s.opts.Rules.GroupRules(ctx, in.scope.ChatID)
	if err != nil {
		s.opts.Logger.Warn("could not load standing rules; going on without them", "chat_id", in.scope.ChatID, "error", err)
		return nil
	}
	return rules
}

// ignored reports a group member whom the rules say not to answer here.
// Owners are never ignored.
func (s *Service) ignored(in incoming, rules []storage.GroupRule) bool {
	if !in.group || s.isOwner(in.msg.From) {
		return false
	}
	return slices.ContainsFunc(rules, func(r storage.GroupRule) bool {
		return r.Kind == storage.RuleIgnore && r.ChatID == in.scope.ChatID && r.UserID == in.userID
	})
}

// respond decides what to send back. An empty outcome sends nothing. rules
// are the standing rules loaded for this turn.
func (s *Service) respond(ctx context.Context, in incoming, rules []storage.GroupRule) outcome {
	switch {
	case strings.HasPrefix(in.text, "/"):
		return say(s.runCommand(ctx, in))
	case utf8.RuneCountInString(in.text) > s.opts.MaxInputChars:
		return say(fmt.Sprintf(textTooLong, s.opts.MaxInputChars))
	case s.hasContent(in):
		return s.ask(ctx, in, rules)
	case in.group:
		// A location, a contact or another kind the bot does not read, or
		// whitespace. Saying so would be one more message in the group.
		return outcome{}
	case in.msg.Text == "":
		return say(textTextOnly(s.canListen())) // a location, a contact or another kind the bot does not read
	default:
		return outcome{} // whitespace only
	}
}

// hasContent reports text, or media the bot can describe, to answer.
func (s *Service) hasContent(in incoming) bool {
	return in.text != "" || s.hasMedia(in)
}

// hasMedia reports media the bot can describe (see mediaOf), on the message
// itself or, in a group, on the one it replies to.
func (s *Service) hasMedia(in incoming) bool {
	_, own := s.mediaOf(in.msg)
	_, replied := s.mediaOf(in.msg.ReplyToMessage)
	return own || (replied && in.group)
}

func (s *Service) ask(ctx context.Context, in incoming, rules []storage.GroupRule) outcome {
	reservation, notice, allowed := s.takePrivateAnswer(ctx, in)
	if !allowed {
		return say(notice)
	}
	if !in.group {
		// Only now, so that a voice message past the limit is not
		// transcribed: that is a call to the model too.
		in.described = s.describe(ctx, in.msg, in.text)
	}
	started := time.Now()
	stopTyping := s.showTyping(ctx, in.msg)
	defer stopTyping()
	sink := &tools.Sink{}
	caller := s.caller(in)
	if caller.Target != nil && caller.Target.UserID == s.opts.Bot.ID && in.group && s.isOwner(in.msg.From) {
		// An owner's "don't answer him" in reply to the bot's answer means
		// the member it answered; only owners give such orders.
		caller.Target.Answered = s.answeredMember(ctx, in.scope.ChatID, caller.Target.MessageID)
	}
	ctx = tools.WithTurn(ctx, caller, sink)
	answer, err := s.opts.Replier.Reply(ctx, s.request(ctx, in, rules))
	// A picture drawn before a later failure is still worth sending.
	drawings := sink.Take()
	if !usedAnAnswer(err, drawings) {
		s.giveBackAnswer(in, reservation)
	}
	out := s.answerOf(in, answer, err, started)
	out.drawings = drawings
	return out
}

// takePrivateAnswer takes one of the sender's answers for the day in a
// private chat, from the quota kept in the database, and returns the
// reservation to give back if the turn uses none. When the day's answers are
// used up it returns allowed false and the notice to send: the limit the
// first time that day, nothing after. Owners are not counted. When the count
// cannot be read the sender is answered anyway: the limit guards the cost,
// and unlike a group's quota, one answer too many disturbs nobody else.
func (s *Service) takePrivateAnswer(ctx context.Context, in incoming) (reservation int64, notice string, allowed bool) {
	limit := s.opts.PrivateDailyLimit
	if in.group || s.opts.PrivateQuota == nil || limit <= 0 || s.isOwner(in.msg.From) {
		return 0, "", true
	}
	ctx, cancel := context.WithTimeout(ctx, logTimeout)
	defer cancel()
	id, allowed, notify, err := s.opts.PrivateQuota.ReservePrivateMessage(ctx, storage.DailyQuota{
		UserID: in.userID,
		Since:  startOfDay(s.opts.Now(), s.opts.Location),
		Limit:  limit,
	})
	switch {
	case err != nil:
		s.opts.Logger.Warn("private message count failed; answering anyway", "user_id", in.userID, "error", err)
		return 0, "", true
	case allowed:
		return id, "", true
	}
	s.opts.Logger.Info("private daily limit reached", "user_id", in.userID, "notified", notify)
	if notify {
		return 0, fmt.Sprintf(textDailyLimit, limit, limit, limit), false
	}
	return 0, "", false
}

// giveBackAnswer gives back the answer a turn that used none took: from the
// private chat's daily quota, reservation, or from the group's, in.groupReply.
// It has its own deadline: the turn's may be what ran out.
func (s *Service) giveBackAnswer(in incoming, reservation int64) {
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	var err error
	switch {
	case reservation != 0:
		err = s.opts.PrivateQuota.ReleasePrivateMessage(ctx, reservation)
	case in.groupReply != 0:
		err = s.opts.GroupReplies.ReleaseGroupReply(ctx, in.groupReply)
	}
	if err != nil {
		s.opts.Logger.Warn("could not give back a failed turn's answer", "user_id", in.userID, "chat_id", in.scope.ChatID, "error", err)
	}
}

// usedAnAnswer reports whether a turn used up one of its answers, of the
// day's or the group's: the model answered, chose to stay silent, or drew a
// picture, which is sent even when the turn then failed. A failed turn did
// not, nor did a duplicate update: the turn that answered it keeps its own
// reservation.
func usedAnAnswer(err error, drawings []tools.Drawing) bool {
	return err == nil || errors.Is(err, agent.ErrSilent) || len(drawings) > 0
}

// startOfDay is midnight, in location, of the day now falls on there.
func startOfDay(now time.Time, location *time.Location) time.Time {
	local := now.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
}

// caller is who the turn is for, as the tools see them. Everything in it
// comes from the update.
func (s *Service) caller(in incoming) tools.Caller {
	return tools.Caller{
		UserID:    in.userID,
		ChatID:    in.scope.ChatID,
		Name:      plainLabel(in.msg.From),
		Owner:     s.isOwner(in.msg.From),
		Human:     !in.fromBot,
		UpdateID:  in.updateID,
		MessageID: in.msg.MessageID,
		Target:    s.target(in.msg.ReplyToMessage),
		Mentions:  s.mentions(in),
		Words:     in.described,
	}
}

// mentions are the members a request tags, for moderation: in the request,
// and in the asker's own message it replies to, which is how someone who
// tagged a member often asks. The bot and the asker are left out, and each
// member is listed once.
func (s *Service) mentions(in incoming) []tools.Mention {
	var found []tools.Mention
	add := func(m tools.Mention) {
		for _, f := range found {
			if (m.UserID != 0 && f.UserID == m.UserID) || (m.Username != "" && strings.EqualFold(f.Username, m.Username)) {
				return
			}
		}
		found = append(found, m)
	}
	asker := ""
	if in.msg.From != nil {
		asker = in.msg.From.Username
	}
	scan := func(msg *telegram.Message) {
		text, entities := msg.Content()
		for _, e := range entities {
			switch e.Type {
			case telegram.EntityTextMention:
				if e.User != nil && e.User.ID != s.opts.Bot.ID && e.User.ID != in.userID {
					add(tools.Mention{UserID: e.User.ID})
				}
			case telegram.EntityMention:
				tag, ok := telegram.EntityText(text, e)
				username := strings.TrimPrefix(tag, "@")
				if ok && username != "" && !strings.EqualFold(username, s.opts.Bot.Username) && !strings.EqualFold(username, asker) {
					add(tools.Mention{Username: username})
				}
			}
		}
	}
	scan(in.msg)
	if replied := in.msg.ReplyToMessage; replied != nil && replied.From != nil && replied.From.ID == in.userID {
		scan(replied)
	}
	return found
}

// answeredMember is the member whose message the bot's message botMessageID
// answered, as stored with the group's conversation, or nil when that is
// not known.
func (s *Service) answeredMember(ctx context.Context, chatID, botMessageID int64) *tools.Target {
	if s.opts.Answers == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, logTimeout)
	defer cancel()
	author, err := s.opts.Answers.AnsweredAuthor(ctx, chatID, botMessageID)
	if err != nil {
		if !errors.Is(err, storage.ErrUnknownAuthor) {
			s.opts.Logger.Warn("could not learn whom an answer was for", "chat_id", chatID, "error", err)
		}
		return nil
	}
	name, owner, bot := unlabel(author.Name)
	if len(s.opts.OwnerIDs) > 0 {
		owner = s.opts.OwnerIDs[author.ID]
	}
	return &tools.Target{MessageID: author.MessageID, UserID: author.ID, Name: name, IsBot: bot, Owner: owner}
}

// confirmedModeration passes on a group member's plain yes or no, written
// as a reply to one of the bot's messages, to moderation, and reports
// whether it was the answer to the bot's question about an action they had
// asked for. Then what came of it is said, and the exchange logged; nothing
// else is done with the message. The model never sees it: whether an action
// is carried out is decided by the person's own words, read in code.
func (s *Service) confirmedModeration(ctx context.Context, in incoming) bool {
	replied := in.msg.ReplyToMessage
	if !in.group || in.fromBot || s.opts.Moderation == nil || replied == nil || replied.From == nil || replied.From.ID != s.opts.Bot.ID {
		return false
	}
	yes, ok := tools.ConfirmationAnswer(in.text)
	if !ok {
		return false
	}
	confirmation, err := s.opts.Moderation.Confirm(ctx, s.caller(in), replied.MessageID, yes)
	if err != nil {
		s.opts.Logger.Warn("moderation confirmation failed", "user_id", in.userID, "chat_id", in.scope.ChatID, "error", err)
	}
	if !confirmation.Found {
		return false
	}
	if confirmation.Text != "" {
		s.reply(in, confirmation.Text)
		s.logFrom(in.msg, in.text)
		s.ambient.add(in.scope.ChatID, 0, s.opts.Bot.ID, botAuthor, confirmation.Text)
	}
	return true
}

// target describes the message a request replied to, for the tools that act
// on a member. It is taken from the update, so the model can never choose
// whom such a tool acts on. A service message is none: its author is whoever
// made something happen, not the member it is about. A member's message the
// bot cannot read, such as a contact or a location, is one.
func (s *Service) target(replied *telegram.Message) *tools.Target {
	if replied == nil || replied.From == nil || replied.IsService() {
		return nil
	}
	text, _ := replied.Content()
	return &tools.Target{
		MessageID: replied.MessageID,
		UserID:    replied.From.ID,
		Name:      plainLabel(replied.From),
		IsBot:     replied.From.IsBot,
		Owner:     s.isOwner(replied.From),
		Text:      truncate(text, tools.MaxTargetRunes),
	}
}

// answerOf turns the agent's answer, or its error, into what is sent back.
func (s *Service) answerOf(in incoming, answer agent.Answer, err error, started time.Time) outcome {
	log := s.opts.Logger.With("user_id", in.userID, "chat_id", in.scope.ChatID,
		"update_id", in.updateID, "duration", time.Since(started).String())

	switch {
	case err == nil && answer.Reaction != "":
		log.Info("turn completed with a reaction", "emoji", answer.Reaction)
		return outcome{reaction: answer.Reaction, answerID: answer.ID}
	case err == nil:
		log.Info("turn completed")
		return outcome{text: s.fitted(in, answer.Text), answerID: answer.ID}
	case errors.Is(err, agent.ErrLeakedThinking):
		log.Warn("the model answered with its own reasoning; nothing was sent")
		return outcome{}
	case errors.Is(err, agent.ErrSilent):
		log.Info("the model chose not to reply")
		return outcome{}
	case in.group && errors.Is(err, ai.ErrEmptyResponse):
		// Models give back nothing where [silent] was meant, such as after a
		// second insult; an error message would be worse than silence.
		log.Warn("the model gave an empty answer; nothing was sent")
		return outcome{}
	case errors.Is(err, agent.ErrDuplicate):
		log.Info("duplicate update skipped")
		return outcome{duplicate: true}
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
// author ("Bek: ..."), because the conversation is shared. The context, for
// this turn only, is the chat header and the owners' standing rules, then in
// a private chat the owner notice, in a group the recent chatter and, when an
// owner replies to a member, whom an order not to answer would mean, and last
// the language to answer in.
func (s *Service) request(ctx context.Context, in incoming, rules []storage.GroupRule) agent.Request {
	req := agent.Request{
		Scope:            in.scope,
		UserID:           in.userID,
		UpdateID:         in.updateID,
		MessageID:        in.msg.MessageID,
		ReplyToMessageID: repliedID(in),
		Text:             in.described,
		Group:            in.group,
		Attachments:      s.attachments(ctx, in),
	}
	var notice, log string
	if in.group {
		req.AuthorName = s.label(in.msg.From)
		req.Text = req.AuthorName + ": " + oneLine(req.Text)
		log = s.ambient.block(in.scope.ChatID, s.opts.Now())
	} else {
		// Its lines are kept, but not its square brackets: the owner's notice
		// is bracketed, and typed in front of a message it would read the
		// same as the real one.
		req.Text = bracketless.Replace(req.Text)
		if s.isOwner(in.msg.From) {
			notice = ownerNotice
		}
	}
	req.Context = paragraphs(s.chatHeader(ctx, in), s.rulesHeader(rules, in), s.profileLine(ctx, in, rules), notice, s.talksElsewhere(ctx, in), log, s.ownerReplyHint(in), languageHint(in.text))
	replied := in.msg.ReplyToMessage
	if replied != nil {
		req.Text = withQuote(req.Text, s.quoted(ctx, replied), s.opts.Bot.ID, plainLabel(replied.From))
	}
	return req
}

// ownerReplyNote names the member an owner replied to. Told "don't answer
// this" in reply to a member, the model stayed silent once instead of saving
// the order with group_rules ignore, whatever the persona said.
const ownerReplyNote = "[Your owner is replying to a message from %s. If they tell you not to answer, in any words, they mean %s from now on: call group_rules ignore before you react; a reaction or [silent] alone saves nothing.]"

// ownerReplyHint is said, right before the message, when an owner replies in
// a group to a member who could be ignored; otherwise it is "".
func (s *Service) ownerReplyHint(in incoming) string {
	replied := in.msg.ReplyToMessage
	if !in.group || s.opts.Rules == nil || !s.isOwner(in.msg.From) || replied == nil ||
		replied.From == nil || replied.From.ID == s.opts.Bot.ID || s.isOwner(replied.From) {
		return ""
	}
	member := plainLabel(replied.From)
	return fmt.Sprintf(ownerReplyNote, member, member)
}

// paragraphs joins the parts that are not empty, a blank line apart.
func paragraphs(parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(p string) bool { return p == "" }), "\n\n")
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
		return HelpText(Abilities{Draw: s.opts.ImagesEnabled, Listen: s.canListen(), Football: s.opts.FootballEnabled})
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
	case "/ignore":
		return s.ignore(ctx, in)
	case "/unignore":
		return s.unignore(ctx, in)
	default:
		return textUnknown
	}
}

// ignoreCommandFor is how long /ignore lasts. An owner who wants longer uses
// the group_rules tool.
const ignoreCommandFor = 24 * time.Hour

// ignore stops the bot answering, for a day, the member whose message the
// command replies to. Like /mute it is for the group's admins and the bot's
// owners. Admins, owners and the bot itself cannot be ignored: the first two
// must always be able to reach it. It replaces an earlier ignore of the
// member, even a longer one an owner set with the tool, so the day starts
// now, as the reply says; whoever may /ignore may /unignore them anyway.
func (s *Service) ignore(ctx context.Context, in incoming) string {
	member, refusal := s.ignoreTarget(ctx, in)
	if refusal != "" {
		return refusal
	}
	if refusal := s.refuseUnignorable(ctx, in, member); refusal != "" {
		return refusal
	}
	if err := s.saveIgnore(ctx, in, member); err != nil {
		return textError
	}
	return fmt.Sprintf(textIgnored, member.Name)
}

// saveIgnore stops the bot answering member in this group for
// ignoreCommandFor, on the word of the sender of in.
func (s *Service) saveIgnore(ctx context.Context, in incoming, member *tools.Target) error {
	_, err := s.opts.Rules.SetGroupRule(ctx, storage.GroupRule{
		ChatID:    in.scope.ChatID,
		Kind:      storage.RuleIgnore,
		UserID:    member.UserID,
		UserName:  member.Name,
		SetBy:     in.userID,
		ExpiresAt: s.opts.Now().Add(ignoreCommandFor),
	})
	if err != nil {
		s.opts.Logger.Error("ignore failed", "user_id", in.userID, "chat_id", in.scope.ChatID, "error", err)
		return err
	}
	s.opts.Logger.Info("member ignored", "user_id", in.userID, "chat_id", in.scope.ChatID, "member_id", member.UserID)
	return nil
}

// unignore answers again a member ignored in this group.
func (s *Service) unignore(ctx context.Context, in incoming) string {
	member, refusal := s.ignoreTarget(ctx, in)
	if refusal != "" {
		return refusal
	}
	removed, err := s.opts.Rules.RemoveIgnore(ctx, in.scope.ChatID, member.UserID)
	if err != nil {
		s.opts.Logger.Error("unignore failed", "user_id", in.userID, "chat_id", in.scope.ChatID, "error", err)
		return textError
	}
	if !removed {
		return fmt.Sprintf(textNotIgnored, member.Name)
	}
	s.opts.Logger.Info("member unignored", "user_id", in.userID, "chat_id", in.scope.ChatID, "member_id", member.UserID)
	return fmt.Sprintf(textUnignored, member.Name)
}

// ignoreTarget checks what /ignore and /unignore have in common, and returns
// the member they are about: the author of the message the command replies
// to, or for one of the bot's own answers, the member it answered; never a
// name typed after it. A message posted on behalf of a channel or of the
// group has no member behind it.
func (s *Service) ignoreTarget(ctx context.Context, in incoming) (*tools.Target, string) {
	switch {
	case s.opts.Rules == nil:
		return nil, textUnknown
	case !in.group:
		return nil, textGroupsOnly
	}
	if refusal := s.refuseUnlessManager(ctx, in); refusal != "" {
		return nil, refusal
	}
	replied := in.msg.ReplyToMessage
	if replied == nil || replied.From == nil || replied.SenderChat != nil || replied.IsService() {
		return nil, textReplyToMember
	}
	if replied.From.ID != s.opts.Bot.ID {
		return s.target(replied), ""
	}
	if member := s.answeredMember(ctx, in.scope.ChatID, replied.MessageID); member != nil {
		return member, ""
	}
	return nil, textRepliedToMe
}

// refuseUnignorable returns the refusal to send when member may not be
// ignored, and "" when they may. When Telegram cannot say whether they are
// an admin, they are not ignored.
func (s *Service) refuseUnignorable(ctx context.Context, in incoming, member *tools.Target) string {
	if member.UserID == s.opts.Bot.ID || member.Owner {
		return textCannotIgnore
	}
	isAdmin, err := s.opts.Members.IsGroupAdmin(ctx, in.scope.ChatID, member.UserID)
	if err != nil {
		s.opts.Logger.Error("group admin check failed", "member_id", member.UserID, "chat_id", in.scope.ChatID, "error", err)
		return textError
	}
	if isAdmin {
		return textCannotIgnore
	}
	return ""
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
		return fmt.Sprintf(textMuted, s.opts.Bot.Username)
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
