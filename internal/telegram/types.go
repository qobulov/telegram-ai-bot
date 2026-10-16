// Package telegram is a small client for the Telegram Bot API: long polling
// for updates, sending messages, and the few admin actions the bot takes.
package telegram

import (
	"fmt"
	"slices"
	"time"
)

// Update is one incoming event: a message, or a change to the bot's own
// membership in a chat. Each kind the update is not is nil.
type Update struct {
	UpdateID     int64              `json:"update_id"`
	Message      *Message           `json:"message"`
	MyChatMember *ChatMemberUpdated `json:"my_chat_member"`
}

// ChatMemberUpdated reports that the bot was added to, promoted in, demoted
// in or removed from a chat.
type ChatMemberUpdated struct {
	Chat Chat `json:"chat"`
	// From is who made the change.
	From          *User      `json:"from"`
	NewChatMember ChatMember `json:"new_chat_member"`
}

// Message is an incoming chat message. Text is empty for photos, stickers,
// documents and other non-text content; a photo or document may carry a
// Caption instead.
type Message struct {
	MessageID       int64           `json:"message_id"`
	From            *User           `json:"from"`
	Chat            Chat            `json:"chat"`
	Text            string          `json:"text"`
	Entities        []MessageEntity `json:"entities"`
	Caption         string          `json:"caption"`
	CaptionEntities []MessageEntity `json:"caption_entities"`
	// ReplyToMessage is the message this one replies to, if any. In a forum
	// topic it may be only the topic's first message; see
	// ImplicitTopicReply.
	ReplyToMessage *Message `json:"reply_to_message"`
	// MessageThreadID is, for a message in a forum topic, the topic: the ID
	// of the service message that created it.
	MessageThreadID int64 `json:"message_thread_id"`
	// IsTopicMessage marks a message sent in a forum topic.
	IsTopicMessage bool `json:"is_topic_message"`
	// SenderChat is set when the message was sent on behalf of a chat: a
	// channel, or a group posting as itself (anonymous administrators).
	SenderChat *Chat `json:"sender_chat"`
	// IsAutomaticForward marks a channel post forwarded automatically to the
	// channel's linked discussion group.
	IsAutomaticForward bool `json:"is_automatic_forward"`
	// ForwardOrigin says where a forwarded message first came from.
	ForwardOrigin *MessageOrigin `json:"forward_origin"`

	// Service messages: members who joined or left. They carry no text.
	NewChatMembers []User `json:"new_chat_members"`
	LeftChatMember *User  `json:"left_chat_member"`

	// Media. At most one of these is set on a message, except that Telegram
	// also sets Document on a GIF, for clients that predate Animation.
	Photo     []PhotoSize `json:"photo"` // the same picture in several sizes, smallest first
	Sticker   *Sticker    `json:"sticker"`
	Animation *Animation  `json:"animation"` // a GIF, which Telegram stores as a video
	Voice     *Voice      `json:"voice"`
	Video     *Video      `json:"video"`
	VideoNote *VideoNote  `json:"video_note"` // a round video
	Document  *Document   `json:"document"`
	Audio     *Audio      `json:"audio"`
	Poll      *Poll       `json:"poll"`
}

// ImplicitTopicReply reports a message whose ReplyToMessage is only the first
// message of the forum topic it was sent in. Telegram sets reply_to_message
// that way on every message in a topic, though nobody replied to anything.
func (m *Message) ImplicitTopicReply() bool {
	return m.IsTopicMessage && m.ReplyToMessage != nil && m.ReplyToMessage.MessageID == m.MessageThreadID
}

// MessageOrigin is where a forwarded message came from. Type is "user",
// "hidden_user" (the sender hides their account), "chat" or "channel"; only
// the fields of that type are set.
type MessageOrigin struct {
	Type           string `json:"type"`
	SenderUser     *User  `json:"sender_user"`
	SenderUserName string `json:"sender_user_name"`
	SenderChat     *Chat  `json:"sender_chat"`
	Chat           *Chat  `json:"chat"`
}

// Name is what the origin is called: a person's first name, or a group's or
// channel's title. It is empty when Telegram gave no name.
func (o *MessageOrigin) Name() string {
	switch {
	case o == nil:
		return ""
	case o.SenderUser != nil && o.SenderUser.FirstName != "":
		return o.SenderUser.FirstName
	case o.SenderUserName != "":
		return o.SenderUserName
	case o.SenderChat != nil && o.SenderChat.Title != "":
		return o.SenderChat.Title
	case o.Chat != nil:
		return o.Chat.Title
	}
	return ""
}

