package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

const (
	maxBroadcastRunes = 4000
	// maxCaptionRunes is the most Telegram puts under a photo or a video.
	maxCaptionRunes = 1024
	// broadcastMaxAge is how long a draft waits for the owner's yes.
	broadcastMaxAge = 10 * time.Minute
	// broadcastTimeout leaves time, within a turn's 45 seconds, to post in
	// every group and up to maxPrivateListed private chats one by one.
	broadcastTimeout = 40 * time.Second
	maxPrivateListed = 200
)

// GroupLister lists the groups the bot knows.
type GroupLister interface {
	Groups(ctx context.Context, since time.Time, limit int) ([]storage.GroupSummary, error)
}

// BroadcastDrafts keeps an owner's draft until they confirm it.
type BroadcastDrafts interface {
	SaveBroadcast(ctx context.Context, b storage.Broadcast) error
	ConfirmBroadcast(ctx context.Context, ownerID, updateID int64, maxAge time.Duration) (storage.Broadcast, error)
	CancelBroadcast(ctx context.Context, ownerID int64) (bool, error)
}

// ChannelLister lists the channels the bot has been made an admin of.
type ChannelLister interface {
	Channels(ctx context.Context, limit int) ([]storage.ChannelSummary, error)
}

// PrivateLister lists the users who wrote to the bot in private, whom it
// may write to.
type PrivateLister interface {
	PrivateChats(ctx context.Context, limit int) ([]int64, error)
}

// GroupSender posts a message in a group or a channel: text, or a copy of
// a message from another chat, under a new caption when one is given.
type GroupSender interface {
	SendToGroup(ctx context.Context, chatID int64, text string) error
	CopyToChat(ctx context.Context, chatID, fromChatID, messageID int64, caption string) error
}

// BroadcastOptions configures the send_to_groups tool. Channels and
// Private are optional: without them a broadcast cannot go to channels or
// private chats.
type BroadcastOptions struct {
	Groups   GroupLister
	Channels ChannelLister
	Private  PrivateLister
	Drafts   BroadcastDrafts
	Sender   GroupSender
}

type broadcastArgs struct {
	Action  string  `json:"action"`
	Text    string  `json:"text"`
	ChatIDs []int64 `json:"chat_ids"`
	// To is where a broadcast without chat_ids goes: broadcastGroups,
	// broadcastChannels, broadcastBoth, broadcastPrivate or broadcastAll.
	To string `json:"to"`
	// Copy posts the message the owner replied to instead of Text, which is
	// then its new caption, if given.
	Copy bool `json:"copy"`
	// Preview false skips sending the owner a preview of a copy, when they
	// are looking at that very message already.
	Preview *bool `json:"preview"`
}

// Where a broadcast without chat_ids goes: groups, channels or both, the
// private chats of those who wrote to the bot, or all of them.
const (
	broadcastGroups   = "groups"
	broadcastChannels = "channels"
	broadcastBoth     = "both"
	broadcastPrivate  = "users"
	broadcastAll      = "all"
)

// BroadcastTool lets an owner post a message in the bot's groups, and in
// the channels it may post in. Nothing is sent until the owner confirms in
// a later message, which the code checks: the model cannot confirm in the
// message that asked for it.
func BroadcastTool(opts BroadcastOptions) Tool {
	return Tool{
		Name: "send_to_groups",
		Description: "Owners only. Post a message in the bot's groups, in the channels where it is an admin who may post, or in the private chats of the users who wrote to it. " +
			"targets: list the groups and channels it can post in, with their chat_ids. " +
			"prepare: save the text, or with copy the message the owner replied to (a photo, a video, a post), posted as it is or, with text too, under that text as its new caption (a description for a video), and optionally to (groups by default; channels; both; users, the private chats of everyone who wrote to the bot; or all of these) or chat_ids from targets; a copy is first sent to the owner's private chat as a preview. Then show the owner the exact text, or say it is their replied message and its caption, and the groups and channels, and ask them to confirm. Nothing is sent to the groups yet. " +
			"confirm: send it, only after the owner said yes in a new message. cancel: drop the draft. " +
			"It is for announcements: a message that calls or pings a person (\"@pronium sizni @tursunoffm chaqiryapti\") is refused, as it would tag them in every group; tell the owner to write to that person directly. " +
			"Never say a message was sent unless confirm reported it.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["targets", "prepare", "confirm", "cancel"]},
				"text": {"type": "string", "description": "For prepare: the exact message to post, in the owner's language; with copy, the new caption under the replied photo or video (at most 1024 characters), or omit it to keep the message as it is."},
				"copy": {"type": "boolean", "description": "For prepare: true to post the message the owner replied to instead of a text message."},
				"preview": {"type": "boolean", "description": "For prepare with copy: false to skip the preview in the owner's private chat. Leave it out."},
				"to": {"type": "string", "enum": ["groups", "channels", "both", "users", "all"], "description": "For prepare without chat_ids: every group (the default), every channel, both, users (the private chats of everyone who wrote to the bot), or all (groups, channels and private chats)."},
				"chat_ids": {"type": "array", "items": {"type": "integer"}, "description": "For prepare: only these groups and channels, from targets."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		OwnerOnly: true,
		Timeout:   broadcastTimeout,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return runBroadcast(ctx, opts, raw)
		},
	}
}

