package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
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
	// skipped is why the message got no answer from the model, as
	// "overloaded", for the skip log; "" when it got one.
	skipped string
	// skipDetail is the error behind skipped, for the owners.
	skipDetail string
	// reaction is the emoji the model answered with instead of text. It is
	// set on the message being answered; no message is sent for it.
	reaction string
	// drawings are pictures made during the turn, sent with the text, and
	// audios the sounds found, such as a song's preview, sent after it.
	drawings []tools.Drawing
	audios   []tools.Audio
	// documents are files a tool made, such as a document, sent as the
	// answer with its text as their caption.
	documents []tools.Document
	// duplicate marks an update that was already answered: Telegram sent it
	// again after a slow turn. The exchange is in the log already.
	duplicate bool
	// sticker is the category of sticker the model asked to send after the
	// text, or alone; "" for none. gif is the same for a GIF.
	sticker, gif string
	// buttons are link buttons under the text.
	buttons [][]telegram.Button
	// toReplied sends the answer as a reply to the member's message the
	// asker replied to, as the model chose for an answer that speaks to
	// them.
	toReplied bool
	// toName is the member the model named for toReplied, whose latest
	// message the answer replies to when the asker replied to nobody.
	toName string
	// invoice sends the invoice for premium after the text.
	invoice bool
}

func say(text string) outcome { return outcome{text: text} }

// process handles one accepted update from start to finish. It uses its own
// context rather than the polling one, so that a shutdown lets the turn in
// progress finish instead of cutting it off.
func (s *Service) process(in incoming) {
	ctx, cancel := context.WithTimeout(context.Background(), s.opts.TurnTimeout)
	defer cancel()

	// A private message nobody may answer gets a reaction only, chosen here
	// so that the model is never asked; a flood gets nothing at all.
	if in.unanswered {
		if _, limited := s.rateLimited(in); !limited {
			s.react(in, privateReactions[rand.IntN(len(privateReactions))])
		}
		s.noteSkip(in, "private_closed", "")
		return
	}
	// A muted group is ignored, except for the commands that control muting.
	// This is checked here, in queue order, so that a message sent right
	// after /unmute is answered and one sent right after /mute is not.
	if s.silenced(in) {
		s.noteSkip(in, "muted", "")
		return
	}
	if in.fromBot && !s.mayAnswerBot(in) {
		s.noteSkip(in, "bot_quota", "")
		return
	}
	if notice, limited := s.rateLimited(in); limited {
		if notice != "" {
			s.reply(in, notice)
		}
		s.passOver(in)
		s.noteSkip(in, "flood", "")
		return
	}
	// A group of fewer than MinGroupMembers is answered only as a private
	// chat is, or a group of two would get around PRIVATE_CHATS; anyone else
	// gets a reaction, made in code, so that the model is never asked. After
	// the flood guard, so that a flood cannot ask Telegram for the count
	// without end.
	if s.isPrivateInDisguise(ctx, in) {
		s.react(in, overQuotaReaction)
		s.passOver(in)
		s.noteSkip(in, "small_group", "")
		return
	}
	// A group where too few people wrote lately gets nothing at all, as
	// the owners asked; only they are answered there.
	if s.tooQuiet(ctx, in) {
		s.passOver(in)
		s.noteSkip(in, "quiet_group", "")
		return
	}
	// An owner's order not to answer a member is handled here, in code.
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
		s.noteSkip(in, "ignored", "")
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
		s.noteSkip(in, "group_quota", "")
		return
	}
	in.groupReply = reservation
	if in.group {
		// The group's log needs it even when no answer follows. A private
		// message is described in ask, within the daily limit.
		in.described = s.describe(ctx, in.msg, in.text)
	}
	out := s.respond(ctx, in, rules)
	if sentID := s.deliver(in, &out); out.answerID != 0 && sentID != 0 {
		s.recordSent(out.answerID, sentID)
	}
	s.logExchange(in, out)
	if out.skipped != "" {
		s.noteSkip(in, out.skipped, out.skipDetail)
	}
}

// maxSkipDetail cuts the error kept with a skip: its start says what failed.
const maxSkipDetail = 500

// noteSkip records that a message addressed to the bot got no answer, and
// why. It is a record for the owners only: failing to write it is logged
// and nothing more.
func (s *Service) noteSkip(in incoming, reason, detail string) {
	if s.opts.Skips == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	err := s.opts.Skips.NoteSkip(ctx, storage.Skip{ChatID: in.scope.ChatID, UserID: in.userID, MessageID: in.msg.MessageID, Reason: reason, Detail: truncate(detail, maxSkipDetail)})
	if err != nil {
		s.opts.Logger.Warn("could not record a skipped message", "chat_id", in.scope.ChatID, "reason", reason, "error", err)
	}
}

