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
	"telegram-ai-bot/internal/tools"
)

// Telegram Business: an owner with Telegram Premium can connect the bot to
// their own account as its chatbot, in Settings → Telegram Business →
// Chatbots, choosing the chats it may answer. Customers who write to that
// account are then answered by the bot, on the account's behalf, as a
// private chat is: the persona, the history and the tools, though not the
// owner's. Only an owner's account is used, and only while the owner has
// auto-reply on (/business on or off in their private chat with the bot).

// BusinessBook keeps the business accounts connected to the bot, the chats
// seen through them and the answers given there.
type BusinessBook interface {
	RecordBusinessConnection(ctx context.Context, c storage.BusinessConnection) (storage.BusinessConnection, error)
	BusinessConnection(ctx context.Context, id string) (storage.BusinessConnection, error)
	OwnerBusinessConnection(ctx context.Context, userID int64) (storage.BusinessConnection, error)
	SetBusinessAutoReply(ctx context.Context, userID int64, on bool) (int64, error)
	SeeBusinessChat(ctx context.Context, chat storage.BusinessChat, byOwner bool) (ownerWrote time.Time, err error)
	ReserveBusinessReply(ctx context.Context, q storage.BusinessQuota) (id int64, allowed bool, err error)
	ReleaseBusinessReply(ctx context.Context, id int64) error
}

// BusinessLookup asks Telegram about a business connection.
type BusinessLookup interface {
	GetBusinessConnection(ctx context.Context, id string) (telegram.BusinessConnection, error)
}

// ownerTakeover is how long the bot leaves a customer to the owner after
// the owner wrote in that chat themselves: two answering at once, the bot
// with no idea what the owner said, would be worse than either alone.
const ownerTakeover = 15 * time.Minute

// businessChat is the business account a customer's message came to.
type businessChat struct {
	connectionID string
	// owner is the account's name, as the turn's note gives it.
	owner string
}

// recordBusinessConnection keeps a connection Telegram reported: made,
// changed or ended. The owner is told what the bot may now do there.
func (s *Service) recordBusinessConnection(c *telegram.BusinessConnection) {
	if s.opts.Business == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	stored, err := s.keepConnection(ctx, *c)
	if err != nil {
		s.opts.Logger.Warn("could not record a business connection", "user_id", c.User.ID, "error", err)
		return
	}
	s.opts.Logger.Info("business connection changed", "user_id", c.User.ID, "enabled", c.IsEnabled, "can_reply", stored.CanReply, "refused", stored.Refused)
	if stored.Refused {
		return
	}
	text := textBusinessGone
	if stored.Enabled {
		text = s.businessStatus(stored)
	}
	sendCtx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if _, err := s.opts.Sender.SendMessage(sendCtx, telegram.Outgoing{ChatID: c.UserChatID, Text: text}); err != nil {
		s.opts.Logger.Warn("could not tell the owner about their business connection", "user_id", c.User.ID, "error", err)
	}
}

// keepConnection records a connection, refused unless it is an owner's:
// an owner known by ID, as BOT_OWNER_IDS lists them, since the account
// speaks for whoever holds it and a username can change hands.
func (s *Service) keepConnection(ctx context.Context, c telegram.BusinessConnection) (storage.BusinessConnection, error) {
	refused := !s.opts.OwnerIDs[c.User.ID]
	if refused {
		s.opts.Logger.Warn("a business account that is none of the owners' connected the bot; ignoring it", "user_id", c.User.ID, "username", c.User.Username)
	}
	rights := telegram.BusinessBotRights{}
	if c.Rights != nil {
		rights = *c.Rights
	}
	return s.opts.Business.RecordBusinessConnection(ctx, storage.BusinessConnection{
		ID: c.ID, UserID: c.User.ID, UserChatID: c.UserChatID, FirstName: c.User.FirstName, Username: c.User.Username,
		CanReply: rights.CanReply, CanManageStories: rights.CanManageStories, Enabled: c.IsEnabled, Refused: refused,
	})
}