func runBroadcast(ctx context.Context, opts BroadcastOptions, raw json.RawMessage) (any, error) {
	var args broadcastArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, ok := CallerFrom(ctx)
	if !ok || !caller.Owner || caller.UpdateID == 0 {
		return nil, errors.New("only the bot's owners may post in its groups")
	}
	switch args.Action {
	case "targets":
		return listTargets(ctx, opts)
	case "prepare":
		return prepareBroadcast(ctx, opts, caller, args)
	case "confirm":
		return confirmBroadcast(ctx, opts, caller)
	case "cancel":
		cancelled, err := opts.Drafts.CancelBroadcast(ctx, caller.UserID)
		return map[string]bool{"cancelled": cancelled}, err
	default:
		return nil, &ArgumentError{Message: "action must be targets, prepare, confirm or cancel"}
	}
}

// broadcastTarget is a group, a channel or a private chat a broadcast can
// go to.
type broadcastTarget struct {
	ChatID  int64  `json:"chat_id"`
	Name    string `json:"name"`
	channel bool
	private bool
}

// listTargets is every group and channel the bot can post in, for the
// owner to choose from.
func listTargets(ctx context.Context, opts BroadcastOptions) (any, error) {
	all, err := allTargets(ctx, opts)
	if err != nil {
		return nil, err
	}
	groups, channels := []broadcastTarget{}, []broadcastTarget{}
	for _, t := range all {
		if t.channel {
			channels = append(channels, t)
		} else {
			groups = append(groups, t)
		}
	}
	return map[string]any{"groups": groups, "channels": channels}, nil
}

func prepareBroadcast(ctx context.Context, opts BroadcastOptions, caller Caller, args broadcastArgs) (any, error) {
	draft, err := newDraft(caller, args)
	if err != nil {
		return nil, err
	}
	targets, err := broadcastTargets(ctx, opts, args.To, args.ChatIDs, caller.UserID)
	if err != nil {
		return nil, err
	}
	groups, channels, private := []string{}, []string{}, 0
	for _, t := range targets {
		draft.ChatIDs = append(draft.ChatIDs, t.ChatID)
		switch {
		case t.private:
			private++
		case t.channel:
			channels = append(channels, t.Name)
		default:
			groups = append(groups, t.Name)
		}
	}
	if err := opts.Drafts.SaveBroadcast(ctx, draft); err != nil {
		return nil, err
	}
	var result map[string]any
	if draft.CopyMessageID != 0 {
		result = copyDraftResult(ctx, opts, caller, draft, groups, channels, args.Preview == nil || *args.Preview)
	} else {
		result = textDraftResult(draft, groups, channels)
	}
	if private > 0 {
		result["private_chats"] = private
	}
	return result, nil
}

// textDraftResult says what was drafted, for the owner to see before they
// confirm.
func textDraftResult(draft storage.Broadcast, groups, channels []string) map[string]any {
	result := map[string]any{
		"draft":  true,
		"groups": groups,
		"text":   draft.Text,
		"note":   "Nothing is sent yet. Show the owner this exact text and these groups, and how many private chats, and ask them to confirm. Call confirm only after they say yes in their next message.",
	}
	if len(channels) > 0 {
		result["channels"] = channels
	}
	return result
}