// deliver sends the outcome and returns the ID of the message that carries
// the answer. A picture goes first, with the answer as its caption when it
// fits; a longer answer follows as its own message. A reaction is set on the
// message being answered and is not a message, so alone it returns 0.
func (s *Service) deliver(in incoming, out *outcome) int64 {
	if out.toReplied {
		in.replyTo = s.repliedMember(in)
		if in.replyTo == 0 && out.toName != "" {
			in.replyTo = s.namedMembersMessage(in, out.toName)
		}
	}
	if id := s.sendDocuments(in, out); id != 0 {
		// A file is the answer, its text the caption.
		out.text, out.drawings, out.sticker, out.gif, out.audios = "", nil, "", "", nil
		return id
	}
	if id := s.sendAudios(in, out.audios); id != 0 {
		// A song asked for is the answer alone, as a sticker is: words with
		// it only say what it is. Only when it cannot be sent do they go.
		out.text, out.drawings, out.sticker, out.gif = "", nil, "", ""
		if out.reaction != "" {
			s.react(in, out.reaction)
		}
		return id
	}
	out.audios = nil
	answerID := s.deliverAnswer(in, out)
	if out.invoice {
		s.sendInvoice(in)
	}
	if id := s.sendMedia(in, out); answerID == 0 {
		answerID = id
	}
	return answerID
}

// namedWindow is how far back the group log is searched for the latest
// message of a member the model named.
const namedWindow = 3 * time.Hour

// namedMembersMessage is the latest message in the group log, of the last
// namedWindow, by the one member whose name or @username is name: "Mari
// Jamesga gif tashla" asks for an answer to James. It is 0 for the asker,
// the bot, a name that fits several members or none.
func (s *Service) namedMembersMessage(in incoming, name string) int64 {
	if !in.group || !s.ambient.enabled() {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	recent, err := s.ambient.log.GroupMessagesSince(ctx, in.scope.ChatID, s.opts.Now().Add(-namedWindow), s.ambient.size)
	if err != nil {
		s.opts.Logger.Warn("could not read the group log for a named member", "chat_id", in.scope.ChatID, "error", err)
		return 0
	}
	wanted := foldName(name)
	var member, messageID int64
	for _, m := range recent {
		if m.AuthorID == 0 || m.AuthorID == in.userID || m.AuthorID == s.opts.Bot.ID || m.MessageID == 0 || !namesMatch(foldName(m.AuthorName), wanted) {
			continue
		}
		if member != 0 && member != m.AuthorID {
			return 0
		}
		member, messageID = m.AuthorID, m.MessageID
	}
	return messageID
}

// foldName is a name's words in lower case, letters and digits only, "@"
// dropped, so "JAMES @james_k" is "james james_k".
func foldName(name string) []string {
	var words []string
	for word := range strings.FieldsSeq(strings.ToLower(name)) {
		word = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
				return r
			}
			return -1
		}, word)
		if word != "" {
			words = append(words, word)
		}
	}
	return words
}

// namesMatch reports an author whose name has every word of wanted, so
// "James" and "@james_k" find "JAMES @james_k".
func namesMatch(author, wanted []string) bool {
	if len(wanted) == 0 {
		return false
	}
	for _, w := range wanted {
		if !slices.Contains(author, w) {
			return false
		}
	}
	return true
}

// repliedMember is the message of another member the asker replied to, or,
// when they replied to the bot's own answer, the member's message it
// answered: "undan yilini so'ra" in reply to the bot means that member.
// It is 0 for the asker's own message, a service message or a member not
// known, and the answer then goes to the asker.
func (s *Service) repliedMember(in incoming) int64 {
	replied := in.msg.ReplyToMessage
	if !in.group || replied == nil || replied.From == nil || replied.IsService() {
		return 0
	}
	if replied.From.ID == s.opts.Bot.ID {
		member := s.answeredMember(context.Background(), in.scope.ChatID, replied.MessageID)
		if member == nil || member.UserID == in.userID {
			return 0
		}
		return member.MessageID
	}
	if replied.From.ID == in.userID {
		return 0
	}
	return replied.MessageID
}

// sendAudios sends the sounds a tool found and returns the first sent
// message's ID, or 0 when none could be sent.
func (s *Service) sendAudios(in incoming, audios []tools.Audio) int64 {
	var firstID int64
	for _, audio := range audios {
		if id := s.sendAudio(in, audio); firstID == 0 {
			firstID = id
		}
	}
	return firstID
}