// PhotoSize is one size of a photo, or a thumbnail.
type PhotoSize struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// Sticker is a sticker. A static sticker is a WebP image; animated (Lottie)
// and video stickers cannot be shown as an image, but have a thumbnail.
type Sticker struct {
	FileID     string     `json:"file_id"`
	Emoji      string     `json:"emoji"`
	IsAnimated bool       `json:"is_animated"`
	IsVideo    bool       `json:"is_video"`
	FileSize   int64      `json:"file_size"`
	Thumbnail  *PhotoSize `json:"thumbnail"`
}

// Animation is a GIF or a silent video clip.
type Animation struct {
	FileID    string     `json:"file_id"`
	MimeType  string     `json:"mime_type"`
	Thumbnail *PhotoSize `json:"thumbnail"`
}

// Voice is a voice message: OGG audio encoded with Opus.
type Voice struct {
	FileID   string `json:"file_id"`
	Duration int    `json:"duration"` // seconds
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

// Video is a video file. Only its thumbnail can be shown as an image.
type Video struct {
	FileID    string     `json:"file_id"`
	Duration  int        `json:"duration"` // seconds
	Thumbnail *PhotoSize `json:"thumbnail"`
}

// VideoNote is a round video message.
type VideoNote struct {
	FileID    string     `json:"file_id"`
	Duration  int        `json:"duration"` // seconds
	Thumbnail *PhotoSize `json:"thumbnail"`
}

// Document is a file sent as a file rather than as a photo or video.
type Document struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
}

// Audio is a music file, as opposed to a Voice message.
type Audio struct {
	FileID    string `json:"file_id"`
	Duration  int    `json:"duration"` // seconds
	Title     string `json:"title"`
	Performer string `json:"performer"`
}

// Poll is a poll someone posted in a chat.
type Poll struct {
	Question string       `json:"question"`
	Options  []PollOption `json:"options"`
}

// PollOption is one answer of a Poll.
type PollOption struct {
	Text string `json:"text"`
}

// User is a Telegram account: the sender of a message, or the bot itself.
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

// ChatMember is a user's standing in a chat. Its Can fields count only for
// an administrator: a creator holds every right regardless, and a restricted
// member's fields of the same name are permissions, not rights.
type ChatMember struct {
	Status string `json:"status"`
	User   *User  `json:"user,omitempty"`
	// UntilDate is when a restriction or ban ends, in Unix seconds; zero
	// means never.
	UntilDate int64 `json:"until_date,omitempty"`

	CanDeleteMessages  bool `json:"can_delete_messages,omitempty"`
	CanRestrictMembers bool `json:"can_restrict_members,omitempty"`
	CanPinMessages     bool `json:"can_pin_messages,omitempty"`
	CanPromoteMembers  bool `json:"can_promote_members,omitempty"`
	CanChangeInfo      bool `json:"can_change_info,omitempty"`
	CanInviteUsers     bool `json:"can_invite_users,omitempty"`
	CanManageTopics    bool `json:"can_manage_topics,omitempty"`
}

// Admin rights, named as in the Bot API. Moderation checks that both the
// person asking and the bot hold the right an action needs.
const (
	RightDeleteMessages  = "can_delete_messages"
	RightRestrictMembers = "can_restrict_members"
	RightPinMessages     = "can_pin_messages"
	RightPromoteMembers  = "can_promote_members"
	RightChangeInfo      = "can_change_info"
	RightInviteUsers     = "can_invite_users"
	RightManageTopics    = "can_manage_topics"
)

// adminRight ties a right's Bot API name to the field that holds it.
type adminRight struct {
	name string
	held func(ChatMember) bool
}

// adminRights is every right, in the order Rights reports them. Has and
// Rights both read this one table, so a new right is added in one place.
var adminRights = []adminRight{
	{RightDeleteMessages, func(m ChatMember) bool { return m.CanDeleteMessages }},
	{RightRestrictMembers, func(m ChatMember) bool { return m.CanRestrictMembers }},
	{RightPinMessages, func(m ChatMember) bool { return m.CanPinMessages }},
	{RightPromoteMembers, func(m ChatMember) bool { return m.CanPromoteMembers }},
	{RightChangeInfo, func(m ChatMember) bool { return m.CanChangeInfo }},
	{RightInviteUsers, func(m ChatMember) bool { return m.CanInviteUsers }},
	{RightManageTopics, func(m ChatMember) bool { return m.CanManageTopics }},
}