// copyDraftResult sends the owner a preview of a copy, in their private
// chat, so they see the post as the groups will, unless preview is false,
// and says what was drafted.
func copyDraftResult(ctx context.Context, opts BroadcastOptions, caller Caller, draft storage.Broadcast, groups, channels []string, preview bool) map[string]any {
	result := map[string]any{"draft": true, "groups": groups, "copy": "the message the owner replied to, posted as it is"}
	if len(channels) > 0 {
		result["channels"] = channels
	}
	if draft.Text != "" {
		result["copy"] = "the message the owner replied to, under the new caption"
		result["caption"] = draft.Text
	}
	note := "Nothing is sent to the groups yet. A preview of the post was sent to the owner's private chat. Tell them so, in one line, with the groups and channels, and ask them to confirm. Call confirm only after they say yes in their next message."
	if !preview {
		result["note"] = "Nothing is sent yet. Ask the owner to confirm. Call confirm only after they say yes in their next message."
		return result
	}
	if err := post(ctx, opts.Sender, caller.UserID, draft); err != nil {
		result["preview_error"] = err.Error()
		note = "Nothing is sent to the groups yet. The preview could not be sent; tell the owner why, from preview_error. If the replied message is text, which takes no caption, prepare a text broadcast instead."
	}
	result["note"] = note
	return result
}

// newDraft is the owner's message to post: their text, or with copy the
// message they replied to, which must not call anyone either.
func newDraft(caller Caller, args broadcastArgs) (storage.Broadcast, error) {
	draft := storage.Broadcast{OwnerID: caller.UserID, UpdateID: caller.UpdateID}
	text := strings.TrimSpace(args.Text)
	if args.Copy {
		switch {
		case caller.Target == nil:
			return storage.Broadcast{}, &ArgumentError{Message: "copy posts the message the owner replied to, and they replied to none: ask them to reply to it with the request"}
		case len([]rune(text)) > maxCaptionRunes:
			return storage.Broadcast{}, &ArgumentError{Message: fmt.Sprintf("a caption is at most %d characters", maxCaptionRunes)}
		case summons(caller.Target.Text), summons(text):
			return storage.Broadcast{}, &ArgumentError{Message: notASummons}
		}
		draft.Text, draft.CopyChatID, draft.CopyMessageID = text, caller.ChatID, caller.Target.MessageID
		return draft, nil
	}
	if text == "" || len([]rune(text)) > maxBroadcastRunes {
		return storage.Broadcast{}, &ArgumentError{Message: fmt.Sprintf("text must be 1 to %d characters", maxBroadcastRunes)}
	}
	if summons(text) {
		return storage.Broadcast{}, &ArgumentError{Message: notASummons}
	}
	draft.Text = text
	return draft, nil
}

// broadcastTargets is where a broadcast goes: the chosen chats, which must
// all be groups the bot is in or channels it may post in, or else every
// group, every channel, the private chats of those who wrote to the bot
// but the owner, who sees the preview, or several of these, as to says.
func broadcastTargets(ctx context.Context, opts BroadcastOptions, to string, chosen []int64, ownerID int64) ([]broadcastTarget, error) {
	if to == "" {
		to = broadcastGroups
	}
	if !slices.Contains([]string{broadcastGroups, broadcastChannels, broadcastBoth, broadcastPrivate, broadcastAll}, to) {
		return nil, &ArgumentError{Message: "to must be groups, channels, both, users or all"}
	}
	all, err := allTargets(ctx, opts)
	if err != nil {
		return nil, err
	}
	var targets []broadcastTarget
	for _, t := range all {
		if (len(chosen) == 0 && wanted(to, t)) || slices.Contains(chosen, t.ChatID) {
			targets = append(targets, t)
		}
	}
	if len(chosen) == 0 && (to == broadcastPrivate || to == broadcastAll) {
		private, err := privateTargets(ctx, opts, ownerID)
		if err != nil {
			return nil, err
		}
		targets = append(targets, private...)
	}
	switch {
	case len(chosen) > 0 && len(targets) != len(chosen):
		return nil, &ArgumentError{Message: "some chat_ids are not groups the bot is in or channels it may post in; take them from targets"}
	case len(targets) == 0 && to == broadcastPrivate:
		return nil, &ArgumentError{Message: "no one but the owner has written to the bot in private, and a bot may not write first"}
	case len(targets) == 0 && to == broadcastChannels:
		return nil, &ArgumentError{Message: "the bot is an admin who may post in no known channel; it must be added to the channel as an admin with the right to post messages"}
	case len(targets) == 0:
		return nil, &ArgumentError{Message: "the bot is in no known groups"}
	}
	return targets, nil
}