// sendDocuments sends the files a tool made, the first with the answer's
// text as its caption, and returns the ID of the first sent; 0 when none
// was, and the text then goes as usual.
func (s *Service) sendDocuments(in incoming, out *outcome) int64 {
	if s.opts.Files == nil {
		return 0
	}
	var firstID int64
	caption := out.text
	if utf8.RuneCountInString(caption) > telegram.CaptionLimit {
		caption = ""
	}
	for _, document := range out.documents {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		id, err := s.opts.Files.SendFile(ctx, telegram.File{
			ChatID: in.msg.Chat.ID, ReplyToMessageID: in.replyTarget(),
			Data: document.Data, Name: document.Name, Caption: caption,
		})
		cancel()
		if err != nil {
			s.opts.Logger.Warn("could not send a file", "chat_id", in.msg.Chat.ID, "file", document.Name, "error", err)
			continue
		}
		if firstID == 0 {
			firstID, caption = id, ""
		}
	}
	return firstID
}

func (s *Service) sendAudio(in incoming, audio tools.Audio) int64 {
	if s.opts.AudioSender == nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	id, err := s.opts.AudioSender.SendAudio(ctx, telegram.Track{
		ChatID: in.msg.Chat.ID, ReplyToMessageID: in.replyTarget(),
		Data: audio.Data, Title: audio.Title, Performer: audio.Performer,
	})
	if err != nil {
		s.opts.Logger.Warn("could not send audio", "chat_id", in.msg.Chat.ID, "error", err)
		return 0
	}
	return id
}

// deliverAnswer sends the outcome's text, pictures and reaction. Several
// pictures go as albums, the caption under the first.
func (s *Service) deliverAnswer(in incoming, out *outcome) int64 {
	if out.reaction != "" {
		s.react(in, out.reaction)
	}
	if len(out.drawings) == 0 {
		if out.text == "" {
			return 0
		}
		return s.replyWithButtons(in, out.text, out.buttons)
	}
	caption := out.text
	if utf8.RuneCountInString(caption) > telegram.CaptionLimit {
		caption = ""
	}
	var answerID int64
	for i, album := range albums(out.drawings) {
		text := ""
		if i == 0 {
			text = caption
		}
		if id := s.replyPictures(in, album, text); answerID == 0 {
			answerID = id
		}
	}
	if caption == "" && out.text != "" {
		answerID = s.reply(in, out.text)
	}
	return answerID
}

// albums splits pictures into the albums Telegram takes, of at most
// telegram.MaxAlbum each; a last one of a single picture goes alone.
func albums(drawings []tools.Drawing) [][]tools.Drawing {
	return slices.Collect(slices.Chunk(drawings, telegram.MaxAlbum))
}

// replyPictures sends pictures as one album, or one picture alone, with
// caption, and returns the ID of the message with the caption. When
// Telegram refuses the album, such as for one picture it cannot read, they
// go one by one, so that the others still arrive; when it does not answer,
// the album may be in the chat, and nothing is sent again.
func (s *Service) replyPictures(in incoming, drawings []tools.Drawing, caption string) int64 {
	if len(drawings) < telegram.MinAlbum {
		return s.replyPhoto(in, drawings[0], caption)
	}
	photos := make([][]byte, len(drawings))
	for i, drawing := range drawings {
		photos[i] = drawing.Data
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	sentID, err := s.opts.Sender.SendMediaGroup(ctx, telegram.Album{
		ChatID: in.msg.Chat.ID, ReplyToMessageID: in.replyTarget(), Photos: photos, Caption: caption,
	})
	var refused *telegram.APIError
	switch {
	case err == nil:
		return sentID
	case !errors.As(err, &refused) || refused.Code >= http.StatusInternalServerError:
		s.opts.Logger.Error("send album failed", "chat_id", in.msg.Chat.ID, "error", err)
		return 0
	}
	s.opts.Logger.Warn("Telegram refused an album; sending its pictures one by one", "chat_id", in.msg.Chat.ID, "error", err)
	var firstID int64
	for i, drawing := range drawings {
		text := ""
		if i == 0 {
			text = caption
		}
		if id := s.replyPhoto(in, drawing, text); firstID == 0 {
			firstID = id
		}
	}
	return firstID
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
		if out.sticker != "" {
			answer = strings.TrimSpace(answer + " <sticker " + out.sticker + ">")
		}
		if out.gif != "" {
			answer = strings.TrimSpace(answer + " <gif " + out.gif + ">")
		}
		for _, document := range out.documents {
			answer = strings.TrimSpace(answer + " <file " + document.Name + ">")
		}
		for _, audio := range out.audios {
			answer = strings.TrimSpace(answer + " <audio " + audio.Performer + " — " + audio.Title + ">")
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
		return textRateLimited.to(in), true
	case deniedAgain:
		return "", true
	}
	return "", false
}

// privateReactions are the friendly reactions a private message that is not
// answered gets one of, at random, so that it is seen to have been read.
var privateReactions = []string{"👍", "❤", "🔥", "😁", "🤗", "😎", "🤝", "🫡", "👌", "😇", "🙈", "🆒", "🦄", "💯", "⚡", "🤩"}

// overQuotaReaction is set on a message the quota, or a group too small,
// leaves unanswered, so that the bot is seen to have read it: silence alone
// looked as if it were broken.
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
		PerMinute: s.replyLimit(ctx, in.scope.ChatID),
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
	case s.onlyTheyRead(ctx, in):
		// Nobody else reads a group of two, so there is nobody to flood; the
		// limit once left someone in distress there unanswered.
		return 0, true
	}
	s.opts.Logger.Info("group reply limit reached", "chat_id", in.scope.ChatID, "user_id", in.userID)
	s.react(in, overQuotaReaction)
	return 0, false
}

