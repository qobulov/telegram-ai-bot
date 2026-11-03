package tools

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
)

// leaveWords ask the bot to leave: "chiqib ket", "guruhdan chiq", "tark
// et", "выйди", "уходи", "leave", "go away".
var leaveWords = []string{
	"chiq", "чиқ", "чик", "ket", "кет", "tark", "тарк",
	"выйд", "выйт", "уйд", "уход", "покин",
	"leave", "exit", "go away", "get out",
}

// silentAnswer ends a turn with nothing sent, as agent.SilentMarker does;
// the agent's package cannot be imported here.
const silentAnswer = "[silent]"

// Leaver posts in a chat and leaves it.
type Leaver interface {
	SendToGroup(ctx context.Context, chatID int64, text string) error
	LeaveChat(ctx context.Context, chatID int64) error
}

// LeaveOptions configures the leave_chat tool. Channels is optional:
// without it only groups can be left.
type LeaveOptions struct {
	Groups   GroupLister
	Channels ChannelLister
	Telegram Leaver
}

type leaveArgs struct {
	ChatID int64 `json:"chat_id"`
}

// leaveTimeout covers the goodbye and leaving.
const leaveTimeout = 15 * time.Second

const (
	leaveNotAsked = "the owner's own words do not ask the bot to leave; it stays. Leave only when they say so, as \"bu guruhdan chiqib ket\" does"
	leaveNoChat   = "say which group or channel to leave: chat_id from send_to_groups targets, or ask in that group itself"
	leaveUnknown  = "chat_id is no group the bot is in or channel where it is an admin; take it from send_to_groups targets"
	leaveGoodbye  = "👋 Xayr! Egamning buyrug'i bilan guruhdan chiqyapman."
)

// LeaveTool lets an owner have the bot leave a group or a channel: the
// group it is asked in, or one named by its chat_id. The owner's own words
// must ask for it, so the model cannot leave on its own; in the group it
// is asked in, the bot says goodbye before it goes and writes nothing
// after.
func LeaveTool(opts LeaveOptions) Tool {
	return Tool{
		Name: "leave_chat",
		Description: "Owners only. Make the bot leave a group or a channel, when the owner asks it to (\"bu guruhdan chiqib ket\", \"X kanaldan chiq\"). " +
			"Without chat_id it leaves the group it is asked in, saying goodbye itself: then write nothing more. " +
			"With chat_id, from send_to_groups targets, it leaves that group or channel; then tell the owner it did. " +
			"Never call it unless the owner asked to leave in their own message.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"chat_id": {"type": "integer", "description": "The group or channel to leave, from send_to_groups targets. Omit it to leave the group the owner asks in."}
			},
			"additionalProperties": false
		}`),
		OwnerOnly: true,
		Timeout:   leaveTimeout,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return runLeave(ctx, opts, raw)
		},
	}
}

func runLeave(ctx context.Context, opts LeaveOptions, raw json.RawMessage) (any, error) {
	var args leaveArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, ok := CallerFrom(ctx)
	switch {
	case !ok || !caller.Owner:
		return nil, errors.New("only the bot's owners may make it leave")
	case !askedToLeave(caller.Words):
		return nil, &DeniedError{Message: leaveNotAsked}
	}
	here := args.ChatID == 0 || args.ChatID == caller.ChatID
	chatID := args.ChatID
	if here {
		chatID = caller.ChatID
	}
	if chatID >= 0 {
		return nil, &ArgumentError{Message: leaveNoChat}
	}
	if !here {
		known, err := leavable(ctx, opts, chatID)
		if err != nil {
			return nil, internal(err)
		}
		if !known {
			return nil, &ArgumentError{Message: leaveUnknown}
		}
	}
	if here {
		// The answer could not reach a chat the bot has left: the goodbye
		// goes first, and the turn ends with nothing sent.
		_ = opts.Telegram.SendToGroup(ctx, chatID, leaveGoodbye)
	}
	if err := opts.Telegram.LeaveChat(ctx, chatID); err != nil {
		return nil, internal(err)
	}
	if here {
		if sink, ok := SinkFrom(ctx); ok {
			sink.Finish(silentAnswer)
		}
	}
	return map[string]any{"left": chatID}, nil
}

// leavable reports a group the bot is in or a channel where it is an admin.
func leavable(ctx context.Context, opts LeaveOptions, chatID int64) (bool, error) {
	groups, err := opts.Groups.Groups(ctx, time.Now(), maxGroupsListed)
	if err != nil {
		return false, err
	}
	inGroup := func(g storage.GroupSummary) bool {
		return g.ChatID == chatID && (Membership{Status: g.BotStatus}).in()
	}
	if slices.ContainsFunc(groups, inGroup) {
		return true, nil
	}
	if opts.Channels == nil {
		return false, nil
	}
	channels, err := opts.Channels.Channels(ctx, maxGroupsListed)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(channels, func(c storage.ChannelSummary) bool {
		return c.ChatID == chatID && c.BotStatus == "administrator"
	}), nil
}

// askedToLeave reports whether words, what the owner wrote themselves, ask
// the bot to leave.
func askedToLeave(words string) bool {
	folded := foldApostrophes.Replace(strings.ToLower(words))
	return slices.ContainsFunc(leaveWords, func(word string) bool { return strings.Contains(folded, word) })
}
