package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// The moderate tool's actions. All but confirm_ban and cancel act on the
// message the person asking replied to, or on its author.
const (
	actionDelete     = "delete"
	actionMute       = "mute"
	actionUnmute     = "unmute"
	actionBan        = "ban"
	actionConfirmBan = "confirm_ban"
	actionUnban      = "unban"
	actionPin        = "pin"
	actionUnpin      = "unpin"
	actionCancel     = "cancel"
)

const (
	moderationTimeout = 20 * time.Second
	// A mute lasts an hour unless the person asking says otherwise, and a
	// week at most.
	defaultMuteMinutes = 60
	maxMuteMinutes     = 7 * 24 * 60
	// pendingBanMaxAge is how long a ban waits for the person who asked to
	// confirm it.
	pendingBanMaxAge = 5 * time.Minute
	// One person may have the bot act maxActions times per actionWindow, so
	// that a careless or stolen admin account cannot empty a group through
	// it.
	maxActions   = 10
	actionWindow = 10 * time.Minute
	// maxReasonRunes keeps the audit's reason short.
	maxReasonRunes = 200
	clockLayout    = "15:04"
)

// Refusals, in words the model can pass on.
const (
	notInAGroup      = "only a person in a group can ask for that"
	noReplyTarget    = "reply to the message of the member you mean"
	notAllowed       = "only this group's admins with that right, or my owners, can ask for that"
	immune           = "I can't do that to admins, my owners, myself, Telegram's service accounts or the person asking"
	nothingToConfirm = "there is no ban to confirm: the same person must ask for it, then confirm in a later message within 5 minutes"
	pendingNote      = "Nothing is done yet. Ask the person who asked to confirm in their next message, then call confirm_ban."
	banDropped       = "the ban no longer waits for confirmation, so it must be asked for again"
	handledNote      = "This message was delivered twice and the first delivery already asked for this, so nothing more was done now. Don't say whether it worked."
	noAnswer         = "Telegram did not answer, so it may not have been done; try again later"
)

// errLookup reports that Telegram could not say who may do what. Moderation
// then does nothing: it never acts on a guess.
var errLookup = errors.New("could not ask Telegram who may do this, so nothing was done; try again later")

// neededRight is the admin right an action needs, from the person asking
// and from the bot. A confirmed ban needs that of ban; cancel needs none.
var neededRight = map[string]string{
	actionDelete: telegram.RightDeleteMessages,
	actionMute:   telegram.RightRestrictMembers,
	actionUnmute: telegram.RightRestrictMembers,
	actionBan:    telegram.RightRestrictMembers,
	actionUnban:  telegram.RightRestrictMembers,
	actionPin:    telegram.RightPinMessages,
	actionUnpin:  telegram.RightPinMessages,
}

// rightWords names the rights moderation needs, for refusals.
var rightWords = map[string]string{
	telegram.RightDeleteMessages:  "delete messages",
	telegram.RightRestrictMembers: "ban and mute members",
	telegram.RightPinMessages:     "pin messages",
}

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

// ModerationStore keeps the audit of what the bot did for whom, and the bans
// that wait for confirmation.
type ModerationStore interface {
	RecordModeration(ctx context.Context, a storage.ModerationAction) (bool, error)
	ForgetModeration(ctx context.Context, updateID int64, action string) error
	ModerationRecorded(ctx context.Context, updateID int64, action string) (bool, error)
	ModerationBy(ctx context.Context, requesterID int64, since time.Time) (int, error)
	SavePendingBan(ctx context.Context, b storage.PendingBan) error
	TakePendingBan(ctx context.Context, requesterID, chatID, updateID int64, maxAge time.Duration) (storage.PendingBan, error)
	CancelPendingBan(ctx context.Context, requesterID, chatID int64) (bool, error)
}

// RightsRecorder stores what Telegram said about the bot in a group.
type RightsRecorder interface {
	SetBotRights(ctx context.Context, chatID int64, status string, rights []string) error
}

// ModerationOptions configures the moderate tool. Every field but Location
// and Now is required.
type ModerationOptions struct {
	// BotID is the bot's own user ID.
	BotID int64
	// Members tells who holds which rights, asked anew for every action.
	Members  MemberLookup
	Telegram Moderator
	Store    ModerationStore
	// Rights keeps the bot's rights learned along the way, for the turn
	// header.
	Rights RightsRecorder
	// Location is the time zone of the times the tool reports.
	Location *time.Location
	Now      func() time.Time
}

type moderationArgs struct {
	Action  string `json:"action"`
	Minutes int    `json:"minutes"`
	Reason  string `json:"reason"`
}