// managerCommands manage the bot in a group, and are for its admins.
var managerCommands = map[string]bool{"/mute": true, "/unmute": true, "/ignore": true, "/unignore": true}

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

// tooQuiet reports a group where fewer than MinActiveMembers people wrote
// within ActiveWindow, for a message from anyone but an owner. When the
// count cannot be read, the group is answered.
func (s *Service) tooQuiet(ctx context.Context, in incoming) bool {
	if !in.group || s.opts.ActiveCounter == nil || s.isOwner(in.msg.From) {
		return false
	}
	minimum := s.minActive(ctx, in.scope.ChatID)
	if minimum <= 0 {
		return false
	}
	count, ok := s.actives.get(in.scope.ChatID)
	if !ok {
		ctx, cancel := context.WithTimeout(ctx, logTimeout)
		defer cancel()
		var err error
		count, err = s.opts.ActiveCounter.ActiveMembers(ctx, in.scope.ChatID, s.opts.Now().Add(-s.activeWindow(ctx, in.scope.ChatID)))
		if err != nil {
			s.opts.Logger.Warn("could not count a group's active members; answering", "chat_id", in.scope.ChatID, "error", err)
			return false
		}
		s.actives.set(in.scope.ChatID, count)
	}
	if count < minimum {
		s.opts.Logger.Info("group too quiet; not answering", "chat_id", in.scope.ChatID, "active", count)
		return true
	}
	return false
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
	case !in.group && commandName(in.text) == "/start" && len(s.opts.RequiredChannels) > 0:
		return s.start(ctx, in)
	case commandName(in.text) == "/premium":
		return s.premiumCommand(ctx, in)
	case strings.HasPrefix(in.text, "/"):
		return say(s.runCommand(ctx, in))
	case utf8.RuneCountInString(in.text) > s.opts.MaxInputChars:
		return say(fmt.Sprintf(textTooLong.to(in), s.opts.MaxInputChars))
	case s.hasContent(in):
		return s.ask(ctx, in, rules)
	case in.group:
		// A location, a contact or another kind the bot does not read, or
		// whitespace. Saying so would be one more message in the group.
		return outcome{}
	case in.msg.Text == "":
		return say(textTextOnly(s.canListen()).to(in)) // a location, a contact or another kind the bot does not read
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
	if notice, joined := s.joinedChannels(ctx, in); !joined {
		return notice
	}
	if out, ok := s.quickAnswer(ctx, in); ok {
		return out
	}
	reservation, notify, allowed := s.takePrivateAnswer(ctx, in)
	if !allowed {
		return s.limitReached(ctx, in, notify)
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
	if in.group && caller.Owner {
		caller.Words = s.withEarlierWords(ctx, in, caller.Words)
	}
	if caller.Target != nil && caller.Target.UserID == s.opts.Bot.ID && in.group && s.isOwner(in.msg.From) {
		// An owner's "don't answer him" in reply to the bot's answer means
		// the member it answered; only owners give such orders.
		caller.Target.Answered = s.answeredMember(ctx, in.scope.ChatID, caller.Target.MessageID)
	}
	req := s.request(ctx, in, rules)
	caller.Picture = firstPicture(req.Attachments)
	ctx = tools.WithTurn(ctx, caller, sink)
	answer, err := s.opts.Replier.Reply(ctx, req)
	// A picture drawn before a later failure is still worth sending.
	drawings := sink.Take()
	documents := sink.TakeDocuments()
	var audios []tools.Audio
	if err == nil || errors.Is(err, ai.ErrEmptyResponse) {
		// A song's preview goes only with an answer, or with none: the
		// model, told it is the answer, may write nothing, and it is still
		// what was asked for.
		audios = sink.TakeAudios()
	}
	if !usedAnAnswer(err, drawings) && len(documents)+len(audios) == 0 {
		s.giveBackAnswer(in, reservation)
	}
	out := s.answerOf(in, answer, err, started)
	out.drawings = drawings
	out.documents = documents
	out.audios = audios
	if len(audios) > 0 {
		out.skipped = ""
	}
	return out
}

// joinedChannels reports whether the sender of a private message may be
// answered under the required channels, and if not the notice, with a
// button for each channel and one to check again, that asks them to join.
// Owners and groups need no channel. When Telegram refuses to say, as it
// does while the bot is no admin of a channel, nobody is let through: the
// owners asked that only the channels' members be answered. When Telegram
// does not answer at all, the sender is: the daily limit still guards the
// cost.
func (s *Service) joinedChannels(ctx context.Context, in incoming) (notice outcome, joined bool) {
	channels := s.opts.RequiredChannels
	if in.group || len(channels) == 0 || s.opts.Channels == nil || s.isOwner(in.msg.From) {
		return outcome{}, true
	}
	if _, ok := s.subscribers.get(in.userID); ok {
		return outcome{}, true
	}
	ctx, cancel := context.WithTimeout(ctx, logTimeout)
	defer cancel()
	var missing []string
	for _, channel := range channels {
		member, err := s.opts.Channels.ChannelMember(ctx, channel, in.userID)
		var refused *telegram.APIError
		switch {
		case errors.As(err, &refused) && refused.Code < http.StatusInternalServerError:
			s.opts.Logger.Error("Telegram will not say who is in a required channel; make the bot an admin there", "channel", channel, "error", err)
			missing = append(missing, channel)
		case err != nil:
			s.opts.Logger.Warn("could not check a required channel; taking it as joined", "user_id", in.userID, "channel", channel, "error", err)
		case !member.Subscribed():
			missing = append(missing, channel)
		}
	}
	if len(missing) > 0 {
		return s.joinNotice(in, missing), false
	}
	s.subscribers.set(in.userID, true)
	return outcome{}, true
}

// checkPayload is what the check button sends as /start's parameter.
const checkPayload = "check"

// joinNotice asks to join channels, with a button that opens each and one
// that sends "/start check", which checks again.
func (s *Service) joinNotice(in incoming, channels []string) outcome {
	var buttons [][]telegram.Button
	for i, channel := range channels {
		link, ok := channelLink(channel)
		if !ok {
			continue
		}
		label := textJoinButton.to(in)
		if len(channels) > 1 {
			label = fmt.Sprintf(textJoinButtonN.to(in), i+1)
		}
		buttons = append(buttons, []telegram.Button{{Text: label, URL: link}})
	}
	switch {
	case s.opts.Buttons != nil:
		buttons = append(buttons, []telegram.Button{{Text: textCheckButton.to(in), Data: checkPayload}})
	case s.opts.Bot.Username != "":
		buttons = append(buttons, []telegram.Button{{Text: textCheckButton.to(in), URL: "https://t.me/" + s.opts.Bot.Username + "?start=" + checkPayload}})
	}
	text := textJoinChannel.to(in)
	if len(channels) > 1 {
		text = textJoinChannels.to(in)
	}
	return outcome{text: text, buttons: buttons}
}

// start greets a private chat when channels are required: the notice to
// join them, until the sender has, then the welcome, or, after the check
// button, word that they may write now.
func (s *Service) start(ctx context.Context, in incoming) outcome {
	if notice, joined := s.joinedChannels(ctx, in); !joined {
		return notice
	}
	if strings.TrimSpace(strings.TrimPrefix(in.text, commandName(in.text))) == checkPayload {
		return say(textChannelsJoined.to(in))
	}
	return say(s.runCommand(ctx, in))
}

// pressed answers a press of the check button: the notice becomes the
// thanks once every channel is joined, and otherwise a box asks to join
// them first; nothing is written in the chat either way. A press of the
// button under a join request is pressedJoin's.
func (s *Service) pressed(q *telegram.CallbackQuery) {
	if s.opts.Buttons == nil || q.From == nil || q.Message == nil {
		return
	}
	if strings.HasPrefix(q.Data, joinPayload) {
		s.pressedJoin(q)
		return
	}
	if q.Data != checkPayload {
		return
	}
	in := incoming{
		msg:    &telegram.Message{MessageID: q.Message.MessageID, From: q.From, Chat: q.Message.Chat},
		scope:  storage.Scope{ChatID: q.Message.Chat.ID, UserID: q.From.ID},
		userID: q.From.ID,
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if _, joined := s.joinedChannels(ctx, in); !joined {
		if err := s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, textNotJoinedYet.to(in), true); err != nil {
			s.opts.Logger.Warn("could not answer the check button", "user_id", in.userID, "error", err)
		}
		return
	}
	thanks := textChannelsJoined.to(in)
	if err := s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, thanks, false); err != nil {
		s.opts.Logger.Warn("could not answer the check button", "user_id", in.userID, "error", err)
	}
	if err := s.opts.Buttons.EditMessageText(ctx, q.Message.Chat.ID, q.Message.MessageID, thanks); err != nil {
		s.opts.Logger.Warn("could not replace the channel notice", "user_id", in.userID, "error", err)
	}
}

