package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/storage"
)

const (
	maxInstructionRunes = 300
	maxAddressRunes     = 40
	maxRuleHours        = 720
	// An instruction lasts a week unless the owner says otherwise; ignoring
	// someone, a day.
	defaultInstructionHours = 7 * 24
	defaultIgnoreHours      = 24
	ruleTimeLayout          = "2 Jan 15:04"
	cannotIgnore            = "admins, the bot's owners and the bot itself cannot be ignored"
	groupRuleOnly           = "this works only in the group the rule is for; ask the owner to say it there. In a private chat only set_address, list and remove work"
)

// RuleStore keeps the owners' standing rules.
type RuleStore interface {
	SetGroupRule(ctx context.Context, r storage.GroupRule) (int64, error)
	GroupRules(ctx context.Context, chatID int64) ([]storage.GroupRule, error)
	RemoveGroupRule(ctx context.Context, chatID, id int64) (bool, error)
	RemoveIgnore(ctx context.Context, chatID, userID int64) (bool, error)
}

// AdminChecker asks Telegram whether a member administers a group.
type AdminChecker interface {
	IsGroupAdmin(ctx context.Context, chatID, userID int64) (bool, error)
}

// RulesOptions configures the group_rules tool.
type RulesOptions struct {
	Store  RuleStore
	Admins AdminChecker
	// BotID is the bot's own user ID: it never ignores itself.
	BotID int64
	// Location is the time zone of the times the tool reports.
	Location *time.Location
	Now      func() time.Time
}

type rulesArgs struct {
	Action string `json:"action"`
	Text   string `json:"text"`
	Hours  int    `json:"hours"`
	ID     int64  `json:"id"`
}

// RulesTool lets an owner give the bot orders that last: they are stored
// apart from the conversation, so they survive /mute, /reset and the end
// of the day, and every turn in the group is told them. Whom to ignore is
// the member the owner replied to, taken from the update: the model cannot
// name anyone, and admins and owners cannot be ignored.
func RulesTool(opts RulesOptions) Tool {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	return Tool{
		Name: "group_rules",
		Description: "Owners only. Standing rules for the group you are in, kept until they expire, even after /mute or /reset. " +
			"add_instruction: save an owner's order meant to last (text; hours, default 168). " +
			"ignore: stop answering the member whose message the owner replied to (hours, default 24); never admins or owners. unignore: answer that member again. " +
			"set_address: how to address the owner who is asking, in every chat (text). " +
			"list: the rules in force here. remove: delete a rule by its id from list. " +
			"add_instruction, ignore and unignore work only in a group. Never say a rule is saved unless this tool reported it.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["add_instruction", "ignore", "unignore", "set_address", "list", "remove"]},
				"text": {"type": "string", "description": "For add_instruction: the order, at most 300 characters. For set_address: the form of address, at most 40 characters."},
				"hours": {"type": "integer", "description": "For add_instruction and ignore: how long the rule lasts, 1 to 720."},
				"id": {"type": "integer", "description": "For remove: the rule's id from list."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		OwnerOnly: true,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return runRules(ctx, opts, raw)
		},
	}
}

func runRules(ctx context.Context, opts RulesOptions, raw json.RawMessage) (any, error) {
	var args rulesArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, ok := CallerFrom(ctx)
	if !ok || !caller.Owner {
		return nil, errors.New("only the bot's owners may set standing rules")
	}
	switch args.Action {
	case "add_instruction":
		return addInstruction(ctx, opts, caller, args)
	case "ignore":
		return ignoreMember(ctx, opts, caller, args)
	case "unignore":
		return unignoreMember(ctx, opts, caller)
	case "set_address":
		return setAddress(ctx, opts, caller, args)
	case "list":
		return listRules(ctx, opts, caller)
	case "remove":
		return removeRule(ctx, opts, caller, args)
	default:
		return nil, &ArgumentError{Message: "action must be add_instruction, ignore, unignore, set_address, list or remove"}
	}
}

func addInstruction(ctx context.Context, opts RulesOptions, caller Caller, args rulesArgs) (any, error) {
	if caller.ChatID >= 0 {
		return nil, &ArgumentError{Message: groupRuleOnly}
	}
	text, err := ruleText(args.Text, maxInstructionRunes)
	if err != nil {
		return nil, err
	}
	until, err := ruleExpiry(opts, args.Hours, defaultInstructionHours)
	if err != nil {
		return nil, err
	}
	id, err := opts.Store.SetGroupRule(ctx, storage.GroupRule{
		ChatID: caller.ChatID, Kind: storage.RuleInstruction, Text: text, SetBy: caller.UserID, ExpiresAt: until,
	})
	if errors.Is(err, storage.ErrTooManyRules) {
		return nil, &ArgumentError{Message: fmt.Sprintf("this group already has %d standing instructions; remove one first", storage.MaxInstructions)}
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"saved": true, "id": id, "until": until.In(opts.Location).Format(ruleTimeLayout)}, nil
}

