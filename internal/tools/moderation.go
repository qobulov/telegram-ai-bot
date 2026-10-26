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

// The moderate tool's actions. Each acts on the message the person asking
// replied to, or on its author.
const (
	actionDelete = "delete"
	actionMute   = "mute"
	actionUnmute = "unmute"
	actionBan    = "ban"
	actionUnban  = "unban"
	actionPin    = "pin"
	actionUnpin  = "unpin"
	// actionNoStickers takes away a member's stickers and GIFs, which
	// Telegram allows or forbids together; actionStickers gives them back.
	actionNoStickers = "no_stickers"
	actionStickers   = "allow_stickers"
	// actionGroupNoStickers takes stickers and GIFs away from the whole
	// group, keeping its other permissions; actionGroupStickers gives them
	// back.
	actionGroupNoStickers = "group_no_stickers"
	actionGroupStickers   = "group_allow_stickers"
	// actionNoPhotos and the rest do the same for photos.
	actionNoPhotos      = "no_photos"
	actionPhotos        = "allow_photos"
	actionGroupNoPhotos = "group_no_photos"
	actionGroupPhotos   = "group_allow_photos"
)

const (
	moderationTimeout = 20 * time.Second
	// A mute lasts an hour unless the person asking says otherwise, and a
	// week at most.
	defaultMuteMinutes = 60
	maxMuteMinutes     = 7 * 24 * 60
	// Stickers are taken away for a day unless the person asking says
	// otherwise, and for a year at most: Telegram counts a longer
	// restriction as forever.
	defaultNoStickersMinutes = 24 * 60
	maxNoStickersMinutes     = 365 * 24 * 60
	// One person may have the bot act maxActions times per actionWindow, so
	// that a careless or stolen admin account cannot empty a group through
	// it.
	maxActions   = 10
	actionWindow = 10 * time.Minute
	// maxReasonRunes keeps the audit's reason short.
	maxReasonRunes  = 200
	clockLayout     = "15:04"
	dateClockLayout = "02.01 15:04"
	yearClockLayout = "02.01.2006 15:04"
)

// Refusals, in words the model can be given.
const (
	notInAGroup   = "only a person in a group can ask for that"
	noReplyTarget = "reply to the message of the member you mean, or tag that one member with @ in the request"
	// Tags that do not name one member Telegram vouches for act on nobody.
	manyMentions   = "more than one member is tagged; reply to the message of the one you mean, or tag only that member"
	unknownMention = "I have not seen that @username in this group, so I can't tell who it is; reply to one of their own messages instead"
	unsureMention  = "Telegram did not confirm that the tagged account is a member here under that name, so nothing was done; reply to one of their own messages instead"
	immune         = "I can't do that to admins, my owners, myself, Telegram's service accounts or the person asking"
	notAsked       = "their own message does not ask for this, so nothing was done; do it only when they ask for it in words, such as \"mute qil\" or \"o'chir\""
	noAnswer       = "Telegram did not answer, so it may not have been done; try again later"
)

// chatText is something the bot says to a group in its own words, never the
// model's. Only the Uzbek is posted, as the owner asked for one language; the
// Russian is kept for when a group needs it.
type chatText struct{ uz, ru string }

func (t chatText) String() string { return t.uz }

// errLookup reports that Telegram could not say who may do what. Moderation
// then does nothing: it never acts on a guess.
var errLookup = errors.New("could not ask Telegram who may do this, so nothing was done; try again later")

