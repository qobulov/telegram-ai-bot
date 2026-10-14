package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// GroupSender posts a message in a group, or copies one there.
type GroupSender interface {
	SendToGroup(ctx context.Context, chatID int64, text string) error
	CopyToGroup(ctx context.Context, chatID, fromChatID, messageID int64) error
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
	Copy    string  `json:"copy"`
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
			"To post a voice message, photo, video or any other message as it is, use copy \"replied\" instead of text: it copies the message the owner's current message replies to. If they did not reply to one, ask them to reply to it with their request. " +
			"confirm: send it, only after the owner said yes in a new message. cancel: drop the draft. " +
			"Never say a message was sent unless confirm reported it.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["prepare", "confirm", "cancel"]},
				"text": {"type": "string", "description": "For prepare: the exact message to post, in the owner's language."},
				"copy": {"type": "string", "enum": ["replied"], "description": "For prepare, instead of text: copy the message the owner replied to."},
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
	draft, preview, err := broadcastContent(caller, args)
	if err != nil {
		return nil, err
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
	draft.OwnerID, draft.UpdateID, draft.ChatIDs = caller.UserID, caller.UpdateID, chatIDs
	if err := opts.Drafts.SaveBroadcast(ctx, draft); err != nil {
		return nil, err
	}
	return map[string]any{
		"draft":  true,
		"post":   preview,
		"groups": titles,
		"note":   "Nothing is sent yet. Show the owner what will be posted and these groups and ask them to confirm. Call confirm only after they say yes in their next message.",
	}, nil
}

// broadcastContent is what the draft posts: the given text, or a copy of the
// message the owner replied to. preview describes it for the owner.
func broadcastContent(caller Caller, args broadcastArgs) (draft storage.Broadcast, preview string, err error) {
	text := strings.TrimSpace(args.Text)
	switch args.Copy {
	case "":
		if text == "" || len([]rune(text)) > maxBroadcastRunes {
			return draft, "", &ArgumentError{Message: fmt.Sprintf("text must be 1 to %d characters, or use copy", maxBroadcastRunes)}
		}
		return storage.Broadcast{Text: text}, text, nil
	case "replied":
		if text != "" {
			return draft, "", &ArgumentError{Message: "give either text or copy, not both"}
		}
		if caller.ReplyToMessageID == 0 {
			return draft, "", &ArgumentError{Message: "there is no message to copy: ask the owner to reply to the voice message, photo or message they want posted, with their request"}
		}
		return storage.Broadcast{FromChatID: caller.ChatID, MessageID: caller.ReplyToMessageID}, "a copy of the message the owner replied to", nil
	default:
		return draft, "", &ArgumentError{Message: `copy must be "replied"`}
	}
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
		if err := post(ctx, opts.Sender, chatID, draft); err != nil {
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

func post(ctx context.Context, sender GroupSender, chatID int64, draft storage.Broadcast) error {
	if draft.Text == "" {
		return sender.CopyToGroup(ctx, chatID, draft.FromChatID, draft.MessageID)
	}
	return sender.SendToGroup(ctx, chatID, draft.Text)
}
