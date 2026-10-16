package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// The moderate tool's actions. All but cancel act on the message the person
// asking replied to, or on its author.
const (
	actionDelete = "delete"
	actionMute   = "mute"
	actionUnmute = "unmute"
	actionBan    = "ban"
	actionUnban  = "unban"
	actionPin    = "pin"
	actionUnpin  = "unpin"
	actionCancel = "cancel"
)

const (
	moderationTimeout = 20 * time.Second
	// A mute lasts an hour unless the person asking says otherwise, and a
	// week at most.
	defaultMuteMinutes = 60
	maxMuteMinutes     = 7 * 24 * 60
	// pendingMaxAge is how long an action waits for the person who asked to
	// confirm it.
	pendingMaxAge = 5 * time.Minute
	// One person may have the bot act maxActions times per actionWindow, so
	// that a careless or stolen admin account cannot empty a group through
	// it.
	maxActions   = 10
	actionWindow = 10 * time.Minute
	// maxReasonRunes keeps the audit's reason short.
	maxReasonRunes  = 200
	clockLayout     = "15:04"
	dateClockLayout = "02.01 15:04"
)

// Refusals, in words the model, or the chat, can be given.
const (
	notInAGroup   = "only a person in a group can ask for that"
	noReplyTarget = "reply to the message of the member you mean"
	immune        = "I can't do that to admins, my owners, myself, Telegram's service accounts or the person asking"
	noAnswer      = "Telegram did not answer, so it may not have been done; try again later"
	noQuestion    = "I could not post the question that asks them to confirm, so nothing will be done; ask again later"
	askedNote     = "Nothing is done yet. I posted my own question asking them to confirm by replying yes to it; their answer is handled without you. Don't ask again and don't say it is done: reply with exactly [react 👌]."
	askedTwice    = "This message was delivered twice and the first delivery already asked them to confirm, so nothing more was done now: reply with exactly [silent]."
	oneAtATime    = "one action at a time: I already posted my question about the other action they asked for in this message. Nothing is asked or done for this one; tell them to ask for it again after answering that question"
)

// chatText is something the bot says to a group in its own words, never the
// model's: in Uzbek, then in Russian on the next line.
type chatText struct{ uz, ru string }

func (t chatText) String() string { return t.uz + "\n" + t.ru }

// What the chat is told when an action is not done: cancelledText when the
// person said no, the others why not, in words that complete "Bajarilmadi:
// ..." and "Не выполнено: ...".
var (
	cancelledText = chatText{"Bekor qilindi: hech narsa qilinmadi.", "Отменено: ничего не сделано."}
	lookupWords   = chatText{
		"Telegram'dan kim nima qila olishini bilib bo'lmadi; keyinroq urinib ko'ring",
		"не удалось узнать у Telegram, кто что может; попробуйте позже",
	}
	noAnswerWords = chatText{
		"Telegram javob bermadi, bajarilmagan bo'lishi mumkin; keyinroq urinib ko'ring",
		"Telegram не ответил, возможно, ничего не сделано; попробуйте позже",
	}
	brokenWords = chatText{"men tomonda nimadir buzildi; keyinroq yana so'rang", "у меня что-то сломалось; попросите позже"}
	immuneWords = chatText{
		"buni adminlarga, egalarimga, o'zimga, Telegram xizmat akkauntlariga yoki so'ragan odamning o'ziga qila olmayman",
		"я не могу сделать это с админами, моими владельцами, собой, служебными аккаунтами Telegram или с тем, кто просит",
	}
)

// errLookup reports that Telegram could not say who may do what. Moderation
// then does nothing: it never acts on a guess.
var errLookup = errors.New("could not ask Telegram who may do this, so nothing was done; try again later")

// refusal is a DeniedError that also says why for the chat, which is told
// when an action it confirmed is refused after all.
type refusal struct {
	DeniedError
	chat chatText
}

func (r *refusal) Unwrap() error { return &r.DeniedError }

func refuse(message string, chat chatText) error {
	return &refusal{DeniedError: DeniedError{Message: message}, chat: chat}
}