// neededRight is the admin right an action needs, from the person asking
// and from the bot.
var neededRight = map[string]string{
	actionDelete: telegram.RightDeleteMessages,
	actionMute:   telegram.RightRestrictMembers,
	actionUnmute: telegram.RightRestrictMembers,
	actionBan:    telegram.RightRestrictMembers,
	actionUnban:  telegram.RightRestrictMembers,
	actionPin:    telegram.RightPinMessages,
	actionUnpin:  telegram.RightPinMessages,

	actionNoStickers: telegram.RightRestrictMembers,
	actionStickers:   telegram.RightRestrictMembers,

	actionGroupNoStickers: telegram.RightRestrictMembers,
	actionGroupStickers:   telegram.RightRestrictMembers,
	actionNoPhotos:        telegram.RightRestrictMembers,
	actionPhotos:          telegram.RightRestrictMembers,
	actionGroupNoPhotos:   telegram.RightRestrictMembers,
	actionGroupPhotos:     telegram.RightRestrictMembers,
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

// UsernameLookup finds the member a group knows by an @username, from the
// bot's member list or the day's log; storage.ErrUnknownAuthor when it knows
// nobody by it. The Bot API cannot
// turn a username into a member, so a tag is only as good as this, checked
// against Telegram.
type UsernameLookup interface {
	AuthorByUsername(ctx context.Context, chatID int64, username string) (int64, error)
}

// Moderator has Telegram act on a group's messages and members.
type Moderator interface {
	DeleteMessage(ctx context.Context, chatID, messageID int64) error
	RestrictChatMember(ctx context.Context, r telegram.Restriction) error
	BanChatMember(ctx context.Context, chatID, userID int64) error
	UnbanChatMember(ctx context.Context, chatID, userID int64) error
	PinChatMessage(ctx context.Context, chatID, messageID int64) error
	UnpinChatMessage(ctx context.Context, chatID, messageID int64) error
	GetChat(ctx context.Context, chatID int64) (telegram.Chat, error)
	SetChatPermissions(ctx context.Context, chatID int64, permissions telegram.ChatPermissions) error
}

// ModerationStore keeps the audit of what the bot did for whom.
type ModerationStore interface {
	RecordModeration(ctx context.Context, a storage.ModerationAction) (bool, error)
	ForgetModeration(ctx context.Context, updateID int64, action string) error
	ModerationBy(ctx context.Context, requesterID int64, since time.Time) (int, error)
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
	Store    ModerationStore
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
	// Owners are the owners' IDs, to spare a tagged owner. Usernames finds
	// tagged members; without it only tags that carry an ID work.
	Owners    map[int64]bool
	Usernames UsernameLookup
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
// reads the whole chat, and a line in it can talk it into anything. So the
// action is done only when the asker's own message asks for it in words, as
// askedFor checks; it is then done at once, without a question to confirm,
// as the owners want.
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
			"no_stickers (minutes, default a day) or allow_stickers: take away or give back its author's stickers and GIFs, which Telegram only allows or forbids together; use it, not ban or ignore, when they ask to block someone's stickers. " +
			"group_no_stickers or group_allow_stickers: take away or give back everyone's stickers and GIFs in the group, keeping its other permissions, when they ask it for all; it needs no reply or tag. " +
			"no_photos (minutes, default a day) or allow_photos, and group_no_photos or group_allow_photos: the same for photos. A member's other limits stay as they are. " +
			"For mute, unmute, ban, unban, no_stickers and allow_stickers without such a reply, or replying to their own message, it acts on the one member they tagged with @, once Telegram confirms who that is. " +
			"It checks everyone's rights and does it at once, with no question, but only when the person's own message asks for it; say it is done only when it reports done.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["delete", "mute", "unmute", "ban", "unban", "pin", "unpin", "no_stickers", "allow_stickers", "group_no_stickers", "group_allow_stickers", "no_photos", "allow_photos", "group_no_photos", "group_allow_photos"]},
				"minutes": {"type": "integer", "description": "For mute: how long, 1 to 10080, default 60. For no_stickers and no_photos: 1 to 525600, default 1440; the most for \"forever\"."},
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
	if _, known := neededRight[args.Action]; !known {
		return nil, &ArgumentError{Message: "action must be delete, mute, unmute, ban, unban, pin, unpin, no_stickers, allow_stickers, group_no_stickers, group_allow_stickers, no_photos, allow_photos, group_no_photos or group_allow_photos"}
	}
	if !askedFor(caller.Words, args.Action) {
		return nil, &DeniedError{Message: notAsked}
	}
	target, err := m.targetOf(ctx, caller, args.Action)
	if err != nil {
		return nil, err
	}
	o := m.newOrder(caller, args.Action, target, args.Minutes, args.Reason)
	if err := m.check(ctx, o); err != nil {
		return nil, err
	}
	// The owners want it done at once, with no question to confirm: the
	// rights were just checked, and the target came from the update.
	acted, err := m.carryOut(ctx, o)
	if err != nil {
		return nil, notCarriedOut(err)
	}
	if !acted {
		return map[string]any{"done": true, "note": alreadyDoneNote}, nil
	}
	return map[string]any{"done": true, "said": m.doneText(o), "note": doneNote}, nil
}

// doneNote tells the model how to report an action the tool carried out;
// alreadyDoneNote, one this message already had done.
const (
	doneNote        = "Done. Tell them in one short line, in their language, like the line in said; do not ask anything."
	alreadyDoneNote = "This was already done for this message; do not say it again: reply with exactly [silent] unless something else needs an answer."
)

// notCarriedOut is why Telegram did not do it, for the model to tell them.
// Anything else stays hidden as the tool's failure, and so reaches the logs,
// a record that could not be dropped after Telegram refused among them.
func notCarriedOut(err error) error {
	if failure, ok := err.(*telegramError); ok {
		return &DeniedError{Message: failure.message}
	}
	return internal(err)
}

// targetOf is what an order acts on. A reply to someone else's message
// names that message and its author. Without a reply, or with a reply to
// their own message, which is how a member who tagged someone often asks,
// an action on a member names the one member they tagged, once Telegram
// confirms that account. Anything less certain acts on nobody: the person
// is asked to reply to the member's message instead.
func (m *Moderation) targetOf(ctx context.Context, caller Caller, action string) (Target, error) {
	if groupWide(action) {
		return Target{}, nil
	}
	replied := caller.Target
	ownReply := replied != nil && replied.UserID == caller.UserID
	switch {
	case replied != nil && (!ownReply || !onMember(action) || len(caller.Mentions) == 0):
		// Acting on oneself is refused when the order is checked.
		return *replied, nil
	case !onMember(action) || len(caller.Mentions) == 0:
		return Target{}, &DeniedError{Message: noReplyTarget}
	case len(caller.Mentions) > 1:
		return Target{}, &DeniedError{Message: manyMentions}
	}
	return m.tagged(ctx, caller.ChatID, caller.Mentions[0], action)
}

// tagged is the member a tag names, as Telegram knows them now: the ID a
// username was last seen with must still have that username, and be in the
// group (or, to be unbanned, have been).
func (m *Moderation) tagged(ctx context.Context, chatID int64, mention Mention, action string) (Target, error) {
	userID := mention.UserID
	if userID == 0 {
		if m.opts.Usernames == nil {
			return Target{}, &DeniedError{Message: unknownMention}
		}
		id, err := m.opts.Usernames.AuthorByUsername(ctx, chatID, mention.Username)
		if errors.Is(err, storage.ErrUnknownAuthor) {
			return Target{}, &DeniedError{Message: unknownMention}
		}
		if err != nil {
			return Target{}, internal(err)
		}
		userID = id
	}
	member, err := m.opts.Members.GetChatMember(ctx, chatID, userID)
	if err != nil {
		return Target{}, errLookup
	}
	user := member.User
	renamed := mention.Username != "" && (user == nil || !strings.EqualFold(user.Username, mention.Username))
	if user == nil || user.ID != userID || renamed || !present(member.Status, action) {
		return Target{}, &DeniedError{Message: unsureMention}
	}
	name := user.FirstName
	if user.Username != "" {
		name = strings.TrimSpace(name + " @" + user.Username)
	}
	return Target{UserID: userID, Name: name, IsBot: user.IsBot, Owner: m.opts.Owners[userID]}, nil
}

// onMember reports an action on a member rather than on a message.
func onMember(action string) bool {
	switch action {
	case actionMute, actionUnmute, actionBan, actionUnban, actionNoStickers, actionStickers, actionNoPhotos, actionPhotos:
		return true
	}
	return false
}

// groupWide reports an action on the whole group rather than on a member
// or a message.
func groupWide(action string) bool {
	switch action {
	case actionGroupNoStickers, actionGroupStickers, actionGroupNoPhotos, actionGroupPhotos:
		return true
	}
	return false
}

// present reports a member status an action may be taken on: someone in
// the group, or for unban someone who was in it.
func present(status, action string) bool {
	switch status {
	case "creator", "administrator", "member", "restricted":
		return true
	case "kicked", "left":
		return action == actionUnban
	}
	return false
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

// newOrder is caller's request. A mute's end is counted from now, when the
// order is carried out.
func (m *Moderation) newOrder(caller Caller, action string, target Target, minutes int, reason string) order {
	o := order{caller: caller, action: action, target: target, reason: cutRunes(strings.TrimSpace(reason), maxReasonRunes)}
	switch action {
	case actionMute:
		o.minutes = muteMinutes(minutes)
	case actionNoStickers, actionNoPhotos:
		o.minutes = clampMinutes(minutes, defaultNoStickersMinutes, maxNoStickersMinutes)
	}
	if o.minutes > 0 {
		o.until = m.opts.Now().Add(time.Duration(o.minutes) * time.Minute)
	}
	return o
}

// muteMinutes is how long a mute lasts: an hour when not given, otherwise
// a minute to a week.
func muteMinutes(minutes int) int {
	return clampMinutes(minutes, defaultMuteMinutes, maxMuteMinutes)
}

// clampMinutes is minutes, or byDefault when not given, kept between a
// minute and most.
func clampMinutes(minutes, byDefault, most int) int {
	if minutes == 0 {
		return byDefault
	}
	return min(max(minutes, 1), most)
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
	if !groupWide(o.action) {
		if err := m.checkTarget(ctx, o); err != nil {
			return err
		}
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
		return &DeniedError{Message: fmt.Sprintf("only admins of this group who have the right to %s, or my owners, can ask me to do that; the person asking is neither", rightWords[right])}
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
		return &DeniedError{Message: fmt.Sprintf("I don't have the right to %s in this group; its admins can give it to me", rightWords[right])}
	}
	return nil
}

// checkTarget keeps the bot off those it must not act on: Telegram's own
// accounts, the bot's owners known by ID and the group's admins. The bot and the person
// asking may have their own messages deleted, pinned or unpinned, but never
// mute or ban themselves.
func (m *Moderation) checkTarget(ctx context.Context, o order) error {
	userID := o.target.UserID
	if userID == m.opts.BotID || userID == o.caller.UserID {
		return ownMessageOnly(o.action)
	}
	if isTelegramAccount(userID) || (o.target.Owner && m.opts.OwnersByID) {
		return &DeniedError{Message: immune}
	}
	member, err := m.opts.Members.GetChatMember(ctx, o.caller.ChatID, userID)
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
func (m *Moderation) checkPace(ctx context.Context, caller Caller) error {
	done, err := m.opts.Store.ModerationBy(ctx, caller.UserID, m.opts.Now().Add(-actionWindow))
	if err != nil {
		return internal(err)
	}
	if done >= maxActions {
		return &DeniedError{Message: fmt.Sprintf("that's %d actions in %d minutes; wait a bit", maxActions, int(actionWindow.Minutes()))}
	}
	return nil
}

// carryOut records o, then has Telegram do it, and reports whether it did
// now. The record comes first and is unique per update and action, so an
// update Telegram delivers twice, or a second call in the same turn, acts
// once. When Telegram refuses, the record is dropped, so that the action may
// be tried again.
func (m *Moderation) carryOut(ctx context.Context, o order) (bool, error) {
	first, err := m.opts.Store.RecordModeration(ctx, o.audit())
	if err != nil || !first {
		return false, err
	}
	if err := m.send(ctx, o); err != nil {
		return false, m.failed(ctx, o, err)
	}
	return true, nil
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
	case actionNoStickers, actionStickers, actionNoPhotos, actionPhotos:
		return m.setMemberMedia(ctx, o)
	case actionBan:
		return m.opts.Telegram.BanChatMember(ctx, chatID, userID)
	case actionUnban:
		return m.opts.Telegram.UnbanChatMember(ctx, chatID, userID)
	case actionPin:
		return m.opts.Telegram.PinChatMessage(ctx, chatID, messageID)
	case actionUnpin:
		return m.opts.Telegram.UnpinChatMessage(ctx, chatID, messageID)
	case actionGroupNoStickers, actionGroupStickers, actionGroupNoPhotos, actionGroupPhotos:
		return m.setGroupMedia(ctx, chatID, o.action)
	}
	return fmt.Errorf("moderation: unknown action %q", o.action)
}

// setMemberMedia takes away or gives back a member's stickers or photos and
// nothing else: a member already muted or without stickers stays so. Given
// back, the rest of their restriction keeps its end.
func (m *Moderation) setMemberMedia(ctx context.Context, o order) error {
	member, err := m.opts.Members.GetChatMember(ctx, o.caller.ChatID, o.target.UserID)
	if err != nil {
		return err
	}
	permissions := member.Permissions()
	allowMedia(&permissions, o.action)
	until := o.until
	if o.action == actionStickers || o.action == actionPhotos {
		until = time.Time{}
		if member.UntilDate > 0 {
			until = time.Unix(member.UntilDate, 0)
		}
	}
	return m.opts.Telegram.RestrictChatMember(ctx, telegram.Restriction{ChatID: o.caller.ChatID, UserID: o.target.UserID, Permissions: permissions, Until: until})
}

// setGroupMedia allows or forbids stickers or photos for the whole group,
// keeping whatever else its members may or may not do.
func (m *Moderation) setGroupMedia(ctx context.Context, chatID int64, action string) error {
	chat, err := m.opts.Telegram.GetChat(ctx, chatID)
	if err != nil {
		return err
	}
	permissions := telegram.AllPermissions()
	if chat.Permissions != nil {
		permissions = *chat.Permissions
	}
	allowMedia(&permissions, action)
	return m.opts.Telegram.SetChatPermissions(ctx, chatID, permissions)
}

// allowMedia sets the one permission a sticker or photo action is about:
// stickers and GIFs, which Telegram allows or forbids together, or photos.
func allowMedia(p *telegram.ChatPermissions, action string) {
	switch action {
	case actionNoStickers, actionGroupNoStickers:
		p.CanSendOtherMessages = false
	case actionStickers, actionGroupStickers:
		p.CanSendOtherMessages = true
	case actionNoPhotos, actionGroupNoPhotos:
		p.CanSendPhotos = false
	case actionPhotos, actionGroupPhotos:
		p.CanSendPhotos = true
	}
}

// telegramError is why Telegram did not act, in words safe for the model.
type telegramError struct {
	message string
}

func (e *telegramError) Error() string { return e.message }

// telegramFailure tells why nothing was done, in Telegram's words when it
// gave a reason.
func telegramFailure(err error) error {
	if refusal, ok := telegramRefusal(err); ok {
		return &telegramError{"Telegram refused: " + refusal.Description}
	}
	return &telegramError{noAnswer}
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
	case actionNoStickers:
		return chatText{"Bajarildi: " + name + " " + until + " gacha stiker va GIF yubora olmaydi.", "Готово: " + name + " не может отправлять стикеры и GIF до " + until + "."}
	case actionStickers:
		return chatText{"Bajarildi: " + name + " yana stiker va GIF yubora oladi.", "Готово: " + name + " снова может отправлять стикеры и GIF."}
	case actionNoPhotos:
		return chatText{"Bajarildi: " + name + " " + until + " gacha rasm yubora olmaydi.", "Готово: " + name + " не может отправлять фото до " + until + "."}
	case actionPhotos:
		return chatText{"Bajarildi: " + name + " yana rasm yubora oladi.", "Готово: " + name + " снова может отправлять фото."}
	case actionGroupNoPhotos:
		return chatText{"Bajarildi: guruhda hech kim rasm yubora olmaydi.", "Готово: в группе никто не может отправлять фото."}
	case actionGroupPhotos:
		return chatText{"Bajarildi: guruhda yana hamma rasm yubora oladi.", "Готово: в группе снова все могут отправлять фото."}
	case actionGroupNoStickers:
		return chatText{"Bajarildi: guruhda hech kim stiker va GIF yubora olmaydi.", "Готово: в группе никто не может отправлять стикеры и GIF."}
	case actionGroupStickers:
		return chatText{"Bajarildi: guruhda yana hamma stiker va GIF yubora oladi.", "Готово: в группе снова все могут отправлять стикеры и GIF."}
	case actionPin:
		return chatText{"Bajarildi: " + name + " xabari qadaldi.", "Готово: сообщение от " + name + " закреплено."}
	}
	return chatText{"Bajarildi: " + name + " xabari qadashdan olindi.", "Готово: сообщение от " + name + " откреплено."}
}

// untilText is when a mute ends: the time, and the date too when it is not
// today, with the year when it is not this year.
func (m *Moderation) untilText(until time.Time) string {
	local, now := until.In(m.opts.Location), m.opts.Now().In(m.opts.Location)
	switch {
	case local.Format(time.DateOnly) == now.Format(time.DateOnly):
		return local.Format(clockLayout)
	case local.Year() != now.Year():
		return local.Format(yearClockLayout)
	}
	return local.Format(dateClockLayout)
}

// actionWords are what a person asking for each action says, written as
// askedFor folds a message. Many are only the start of a word, so that
// "o'chirib tashla", "mute qilvor" and "заблокируй" count.
var actionWords = map[string][]string{
	actionDelete: {"ochir", "delete", "del ", "sil", "olib tashla", "yoq qil", "удал", "сотри"},
	actionMute:   {"mute", "myut", "мут", "мют", "ovozsiz", "jim qil", "yozolmasin", "yoza olmasin", "yozmasin", "yozdirma", "заглуш", "молч", "readonly", "read only"},
	actionUnmute: {"mute", "мут", "мют", "ovozsiz", "yozsin", "yoza olsin", "ochib qoy", "ochvor", "разреш"},
	actionBan:    {"ban", "бан", "blok", "chiqar", "hayda", "kick", "кик", "выгн", "выкин", "заблок"},
	actionUnban:  {"ban", "бан", "blok", "qaytar", "верн", "разблок"},
	actionPin:    {"pin", "qada", "закреп"},
	// Asking about someone's stickers is enough: the model picks which.
	actionNoStickers: {"stik", "stic", "стик", "gif", "гиф"},
	actionStickers:   {"stik", "stic", "стик", "gif", "гиф"},

	actionGroupNoStickers: {"stik", "stic", "стик", "gif", "гиф"},
	actionGroupStickers:   {"stik", "stic", "стик", "gif", "гиф"},
	actionNoPhotos:        photoWords,
	actionPhotos:          photoWords,
	actionGroupNoPhotos:   photoWords,
	actionGroupPhotos:     photoWords,
	actionUnpin:           {"pin", "qada", "закреп", "откреп"},
}

var foldApostrophes = strings.NewReplacer("'", "", "ʻ", "", "ʼ", "", "’", "", "‘", "", "`", "")

// askedFor reports whether words, what the person asking wrote themselves,
// ask for action. With no question to confirm, this is what keeps a line
// in the chat from talking the model into moderating on someone's turn.
// photoWords ask for something about photos: "rasm", "surat", "фото".
var photoWords = []string{"rasm", "rasim", "surat", "foto", "photo", "фото", "картин", "расм", "сурат"}

func askedFor(words, action string) bool {
	folded := foldApostrophes.Replace(strings.ToLower(words)) + " "
	for _, word := range actionWords[action] {
		if strings.Contains(folded, word) {
			return true
		}
	}
	return false
}
