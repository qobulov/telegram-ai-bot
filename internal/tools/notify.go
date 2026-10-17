package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"telegram-ai-bot/internal/telegram"
)

const (
	// maxNoticeRunes bounds the asker's words passed on.
	maxNoticeRunes = 1000

	noticeNotInAGroup = "this works only in a group, for someone there"
	noticeNoWords     = "there is nothing in their message to pass on"
	noticeNotAnOwner  = "that is not one of my owners; I pass messages on only to my owners"
	noticeOncePerDay  = "this group already sent its one message to my owners today; it works once a day, so they can ask again tomorrow"
	noticeNotStarted  = "my owner has not started a private chat with me, so Telegram does not let me write to them; they need to send me /start in a private chat first"
	noticeUnavailable = "I could not send it just now; try again later"
	noticeSentNote    = "Their own words were sent to the owners listed in sent. Say so in one short line."
)

// NoticeQuota keeps a group to one message to its owners a day.
type NoticeQuota interface {
	ReserveOwnerNotice(ctx context.Context, chatID, requesterID int64, since time.Time) (int64, bool, error)
	ReleaseOwnerNotice(ctx context.Context, id int64) error
}

// NoticeSender looks up chats and sends messages on Telegram.
type NoticeSender interface {
	GetChat(ctx context.Context, chatID int64) (telegram.Chat, error)
	SendMessage(ctx context.Context, msg telegram.Outgoing) (int64, error)
}

// NotifyOptions configures the notify_owner tool. Quota, Telegram and Owners
// are required.
type NotifyOptions struct {
	Quota    NoticeQuota
	Telegram NoticeSender
	// Owners are the owners' Telegram IDs, the only people it writes to.
	Owners []int64
	// Location is where a day begins for the daily limit.
	Location *time.Location
	Now      func() time.Time
}

type notifyArgs struct {
	Owner string `json:"owner"`
}

type notifyResult struct {
	Sent []string `json:"sent"`
	Note string   `json:"note"`
}

// NotifyOwnerTool lets someone in a group have a message passed to the
// bot's owners in a private chat. What is sent is the asker's own words with
// the group and their name, never text the model writes, and only to the
// owners, so it cannot be used to message anyone else. A group may send one
// such message a day.
func NotifyOwnerTool(opts NotifyOptions) Tool {
	return Tool{
		Name: "notify_owner",
		Description: "Pass the asker's own message to your owners in a private chat. Use it when someone in a group asks you to tell, write to or forward something to an owner (\"@owner ga yoz ...\", \"egangga ayt\"). " +
			"owner: the owner's @username if they named one; leave it out for all owners. Their words are sent as they wrote them, at most once per group a day. Never say it was sent unless the tool says so.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {"owner": {"type": "string", "description": "The owner's @username, if one is named."}},
			"additionalProperties": false
		}`),
		GroupOnly: true,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return notifyOwner(ctx, opts, raw)
		},
	}
}

func notifyOwner(ctx context.Context, opts NotifyOptions, raw json.RawMessage) (any, error) {
	var args notifyArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, ok := CallerFrom(ctx)
	if !ok || !caller.Human || caller.ChatID >= 0 {
		return nil, &DeniedError{Message: noticeNotInAGroup}
	}
	words := strings.TrimSpace(caller.Words)
	if words == "" {
		return nil, &DeniedError{Message: noticeNoWords}
	}
	owners, err := pickOwners(ctx, opts, args.Owner)
	if err != nil {
		return nil, err
	}
	id, ok, err := opts.Quota.ReserveOwnerNotice(ctx, caller.ChatID, caller.UserID, startOfDayIn(opts.now(), opts.Location))
	if err != nil {
		return nil, &InternalError{Err: err, Message: noticeUnavailable}
	}
	if !ok {
		return nil, &DeniedError{Message: noticeOncePerDay}
	}
	text := noticeText(ctx, opts, caller, words)
	var sent []string
	notStarted := false
	for _, owner := range owners {
		_, err := opts.Telegram.SendMessage(ctx, telegram.Outgoing{ChatID: owner.id, Text: text})
		var apiErr *telegram.APIError
		switch {
		case err == nil:
			sent = append(sent, owner.label)
		case errors.As(err, &apiErr) && apiErr.Code == http.StatusForbidden:
			notStarted = true
		}
	}
	if len(sent) == 0 {
		// Nothing went out, so the day's message is not spent.
		_ = opts.Quota.ReleaseOwnerNotice(context.WithoutCancel(ctx), id)
		if notStarted {
			return nil, &DeniedError{Message: noticeNotStarted}
		}
		return nil, &DeniedError{Message: noticeUnavailable}
	}
	return notifyResult{Sent: sent, Note: noticeSentNote}, nil
}

type ownerChat struct {
	id    int64
	label string
}

// pickOwners is the owner named by @username, or every owner. Usernames are
// matched against Telegram's own record of each owner's chat, never against
// a name anyone types: the ID is what makes an owner.
func pickOwners(ctx context.Context, opts NotifyOptions, named string) ([]ownerChat, error) {
	named = strings.TrimPrefix(strings.TrimSpace(named), "@")
	var owners []ownerChat
	for _, id := range opts.Owners {
		label := "owner"
		if chat, err := opts.Telegram.GetChat(ctx, id); err == nil && chat.Username != "" {
			label = "@" + chat.Username
			if named != "" && !strings.EqualFold(chat.Username, named) {
				continue
			}
		} else if named != "" {
			continue
		}
		owners = append(owners, ownerChat{id: id, label: label})
	}
	if len(owners) == 0 {
		return nil, &DeniedError{Message: noticeNotAnOwner}
	}
	return owners, nil
}

// noticeText is what an owner gets: where it came from, who asked, and
// their words.
func noticeText(ctx context.Context, opts NotifyOptions, caller Caller, words string) string {
	group := "guruh"
	if chat, err := opts.Telegram.GetChat(ctx, caller.ChatID); err == nil && chat.Title != "" {
		group = "«" + chat.Title + "» guruhi"
	}
	who := caller.Name
	if who == "" {
		who = "A'zo"
	}
	return "📩 " + group + "dan xabar\n" + who + " so'radi:\n" + cutRunes(words, maxNoticeRunes)
}

func (opts NotifyOptions) now() time.Time {
	if opts.Now != nil {
		return opts.Now()
	}
	return time.Now()
}

// startOfDayIn is the midnight that began the day of t in location.
func startOfDayIn(t time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}
	t = t.In(location)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, location)
}