// neededRight is the admin right an action needs, from the person asking
// and from the bot. cancel needs none.
var neededRight = map[string]string{
	actionDelete: telegram.RightDeleteMessages,
	actionMute:   telegram.RightRestrictMembers,
	actionUnmute: telegram.RightRestrictMembers,
	actionBan:    telegram.RightRestrictMembers,
	actionUnban:  telegram.RightRestrictMembers,
	actionPin:    telegram.RightPinMessages,
	actionUnpin:  telegram.RightPinMessages,
}

// rightWords names the rights moderation needs, for refusals; rightChat,
// for the chat.
var (
	rightWords = map[string]string{
		telegram.RightDeleteMessages:  "delete messages",
		telegram.RightRestrictMembers: "ban and mute members",
		telegram.RightPinMessages:     "pin messages",
	}
	rightChat = map[string]chatText{
		telegram.RightDeleteMessages:  {"xabarlarni o'chirish", "удалять сообщения"},
		telegram.RightRestrictMembers: {"a'zolarni bloklash va ovozsiz qilish", "банить и заглушать участников"},
		telegram.RightPinMessages:     {"xabarlarni qadash", "закреплять сообщения"},
	}
)

// MemberLookup asks Telegram about a member of a chat.
type MemberLookup interface {
	GetChatMember(ctx context.Context, chatID, userID int64) (telegram.ChatMember, error)
}

// Moderator has Telegram act on a group's messages and members.
type Moderator interface {
	DeleteMessage(ctx context.Context, chatID, messageID int64) error
	RestrictChatMember(ctx context.Context, r telegram.Restriction) error
	BanChatMember(ctx context.Context, chatID, userID int64) error
	UnbanChatMember(ctx context.Context, chatID, userID int64) error
	PinChatMessage(ctx context.Context, chatID, messageID int64) error
	UnpinChatMessage(ctx context.Context, chatID, messageID int64) error
}

// QuestionSender posts the bot's question that asks to confirm an action.
type QuestionSender interface {
	SendMessage(ctx context.Context, msg telegram.Outgoing) (int64, error)
}

// ModerationStore keeps the audit of what the bot did for whom, and the
// actions that wait for confirmation.
type ModerationStore interface {
	RecordModeration(ctx context.Context, a storage.ModerationAction) (bool, error)
	ForgetModeration(ctx context.Context, updateID int64, action string) error
	UpdateModerated(ctx context.Context, updateID int64) (bool, error)
	ModerationBy(ctx context.Context, requesterID int64, since time.Time) (int, error)
	SavePendingModeration(ctx context.Context, p storage.PendingModeration) (bool, error)
	SetModerationPrompt(ctx context.Context, requesterID, chatID, updateID, promptID int64) error
	TakePendingModeration(ctx context.Context, requesterID, chatID, promptID int64, maxAge time.Duration) (storage.PendingModeration, error)
	CancelPendingModeration(ctx context.Context, requesterID, chatID int64) (bool, error)
}

// RightsRecorder stores what Telegram said about the bot in a group.
type RightsRecorder interface {
	SetBotRights(ctx context.Context, chatID int64, status string, rights []string) error
}

// ModerationOptions configures moderation. Every field but Location and Now
// is required.
type ModerationOptions struct {
	// BotID is the bot's own user ID.
	BotID int64
	// Members tells who holds which rights, asked anew for every action.
	Members  MemberLookup
	Telegram Moderator
	// Questions posts the question that asks to confirm an action.
	Questions QuestionSender
	Store     ModerationStore
	// Rights keeps the bot's rights learned along the way, for the turn
	// header.
	Rights RightsRecorder
	// Location is the time zone of the times reported.
	Location *time.Location
	Now      func() time.Time
	// OwnersByID is true when the bot's owners are recognised by their
	// Telegram ID. Only then may an owner moderate without being an admin,
	// and is an owner spared: a username can be given up and taken by
	// someone else, who would get both.
	OwnersByID bool
}

type moderationArgs struct {
	Action  string `json:"action"`
	Minutes int    `json:"minutes"`
	Reason  string `json:"reason"`
}

