package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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
	// actionNoPhotos and actionPhotos do the same for photos.
	actionNoPhotos = "no_photos"
	actionPhotos   = "allow_photos"
	// actionPromote makes a member an admin with the bot's own moderation
	// rights at most; actionDemote takes that back, and actionAdminTitle
	// names an admin the bot made.
	actionPromote    = "promote"
	actionDemote     = "demote"
	actionAdminTitle = "admin_title"

	// Actions on the group itself, which need no reply or tag. actionLock
	// forbids one kind of message to everyone, keeping the group's other
	// permissions; actionUnlock allows it again.
	actionSetTitle       = "set_title"
	actionSetDescription = "set_description"
	actionSetPhoto       = "set_photo"
	actionDeletePhoto    = "delete_photo"
	actionInviteLink     = "invite_link"
	actionUnpinAll       = "unpin_all"
	actionLock           = "lock"
	actionUnlock         = "unlock"
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
	unnamedMember  = "their own message does not name that member, or I have not seen a member here by that name; name them by the @username or first name group_members lists, or ask them to reply to the member's message"
	manyNamed      = "more than one member here goes by that name; ask them to tag the one they mean with @ or reply to their message"
	unknownMention = "I have not seen that @username in this group, so I can't tell who it is; reply to one of their own messages instead"
	unsureMention  = "Telegram did not confirm that the tagged account is a member here under that name, so nothing was done; reply to one of their own messages instead"
	immune         = "I can't do that to admins, my owners, myself, Telegram's service accounts or the person asking"
	notAsked       = "their own message does not ask for this, so nothing was done; do it only when they ask for it in words, such as \"mute qil\" or \"o'chir\""
	noAnswer       = "Telegram did not answer, so it may not have been done; try again later"
	notAnAdmin     = "that member is not an admin I can change: I can only demote or title admins I made myself"
	// A channel is no member: it can be banned from posting here, and its
	// posts deleted or pinned, but nothing else.
	onChannel = "that message was posted on behalf of a channel, not by a member: I can only delete or pin it, or ban the channel from posting here and unban it; muting, stickers, photos, adminship and purge are for members"
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
	actionPurge:  telegram.RightDeleteMessages,

	actionNoStickers: telegram.RightRestrictMembers,
	actionStickers:   telegram.RightRestrictMembers,

	actionNoPhotos:   telegram.RightRestrictMembers,
	actionPhotos:     telegram.RightRestrictMembers,
	actionPromote:    telegram.RightPromoteMembers,
	actionDemote:     telegram.RightPromoteMembers,
	actionAdminTitle: telegram.RightPromoteMembers,

	actionSetTitle:       telegram.RightChangeInfo,
	actionSetDescription: telegram.RightChangeInfo,
	actionSetPhoto:       telegram.RightChangeInfo,
	actionDeletePhoto:    telegram.RightChangeInfo,
	actionInviteLink:     telegram.RightInviteUsers,
	actionRevokeLink:     telegram.RightInviteUsers,
	actionEditLink:       telegram.RightInviteUsers,
	actionUnpinAll:       telegram.RightPinMessages,
	actionLock:           telegram.RightRestrictMembers,
	actionUnlock:         telegram.RightRestrictMembers,
	// Undo needs the right of the action it puts back.
	actionUndo: "",
}