// channelLink is where a channel can be joined: its t.me link, when it has
// a public name.
func channelLink(channel string) (string, bool) {
	name, ok := strings.CutPrefix(channel, "@")
	return "https://t.me/" + name, ok
}

// takePrivateAnswer takes one of the sender's answers for the day in a
// private chat, from the quota kept in the database, and returns the
// reservation to give back if the turn uses none. The limit is the premium
// one while the sender's premium lasts. When the day's answers are used up
// it returns allowed false, and notify true the first time that day, for
// the notice (see limitReached). Owners are not counted. When the count
// cannot be read the sender is answered anyway: the limit guards the cost,
// and unlike a group's quota, one answer too many disturbs nobody else.
func (s *Service) takePrivateAnswer(ctx context.Context, in incoming) (reservation int64, notify, allowed bool) {
	limit := s.opts.PrivateDailyLimit
	if in.group || s.opts.PrivateQuota == nil || limit <= 0 || s.isOwner(in.msg.From) {
		return 0, false, true
	}
	ctx, cancel := context.WithTimeout(ctx, logTimeout)
	defer cancel()
	id, allowed, notify, err := s.opts.PrivateQuota.ReservePrivateMessage(ctx, storage.DailyQuota{
		UserID:       in.userID,
		Since:        startOfDay(s.opts.Now(), s.opts.Location),
		Limit:        limit,
		PremiumLimit: s.premiumLimit(),
	})
	switch {
	case err != nil:
		s.opts.Logger.Warn("private message count failed; answering anyway", "user_id", in.userID, "error", err)
		return 0, false, true
	case allowed:
		return id, false, true
	}
	s.opts.Logger.Info("private daily limit reached", "user_id", in.userID, "notified", notify)
	return 0, notify, false
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
		Words:     ownWords(in),
		Forum:     in.msg.Chat.IsForum,
		TopicID:   topicOf(in.msg),
	}
}