// wanted reports whether a group or a channel is among the chats to names.
func wanted(to string, t broadcastTarget) bool {
	switch to {
	case broadcastBoth, broadcastAll:
		return true
	case broadcastChannels:
		return t.channel
	case broadcastGroups:
		return !t.channel
	}
	return false
}

// privateTargets is the private chats of those who wrote to the bot, but
// the owner's.
func privateTargets(ctx context.Context, opts BroadcastOptions, ownerID int64) ([]broadcastTarget, error) {
	if opts.Private == nil {
		return nil, nil
	}
	ids, err := opts.Private.PrivateChats(ctx, maxPrivateListed)
	if err != nil {
		return nil, err
	}
	var targets []broadcastTarget
	for _, id := range ids {
		if id != ownerID {
			targets = append(targets, broadcastTarget{ChatID: id, private: true})
		}
	}
	return targets, nil
}

// allTargets is every group the bot is in, then every channel where it is
// an admin with the right to post.
func allTargets(ctx context.Context, opts BroadcastOptions) ([]broadcastTarget, error) {
	groups, err := opts.Groups.Groups(ctx, time.Now(), maxGroupsListed)
	if err != nil {
		return nil, err
	}
	var targets []broadcastTarget
	for _, g := range groups {
		if (Membership{Status: g.BotStatus}).in() {
			targets = append(targets, broadcastTarget{ChatID: g.ChatID, Name: chatName(g.Title, g.ChatID)})
		}
	}
	if opts.Channels == nil {
		return targets, nil
	}
	channels, err := opts.Channels.Channels(ctx, maxGroupsListed)
	if err != nil {
		return nil, err
	}
	for _, c := range channels {
		if c.BotStatus == "administrator" && slices.Contains(c.Rights, telegram.RightPostMessages) {
			targets = append(targets, broadcastTarget{ChatID: c.ChatID, Name: chatName(c.Title, c.ChatID), channel: true})
		}
	}
	return targets, nil
}

// chatName is a group's or channel's title, or its ID when it has none.
func chatName(title string, chatID int64) string {
	if title != "" {
		return title
	}
	return fmt.Sprint(chatID)
}

func confirmBroadcast(ctx context.Context, opts BroadcastOptions, caller Caller) (any, error) {
	draft, err := opts.Drafts.ConfirmBroadcast(ctx, caller.UserID, caller.UpdateID, broadcastMaxAge)
	if errors.Is(err, storage.ErrNoBroadcast) {
		return nil, &ArgumentError{Message: "nothing to send: there is no draft from this owner made in an earlier message in the last 10 minutes. Prepare one, show it, and wait for their yes."}
	}
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	if targets, err := allTargets(ctx, opts); err == nil {
		for _, t := range targets {
			names[t.ChatID] = t.Name
		}
	}
	sent, failed, unreached := 0, []string{}, 0
	for _, chatID := range draft.ChatIDs {
		if err := post(ctx, opts.Sender, chatID, draft); err != nil {
			if chatID > 0 {
				// A private chat whose user blocked the bot or left it.
				unreached++
			} else {
				failed = append(failed, chatName(names[chatID], chatID))
			}
			continue
		}
		sent++
	}
	result := map[string]any{"sent": sent, "failed": failed}
	if unreached > 0 {
		result["private_unreached"] = unreached
	}
	return result, nil
}

// post sends draft to one chat: its text, or a copy of its message under
// its text as the caption, if it has one.
func post(ctx context.Context, sender GroupSender, chatID int64, draft storage.Broadcast) error {
	if draft.CopyMessageID != 0 {
		return sender.CopyToChat(ctx, chatID, draft.CopyChatID, draft.CopyMessageID, draft.Text)
	}
	return sender.SendToGroup(ctx, chatID, draft.Text)
}

// notASummons refuses a broadcast that calls someone.
const notASummons = "this calls or pings a person, which would tag them in every group; nothing was prepared. Tell the owner, in one line, to write to that person directly; broadcasts are for announcements"

var (
	// tagged finds an @username.
	tagged = regexp.MustCompile(`@[A-Za-z][A-Za-z0-9_]{3,}`)
	// callingWords are the words of a message that calls someone: "sizni
	// X chaqiryapti", "тебя зовёт X", "X is calling you".
	callingWords = regexp.MustCompile(`(?i)chaqir|чақир|чакир|зов[её]т|позов|calling you|wants you`)
)

// summons reports a message that tags someone and calls them.
func summons(text string) bool {
	return tagged.MatchString(text) && callingWords.MatchString(text)
}