// ModerationTool lets a group's admins, and the bot's owners, have the bot
// delete, mute, ban or pin there. The model only picks the action: whom it
// acts on is the message the person asking replied to, taken from the
// update, and every permission is checked here against Telegram, never
// taken from the model or the chat. A ban waits until the person who asked
// confirms it in a later message.
func ModerationTool(opts ModerationOptions) Tool {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	m := moderation{opts}
	return Tool{
		Name: "moderate",
		Description: "Only when a group admin or one of your owners asks: act on the message they replied to. " +
			"delete: delete that message. mute (minutes, default 60) or unmute: its author. unban: its author. pin or unpin: that message. " +
			"ban: its author, once confirmed; the ban call itself does nothing yet, so ask them to confirm, and call confirm_ban only after they say yes in their next message. cancel: drop that ban. " +
			"The tool checks everyone's rights and refuses with a reason. Never say something was done unless it reported done.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["delete", "mute", "unmute", "ban", "confirm_ban", "unban", "pin", "unpin", "cancel"]},
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

type moderation struct {
	ModerationOptions
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

func (m moderation) run(ctx context.Context, raw json.RawMessage) (any, error) {
	var args moderationArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, err := groupRequester(ctx)
	if err != nil {
		return nil, err
	}
	switch args.Action {
	case actionCancel:
		cancelled, err := m.Store.CancelPendingBan(ctx, caller.UserID, caller.ChatID)
		if err != nil {
			return nil, err
		}
		return map[string]bool{"cancelled": cancelled}, nil
	case actionConfirmBan:
		return m.confirmBan(ctx, caller, args)
	}
	if _, known := neededRight[args.Action]; !known {
		return nil, &ArgumentError{Message: "action must be delete, mute, unmute, ban, confirm_ban, unban, pin, unpin or cancel"}
	}
	if caller.Target == nil {
		return nil, &DeniedError{Message: noReplyTarget}
	}
	o := m.newOrder(caller, args.Action, *caller.Target, args)
	if err := m.check(ctx, o); err != nil {
		return nil, err
	}
	if o.action == actionBan {
		return m.askToConfirm(ctx, o)
	}
	return m.carryOut(ctx, o)
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

func (m moderation) newOrder(caller Caller, action string, target Target, args moderationArgs) order {
	o := order{caller: caller, action: action, target: target, reason: cutRunes(strings.TrimSpace(args.Reason), maxReasonRunes)}
	if action == actionMute {
		o.minutes = muteMinutes(args.Minutes)
		o.until = m.Now().Add(time.Duration(o.minutes) * time.Minute)
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
func (m moderation) check(ctx context.Context, o order) error {
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

// checkRequester lets an owner ask in any group, and anyone else only when
// Telegram says they hold the right themselves.
func (m moderation) checkRequester(ctx context.Context, caller Caller, right string) error {
	if caller.Owner {
		return nil
	}
	member, err := m.Members.GetChatMember(ctx, caller.ChatID, caller.UserID)
	if err != nil {
		return errLookup
	}
	if !member.Has(right) {
		return &DeniedError{Message: notAllowed}
	}
	return nil
}

// checkBot asks Telegram whether the bot holds the right, and keeps the
// answer, which also corrects the rights the turn header shows.
func (m moderation) checkBot(ctx context.Context, chatID int64, right string) error {
	me, err := m.Members.GetChatMember(ctx, chatID, m.BotID)
	if err != nil {
		return errLookup
	}
	// Keeping it only saves asking again; the answer holds either way.
	_ = m.Rights.SetBotRights(ctx, chatID, me.Status, me.Rights())
	if !me.Has(right) {
		return &DeniedError{Message: fmt.Sprintf("I don't have the right to %s here", rightWords[right])}
	}
	return nil
}

// checkTarget keeps the bot off those it must not act on: Telegram's own
// accounts, the bot's owners and the group's admins. The bot and the person
// asking may have their own messages deleted, pinned or unpinned, but never
// mute or ban themselves. A confirmed ban's target is not known to be an
// owner or not; that was checked when the ban was asked for.
func (m moderation) checkTarget(ctx context.Context, o order) error {
	userID := o.target.UserID
	if userID == m.BotID || userID == o.caller.UserID {
		return ownMessageOnly(o.action)
	}
	if isTelegramAccount(userID) || o.target.Owner {
		return &DeniedError{Message: immune}
	}
	member, err := m.Members.GetChatMember(ctx, o.caller.ChatID, userID)
	if err != nil {
		return errLookup
	}
	if member.IsAdmin() {
		return &DeniedError{Message: immune}
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
	return &DeniedError{Message: immune}
}

// checkPace allows maxActions per actionWindow per person, counted in the
// audit, in every group together.
func (m moderation) checkPace(ctx context.Context, caller Caller) error {
	done, err := m.Store.ModerationBy(ctx, caller.UserID, m.Now().Add(-actionWindow))
	if err != nil {
		return err
	}
	if done >= maxActions {
		return &DeniedError{Message: fmt.Sprintf("that's %d actions in %d minutes; wait a bit", maxActions, int(actionWindow.Minutes()))}
	}
	return nil
}

// askToConfirm keeps a ban until the person who asked confirms it.
func (m moderation) askToConfirm(ctx context.Context, o order) (any, error) {
	err := m.Store.SavePendingBan(ctx, storage.PendingBan{
		RequesterID: o.caller.UserID, ChatID: o.caller.ChatID,
		TargetUserID: o.target.UserID, TargetName: o.target.Name, UpdateID: o.caller.UpdateID,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"pending": true, "target": o.target.Name, "note": pendingNote}, nil
}

// confirmBan carries out the ban the person asked for in an earlier message.
// Everything is checked again: rights may have changed while it waited.
func (m moderation) confirmBan(ctx context.Context, caller Caller, args moderationArgs) (any, error) {
	pending, err := m.Store.TakePendingBan(ctx, caller.UserID, caller.ChatID, caller.UpdateID, pendingBanMaxAge)
	if errors.Is(err, storage.ErrNoPendingBan) {
		return m.noBanWaiting(ctx, caller)
	}
	if err != nil {
		return nil, err
	}
	o := m.newOrder(caller, actionBan, Target{UserID: pending.TargetUserID, Name: pending.TargetName}, args)
	if err := m.check(ctx, o); err != nil {
		return nil, withBanDropped(err)
	}
	result, err := m.carryOut(ctx, o)
	if err != nil {
		return nil, withBanDropped(err)
	}
	return result, nil
}

// noBanWaiting refuses a confirmation that finds no ban waiting, unless this
// update has already confirmed one: Telegram delivered it again.
func (m moderation) noBanWaiting(ctx context.Context, caller Caller) (any, error) {
	recorded, err := m.Store.ModerationRecorded(ctx, caller.UpdateID, actionBan)
	if err != nil {
		return nil, err
	}
	if recorded {
		return alreadyHandled(actionBan), nil
	}
	return nil, &DeniedError{Message: nothingToConfirm}
}

// withBanDropped adds to why a confirmed ban was not carried out that a
// later confirmation will not find it, so that nobody is told to simply try
// again. The kind of err is kept.
func withBanDropped(err error) error {
	return fmt.Errorf("%w; %s", err, banDropped)
}

// carryOut records o, then has Telegram do it. The record comes first and
// is unique per update and action, so an update Telegram delivers twice is
// acted on once. When Telegram does not act, the record is dropped, so that
// the action may be tried again.
func (m moderation) carryOut(ctx context.Context, o order) (any, error) {
	first, err := m.Store.RecordModeration(ctx, o.audit())
	if err != nil {
		return nil, err
	}
	if !first {
		return alreadyHandled(o.action), nil
	}
	if err := m.send(ctx, o); err != nil {
		return nil, m.failed(ctx, o, err)
	}
	return m.done(o), nil
}

// alreadyHandled answers an update Telegram delivered again. The first
// delivery recorded the action and did it, or is still doing it and may yet
// fail, so this one neither acts nor says it was done.
func alreadyHandled(action string) map[string]any {
	return map[string]any{"already_handled": true, "action": action, "note": handledNote}
}

// failed drops the record of o, which Telegram did not take, and says why.
// A record that cannot be dropped is reported too: the audit then holds an
// action that was never taken, and counts it.
func (m moderation) failed(ctx context.Context, o order, sendErr error) error {
	failure := telegramFailure(sendErr)
	if err := m.Store.ForgetModeration(context.WithoutCancel(ctx), o.caller.UpdateID, o.action); err != nil {
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

func (m moderation) send(ctx context.Context, o order) error {
	chatID, userID, messageID := o.caller.ChatID, o.target.UserID, o.target.MessageID
	switch o.action {
	case actionDelete:
		return m.Telegram.DeleteMessage(ctx, chatID, messageID)
	case actionMute:
		return m.Telegram.RestrictChatMember(ctx, telegram.Restriction{ChatID: chatID, UserID: userID, Permissions: telegram.NoPermissions(), Until: o.until})
	case actionUnmute:
		return m.Telegram.RestrictChatMember(ctx, telegram.Restriction{ChatID: chatID, UserID: userID, Permissions: telegram.AllPermissions()})
	case actionBan:
		return m.Telegram.BanChatMember(ctx, chatID, userID)
	case actionUnban:
		return m.Telegram.UnbanChatMember(ctx, chatID, userID)
	case actionPin:
		return m.Telegram.PinChatMessage(ctx, chatID, messageID)
	case actionUnpin:
		return m.Telegram.UnpinChatMessage(ctx, chatID, messageID)
	}
	return fmt.Errorf("moderation: unknown action %q", o.action)
}

// telegramFailure tells the model why nothing was done, in Telegram's words
// when it gave a reason.
func telegramFailure(err error) error {
	if refusal, ok := telegramRefusal(err); ok {
		return fmt.Errorf("Telegram refused: %s", refusal.Description)
	}
	return errors.New(noAnswer)
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

func (m moderation) done(o order) map[string]any {
	result := map[string]any{"done": true, "action": o.action, "target": o.target.Name}
	if o.action == actionMute {
		result["until"] = m.untilText(o.until)
	}
	return result
}

// untilText is when a mute ends: the time, and the date too when it is not
// today.
func (m moderation) untilText(until time.Time) string {
	local, now := until.In(m.Location), m.Now().In(m.Location)
	if local.Format(time.DateOnly) == now.Format(time.DateOnly) {
		return local.Format(clockLayout)
	}
	return local.Format(ruleTimeLayout)
}
