package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// A group whose admins approve new members, or an invite link that asks for
// approval, sends the bot every request to join, where it may let people
// in. It checks them, in code and with no model: a known spammer is declined
// at once; anyone else is sent, in a private message, a button to show they
// are human, and let in on pressing it, or declined when joinWait passes
// without a press. Spam bots that ask to join never press it.
//
// The private message is the only way to reach someone who never wrote to
// the bot, and Telegram allows it for a few minutes after the request. When
// it cannot be sent, the request is left to the group's admins, as it was
// before the bot: declining people for the bot's failure would turn away
// real ones. A muted group's requests are left to them too, as are all of a
// group where the bot may not invite users.

const (
	// joinWait is how long the button waits for a press; the texts say ten
	// minutes.
	joinWait = 10 * time.Minute
	joinTime = 15 * time.Second
	// joinPayload starts the button's data, followed by the group's ID.
	joinPayload = "join:"
	// lateJoinsAtOnce bounds the late requests declined in one sweep, and
	// joinSweepEvery how often an instance sweeps while handling updates.
	lateJoinsAtOnce = 50
	joinSweepEvery  = time.Minute
)

// JoinBook keeps the join requests waiting for a press.
type JoinBook interface {
	AddJoinRequest(ctx context.Context, r storage.JoinRequest) (bool, error)
	TakeJoinRequest(ctx context.Context, chatID, userID int64) (storage.JoinRequest, bool, error)
	TakeLateJoinRequests(ctx context.Context, now time.Time, limit int) ([]storage.JoinRequest, error)
}

// JoinDecliner turns join requests down.
type JoinDecliner interface {
	DeclineChatJoinRequest(ctx context.Context, chatID, userID int64) error
}

// JoinGate answers join requests, and tells whether the bot may.
type JoinGate interface {
	JoinDecliner
	ApproveChatJoinRequest(ctx context.Context, chatID, userID int64) error
	GetChatMember(ctx context.Context, chatID, userID int64) (telegram.ChatMember, error)
}

// The private messages about a join request, which the requester reads in
// the language of their Telegram app.
var (
	textAdmitCheck = localText{
		uz: "Salom! «%s» guruhiga qo'shilish so'rovingizni ko'rdim. Odam ekaningizni tasdiqlash uchun 10 daqiqa ichida pastdagi tugmani bosing, shunda guruhga qabul qilinasiz.",
		ru: "Здравствуйте! Вы подали заявку в группу «%s». Чтобы подтвердить, что вы человек, нажмите кнопку ниже в течение 10 минут, и вас примут в группу.",
		en: "Hi! You asked to join «%s». To show you are human, press the button below within 10 minutes, and you will be let in.",
	}
	textHumanButton = localText{uz: "Men odamman ✅", ru: "Я человек ✅", en: "I'm human ✅"}
	textAdmitted    = localText{uz: "Rahmat! Guruhga qabul qilindingiz ✅", ru: "Спасибо! Вы приняты в группу ✅", en: "Thank you! You are in the group now ✅"}
	textAdmitLate   = localText{uz: "Afsus, 10 daqiqa o'tib ketdi va so'rov rad etildi. Guruhga qaytadan so'rov yuborishingiz mumkin.", ru: "К сожалению, 10 минут прошли, и заявка отклонена. Вы можете подать её снова.", en: "Sorry, the 10 minutes are up and the request was declined. You may ask to join again."}
	textAdmitGone   = localText{uz: "Bu so'rov allaqachon ko'rib chiqilgan.", ru: "Эта заявка уже рассмотрена.", en: "This request has already been handled."}
	textAdmitRetry  = localText{uz: "Hozir qabul qila olmadim, birozdan keyin tugmani yana bosing.", ru: "Сейчас не получилось принять заявку, нажмите кнопку ещё раз чуть позже.", en: "I couldn't let you in just now; press the button again in a moment."}
)

// joinsReady reports whether the bot answers join requests at all.
func (s *Service) joinsReady() bool {
	return s.opts.Joins != nil && s.opts.JoinGate != nil && s.opts.Buttons != nil
}