// Has reports whether the member holds an admin right. Anyone who is not
// the creator or an administrator holds none, and nobody holds a right
// that is not in adminRights.
func (m ChatMember) Has(name string) bool {
	i := slices.IndexFunc(adminRights, func(r adminRight) bool { return r.name == name })
	return i >= 0 && m.holds(adminRights[i])
}

// Rights lists the admin rights the member holds; nil when they hold none.
func (m ChatMember) Rights() []string {
	var rights []string
	for _, r := range adminRights {
		if m.holds(r) {
			rights = append(rights, r.name)
		}
	}
	return rights
}

// holds applies the status rule: a creator holds every right whatever the
// fields say, an administrator the ones granted, anyone else none.
func (m ChatMember) holds(r adminRight) bool {
	switch m.Status {
	case "creator":
		return true
	case "administrator":
		return r.held(m)
	}
	return false
}

// IsMember reports whether the user is still in the chat.
func (m ChatMember) IsMember() bool {
	return m.Status != "" && m.Status != "left" && m.Status != "kicked"
}

// IsAdmin reports whether the member owns or administers the chat.
func (m ChatMember) IsAdmin() bool {
	return m.Status == "creator" || m.Status == "administrator"
}

// Chat is where a message was sent.
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
	// Title and Username are set for groups; Username only for public ones.
	Title    string `json:"title,omitempty"`
	Username string `json:"username,omitempty"`
}

// Chat types. Channels are not handled by the bot.
const (
	ChatTypePrivate    = "private"
	ChatTypeGroup      = "group"
	ChatTypeSupergroup = "supergroup"
)

// Outgoing is a text message to send.
type Outgoing struct {
	ChatID int64
	Text   string
	// ReplyToMessageID makes the message a reply to that message in the same
	// chat. Zero sends an ordinary message.
	ReplyToMessageID int64
}

// ChatPermissions is what a member may do in a group. Every field is always
// sent, false included, so a restriction means exactly what it says rather
// than whatever Telegram assumes for a missing field.
type ChatPermissions struct {
	CanSendMessages       bool `json:"can_send_messages"`
	CanSendAudios         bool `json:"can_send_audios"`
	CanSendDocuments      bool `json:"can_send_documents"`
	CanSendPhotos         bool `json:"can_send_photos"`
	CanSendVideos         bool `json:"can_send_videos"`
	CanSendVideoNotes     bool `json:"can_send_video_notes"`
	CanSendVoiceNotes     bool `json:"can_send_voice_notes"`
	CanSendPolls          bool `json:"can_send_polls"`
	CanSendOtherMessages  bool `json:"can_send_other_messages"`
	CanAddWebPagePreviews bool `json:"can_add_web_page_previews"`
	CanReactToMessages    bool `json:"can_react_to_messages"`
	CanChangeInfo         bool `json:"can_change_info"`
	CanInviteUsers        bool `json:"can_invite_users"`
	CanPinMessages        bool `json:"can_pin_messages"`
	CanManageTopics       bool `json:"can_manage_topics"`
}

// NoPermissions mutes a member: they may do nothing but read.
func NoPermissions() ChatPermissions {
	return ChatPermissions{}
}

// AllPermissions lifts a member's restrictions: the Bot API treats a
// restriction that allows everything as none.
func AllPermissions() ChatPermissions {
	return ChatPermissions{
		CanSendMessages:       true,
		CanSendAudios:         true,
		CanSendDocuments:      true,
		CanSendPhotos:         true,
		CanSendVideos:         true,
		CanSendVideoNotes:     true,
		CanSendVoiceNotes:     true,
		CanSendPolls:          true,
		CanSendOtherMessages:  true,
		CanAddWebPagePreviews: true,
		CanReactToMessages:    true,
		CanChangeInfo:         true,
		CanInviteUsers:        true,
		CanPinMessages:        true,
		CanManageTopics:       true,
	}
}

// Restriction sets what a member of a supergroup may do, until a time.
type Restriction struct {
	ChatID      int64
	UserID      int64
	Permissions ChatPermissions
	// Until is when the restriction ends. Zero means never.
	Until time.Time
}

// OutgoingPoll is an anonymous poll to send.
type OutgoingPoll struct {
	ChatID   int64
	Question string
	Options  []string
	// Multiple lets a voter pick more than one option.
	Multiple bool
}

// APIError is an error returned by the Bot API.
type APIError struct {
	Code        int
	Description string
	RetryAfter  time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram: %d %s", e.Code, e.Description)
}