func ignoreMember(ctx context.Context, opts RulesOptions, caller Caller, args rulesArgs) (any, error) {
	if caller.ChatID >= 0 {
		return nil, &ArgumentError{Message: groupRuleOnly}
	}
	until, err := ruleExpiry(opts, args.Hours, defaultIgnoreHours)
	if err != nil {
		return nil, err
	}
	target, err := ignorable(ctx, opts, caller)
	if err != nil {
		return nil, err
	}
	id, err := opts.Store.SetGroupRule(ctx, storage.GroupRule{
		ChatID: caller.ChatID, Kind: storage.RuleIgnore, UserID: target.UserID, UserName: target.Name,
		SetBy: caller.UserID, ExpiresAt: until,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"saved": true, "id": id, "ignored": target.Name, "until": until.In(opts.Location).Format(ruleTimeLayout)}, nil
}

// ignorable is the member the owner replied to, if the bot may ignore them.
// When Telegram cannot say whether they are an admin, the answer is no.
func ignorable(ctx context.Context, opts RulesOptions, caller Caller) (*Target, error) {
	target := caller.Target
	if target == nil {
		return nil, &ArgumentError{Message: "the owner must reply to a message of the member to ignore, and ask again"}
	}
	if target.UserID == opts.BotID || target.Owner {
		return nil, &ArgumentError{Message: cannotIgnore}
	}
	admin, err := opts.Admins.IsGroupAdmin(ctx, caller.ChatID, target.UserID)
	if err != nil {
		return nil, errors.New("could not learn from Telegram whether this member is an admin; try again later")
	}
	if admin {
		return nil, &ArgumentError{Message: cannotIgnore}
	}
	return target, nil
}

func unignoreMember(ctx context.Context, opts RulesOptions, caller Caller) (any, error) {
	if caller.ChatID >= 0 {
		return nil, &ArgumentError{Message: groupRuleOnly}
	}
	if caller.Target == nil {
		return nil, &ArgumentError{Message: "the owner must reply to a message of the member to answer again, and ask again"}
	}
	removed, err := opts.Store.RemoveIgnore(ctx, caller.ChatID, caller.Target.UserID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"removed": removed, "member": caller.Target.Name}, nil
}

// setAddress saves how the bot addresses the calling owner. It holds in
// every chat, so it is stored for chat 0.
func setAddress(ctx context.Context, opts RulesOptions, caller Caller, args rulesArgs) (any, error) {
	text, err := ruleText(args.Text, maxAddressRunes)
	if err != nil {
		return nil, err
	}
	id, err := opts.Store.SetGroupRule(ctx, storage.GroupRule{
		ChatID: 0, Kind: storage.RuleAddress, UserID: caller.UserID, Text: text, SetBy: caller.UserID,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"saved": true, "id": id, "address": text}, nil
}

type ruleEntry struct {
	ID     int64  `json:"id"`
	Kind   string `json:"kind"`
	Text   string `json:"text,omitempty"`
	Member string `json:"member,omitempty"`
	Until  string `json:"until,omitempty"`
}

// listRules lists the chat's rules and the calling owner's form of address.
// Another owner's form of address is theirs, and left out.
func listRules(ctx context.Context, opts RulesOptions, caller Caller) (any, error) {
	rules, err := opts.Store.GroupRules(ctx, caller.ChatID)
	if err != nil {
		return nil, err
	}
	entries := make([]ruleEntry, 0, len(rules))
	for _, r := range rules {
		if r.Kind == storage.RuleAddress && r.UserID != caller.UserID {
			continue
		}
		entry := ruleEntry{ID: r.ID, Kind: r.Kind, Text: r.Text}
		if r.Kind == storage.RuleIgnore {
			entry.Member = r.UserName
		}
		if !r.ExpiresAt.IsZero() {
			entry.Until = r.ExpiresAt.In(opts.Location).Format(ruleTimeLayout)
		}
		entries = append(entries, entry)
	}
	return map[string]any{"rules": entries}, nil
}

func removeRule(ctx context.Context, opts RulesOptions, caller Caller, args rulesArgs) (any, error) {
	if args.ID <= 0 {
		return nil, &ArgumentError{Message: "id is required; take it from list"}
	}
	removed, err := opts.Store.RemoveGroupRule(ctx, caller.ChatID, args.ID)
	if err != nil {
		return nil, err
	}
	return map[string]bool{"removed": removed}, nil
}

// ruleText trims text and checks it has 1 to limit characters.
func ruleText(text string, limit int) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > limit {
		return "", &ArgumentError{Message: fmt.Sprintf("text must be 1 to %d characters", limit)}
	}
	return text, nil
}

// ruleExpiry is when a rule lasting hours ends; zero hours means
// defaultHours.
func ruleExpiry(opts RulesOptions, hours, defaultHours int) (time.Time, error) {
	if hours == 0 {
		hours = defaultHours
	}
	if hours < 1 || hours > maxRuleHours {
		return time.Time{}, &ArgumentError{Message: fmt.Sprintf("hours must be 1 to %d", maxRuleHours)}
	}
	return opts.Now().Add(time.Duration(hours) * time.Hour), nil
}