// requestedJoin checks a request to join a group: a known spammer is
// declined, anyone else sent the button.
func (s *Service) requestedJoin(r *telegram.ChatJoinRequest) {
	if !s.joinsReady() || r.From == nil || r.From.IsBot {
		return
	}
	chatID, userID := r.Chat.ID, r.From.ID
	ctx, cancel := context.WithTimeout(context.Background(), joinTime)
	defer cancel()
	if s.isMutedLately(chatID) || !s.canLetIn(ctx, chatID) {
		return
	}
	if s.opts.Guards != nil {
		if source, bad := s.spammer(ctx, userID); bad {
			s.declineJoin(ctx, chatID, userID, "spam list "+source)
			return
		}
	}
	// Kept first, so that a request Telegram delivers twice gets one button.
	added, err := s.opts.Joins.AddJoinRequest(ctx, storage.JoinRequest{
		ChatID: chatID, UserID: userID, UserChatID: r.UserChatID, Expires: s.opts.Now().Add(joinWait),
	})
	if err != nil {
		s.opts.Logger.Warn("join request: could not keep it; leaving it to the admins", "chat_id", chatID, "user_id", userID, "error", err)
		return
	}
	if !added {
		return
	}
	if err := s.sendJoinCheck(ctx, r); err != nil {
		s.opts.Logger.Warn("join request: could not send the button; leaving it to the admins", "chat_id", chatID, "user_id", userID, "error", err)
		if _, _, err := s.opts.Joins.TakeJoinRequest(ctx, chatID, userID); err != nil {
			s.opts.Logger.Warn("join request: could not forget it", "chat_id", chatID, "user_id", userID, "error", err)
		}
		return
	}
	s.opts.Logger.Info("join request: button sent", "chat_id", chatID, "user_id", userID)
}

// canLetIn reports whether the bot may approve requests to join a group:
// Telegram is asked, as the right can be taken away at any time.
func (s *Service) canLetIn(ctx context.Context, chatID int64) bool {
	me, err := s.opts.JoinGate.GetChatMember(ctx, chatID, s.opts.Bot.ID)
	if err != nil {
		s.opts.Logger.Warn("join request: could not ask for the bot's rights; leaving it to the admins", "chat_id", chatID, "error", err)
		return false
	}
	return me.Has(telegram.RightInviteUsers)
}

// sendJoinCheck sends the requester the button, in a private message.
func (s *Service) sendJoinCheck(ctx context.Context, r *telegram.ChatJoinRequest) error {
	in := joinIncoming(r.From, r.UserChatID)
	button := telegram.Button{Text: textHumanButton.to(in), Data: joinPayload + strconv.FormatInt(r.Chat.ID, 10)}
	_, err := s.opts.Sender.SendMessage(ctx, telegram.Outgoing{
		ChatID:  r.UserChatID,
		Text:    fmt.Sprintf(textAdmitCheck.to(in), groupName(r.Chat)),
		Buttons: [][]telegram.Button{{button}},
	})
	return err
}

// joinIncoming is a requester as localText needs them, for the language of
// their app.
func joinIncoming(user *telegram.User, privateChatID int64) incoming {
	return incoming{msg: &telegram.Message{From: user, Chat: telegram.Chat{ID: privateChatID, Type: telegram.ChatTypePrivate}}, userID: user.ID}
}

// groupName is how the private message names a group.
func groupName(chat telegram.Chat) string {
	if title := strings.TrimSpace(chat.Title); title != "" {
		return title
	}
	return "@" + chat.Username
}

// pressedJoin lets in whoever pressed the button under their request, while
// it waits and is theirs: the group comes from the button, the person from
// the press, which Telegram vouches for.
func (s *Service) pressedJoin(q *telegram.CallbackQuery) {
	chatID, err := strconv.ParseInt(strings.TrimPrefix(q.Data, joinPayload), 10, 64)
	if err != nil || !s.joinsReady() {
		return
	}
	in := joinIncoming(q.From, q.Message.Chat.ID)
	ctx, cancel := context.WithTimeout(context.Background(), joinTime)
	defer cancel()
	request, found, err := s.opts.Joins.TakeJoinRequest(ctx, chatID, q.From.ID)
	switch {
	case err != nil:
		s.opts.Logger.Warn("join request: could not read it", "chat_id", chatID, "user_id", q.From.ID, "error", err)
		s.answerPress(ctx, q, textAdmitRetry.to(in), false)
		return
	case !found:
		s.answerPress(ctx, q, textAdmitGone.to(in), true)
		return
	case !s.opts.Now().Before(request.Expires):
		// Pressed after its time, before a sweep declined it.
		s.declineJoin(ctx, chatID, q.From.ID, "late")
		s.answerPress(ctx, q, textAdmitLate.to(in), true)
		return
	}
	if err := s.opts.JoinGate.ApproveChatJoinRequest(ctx, chatID, q.From.ID); err != nil {
		s.failedApproval(ctx, q, in, request, err)
		return
	}
	s.opts.Logger.Info("join request: approved after the button", "chat_id", chatID, "user_id", q.From.ID)
	s.answerPress(ctx, q, textAdmitted.to(in), true)
}