// businessConnection is the connection a message came through. One the bot
// has no record of, as when it was made before the bot asked for business
// updates, is asked of Telegram and recorded.
func (s *Service) businessConnection(ctx context.Context, id string) (storage.BusinessConnection, bool) {
	c, err := s.opts.Business.BusinessConnection(ctx, id)
	if err == nil {
		return c, true
	}
	if !errors.Is(err, storage.ErrUnknownBusinessConnection) || s.opts.BusinessLookup == nil {
		s.opts.Logger.Warn("could not read a business connection", "error", err)
		return storage.BusinessConnection{}, false
	}
	fresh, err := s.opts.BusinessLookup.GetBusinessConnection(ctx, id)
	if err != nil {
		s.opts.Logger.Warn("could not learn a business connection from Telegram", "error", err)
		return storage.BusinessConnection{}, false
	}
	if c, err = s.keepConnection(ctx, fresh); err != nil {
		s.opts.Logger.Warn("could not record a business connection", "user_id", fresh.User.ID, "error", err)
		return storage.BusinessConnection{}, false
	}
	return c, true
}

// acceptBusiness keeps a customer's message to an owner's business account
// that the bot may answer. Every message there is noted, for the owners'
// checklists and so that the bot knows when the owner wrote; but the
// owner's own messages are never answered, nor those a bot sent for the
// account, the bot's own answers among them, nor another owner's, who talks
// to the bot in its own chat.
func (s *Service) acceptBusiness(update telegram.Update) (incoming, bool) {
	msg := update.BusinessMessage
	if s.opts.Business == nil || msg.From == nil || msg.Chat.Type != telegram.ChatTypePrivate || msg.BusinessConnectionID == "" {
		return incoming{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	connection, ok := s.businessConnection(ctx, msg.BusinessConnectionID)
	if !ok || connection.Refused {
		return incoming{}, false
	}
	byOwner := msg.From.ID == connection.UserID && msg.SenderBusinessBot == nil
	ownerWrote := s.seeBusinessChat(ctx, connection, msg, byOwner)
	switch {
	case msg.From.ID == connection.UserID, msg.SenderBusinessBot != nil, msg.From.IsBot, msg.IsService(), s.isOwner(msg.From):
		return incoming{}, false
	case !connection.Enabled || !connection.CanReply || !connection.AutoReply:
		return incoming{}, false
	case !ownerWrote.IsZero() && s.opts.Now().Sub(ownerWrote) < ownerTakeover:
		s.opts.Logger.Info("the owner is answering this customer; leaving it to them", "user_id", connection.UserID)
		return incoming{}, false
	}
	text, _ := msg.Content()
	return incoming{
		updateID: update.UpdateID,
		msg:      msg,
		// The conversation is the customer's with the account, kept under
		// the account's owner: apart from the customer's own private chat
		// with the bot, whose scope is the customer's alone.
		scope:    storage.Scope{ChatID: msg.Chat.ID, UserID: connection.UserID},
		userID:   msg.From.ID,
		text:     strings.TrimSpace(text),
		business: &businessChat{connectionID: connection.ID, owner: authorLabel(connection.FirstName)},
	}, true
}

// seeBusinessChat notes a message in a business chat and returns when the
// owner last wrote there; zero when unknown. The customer's name comes
// from their own message, or else from the chat.
func (s *Service) seeBusinessChat(ctx context.Context, connection storage.BusinessConnection, msg *telegram.Message, byOwner bool) time.Time {
	chat := storage.BusinessChat{ConnectionID: connection.ID, ChatID: msg.Chat.ID, FirstName: msg.Chat.FirstName, Username: msg.Chat.Username}
	if !byOwner && msg.From.ID == msg.Chat.ID {
		chat.FirstName, chat.Username = msg.From.FirstName, msg.From.Username
	}
	ownerWrote, err := s.opts.Business.SeeBusinessChat(ctx, chat, byOwner)
	if err != nil {
		s.opts.Logger.Warn("could not note a business chat", "user_id", connection.UserID, "error", err)
	}
	return ownerWrote
}

// processBusiness answers a customer of a business account, within the
// flood guard and the customer's daily answers. A customer is never sent a
// notice, a refusal or an error, which would come from the owner's account:
// past a limit, or when the turn fails, the bot says nothing.
func (s *Service) processBusiness(in incoming) {
	ctx, cancel := context.WithTimeout(context.Background(), s.opts.TurnTimeout)
	defer cancel()
	if _, limited := s.rateLimited(in); limited {
		s.noteSkip(in, "flood", "")
		return
	}
	if !s.hasContent(in) || utf8.RuneCountInString(in.text) > s.opts.MaxInputChars {
		return
	}
	out := s.askBusiness(ctx, in)
	if sentID := s.deliverBusiness(in, &out); out.answerID != 0 && sentID != 0 {
		s.recordSent(out.answerID, sentID)
	}
	if out.skipped != "" {
		s.noteSkip(in, out.skipped, out.skipDetail)
	}
}

// askBusiness has the model answer a customer, as ask does in a private
// chat. A command is only text here: the bot's commands are for its own
// chats.
func (s *Service) askBusiness(ctx context.Context, in incoming) outcome {
	reservation, allowed := s.takeBusinessReply(ctx, in)
	if !allowed {
		return outcome{skipped: "business_limit"}
	}
	in.described = s.describe(ctx, in.msg, in.text)
	started := time.Now()
	sink := &tools.Sink{}
	caller := s.caller(in)
	caller.Business = true
	req := s.businessRequest(ctx, in)
	caller.Picture = firstPicture(req.Attachments)
	answer, err := s.opts.Replier.Reply(tools.WithTurn(ctx, caller, sink), req)
	drawings := sink.Take()
	if !usedAnAnswer(err, drawings) && reservation != 0 {
		s.releaseBusinessReply(in, reservation)
	}
	out := s.answerOf(in, answer, err, started)
	out.drawings = drawings
	return out
}

// businessNote tells the model whose account it speaks for, every turn.
const businessNote = "[This is not your own chat. Your owner %s connected you to their Telegram Business account as its chatbot, and %s wrote to that account: you answer them on %s's behalf, and they see your words as sent from %s's account. " +
	"Be warm, brief and helpful, as %s's assistant. Answer what you can; for prices, orders, payments, meetings or anything else only %s can promise, say %s will answer that. " +
	"Only your words and pictures reach them here: no stickers, GIFs or reactions.]"

// businessRequest builds what the agent needs for a customer's message: the
// time, whose account the bot speaks for and the language, without the
// owners' rules, profiles or talks elsewhere, which are not the customer's.
func (s *Service) businessRequest(ctx context.Context, in incoming) agent.Request {
	req := agent.Request{
		Scope:            in.scope,
		UserID:           in.userID,
		UpdateID:         in.updateID,
		MessageID:        in.msg.MessageID,
		ReplyToMessageID: repliedID(in),
		Text:             bracketless.Replace(in.described),
		Attachments:      s.attachments(ctx, in),
	}
	owner, customer := in.business.owner, plainLabel(in.msg.From)
	note := fmt.Sprintf(businessNote, owner, customer, owner, owner, owner, owner, owner)
	req.Context = paragraphs(s.chatHeader(ctx, in), note, languageHint(in.text))
	if replied := in.msg.ReplyToMessage; replied != nil {
		req.Text = withQuote(req.Text, s.quoted(ctx, replied), s.opts.Bot.ID, plainLabel(replied.From))
	}
	return req
}

// deliverBusiness sends a customer the answer's words and pictures, on the
// account's behalf. Nothing else goes: stickers, GIFs and files are not
// sent through the account, and a bot cannot react for it. A failed turn
// sends nothing, not even the notice a private chat would get.
func (s *Service) deliverBusiness(in incoming, out *outcome) int64 {
	if out.skipped != "" || out.duplicate {
		return 0
	}
	out.reaction, out.sticker, out.gif, out.buttons = "", "", "", nil
	out.documents, out.audios, out.invoice = nil, nil, false
	return s.deliverAnswer(in, out)
}

// takeBusinessReply takes one of the customer's answers for the day, and
// returns the reservation to give back if the turn uses none. When the
// count cannot be read the customer is answered anyway, as in a private
// chat: the limit guards the cost.
func (s *Service) takeBusinessReply(ctx context.Context, in incoming) (reservation int64, allowed bool) {
	limit := s.opts.BusinessDailyLimit
	if limit <= 0 {
		return 0, true
	}
	ctx, cancel := context.WithTimeout(ctx, logTimeout)
	defer cancel()
	id, allowed, err := s.opts.Business.ReserveBusinessReply(ctx, storage.BusinessQuota{
		ConnectionID: in.business.connectionID,
		ChatID:       in.msg.Chat.ID,
		Since:        startOfDay(s.opts.Now(), s.opts.Location),
		Limit:        limit,
	})
	switch {
	case err != nil:
		s.opts.Logger.Warn("business reply count failed; answering anyway", "user_id", in.userID, "error", err)
		return 0, true
	case !allowed:
		s.opts.Logger.Info("business daily limit reached", "user_id", in.userID, "owner_id", in.scope.UserID)
	}
	return id, allowed
}

func (s *Service) releaseBusinessReply(in incoming, reservation int64) {
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	if err := s.opts.Business.ReleaseBusinessReply(ctx, reservation); err != nil {
		s.opts.Logger.Warn("could not give back a failed turn's answer", "user_id", in.userID, "error", err)
	}
}

// Replies to /business, an owner's command in their private chat with the
// bot. Like the other owners' commands they are in English.
const (
	textBusinessUsage = "/business shows your business account; /business on or /business off turns my answers to its customers on or off."
	textBusinessNone  = "No business account of yours is connected. With Telegram Premium: Settings → Telegram Business → Chatbots, add @%s, choose the chats I may answer, and let me reply to messages (and manage stories, for stories)."
	textBusinessGone  = "Your business account disconnected me: I no longer answer its customers."
)

// businessCommand shows an owner their business account, or turns the
// bot's answers to its customers on or off. It is an owner's only, known
// by ID as for the connection itself, and only in their private chat.
func (s *Service) businessCommand(ctx context.Context, in incoming) string {
	if s.opts.Business == nil || in.group || !s.opts.OwnerIDs[in.userID] {
		return textUnknown.to(in)
	}
	words := strings.Fields(strings.ToLower(in.text))
	switch {
	case len(words) == 1:
	case len(words) == 2 && (words[1] == "on" || words[1] == "off"):
		if _, err := s.opts.Business.SetBusinessAutoReply(ctx, in.userID, words[1] == "on"); err != nil {
			s.opts.Logger.Error("business auto-reply change failed", "user_id", in.userID, "error", err)
			return textError.to(in)
		}
		s.opts.Logger.Info("business auto-reply changed", "user_id", in.userID, "on", words[1] == "on")
	default:
		return textBusinessUsage
	}
	connection, err := s.opts.Business.OwnerBusinessConnection(ctx, in.userID)
	switch {
	case errors.Is(err, storage.ErrUnknownBusinessConnection):
		return fmt.Sprintf(textBusinessNone, s.opts.Bot.Username)
	case err != nil:
		s.opts.Logger.Error("could not read the owner's business connection", "user_id", in.userID, "error", err)
		return textError.to(in)
	}
	return s.businessStatus(connection)
}

// businessStatus says what the bot does on an owner's business account and
// what it lacks to do more.
func (s *Service) businessStatus(c storage.BusinessConnection) string {
	lines := []string{"Business account connected."}
	switch {
	case !c.CanReply:
		lines = append(lines, "I may not reply there: in Telegram Business → Chatbots, open my settings and let me reply to messages.")
	case !c.AutoReply:
		lines = append(lines, "Auto-reply is off: I don't answer its customers. /business on turns it on.")
	default:
		limit := "as many answers as they ask for"
		if s.opts.BusinessDailyLimit > 0 {
			limit = fmt.Sprintf("up to %d answers a day each", s.opts.BusinessDailyLimit)
		}
		lines = append(lines, fmt.Sprintf("Auto-reply is on: I answer customers who write to it in the chats you gave me, %s, and leave a chat to you for %d minutes after you write there. /business off turns it off.", limit, int(ownerTakeover.Minutes())))
	}
	if c.CanManageStories {
		lines = append(lines, "Stories: ask me here to post one, with a photo or a picture I draw.")
	} else {
		lines = append(lines, "Stories: let me manage stories in my chatbot settings, and I can post them for you.")
	}
	lines = append(lines, "Checklists: ask me here to send one to a customer who wrote in the last day.")
	return strings.Join(lines, "\n")
}