// rightWords names the rights moderation needs, for refusals.
var rightWords = map[string]string{
	telegram.RightDeleteMessages:  "delete messages",
	telegram.RightRestrictMembers: "ban and mute members",
	telegram.RightPinMessages:     "pin messages",
	telegram.RightChangeInfo:      "change the group's info",
	telegram.RightInviteUsers:     "invite users",
	telegram.RightPromoteMembers:  "add new admins",
	telegram.RightManageTopics:    "manage topics",
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
	// BanChatSenderChat and UnbanChatSenderChat ban a channel from posting
	// in a group on its own behalf, and let it again.
	BanChatSenderChat(ctx context.Context, chatID, senderChatID int64) error
	UnbanChatSenderChat(ctx context.Context, chatID, senderChatID int64) error
	// DeleteMessages deletes up to telegram.MaxDeletedAtOnce messages, for
	// purge.
	DeleteMessages(ctx context.Context, chatID int64, messageIDs []int64) error
	PinChatMessage(ctx context.Context, chatID, messageID int64) error
	UnpinChatMessage(ctx context.Context, chatID, messageID int64) error
	GetChat(ctx context.Context, chatID int64) (telegram.Chat, error)
	SetChatPermissions(ctx context.Context, chatID int64, permissions telegram.ChatPermissions) error
	SetChatTitle(ctx context.Context, chatID int64, title string) error
	SetChatDescription(ctx context.Context, chatID int64, description string) error
	SetChatPhoto(ctx context.Context, chatID int64, jpeg []byte) error
	DeleteChatPhoto(ctx context.Context, chatID int64) error
	CreateChatInviteLink(ctx context.Context, l telegram.InviteLink) (string, error)
	UnpinAllChatMessages(ctx context.Context, chatID int64) error
	PromoteChatMember(ctx context.Context, chatID, userID int64, rights telegram.AdminRights) error
	SetChatAdministratorCustomTitle(ctx context.Context, chatID, userID int64, title string) error
	RevokeChatInviteLink(ctx context.Context, chatID int64, link string) error
	EditChatInviteLink(ctx context.Context, link string, l telegram.InviteLink) error
	// DownloadFile fetches an old group photo, to put it back.
	DownloadFile(ctx context.Context, fileID string, maxBytes int64) ([]byte, error)
}

// ModerationStore keeps the audit of what the bot did for whom.
type ModerationStore interface {
	RecordModeration(ctx context.Context, a storage.ModerationAction) (bool, error)
	ForgetModeration(ctx context.Context, updateID int64, action string) error
	ModerationBy(ctx context.Context, requesterID int64, since time.Time) (int, error)
	SetModerationUndo(ctx context.Context, updateID int64, action string, undo json.RawMessage) error
	LastUndoable(ctx context.Context, chatID int64) (storage.ModerationAction, bool, error)
	MarkUndone(ctx context.Context, id int64) (bool, error)
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
	// Roster finds a member the asker named by first name or username,
	// without a tag; without it only replies and tags name a member.
	Roster MemberLister
	// Links finds the invite links the bot made, to revoke or edit them;
	// without it neither can be done.
	Links InviteLinks
	// Messages finds a member's messages in the group's log, for purge;
	// without it purge is refused.
	Messages MessageLog
}