// failedApproval says why a press let nobody in. Refused, the request is
// gone, most often answered by an admin meanwhile. Without an answer, it is
// kept again, so that another press, or the sweep, settles it.
func (s *Service) failedApproval(ctx context.Context, q *telegram.CallbackQuery, in incoming, request storage.JoinRequest, err error) {
	var refusal *telegram.APIError
	if errors.As(err, &refusal) && refusal.Code < http.StatusInternalServerError {
		s.opts.Logger.Info("join request: Telegram refused the approval", "chat_id", request.ChatID, "user_id", request.UserID, "error", err)
		s.answerPress(ctx, q, textAdmitGone.to(in), true)
		return
	}
	s.opts.Logger.Warn("join request: could not approve", "chat_id", request.ChatID, "user_id", request.UserID, "error", err)
	if _, err := s.opts.Joins.AddJoinRequest(context.WithoutCancel(ctx), request); err != nil {
		s.opts.Logger.Warn("join request: could not keep it again", "chat_id", request.ChatID, "user_id", request.UserID, "error", err)
	}
	s.answerPress(ctx, q, textAdmitRetry.to(in), false)
}

// answerPress ends the press's spinner with text, and when done, puts text
// in place of the message with the button, which then has nothing more to
// do.
func (s *Service) answerPress(ctx context.Context, q *telegram.CallbackQuery, text string, done bool) {
	if err := s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, text, !done); err != nil {
		s.opts.Logger.Warn("join request: could not answer the button", "user_id", q.From.ID, "error", err)
	}
	if !done {
		return
	}
	if err := s.opts.Buttons.EditMessageText(ctx, q.Message.Chat.ID, q.Message.MessageID, text); err != nil {
		s.opts.Logger.Warn("join request: could not replace the button", "user_id", q.From.ID, "error", err)
	}
}

// declineJoin turns a request down and logs why.
func (s *Service) declineJoin(ctx context.Context, chatID, userID int64, why string) {
	if err := s.opts.JoinGate.DeclineChatJoinRequest(ctx, chatID, userID); err != nil {
		s.opts.Logger.Warn("join request: could not decline", "chat_id", chatID, "user_id", userID, "reason", why, "error", err)
		return
	}
	s.opts.Logger.Info("join request: declined", "chat_id", chatID, "user_id", userID, "reason", why)
}

// DeclineLateJoins declines the requests whose button was not pressed in
// time, at most lateJoinsAtOnce of them, and returns how many it declined.
// Each is taken before it is declined, so that none is declined twice; one
// Telegram refuses was most often answered by an admin meanwhile, and is
// dropped all the same.
func DeclineLateJoins(ctx context.Context, book JoinBook, gate JoinDecliner, now time.Time, logger *slog.Logger) (int, error) {
	late, err := book.TakeLateJoinRequests(ctx, now, lateJoinsAtOnce)
	if err != nil {
		return 0, err
	}
	declined := 0
	for _, r := range late {
		if err := gate.DeclineChatJoinRequest(ctx, r.ChatID, r.UserID); err != nil {
			logger.Info("join request: could not decline a late one", "chat_id", r.ChatID, "user_id", r.UserID, "error", err)
			continue
		}
		declined++
	}
	if declined > 0 {
		logger.Info("join requests: late ones declined", "declined", declined)
	}
	return declined, nil
}

// DeclineLateJoins declines the late join requests now: the long-polling
// command calls it on a timer.
func (s *Service) DeclineLateJoins(ctx context.Context) {
	if !s.joinsReady() {
		return
	}
	if _, err := DeclineLateJoins(ctx, s.opts.Joins, s.opts.JoinGate, s.opts.Now(), s.opts.Logger); err != nil && ctx.Err() == nil {
		s.opts.Logger.Warn("join requests: could not decline the late ones", "error", err)
	}
}

// sweepJoins declines the late join requests at most once per
// joinSweepEvery in this instance. On serverless hosting nothing runs
// between updates, so an update is what notices that a request's time is
// up; the daily cleanup catches what no update did.
func (s *Service) sweepJoins() {
	now := s.opts.Now().Unix()
	last := s.joinSweep.Load()
	if !s.joinsReady() || now-last < int64(joinSweepEvery/time.Second) || !s.joinSweep.CompareAndSwap(last, now) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), joinTime)
	defer cancel()
	s.DeclineLateJoins(ctx)
}