// Moderation lets a group's admins, and the bot's owners, have the bot
// delete, mute, ban or pin there. The model only picks the action: whom it
// acts on is the message the person asking replied to, taken from the
// update, and every permission is checked here against Telegram, never taken
// from the model or the chat.
//
// The model is not trusted to have understood that it was asked, either: it
// reads the whole chat, and a line in it can talk it into anything. So
// nothing is done when it calls the tool. The bot posts a question of its
// own, saying exactly what it will do, and acts only when the person who
// asked replies yes to that question, which Confirm checks without the
// model.
type Moderation struct {
	opts ModerationOptions
}

// NewModeration returns moderation configured by opts.
func NewModeration(opts ModerationOptions) *Moderation {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	return &Moderation{opts}
}

// Tool is the moderate tool, through which the model asks for an action.
func (m *Moderation) Tool() Tool {
	return Tool{
		Name: "moderate",
		Description: "Whenever someone in a group asks you to: have something done to the message they replied to. It checks who may ask, which are the group's admins with the right and your owners, and refuses anyone else with the reason. " +
			"delete: delete that message. mute (minutes, default 60) or unmute: its author. ban or unban: its author. pin or unpin: that message. " +
			"Nothing is done at once: the tool checks everyone's rights, then posts its own question asking the person to confirm, and their reply is handled without you. " +
			"cancel: drop what they asked for. Never say something was done.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["delete", "mute", "unmute", "ban", "unban", "pin", "unpin", "cancel"]},
				"minutes": {"type": "integer", "description": "For mute: how long, 1 to 10080. Default 60."},
				"reason": {"type": "string", "description": "Optional: why, in a few words."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		GroupOnly: true,
		Timeout:   moderationTimeout,
		Run:       m.run,
	}
}

// order is one request to moderate, after its arguments are read.
type order struct {
	caller Caller
	action string
	target Target
	reason string
	// minutes and until are a mute's.
	minutes int
	until   time.Time
}

func (m *Moderation) run(ctx context.Context, raw json.RawMessage) (any, error) {
	var args moderationArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, err := groupRequester(ctx)
	if err != nil {
		return nil, err
	}
	if args.Action == actionCancel {
		cancelled, err := m.opts.Store.CancelPendingModeration(ctx, caller.UserID, caller.ChatID)
		if err != nil {
			return nil, internal(err)
		}
		return map[string]bool{"cancelled": cancelled}, nil
	}
	if _, known := neededRight[args.Action]; !known {
		return nil, &ArgumentError{Message: "action must be delete, mute, unmute, ban, unban, pin, unpin or cancel"}
	}
	if caller.Target == nil {
		return nil, &DeniedError{Message: noReplyTarget}
	}
	o := m.newOrder(caller, args.Action, *caller.Target, args.Minutes, args.Reason)
	if err := m.check(ctx, o); err != nil {
		return nil, err
	}
	return m.ask(ctx, o)
}

// groupRequester is who the turn is for, if a person in a group: a bot, or
// a turn without an update to record, may not moderate.
func groupRequester(ctx context.Context) (Caller, error) {
	caller, ok := CallerFrom(ctx)
	if !ok || !caller.Human || caller.ChatID >= 0 || caller.UpdateID == 0 {
		return Caller{}, &DeniedError{Message: notInAGroup}
	}
	return caller, nil
}

// newOrder is caller's request. A mute's end is counted from now: from the
// confirmation, when the order is carried out.
func (m *Moderation) newOrder(caller Caller, action string, target Target, minutes int, reason string) order {
	o := order{caller: caller, action: action, target: target, reason: cutRunes(strings.TrimSpace(reason), maxReasonRunes)}
	if action == actionMute {
		o.minutes = muteMinutes(minutes)
		o.until = m.opts.Now().Add(time.Duration(o.minutes) * time.Minute)
	}
	return o
}

// muteMinutes is how long a mute lasts: an hour when not given, otherwise
// a minute to a week.
func muteMinutes(minutes int) int {
	if minutes == 0 {
		return defaultMuteMinutes
	}
	return min(max(minutes, 1), maxMuteMinutes)
}

// check decides whether the bot may carry out o: whether the person asking
// may ask, the bot may act, the target may be acted on, and the person has
// asked too often. Telegram is asked anew each time, and when it cannot
// answer, the answer is no.
func (m *Moderation) check(ctx context.Context, o order) error {
	right := neededRight[o.action]
	if err := m.checkRequester(ctx, o.caller, right); err != nil {
		return err
	}
	if err := m.checkBot(ctx, o.caller.ChatID, right); err != nil {
		return err
	}
	if err := m.checkTarget(ctx, o); err != nil {
		return err
	}
	return m.checkPace(ctx, o.caller)
}

// checkRequester lets an owner known by ID ask in any group, and anyone
// else only when Telegram says they hold the right themselves.
func (m *Moderation) checkRequester(ctx context.Context, caller Caller, right string) error {
	if caller.Owner && m.opts.OwnersByID {
		return nil
	}
	member, err := m.opts.Members.GetChatMember(ctx, caller.ChatID, caller.UserID)
	if err != nil {
		return errLookup
	}
	if !member.Has(right) {
		words := rightChat[right]
		return refuse(
			fmt.Sprintf("only admins of this group who have the right to %s, or my owners, can ask me to do that; the person asking is neither", rightWords[right]),
			chatText{
				"buni faqat guruhning " + words.uz + " huquqi bor adminlari yoki egalarim so'ray oladi; so'ragan odam ulardan emas",
				"об этом могут просить только админы группы с правом " + words.ru + " или мои владельцы; тот, кто просит, к ним не относится",
			})
	}
	return nil
}

// checkBot asks Telegram whether the bot holds the right, and keeps the
// answer, which also corrects the rights the turn header shows.
func (m *Moderation) checkBot(ctx context.Context, chatID int64, right string) error {
	me, err := m.opts.Members.GetChatMember(ctx, chatID, m.opts.BotID)
	if err != nil {
		return errLookup
	}
	// Keeping it only saves asking again; the answer holds either way.
	_ = m.opts.Rights.SetBotRights(ctx, chatID, me.Status, me.Rights())
	if !me.Has(right) {
		words := rightChat[right]
		return refuse(
			fmt.Sprintf("I don't have the right to %s in this group; its admins can give it to me", rightWords[right]),
			chatText{
				"menda bu guruhda " + words.uz + " huquqi yo'q; uni adminlar bera oladi",
				"у меня нет права " + words.ru + " в этой группе; его могут дать админы",
			})
	}
	return nil
}

// checkTarget keeps the bot off those it must not act on: Telegram's own
// accounts, the bot's owners known by ID and the group's admins. The bot and the person
// asking may have their own messages deleted, pinned or unpinned, but never
// mute or ban themselves. A confirmed action's target is not known to be an
// owner or not; that was checked when it was asked for.
func (m *Moderation) checkTarget(ctx context.Context, o order) error {
	userID := o.target.UserID
	if userID == m.opts.BotID || userID == o.caller.UserID {
		return ownMessageOnly(o.action)
	}
	if isTelegramAccount(userID) || (o.target.Owner && m.opts.OwnersByID) {
		return refuse(immune, immuneWords)
	}
	member, err := m.opts.Members.GetChatMember(ctx, o.caller.ChatID, userID)
	if err != nil {
		return errLookup
	}
	if member.IsAdmin() {
		return refuse(immune, immuneWords)
	}
	return nil
}

// ownMessageOnly allows the actions that touch a message rather than its
// author.
func ownMessageOnly(action string) error {
	switch action {
	case actionDelete, actionPin, actionUnpin:
		return nil
	}
	return refuse(immune, immuneWords)
}

// checkPace allows maxActions per actionWindow per person, counted in the
// audit, in every group together.
func (m *Moderation) checkPace(ctx context.Context, caller Caller) error {
	done, err := m.opts.Store.ModerationBy(ctx, caller.UserID, m.opts.Now().Add(-actionWindow))
	if err != nil {
		return internal(err)
	}
	if done >= maxActions {
		minutes := int(actionWindow.Minutes())
		return refuse(
			fmt.Sprintf("that's %d actions in %d minutes; wait a bit", maxActions, minutes),
			chatText{
				fmt.Sprintf("%d daqiqada %d ta amal bo'ldi; biroz kuting", minutes, maxActions),
				fmt.Sprintf("это уже %d действий за %d минут; подождите немного", maxActions, minutes),
			})
	}
	return nil
}

// ask keeps o until the person who asked confirms it, and posts the bot's
// question to them, as a reply to their request. An update Telegram
// delivers again asks nothing more: its first delivery asked. A second
// action in the same update is refused: one question waits at a time.
func (m *Moderation) ask(ctx context.Context, o order) (any, error) {
	saved, err := m.opts.Store.SavePendingModeration(ctx, o.pending())
	if errors.Is(err, storage.ErrOtherModerationPending) {
		return nil, &DeniedError{Message: oneAtATime}
	}
	if err != nil {
		return nil, internal(err)
	}
	if !saved {
		return map[string]any{"already_asked": true, "note": askedTwice}, nil
	}
	promptID, err := m.opts.Questions.SendMessage(ctx, telegram.Outgoing{
		ChatID: o.caller.ChatID, Text: m.question(o), ReplyToMessageID: o.caller.MessageID,
	})
	if err == nil {
		err = m.opts.Store.SetModerationPrompt(ctx, o.caller.UserID, o.caller.ChatID, o.caller.UpdateID, promptID)
	}
	if err != nil {
		// Without a question to reply to, the action can never be confirmed.
		// Why the question failed is for the logs.
		_, _ = m.opts.Store.CancelPendingModeration(context.WithoutCancel(ctx), o.caller.UserID, o.caller.ChatID)
		return nil, errors.Join(errors.New(noQuestion), internal(err))
	}
	return map[string]any{"asked": true, "action": o.action, "target": o.target.Name, "note": askedNote}, nil
}

func (o order) pending() storage.PendingModeration {
	return storage.PendingModeration{
		RequesterID: o.caller.UserID, ChatID: o.caller.ChatID, Action: o.action,
		TargetUserID: o.target.UserID, TargetMessageID: o.target.MessageID,
		TargetName: o.target.Name, TargetText: o.target.Text,
		Minutes: o.minutes, Reason: o.reason, UpdateID: o.caller.UpdateID,
	}
}

// question is what the bot asks before it acts. It is the bot's own words,
// never the model's, so that what the person confirms is what will be done.
// The model's reason is left out for the same cause.
func (m *Moderation) question(o order) string {
	what, minutes := o.describe(), int(pendingMaxAge.Minutes())
	return chatText{
		fmt.Sprintf("Tasdiqlaysizmi: %s? Shu xabarga «ha» deb javob bering (%d daqiqa ichida), yo'q bo'lsa «yo'q».", what.uz, minutes),
		fmt.Sprintf("Подтвердите: %s? Ответьте «да» на это сообщение (в течение %d минут), иначе «нет».", what.ru, minutes),
	}.String()
}

// describe says what o will do, in words that complete "Tasdiqlaysizmi:
// ...?" and "Подтвердите: ...?".
func (o order) describe() chatText {
	name := o.target.Name
	switch o.action {
	case actionDelete:
		return chatText{name + " xabari o'chirilsinmi", "удалить сообщение от " + name}
	case actionMute:
		return chatText{name + " " + uzbekDuration(o.minutes) + "ga ovozsiz qilinsinmi", "заглушить " + name + " на " + russianDuration(o.minutes)}
	case actionUnmute:
		return chatText{name + " ovozsizlikdan chiqarilsinmi", "снова разрешить " + name + " писать"}
	case actionBan:
		return chatText{name + " bloklansinmi", "забанить " + name}
	case actionUnban:
		return chatText{name + " blokdan chiqarilsinmi", "разбанить " + name}
	case actionPin:
		return chatText{name + " xabari qadalsinmi", "закрепить сообщение от " + name}
	}
	return chatText{name + " xabari qadashdan olinsinmi", "открепить сообщение от " + name}
}

// uzbekDuration writes minutes the way a person would: "90 daqiqa",
// "2 soat", "7 kun".
func uzbekDuration(minutes int) string {
	switch {
	case minutes%(24*60) == 0:
		return fmt.Sprintf("%d kun", minutes/(24*60))
	case minutes%60 == 0:
		return fmt.Sprintf("%d soat", minutes/60)
	}
	return fmt.Sprintf("%d daqiqa", minutes)
}

// russianDuration is uzbekDuration in Russian, in the form that follows
// "на": "1 минуту", "2 часа", "7 дней".
func russianDuration(minutes int) string {
	switch {
	case minutes%(24*60) == 0:
		return russianCount(minutes/(24*60), "день", "дня", "дней")
	case minutes%60 == 0:
		return russianCount(minutes/60, "час", "часа", "часов")
	}
	return russianCount(minutes, "минуту", "минуты", "минут")
}

// russianCount is n with the word in the form Russian puts after it: one
// for 1, 21, 31; few for 2-4, 22-24; many for the rest, 11-14 among them.
func russianCount(n int, one, few, many string) string {
	word := many
	switch {
	case n%10 == 1 && n%100 != 11:
		word = one
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		word = few
	}
	return fmt.Sprintf("%d %s", n, word)
}

// Confirmation is what came of someone's answer to the bot's question about
// an action they asked for.
type Confirmation struct {
	// Found is false when no action waited for that person's answer to that
	// message: what they wrote is then an ordinary message.
	Found bool
	// Text says, for the chat, what was done or why not; "" when nothing
	// needs saying, as for an answer Telegram delivered again.
	Text string
}

// Confirm carries out, or cancels, the action caller asked for and the bot
// asked them about in promptID, now that they replied to that question with
// yes or no. Everything is checked again first: rights may have changed
// while it waited. The error, when there is one, is only for the logs: the
// Confirmation already says, in safe words, that it failed.
func (m *Moderation) Confirm(ctx context.Context, caller Caller, promptID int64, yes bool) (Confirmation, error) {
	if !caller.Human || caller.ChatID >= 0 || caller.UpdateID == 0 {
		return Confirmation{}, nil
	}
	pending, err := m.opts.Store.TakePendingModeration(ctx, caller.UserID, caller.ChatID, promptID, pendingMaxAge)
	if errors.Is(err, storage.ErrNoPendingModeration) {
		return m.answeredBefore(ctx, caller)
	}
	if err != nil {
		return Confirmation{}, err
	}
	if !yes {
		return Confirmation{Found: true, Text: cancelledText.String()}, nil
	}
	target := Target{UserID: pending.TargetUserID, MessageID: pending.TargetMessageID, Name: pending.TargetName, Text: pending.TargetText}
	o := m.newOrder(caller, pending.Action, target, pending.Minutes, pending.Reason)
	if err := m.check(ctx, o); err != nil {
		return notDone(err)
	}
	if err := m.carryOut(ctx, o); err != nil {
		return notDone(err)
	}
	return Confirmation{Found: true, Text: m.doneText(o)}, nil
}

// answeredBefore handles an answer that finds nothing waiting. When the
// same update already acted, Telegram delivered it again, and nothing more
// is said; otherwise it was no answer to a question, and is left alone.
func (m *Moderation) answeredBefore(ctx context.Context, caller Caller) (Confirmation, error) {
	acted, err := m.opts.Store.UpdateModerated(ctx, caller.UpdateID)
	if err != nil || !acted {
		return Confirmation{}, err
	}
	return Confirmation{Found: true}, nil
}

// notDone says why a confirmed action was not carried out. A refusal, or
// Telegram's answer, is safe to show; anything else, such as the database's
// error, is told only as something going wrong. What the chat is not told is
// returned for the logs.
func notDone(err error) (Confirmation, error) {
	var (
		refused *refusal
		failure *telegramError
		why     chatText
	)
	switch {
	case errors.As(err, &refused):
		why = refused.chat
	case errors.Is(err, errLookup):
		why = lookupWords
	case errors.As(err, &failure):
		why = failure.chat
	default:
		return notDoneText(brokenWords), cause(err)
	}
	if failure != nil && err != error(failure) {
		// The store could not drop the record of what Telegram did not do.
		return notDoneText(why), cause(err)
	}
	return notDoneText(why), nil
}

func notDoneText(why chatText) Confirmation {
	return Confirmation{Found: true, Text: chatText{"Bajarilmadi: " + why.uz + ".", "Не выполнено: " + why.ru + "."}.String()}
}

// carryOut records o, then has Telegram do it. The record comes first and
// is unique per update and action, so an update Telegram delivers twice is
// acted on once. When Telegram refuses, the record is dropped, so that the
// action may be tried again.
func (m *Moderation) carryOut(ctx context.Context, o order) error {
	first, err := m.opts.Store.RecordModeration(ctx, o.audit())
	if err != nil {
		return err
	}
	if !first {
		return nil
	}
	if err := m.send(ctx, o); err != nil {
		return m.failed(ctx, o, err)
	}
	return nil
}

// failed says why o was not carried out, and drops its record when
// Telegram refused it. Without an answer, or with a server's error instead,
// Telegram may have acted, so the record stays: the audit keeps what may
// have been done, a deleted message's text with it, the pace counts it, and
// the same update cannot act twice. A record that cannot be dropped is
// reported too: the audit then holds an action that was never taken, and
// counts it.
func (m *Moderation) failed(ctx context.Context, o order, sendErr error) error {
	failure := telegramFailure(sendErr)
	if _, refused := telegramRefusal(sendErr); !refused {
		return failure
	}
	if err := m.opts.Store.ForgetModeration(context.WithoutCancel(ctx), o.caller.UpdateID, o.action); err != nil {
		return errors.Join(failure, err)
	}
	return failure
}

// audit is the record of o. The text of a deleted message is kept, since
// the chat no longer has it; other actions keep no text.
func (o order) audit() storage.ModerationAction {
	a := storage.ModerationAction{
		ChatID: o.caller.ChatID, Action: o.action, RequesterID: o.caller.UserID,
		TargetUserID: o.target.UserID, TargetMessageID: o.target.MessageID,
		Minutes: o.minutes, Reason: o.reason, UpdateID: o.caller.UpdateID,
	}
	if o.action == actionDelete {
		a.TargetText = o.target.Text
	}
	return a
}

func (m *Moderation) send(ctx context.Context, o order) error {
	chatID, userID, messageID := o.caller.ChatID, o.target.UserID, o.target.MessageID
	switch o.action {
	case actionDelete:
		return m.opts.Telegram.DeleteMessage(ctx, chatID, messageID)
	case actionMute:
		return m.opts.Telegram.RestrictChatMember(ctx, telegram.Restriction{ChatID: chatID, UserID: userID, Permissions: telegram.NoPermissions(), Until: o.until})
	case actionUnmute:
		return m.opts.Telegram.RestrictChatMember(ctx, telegram.Restriction{ChatID: chatID, UserID: userID, Permissions: telegram.AllPermissions()})
	case actionBan:
		return m.opts.Telegram.BanChatMember(ctx, chatID, userID)
	case actionUnban:
		return m.opts.Telegram.UnbanChatMember(ctx, chatID, userID)
	case actionPin:
		return m.opts.Telegram.PinChatMessage(ctx, chatID, messageID)
	case actionUnpin:
		return m.opts.Telegram.UnpinChatMessage(ctx, chatID, messageID)
	}
	return fmt.Errorf("moderation: unknown action %q", o.action)
}

// telegramError is why Telegram did not act, in words safe for the model
// and, in Uzbek and Russian, for the chat.
type telegramError struct {
	message string
	chat    chatText
}

func (e *telegramError) Error() string { return e.message }

func isTelegramFailure(err error) bool {
	var failure *telegramError
	return errors.As(err, &failure)
}

// telegramFailure tells why nothing was done, in Telegram's words when it
// gave a reason.
func telegramFailure(err error) error {
	if refusal, ok := telegramRefusal(err); ok {
		reason := refusal.Description
		return &telegramError{"Telegram refused: " + reason, chatText{"Telegram rad etdi: " + reason, "Telegram отказал: " + reason}}
	}
	return &telegramError{noAnswer, noAnswerWords}
}

// telegramRefusal is Telegram's answer when it turned a request down, and
// so surely did not act. A server error, such as a proxy's 502 page, is no
// refusal: Telegram may have acted before it.
func telegramRefusal(err error) (*telegram.APIError, bool) {
	var apiErr *telegram.APIError
	if errors.As(err, &apiErr) && apiErr.Code < http.StatusInternalServerError {
		return apiErr, true
	}
	return nil, false
}

// doneText says, for the chat, what o did.
func (m *Moderation) doneText(o order) string {
	return o.done(m.untilText(o.until)).String()
}

// done says what o did, given when a mute ends.
func (o order) done(until string) chatText {
	name := o.target.Name
	switch o.action {
	case actionDelete:
		return chatText{"Bajarildi: " + name + " xabari o'chirildi.", "Готово: сообщение от " + name + " удалено."}
	case actionMute:
		return chatText{"Bajarildi: " + name + " " + until + " gacha yoza olmaydi.", "Готово: " + name + " не может писать до " + until + "."}
	case actionUnmute:
		return chatText{"Bajarildi: " + name + " yana yoza oladi.", "Готово: " + name + " снова может писать."}
	case actionBan:
		return chatText{"Bajarildi: " + name + " bloklandi.", "Готово: " + name + " забанен(а)."}
	case actionUnban:
		return chatText{"Bajarildi: " + name + " blokdan chiqarildi va qayta qo'shila oladi.", "Готово: " + name + " разбанен(а) и может вернуться."}
	case actionPin:
		return chatText{"Bajarildi: " + name + " xabari qadaldi.", "Готово: сообщение от " + name + " закреплено."}
	}
	return chatText{"Bajarildi: " + name + " xabari qadashdan olindi.", "Готово: сообщение от " + name + " откреплено."}
}

// untilText is when a mute ends: the time, and the date too when it is not
// today.
func (m *Moderation) untilText(until time.Time) string {
	local, now := until.In(m.opts.Location), m.opts.Now().In(m.opts.Location)
	if local.Format(time.DateOnly) == now.Format(time.DateOnly) {
		return local.Format(clockLayout)
	}
	return local.Format(dateClockLayout)
}

// The answers to the bot's question, checked in code. Only the whole message
// counts: "ha, but not him" is no yes.
var (
	yesWords = []string{
		"yes", "y", "yep", "yeah", "sure", "ok", "okay",
		"ha", "xa", "haa", "ҳа", "ха", "албатта", "albatta", "mayli", "майли",
		"да", "da", "ага", "конечно",
		"+", "👍", "✅", "👌",
	}
	noWords = []string{
		"no", "n", "nope", "cancel",
		"yo'q", "yoq", "yo‘q", "yo’q", "yo`q", "йўқ", "йук", "kerakmas", "kerak emas",
		"нет", "net", "не", "не надо", "отмена",
		"-", "👎", "❌",
	}
)

// ConfirmationAnswer reads a reply to the bot's question about an action:
// yes, no, or, when ok is false, neither.
func ConfirmationAnswer(text string) (yes, ok bool) {
	answer := plainAnswer(text)
	switch {
	case slices.Contains(yesWords, answer):
		return true, true
	case slices.Contains(noWords, answer):
		return false, true
	}
	return false, false
}

// plainAnswer is text in lower case without what does not change its
// meaning: spaces, end punctuation, a smiley's brackets, and the invisible
// variation selector and skin tones of an emoji.
func plainAnswer(text string) string {
	text = strings.Map(func(char rune) rune {
		if char == '️' || (char >= 0x1F3FB && char <= 0x1F3FF) {
			return -1
		}
		return char
	}, strings.ToLower(text))
	return strings.TrimFunc(text, func(char rune) bool {
		return unicode.IsSpace(char) || strings.ContainsRune(".!,)", char)
	})
}