// topicOf is the forum topic msg was written in, or else that of the
// message it replies to; zero in the General topic. Outside a forum a reply
// carries a message_thread_id too, the reply chain's, which is no topic.
func topicOf(msg *telegram.Message) int64 {
	switch replied := msg.ReplyToMessage; {
	case msg.IsTopicMessage:
		return msg.MessageThreadID
	case replied != nil && replied.IsTopicMessage:
		return replied.MessageThreadID
	}
	return 0
}

// ownWords is what the caller wrote: the message being answered, and their
// own earlier message it replies to, which is how an owner often adds "did
// you get that?" to an order they gave a moment before.
func ownWords(in incoming) string {
	replied := in.msg.ReplyToMessage
	if replied == nil || replied.From == nil || in.msg.From == nil || replied.From.ID != in.msg.From.ID {
		return in.described
	}
	text, _ := replied.Content()
	return strings.TrimSpace(text + "\n" + in.described)
}

// earlierWordsWindow is how far back an owner's own messages in a group
// count as their words.
const earlierWordsWindow = 5 * time.Minute

// withEarlierWords adds to words an owner's own messages in this group of
// the last few minutes. Told that an order about a member needs a reply to
// that member's message, an owner replies to it with only "Mari": the order
// is the message before.
func (s *Service) withEarlierWords(ctx context.Context, in incoming, words string) string {
	if !s.ambient.enabled() {
		return words
	}
	ctx, cancel := context.WithTimeout(ctx, logTimeout)
	defer cancel()
	recent, err := s.ambient.log.GroupMessagesSince(ctx, in.scope.ChatID, s.opts.Now().Add(-earlierWordsWindow), s.ambient.size)
	if err != nil {
		s.opts.Logger.Warn("could not read an owner's earlier messages", "chat_id", in.scope.ChatID, "error", err)
		return words
	}
	var earlier []string
	for _, m := range recent {
		if m.AuthorID == in.userID && m.MessageID != in.msg.MessageID {
			earlier = append(earlier, m.Text)
		}
	}
	return strings.Join(append(earlier, words), "\n")
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

// target describes the message a request replied to, for the tools that act
// on a member. It is taken from the update, so the model can never choose
// whom such a tool acts on. A service message is none: its author is whoever
// made something happen, not the member it is about. A member's message the
// bot cannot read, such as a contact or a location, is one.
//
// A message posted on behalf of a channel names the channel too, which the
// moderate tool bans rather than the account Telegram posts it as. One the
// group posts as itself, an anonymous admin's, and a post forwarded from the
// linked channel by Telegram itself, name no channel.
func (s *Service) target(replied *telegram.Message) *tools.Target {
	if replied == nil || replied.From == nil || replied.IsService() {
		return nil
	}
	text, _ := replied.Content()
	if channel := replied.SenderChat; channel != nil && channel.Type == telegram.ChatTypeChannel && !replied.IsAutomaticForward {
		return &tools.Target{
			MessageID: replied.MessageID,
			UserID:    replied.From.ID,
			ChannelID: channel.ID,
			Name:      withUsername(authorLabel(channel.Title), channel.Username),
			Text:      truncate(text, tools.MaxTargetRunes),
		}
	}
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
		log.Info("turn completed with a reaction", "emoji", answer.Reaction, "route", answer.Route)
		return outcome{reaction: answer.Reaction, answerID: answer.ID}
	case err == nil:
		log.Info("turn completed", "route", answer.Route)
		out := outcome{text: s.fitted(in, answer.Text), answerID: answer.ID, toReplied: answer.ToReplied, toName: answer.To}
		if answer.Text == "" {
			// A sticker or a GIF is an answer of its own, never one more
			// message after the words.
			out.sticker, out.gif = answer.Sticker, answer.GIF
		}
		return out
	case errors.Is(err, agent.ErrToolAsText):
		log.Warn("the model wrote a tool call as text twice; nothing was sent")
		return outcome{skipped: "tool_as_text"}
	case errors.Is(err, agent.ErrLeakedThinking):
		log.Warn("the model answered with its own reasoning; nothing was sent")
		return outcome{skipped: "leaked_thinking"}
	case errors.Is(err, agent.ErrSilent):
		log.Info("the model chose not to reply")
		return outcome{skipped: "model_silent"}
	case in.group && errors.Is(err, ai.ErrEmptyResponse):
		// Models give back nothing where [silent] was meant, such as after a
		// second insult; an error message would be worse than silence.
		log.Warn("the model gave an empty answer; nothing was sent")
		return outcome{skipped: "model_empty"}
	case errors.Is(err, ai.ErrTruncated):
		log.Warn("the model ran out of tokens mid-answer; the fragment was not sent")
		return outcome{text: textCutOff.to(in), skipped: "truncated"}
	case errors.Is(err, agent.ErrDuplicate):
		log.Info("duplicate update skipped")
		return outcome{duplicate: true}
	case ai.IsOverloaded(err):
		log.Warn("model provider overloaded or over its limits", "error", err)
		return outcome{text: textOverloaded.to(in), skipped: "overloaded", skipDetail: err.Error()}
	case errors.Is(err, context.DeadlineExceeded):
		log.Warn("turn timed out")
		return outcome{text: textTimeout.to(in), skipped: "timeout", skipDetail: err.Error()}
	default:
		// The error is logged in full but never shown: it can carry provider
		// or database details the user must not see.
		log.Error("turn failed", "error", err)
		return outcome{text: textError.to(in), skipped: "error", skipDetail: err.Error()}
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
	req.Context = paragraphs(s.chatHeader(ctx, in), s.rulesHeader(rules, in), s.profileLine(ctx, in, rules), notice, s.talksElsewhere(ctx, in), log, s.noStickersHint(ctx), s.ownerReplyHint(in), s.toThemHint(in), s.ownersHereLine(ctx, in), languageHint(in.text))
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

const toThemNote = "[%s is replying to a message from %s. If they ask you to say something to %s, in any words (\"nimadir de\", \"bunga bir nima de\", \"unga ayt\"), say it: start with exactly [to them], those two words and no name, then speak to %s, by name, not to %s, and never ask %s what to say. Without such a request, answer %s and leave out [to them].]"

// toThemHint names, right before the message, the member whose message a
// group message replies to, so that "nimadir de" is said to that member
// and [to them] goes only with words for them; otherwise it is "".
func (s *Service) toThemHint(in incoming) string {
	replied := in.msg.ReplyToMessage
	if !in.group || replied == nil || replied.From == nil || replied.IsService() ||
		replied.From.ID == s.opts.Bot.ID || replied.From.ID == in.userID {
		return ""
	}
	asker, member := plainLabel(in.msg.From), plainLabel(replied.From)
	return fmt.Sprintf(toThemNote, asker, member, member, member, asker, asker, asker)
}

// ownersHereLine tells the model, in a group, which owners are members
// there and may be tagged with @; the others are named without it, as the
// persona asks. It is "" outside groups or with no owner usernames.
func (s *Service) ownersHereLine(ctx context.Context, in incoming) string {
	if !in.group || s.opts.OwnerPresence == nil || len(s.opts.OwnerUsernames) == 0 {
		return ""
	}
	here, ok := s.ownersHere.get(in.scope.ChatID)
	if !ok {
		ctx, cancel := context.WithTimeout(ctx, logTimeout)
		defer cancel()
		here = []string{}
		for _, owner := range s.opts.OwnerUsernames {
			if _, err := s.opts.OwnerPresence.AuthorByUsername(ctx, in.scope.ChatID, owner); err == nil {
				here = append(here, "@"+owner)
			}
		}
		s.ownersHere.set(in.scope.ChatID, here)
	}
	if len(here) == 0 {
		return "[None of your owners is in this group: name them without @.]"
	}
	return "[Your owners in this group, whom you may tag: " + strings.Join(here, ", ") + ". Name any other owner without @.]"
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
	return truncateUnits(answer, s.replyLength(context.Background(), in.scope.ChatID))
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
		return textWelcome.to(in)
	case "/help":
		help := HelpTextIn(languageOf(in), Abilities{Draw: s.opts.ImagesEnabled, Listen: s.canListen(), Football: s.opts.FootballEnabled})
		if s.opts.Bot.Username != "" {
			// The bot's own username makes the group commands tappable.
			help = strings.ReplaceAll(help, "@username", "@"+s.opts.Bot.Username)
		}
		return help
	case "/about":
		return textAbout.to(in)
	case "/model":
		// Which model runs her is a secret but to her owners.
		if !s.isOwner(in.msg.From) {
			return textUnknown.to(in)
		}
		return fmt.Sprintf(textModel, s.opts.Model)
	case "/mute":
		return s.setMuted(ctx, in, true)
	case "/unmute":
		return s.setMuted(ctx, in, false)
	case "/ignore":
		return s.ignore(ctx, in)
	case "/unignore":
		return s.unignore(ctx, in)
	case "/import_stickers":
		return s.importStickers(ctx, in)
	case "/settings":
		return s.showSettings(ctx, in)
	case "/set":
		return s.setSetting(ctx, in)
	case "/limit":
		return s.setReplyLimit(ctx, in)
	case "/active":
		return s.setMinActive(ctx, in)
	case "/payments":
		return s.showPayments(ctx, in)
	case "/refund":
		return s.refund(ctx, in)
	case "/paysupport":
		return s.paySupport(in)
	default:
		return textUnknown.to(in)
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
		return textError.to(in)
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
		return textError.to(in)
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
		return nil, textUnknown.to(in)
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
		return textError.to(in)
	}
	if isAdmin {
		return textCannotIgnore
	}
	return ""
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
	was := s.isMutedLately(in.scope.ChatID)
	if err := s.opts.Muter.SetMuted(ctx, in.scope.ChatID, muted, in.userID); err != nil {
		s.opts.Logger.Error("mute change failed", "user_id", in.userID, "chat_id", in.scope.ChatID, "error", err)
		return textError.to(in)
	}
	s.markMuted(in.scope.ChatID, muted)
	s.opts.Logger.Info("mute changed", "user_id", in.userID, "chat_id", in.scope.ChatID, "muted", muted)
	// What was said before or during the mute should not resurface after it.
	// A /mute or /unmute that changes nothing keeps the log: owners sent
	// /unmute to groups that were never muted, and each wiped the day.
	if was != muted {
		s.ambient.clear(in.scope.ChatID)
	}
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
		return textError.to(in)
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
