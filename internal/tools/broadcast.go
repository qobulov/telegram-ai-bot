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
)

const (
	maxBroadcastRunes = 4000
	// broadcastMaxAge is how long a draft waits for the owner's yes.
	broadcastMaxAge  = 10 * time.Minute
	broadcastTimeout = 30 * time.Second
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

// GroupSender posts a message in a group.
type GroupSender interface {
	SendToGroup(ctx context.Context, chatID int64, text string) error
}

// BroadcastOptions configures the send_to_groups tool.
type BroadcastOptions struct {
	Groups GroupLister
	Drafts BroadcastDrafts
	Sender GroupSender
}

type broadcastArgs struct {
	Action  string  `json:"action"`
	Text    string  `json:"text"`
	ChatIDs []int64 `json:"chat_ids"`
}

// BroadcastTool lets an owner post a message in the bot's groups. Nothing is
// sent until the owner confirms in a later message, which the code checks:
// the model cannot confirm in the message that asked for it.
func BroadcastTool(opts BroadcastOptions) Tool {
	return Tool{
		Name: "send_to_groups",
		Description: "Owners only. Post a message in the bot's groups. " +
			"prepare: save the text (and optionally chat_ids from bot_database; default all groups the bot is in), then show the owner the exact text and the groups and ask them to confirm. Nothing is sent yet. " +
			"confirm: send it, only after the owner said yes in a new message. cancel: drop the draft. " +
			"It is for announcements: a message that calls or pings a person (\"@pronium sizni @tursunoffm chaqiryapti\") is refused, as it would tag them in every group; tell the owner to write to that person directly. " +
			"Never say a message was sent unless confirm reported it.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["prepare", "confirm", "cancel"]},
				"text": {"type": "string", "description": "For prepare: the exact message to post, in the owner's language."},
				"chat_ids": {"type": "array", "items": {"type": "integer"}, "description": "For prepare: only these groups. Omit for all."}
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
	case "prepare":
		return prepareBroadcast(ctx, opts, caller, args)
	case "confirm":
		return confirmBroadcast(ctx, opts, caller)
	case "cancel":
		cancelled, err := opts.Drafts.CancelBroadcast(ctx, caller.UserID)
		return map[string]bool{"cancelled": cancelled}, err
	default:
		return nil, &ArgumentError{Message: "action must be prepare, confirm or cancel"}
	}
}

func prepareBroadcast(ctx context.Context, opts BroadcastOptions, caller Caller, args broadcastArgs) (any, error) {
	text := strings.TrimSpace(args.Text)
	if text == "" || len([]rune(text)) > maxBroadcastRunes {
		return nil, &ArgumentError{Message: fmt.Sprintf("text must be 1 to %d characters", maxBroadcastRunes)}
	}
	if summons(text) {
		return nil, &ArgumentError{Message: notASummons}
	}
	targets, err := broadcastTargets(ctx, opts.Groups, args.ChatIDs)
	if err != nil {
		return nil, err
	}
	chatIDs := make([]int64, len(targets))
	titles := make([]string, len(targets))
	for i, g := range targets {
		chatIDs[i], titles[i] = g.ChatID, groupName(g)
	}
	draft := storage.Broadcast{OwnerID: caller.UserID, UpdateID: caller.UpdateID, Text: text, ChatIDs: chatIDs}
	if err := opts.Drafts.SaveBroadcast(ctx, draft); err != nil {
		return nil, err
	}
	return map[string]any{
		"draft":  true,
		"text":   text,
		"groups": titles,
		"note":   "Nothing is sent yet. Show the owner this exact text and these groups and ask them to confirm. Call confirm only after they say yes in their next message.",
	}, nil
}

// broadcastTargets is every group the bot is in, or the chosen ones, which
// must all be among them.
func broadcastTargets(ctx context.Context, lister GroupLister, chosen []int64) ([]storage.GroupSummary, error) {
	groups, err := lister.Groups(ctx, time.Now(), maxGroupsListed)
	if err != nil {
		return nil, err
	}
	var targets []storage.GroupSummary
	for _, g := range groups {
		in := Membership{Status: g.BotStatus}.in()
		if in && (len(chosen) == 0 || slices.Contains(chosen, g.ChatID)) {
			targets = append(targets, g)
		}
	}
	if len(chosen) > 0 && len(targets) != len(chosen) {
		return nil, &ArgumentError{Message: "some chat_ids are not groups the bot is in; take them from bot_database groups"}
	}
	if len(targets) == 0 {
		return nil, &ArgumentError{Message: "the bot is in no known groups"}
	}
	return targets, nil
}

func groupName(g storage.GroupSummary) string {
	if g.Title != "" {
		return g.Title
	}
	return fmt.Sprint(g.ChatID)
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
	if groups, err := opts.Groups.Groups(ctx, time.Now(), maxGroupsListed); err == nil {
		for _, g := range groups {
			names[g.ChatID] = groupName(g)
		}
	}
	sent, failed := 0, []string{}
	for _, chatID := range draft.ChatIDs {
		if err := opts.Sender.SendToGroup(ctx, chatID, draft.Text); err != nil {
			name := names[chatID]
			if name == "" {
				name = fmt.Sprint(chatID)
			}
			failed = append(failed, name)
			continue
		}
		sent++
	}
	return map[string]any{"sent": sent, "failed": failed}, nil
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