type moderationArgs struct {
	Action  string `json:"action"`
	Minutes int    `json:"minutes"`
	Reason  string `json:"reason"`
	// Text is a new title, description or admin title, or an invite link's
	// name; Kind is what lock and unlock act on.
	Text string `json:"text"`
	Kind string `json:"kind"`
	// Force puts an action back even after someone changed it by hand.
	Force bool `json:"force"`
	// Member is who the asker named, without a reply or a tag.
	Member string `json:"member"`
	// MemberLimit, JoinRequest and Link are an invite link's: see
	// inviteRequest.
	MemberLimit *int   `json:"member_limit"`
	JoinRequest *bool  `json:"join_request"`
	Link        string `json:"link"`
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
			"no_photos (minutes, default a day) or allow_photos: the same for photos. A member's other limits stay as they are. " +
			"promote: make its author an admin, with your own delete, mute and pin rights at most; demote: take that back; admin_title (text, 16 characters at most): the title shown for an admin you made. " +
			"purge (minutes, default 60, at most 2880): delete every message its author wrote here in the last minutes, as far as your log of the group has them, when they ask to clear out or delete all of someone's messages; it cannot be undone. " +
			"A message posted on behalf of a channel can only be deleted, pinned, or banned or unbanned, which bans the channel from posting here. " +
			"For the group itself, with no reply or tag: set_title (text), set_description (text, empty to remove), set_photo (the photo in their message), delete_photo, invite_link (a new link; text names it, minutes ends it), unpin_all, and lock or unlock (kind) to forbid or allow one kind of message to everyone, keeping the group's other permissions: when they ask to block stickers or photos for all, it is lock, never a guard. " +
			"An invite_link may let only member_limit people join, or with join_request make each ask the admins to approve them, never both. revoke_link stops a link you made from working, and edit_link changes its text, minutes, member_limit (0 for none) or join_request; both act on link, or without it on the latest link you made here. " +
			"undo: put back the last thing you did here with this tool, whatever it was; a deleted message never comes back. With force only when, told it was changed by hand since, they still want it back. " +
			"For member actions without such a reply, or replying to their own message, it acts on the one member they tagged with @, once Telegram confirms who that is; when they named the member without a tag (\"Azamatni mute qil\"), pass member: never ask them to tag or reply instead. " +
			"It checks everyone's rights and does it at once, with no question, but only when the person's own message asks for it; say it is done only when it reports done.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["delete", "purge", "mute", "unmute", "ban", "unban", "pin", "unpin", "no_stickers", "allow_stickers", "no_photos", "allow_photos", "promote", "demote", "admin_title", "set_title", "set_description", "set_photo", "delete_photo", "invite_link", "revoke_link", "edit_link", "unpin_all", "lock", "unlock", "undo"]},
				"minutes": {"type": "integer", "description": "For mute: how long, 1 to 10080, default 60. For purge: how far back, 1 to 2880, default 60. For no_stickers and no_photos: 1 to 525600, default 1440; the most for \"forever\". For invite_link: when it stops working; none for never."},
				"text": {"type": "string", "description": "For set_title, set_description and admin_title: the new text, in the words they gave. For invite_link: an optional name."},
				"force": {"type": "boolean", "description": "For undo: true only when they want it back though it was changed by hand since."},
				"kind": {"type": "string", "enum": ["messages", "photos", "videos", "stickers", "voice", "files", "music", "polls", "link_previews"], "description": "For lock and unlock: stickers covers GIFs too, voice covers round videos."},
				"reason": {"type": "string", "description": "Optional: why, in a few words."},
				"member": {"type": "string", "description": "For an action on a member they named without a reply or a tag: that member's first name or @username, as they wrote it or group_members lists it."},
				"member_limit": {"type": "integer", "description": "For invite_link and edit_link: how many people may join through it, 1 to 99999; for edit_link 0 removes the limit."},
				"join_request": {"type": "boolean", "description": "For invite_link and edit_link: true when each person who opens it must be approved by the admins."},
				"link": {"type": "string", "description": "For revoke_link and edit_link: the link they mean, as they wrote it; omit for the latest you made here."}
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
	// minutes and until are a mute's, a restriction's or a link's.
	minutes int
	until   time.Time
	// text, kind and picture are what an action on the group or an admin
	// sets.
	text    string
	kind    string
	picture []byte
	// invite is what an invite link action asks for besides its name.
	invite inviteRequest
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
		return nil, &ArgumentError{Message: "action must be one of the tool's actions"}
	}
	if !askedFor(caller.Words, args.Action) {
		return nil, &DeniedError{Message: notAsked}
	}
	if args.Action == actionUndo {
		return m.undo(ctx, caller, args.Force)
	}
	target, err := m.targetOf(ctx, caller, args.Action, args.Member)
	if err != nil {
		return nil, err
	}
	o := m.newOrder(caller, args.Action, target, args.Minutes, args.Reason)
	if err := o.setGroupArgs(args, caller.Picture); err != nil {
		return nil, err
	}
	if err := m.check(ctx, o); err != nil {
		return nil, err
	}
	if onInviteLink(o.action) {
		return m.runLink(ctx, o)
	}
	if o.action == actionPurge {
		return m.purge(ctx, o)
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

// Limits Telegram sets on a group's texts.
const (
	maxTitleRunes       = 128
	maxDescriptionRunes = 255
	maxLinkNameRunes    = 32
	maxLinkMinutes      = 366 * 24 * 60
)

// setGroupArgs reads what an action on the group or an admin needs: its
// text, the kind to lock, or the photo in the message.
func (o *order) setGroupArgs(args moderationArgs, picture []byte) error {
	text := strings.TrimSpace(args.Text)
	switch o.action {
	case actionSetTitle:
		if text == "" || utf8.RuneCountInString(text) > maxTitleRunes {
			return &ArgumentError{Message: fmt.Sprintf("text must be the new title, 1 to %d characters", maxTitleRunes)}
		}
	case actionSetDescription:
		if utf8.RuneCountInString(text) > maxDescriptionRunes {
			return &ArgumentError{Message: fmt.Sprintf("text must be the new description, at most %d characters", maxDescriptionRunes)}
		}
	case actionAdminTitle:
		if utf8.RuneCountInString(text) > maxAdminTitleRunes {
			return &ArgumentError{Message: fmt.Sprintf("an admin title has at most %d characters", maxAdminTitleRunes)}
		}
	case actionInviteLink, actionRevokeLink, actionEditLink:
		text = cutRunes(text, maxLinkNameRunes)
		if err := o.setLinkArgs(args); err != nil {
			return err
		}
	case actionLock, actionUnlock:
		if _, known := lockKinds[args.Kind]; !known {
			return &ArgumentError{Message: "kind must be messages, photos, videos, stickers, voice, files, music, polls or link_previews"}
		}
		o.kind = args.Kind
	case actionSetPhoto:
		if len(picture) == 0 {
			return &ArgumentError{Message: "there is no photo in the message: ask them to send the photo with the request, or reply to one"}
		}
		photo, err := asJPEG(picture)
		if err != nil {
			return err
		}
		o.picture = photo
	}
	o.text = text
	return nil
}

// doneNote tells the model how to report an action the tool carried out;
// alreadyDoneNote, one this message already had done.
const (
	doneNote        = "Done. Tell them in one short line, in their language, like the line in said; do not ask anything."
	linkNote        = "Done. Give them the link exactly as it is, in one short line, in their language."
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
// an action on a member names the one member they tagged, or else the one
// they named in words, once Telegram confirms that account. Anything less
// certain acts on nobody: the person is asked to reply to the member's
// message instead.
func (m *Moderation) targetOf(ctx context.Context, caller Caller, action, member string) (Target, error) {
	if groupWide(action) {
		return Target{}, nil
	}
	replied := caller.Target
	ownReply := replied != nil && replied.UserID == caller.UserID
	named := len(caller.Mentions) > 0 || member != ""
	switch {
	case replied != nil && (!ownReply || !onMember(action) || !named):
		// Acting on oneself is refused when the order is checked.
		return *replied, nil
	case !onMember(action) || !named:
		return Target{}, &DeniedError{Message: noReplyTarget}
	case len(caller.Mentions) > 1:
		return Target{}, &DeniedError{Message: manyMentions}
	case len(caller.Mentions) == 1:
		return m.tagged(ctx, caller.ChatID, caller.Mentions[0], action)
	}
	return m.named(ctx, caller, member, action)
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

// named is the member the asker named in words, without a tag: the one
// member seen here whose first name or username is member. The model only
// picks among the asker's words: their own message must name that member,
// and a first name must be no other seen member's, or nobody is acted on.
func (m *Moderation) named(ctx context.Context, caller Caller, member, action string) (Target, error) {
	if m.opts.Roster == nil {
		return Target{}, &DeniedError{Message: noReplyTarget}
	}
	members, _, err := m.opts.Roster.GroupMembers(ctx, caller.ChatID, maxMembersListed)
	if err != nil {
		return Target{}, internal(err)
	}
	var found []storage.GroupMember
	for _, seen := range members {
		if goesBy(seen, member) {
			found = append(found, seen)
		}
	}
	if len(found) > 1 {
		return Target{}, &DeniedError{Message: manyNamed}
	}
	if len(found) == 0 {
		return Target{}, &DeniedError{Message: unnamedMember}
	}
	chosen := found[0]
	words := wordsOf(caller.Words)
	if chosen.Username == "" || !slices.Contains(words, strings.ToLower(chosen.Username)) {
		first := firstName(chosen.FirstName)
		if !namedBy(words, first) {
			return Target{}, &DeniedError{Message: unnamedMember}
		}
		for _, seen := range members {
			if seen.UserID != chosen.UserID && strings.EqualFold(firstName(seen.FirstName), first) {
				return Target{}, &DeniedError{Message: manyNamed}
			}
		}
	}
	return m.tagged(ctx, caller.ChatID, Mention{UserID: chosen.UserID}, action)
}

// minNameRunes is the shortest first name taken from someone's words: a
// shorter one is part of too many other words.
const minNameRunes = 3

// nameEndings are what Uzbek and Russian add to a name in a sentence:
// "Azamatni", "Azamatga", "Азамата".
var nameEndings = []string{"", "ni", "ning", "ga", "ka", "qa", "da", "dan", "jon", "a", "u", "а", "у", "е", "ом"}

// goesBy reports whether member is a seen member's username, first name or
// the first word of it.
func goesBy(seen storage.GroupMember, member string) bool {
	member = strings.TrimPrefix(strings.TrimSpace(member), "@")
	if member == "" {
		return false
	}
	return strings.EqualFold(seen.Username, member) || strings.EqualFold(seen.FirstName, member) ||
		strings.EqualFold(firstName(seen.FirstName), member)
}

// firstName is the first word of a first name: "Azamat" of "Azamat 아자마트".
func firstName(name string) string {
	if words := wordsOf(name); len(words) > 0 {
		return words[0]
	}
	return ""
}

// wordsOf is text in lower case, split into words of letters and digits;
// an underscore stays, as usernames have it.
func wordsOf(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
}

// namedBy reports whether one of words is name, as it is or with an ending
// a sentence gives it.
func namedBy(words []string, name string) bool {
	name = strings.ToLower(name)
	if utf8.RuneCountInString(name) < minNameRunes {
		return false
	}
	for _, word := range words {
		if ending, ok := strings.CutPrefix(word, name); ok && slices.Contains(nameEndings, ending) {
			return true
		}
	}
	return false
}

// onMember reports an action on a member rather than on a message.
func onMember(action string) bool {
	switch action {
	case actionMute, actionUnmute, actionBan, actionUnban, actionNoStickers, actionStickers, actionNoPhotos, actionPhotos,
		actionPromote, actionDemote, actionAdminTitle, actionPurge:
		return true
	}
	return false
}

// groupWide reports an action on the whole group rather than on a member
// or a message.
func groupWide(action string) bool {
	switch action {
	case actionSetTitle, actionSetDescription, actionSetPhoto, actionDeletePhoto, actionInviteLink, actionRevokeLink, actionEditLink, actionUnpinAll, actionLock, actionUnlock:
		return true
	}
	return false
}

// onAdmin reports an action on an admin rather than on a member.
func onAdmin(action string) bool {
	return action == actionDemote || action == actionAdminTitle
}

// present reports a member status an action may be taken on: someone in
// the group, or for unban someone who was in it, as for purge, which most
// often clears out a spammer just banned.
func present(status, action string) bool {
	switch status {
	case "creator", "administrator", "member", "restricted":
		return true
	case "kicked", "left":
		return action == actionUnban || action == actionPurge
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
	case actionInviteLink, actionEditLink:
		if minutes > 0 {
			o.minutes = min(minutes, maxLinkMinutes)
		}
	case actionPurge:
		// How far back, not how long: purge has no end.
		o.minutes = clampMinutes(minutes, defaultPurgeMinutes, maxPurgeMinutes)
		return o
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
//
// A channel's post is the channel's, not the account Telegram posts it as:
// it may be deleted or pinned, and the channel banned, but no member's
// action fits a channel.
func (m *Moderation) checkTarget(ctx context.Context, o order) error {
	if o.target.ChannelID != 0 {
		return channelOnly(o.action)
	}
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
	if onAdmin(o.action) {
		// Telegram lets a bot change only the admins it made; the creator is
		// never one of them.
		if member.Status != "administrator" {
			return &DeniedError{Message: notAnAdmin}
		}
		return nil
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

// channelOnly allows the actions that fit a channel's post.
func channelOnly(action string) error {
	switch action {
	case actionDelete, actionPin, actionUnpin, actionBan, actionUnban:
		return nil
	}
	return &DeniedError{Message: onChannel}
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
	undo, err := m.snapshot(ctx, o)
	if err != nil {
		return false, telegramFailure(err)
	}
	record := o.audit()
	record.Undo = undo
	first, err := m.opts.Store.RecordModeration(ctx, record)
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
// the chat no longer has it; other actions keep no text. A channel is
// recorded as the target by its chat ID, which, unlike a user's, is
// negative: undo tells them apart by it.
func (o order) audit() storage.ModerationAction {
	a := storage.ModerationAction{
		ChatID: o.caller.ChatID, Action: o.action, RequesterID: o.caller.UserID,
		TargetUserID: cmp.Or(o.target.ChannelID, o.target.UserID), TargetMessageID: o.target.MessageID,
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
		if channel := o.target.ChannelID; channel != 0 {
			return m.opts.Telegram.BanChatSenderChat(ctx, chatID, channel)
		}
		return m.opts.Telegram.BanChatMember(ctx, chatID, userID)
	case actionUnban:
		if channel := o.target.ChannelID; channel != 0 {
			return m.opts.Telegram.UnbanChatSenderChat(ctx, chatID, channel)
		}
		return m.opts.Telegram.UnbanChatMember(ctx, chatID, userID)
	case actionPin:
		return m.opts.Telegram.PinChatMessage(ctx, chatID, messageID)
	case actionUnpin:
		return m.opts.Telegram.UnpinChatMessage(ctx, chatID, messageID)
	case actionPromote:
		return m.promote(ctx, chatID, userID)
	case actionDemote:
		return m.opts.Telegram.PromoteChatMember(ctx, chatID, userID, telegram.AdminRights{})
	case actionAdminTitle:
		return m.opts.Telegram.SetChatAdministratorCustomTitle(ctx, chatID, userID, o.text)
	case actionSetTitle:
		return m.opts.Telegram.SetChatTitle(ctx, chatID, o.text)
	case actionSetDescription:
		return m.opts.Telegram.SetChatDescription(ctx, chatID, o.text)
	case actionSetPhoto:
		return m.opts.Telegram.SetChatPhoto(ctx, chatID, o.picture)
	case actionDeletePhoto:
		return m.opts.Telegram.DeleteChatPhoto(ctx, chatID)
	case actionUnpinAll:
		return m.opts.Telegram.UnpinAllChatMessages(ctx, chatID)
	case actionLock, actionUnlock:
		return m.setGroupKind(ctx, chatID, o.kind, o.action == actionUnlock)
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

// promote makes a member an admin with the moderation rights the bot itself
// holds, never the right to make admins: Telegram lets a bot give only its
// own rights.
func (m *Moderation) promote(ctx context.Context, chatID, userID int64) error {
	me, err := m.opts.Members.GetChatMember(ctx, chatID, m.opts.BotID)
	if err != nil {
		return err
	}
	return m.opts.Telegram.PromoteChatMember(ctx, chatID, userID, telegram.AdminRights{
		CanDeleteMessages:  me.CanDeleteMessages,
		CanRestrictMembers: me.CanRestrictMembers,
		CanPinMessages:     me.CanPinMessages,
		CanInviteUsers:     me.CanInviteUsers,
	})
}

// setGroupKind allows or forbids one kind of message for the whole group,
// keeping whatever else its members may or may not do.
func (m *Moderation) setGroupKind(ctx context.Context, chatID int64, kind string, allowed bool) error {
	chat, err := m.opts.Telegram.GetChat(ctx, chatID)
	if err != nil {
		return err
	}
	permissions := groupPermissions(chat)
	lockKinds[kind].set(&permissions, allowed)
	return m.opts.Telegram.SetChatPermissions(ctx, chatID, permissions)
}

// groupPermissions are what a group's members may do by default, or
// everything when Telegram did not say.
func groupPermissions(chat telegram.Chat) telegram.ChatPermissions {
	if chat.Permissions == nil {
		return telegram.AllPermissions()
	}
	return *chat.Permissions
}

// lockKind is one kind of message lock and unlock act on: the permissions
// it is, and how the bot names it.
type lockKind struct {
	set  func(p *telegram.ChatPermissions, allowed bool)
	name chatText
}

var lockKinds = map[string]lockKind{
	"messages": {func(p *telegram.ChatPermissions, a bool) { p.CanSendMessages = a }, chatText{"xabar yozish", "писать сообщения"}},
	"photos":   {func(p *telegram.ChatPermissions, a bool) { p.CanSendPhotos = a }, chatText{"rasm yuborish", "отправлять фото"}},
	"videos":   {func(p *telegram.ChatPermissions, a bool) { p.CanSendVideos = a }, chatText{"video yuborish", "отправлять видео"}},
	"stickers": {func(p *telegram.ChatPermissions, a bool) { p.CanSendOtherMessages = a }, chatText{"stiker va GIF yuborish", "отправлять стикеры и GIF"}},
	"voice": {func(p *telegram.ChatPermissions, a bool) { p.CanSendVoiceNotes, p.CanSendVideoNotes = a, a },
		chatText{"ovozli xabar va dumaloq video yuborish", "отправлять голосовые и кружочки"}},
	"files":         {func(p *telegram.ChatPermissions, a bool) { p.CanSendDocuments = a }, chatText{"fayl yuborish", "отправлять файлы"}},
	"music":         {func(p *telegram.ChatPermissions, a bool) { p.CanSendAudios = a }, chatText{"musiqa yuborish", "отправлять музыку"}},
	"polls":         {func(p *telegram.ChatPermissions, a bool) { p.CanSendPolls = a }, chatText{"so'rovnoma yuborish", "отправлять опросы"}},
	"link_previews": {func(p *telegram.ChatPermissions, a bool) { p.CanAddWebPagePreviews = a }, chatText{"havolalar ko'rinishi", "превью ссылок"}},
}

// allowMedia sets the one permission a sticker or photo action is about:
// stickers and GIFs, which Telegram allows or forbids together, or photos.
func allowMedia(p *telegram.ChatPermissions, action string) {
	switch action {
	case actionNoStickers:
		p.CanSendOtherMessages = false
	case actionStickers:
		p.CanSendOtherMessages = true
	case actionNoPhotos:
		p.CanSendPhotos = false
	case actionPhotos:
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
	if o.target.ChannelID != 0 {
		switch o.action {
		case actionBan:
			return chatText{"Bajarildi: endi «" + name + "» kanali nomidan bu guruhda yozib bo'lmaydi.", "Готово: от имени канала «" + name + "» больше нельзя писать в этой группе."}
		case actionUnban:
			return chatText{"Bajarildi: «" + name + "» kanali nomidan yana yozish mumkin.", "Готово: от имени канала «" + name + "» снова можно писать."}
		}
	}
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
	case actionPromote:
		return chatText{"Bajarildi: " + name + " endi admin.", "Готово: " + name + " теперь админ."}
	case actionDemote:
		return chatText{"Bajarildi: " + name + " endi admin emas.", "Готово: " + name + " больше не админ."}
	case actionAdminTitle:
		return chatText{"Bajarildi: " + name + " unvoni o'zgardi.", "Готово: у " + name + " новое звание."}
	case actionSetTitle:
		return chatText{"Bajarildi: guruh nomi «" + o.text + "» bo'ldi.", "Готово: группа теперь называется «" + o.text + "»."}
	case actionSetDescription:
		return chatText{"Bajarildi: guruh tavsifi yangilandi.", "Готово: описание группы обновлено."}
	case actionSetPhoto:
		return chatText{"Bajarildi: guruh rasmi yangilandi.", "Готово: фото группы обновлено."}
	case actionDeletePhoto:
		return chatText{"Bajarildi: guruh rasmi olib tashlandi.", "Готово: фото группы удалено."}
	case actionUnpinAll:
		return chatText{"Bajarildi: hamma qadalgan xabarlar olindi.", "Готово: все закреплённые сообщения откреплены."}
	case actionLock:
		kind := lockKinds[o.kind].name
		return chatText{"Bajarildi: guruhda " + kind.uz + " yopildi.", "Готово: в группе запрещено " + kind.ru + "."}
	case actionUnlock:
		kind := lockKinds[o.kind].name
		return chatText{"Bajarildi: guruhda " + kind.uz + " ochildi.", "Готово: в группе снова можно " + kind.ru + "."}
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
	// Deleting one message is not clearing out them all: "tozala",
	// "hammasini o'chir", "очисти", "удали все".
	actionPurge: {"tozala", "hammasini ochir", "barchasini ochir", "hammasini del", "xabarlarini ochir", "hamma xabar", "barcha xabar",
		"очист", "удали все", "удали всё", "удалить все", "удалить всё", "удали его сообщения", "purge", "clear", "clean"},
	// Asking about someone's stickers is enough: the model picks which.
	actionNoStickers: {"stik", "stic", "стик", "gif", "гиф"},
	actionStickers:   {"stik", "stic", "стик", "gif", "гиф"},

	actionNoPhotos:   photoWords,
	actionPhotos:     photoWords,
	actionPromote:    adminWords,
	actionDemote:     adminWords,
	actionAdminTitle: {"admin", "админ", "unvon", "titul", "титул", "звани", "title", "lavozim", "должн"},

	actionSetTitle:       append([]string{"nom", "title", "назв", "имя"}, changeWords...),
	actionSetDescription: append([]string{"tavsif", "description", "описан", "bio"}, changeWords...),
	actionSetPhoto:       append([]string{"avatar", "аватар", "ava "}, photoWords...),
	actionDeletePhoto:    append([]string{"avatar", "аватар", "ava "}, photoWords...),
	actionInviteLink:     linkWords,
	actionRevokeLink:     linkWords,
	actionEditLink:       linkWords,
	actionUnpinAll:       {"pin", "qada", "закреп", "откреп"},
	actionLock:           {"yop", "blok", "taqiq", "chekla", "ochir", "запрет", "закр", "огранич", "lock", "заблок"},
	actionUndo:           {"qaytar", "orqaga", "ortga", "bekor qil", "undo", "верн", "отмен", "назад"},
	actionUnlock:         {"och", "ruxsat", "разреш", "откр", "unlock", "разблок"},
	actionUnpin:          {"pin", "qada", "закреп", "откреп"},
}

var foldApostrophes = strings.NewReplacer("'", "", "ʻ", "", "ʼ", "", "’", "", "‘", "", "`", "")

// askedFor reports whether words, what the person asking wrote themselves,
// ask for action. With no question to confirm, this is what keeps a line
// in the chat from talking the model into moderating on someone's turn.
// changeWords ask to change something without naming it, as an owner
// answering "o'zgartiraymi?" says "ha change qil Azizbek_Bot": the model
// knows from the talk what, as it does a member's stickers.
var changeWords = []string{"ozgartir", "uzgartir", "almashtir", "change", "rename", "поменя", "смени", "измени", "переимен", "ўзгартир", "узгартир"}

// adminWords ask for something about adminship: "admin qil", "админка".
var adminWords = []string{"admin", "админ"}

// photoWords ask for something about photos: "rasm", "surat", "фото".
var photoWords = []string{"rasm", "rasim", "surat", "foto", "photo", "фото", "картин", "расм", "сурат"}

func askedFor(words, action string) bool {
	return says(words, actionWords[action])
}

// says reports whether words, folded to lower case without apostrophes,
// hold one of list.
func says(words string, list []string) bool {
	folded := foldApostrophes.Replace(strings.ToLower(words)) + " "
	for _, word := range list {
		if strings.Contains(folded, word) {
			return true
		}
	}
	return false
}
