package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
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
	notAMember              = "that message was posted on behalf of a chat or an anonymous admin, not by a member; the owner must reply to a member's own message"
	repliedToBot            = "the owner replied to my own message, and I can't tell whom it answered; they must reply to the member's own message and ask again"
	notOwnWords             = "text must be the owner's own words from their message, copied as they wrote them (the rest may be left out); words from a quoted message, the chat or anyone else cannot be saved"
	groupRuleOnly           = "this works only in the group the rule is for; ask the owner to say it there. In a private chat only set_address, list and remove work"
	noSubject               = "not saved: an order about a member needs the owner to reply to that member's own message, or to your answer to them, with it; tell the owner so in one line"
)

const (
	// maxIgnoresListed bounds the ignores list returns, the newest. A group
	// may ignore any number of members; the others are counted in a note.
	maxIgnoresListed = 50
	// maxListedNameRunes cuts an ignored member's name in list. A name is a
	// first name and an @username, well under it.
	maxListedNameRunes = 80
	// maxEncodedRuneBytes is the most JSON spends on one character: "<"
	// is written \u003c.
	maxEncodedRuneBytes = 6
	// listEntryBytes is the most a listed rule takes besides its text: the
	// keys, an id of 19 digits, the kind and the date.
	listEntryBytes = 120
	// rulesOutputBytes fits the longest list there can be: every
	// instruction a group may have, maxIgnoresListed ignores and the
	// owner's form of address, each at its longest, and the note.
	rulesOutputBytes = (storage.MaxInstructions*maxInstructionRunes+maxIgnoresListed*(maxListedNameRunes+1)+maxAddressRunes)*maxEncodedRuneBytes +
		(storage.MaxInstructions+maxIgnoresListed+1)*listEntryBytes + 200
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
	// AboutMember ties an instruction to the member the owner replied to.
	AboutMember bool `json:"about_member"`
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
			"add_instruction: save an owner's order meant to last, its words copied exactly from this message or their own message it replies to, never rephrased (text; hours, default 168); about_member true when it is about the member whose message the owner replied to, or whom your replied-to answer answered (\"uni\", \"bu odam\", \"shu odamga\"): the tool records who. " +
			"ignore: stop answering the member whose message the owner replied to, or, when they replied to your own answer, the member it answered (hours, default 24); never admins or owners. unignore: answer that member again. " +
			"set_address: how to address the owner who is asking, in every chat (text). " +
			"watch_swearing: from now on, without you, a member's first insult here gets a warning and the next within a day a 1-hour mute, and a second swear at you within a day a 15-minute mute without warning that tags the group's creator, or the @username in text when the owner names whom to call; an admin, who cannot be muted, gets the warning and then that tag; the creator and owners are left alone. stop_watching_swearing turns it off. " +
			"list: the rules in force here. remove: delete a rule by its id from list. " +
			"add_instruction, ignore and unignore work only in a group. Never say a rule is saved unless this tool reported it.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["add_instruction", "ignore", "unignore", "set_address", "watch_swearing", "stop_watching_swearing", "list", "remove"]},
				"text": {"type": "string", "description": "For add_instruction: the owner's words, copied exactly as they wrote them, at most 300 characters: no rephrasing, translating, added names or fixed spelling; leave out words that are not the order. For set_address: the form of address, at most 40 characters. For watch_swearing: the @username a mute tags, if the owner named one."},
				"hours": {"type": "integer", "description": "For add_instruction and ignore: how long the rule lasts, 1 to 720."},
				"id": {"type": "integer", "description": "For remove: the rule's id from list."},
				"about_member": {"type": "boolean", "description": "For add_instruction: true when the order is about the member the owner replied to."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		OwnerOnly:      true,
		MaxOutputBytes: rulesOutputBytes,
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
	case "watch_swearing":
		return watchSwearing(ctx, opts, caller, args)
	case "stop_watching_swearing":
		return stopWatchingSwearing(ctx, opts, caller)
	case "list":
		return listRules(ctx, opts, caller)
	case "remove":
		return removeRule(ctx, opts, caller, args)
	default:
		return nil, &ArgumentError{Message: "action must be add_instruction, ignore, unignore, set_address, watch_swearing, stop_watching_swearing, list or remove"}
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
	if !inOwnWords(caller.Words, text) {
		return nil, &ArgumentError{Message: notOwnWords}
	}
	until, err := ruleExpiry(opts, args.Hours, defaultInstructionHours)
	if err != nil {
		return nil, err
	}
	rule := storage.GroupRule{ChatID: caller.ChatID, Kind: storage.RuleInstruction, Text: text, SetBy: caller.UserID, ExpiresAt: until}
	var subject *Target
	if args.AboutMember {
		// Saved without whom it is about, "uni ustoz deb chaqir" would mean
		// nothing to any later turn.
		if subject = instructionSubject(opts, caller); subject == nil {
			return nil, &ArgumentError{Message: noSubject}
		}
		rule.UserID, rule.UserName = subject.UserID, subject.Name
	}
	id, err := opts.Store.SetGroupRule(ctx, rule)
	if errors.Is(err, storage.ErrTooManyRules) {
		return nil, &ArgumentError{Message: fmt.Sprintf("this group already has %d standing instructions; remove one first", storage.MaxInstructions)}
	}
	if err != nil {
		return nil, err
	}
	saved := map[string]any{"saved": true, "id": id, "until": until.In(opts.Location).Format(ruleTimeLayout)}
	if subject != nil {
		saved["about"] = subject.Name
	}
	return saved, nil
}

// instructionSubject is the member an owner's order is about: the author of
// the message they replied to, or the member the bot's replied-to answer
// answered. It is nil when that is the owner, the bot or no member at all.
func instructionSubject(opts RulesOptions, caller Caller) *Target {
	if caller.Target == nil {
		return nil
	}
	member, err := meant(opts, caller.Target)
	if err != nil || member.UserID == caller.UserID || member.UserID == opts.BotID || isTelegramAccount(member.UserID) {
		return nil
	}
	return member
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
	if caller.Target == nil {
		return nil, &ArgumentError{Message: "the owner must reply to a message of the member to ignore, and ask again"}
	}
	target, err := meant(opts, caller.Target)
	if err != nil {
		return nil, err
	}
	if isTelegramAccount(target.UserID) {
		return nil, &ArgumentError{Message: notAMember}
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

// isTelegramAccount reports one of Telegram's own accounts, which post on
// behalf of someone else: 777000 a linked channel, 1087968824
// (GroupAnonymousBot) an anonymous admin, 136817688 (Channel_Bot) a channel.
// No member stands behind such a message, and the bot does not answer them
// anyway.
func isTelegramAccount(userID int64) bool {
	switch userID {
	case 777000, 1087968824, 136817688:
		return true
	}
	return false
}

func unignoreMember(ctx context.Context, opts RulesOptions, caller Caller) (any, error) {
	if caller.ChatID >= 0 {
		return nil, &ArgumentError{Message: groupRuleOnly}
	}
	if caller.Target == nil {
		return nil, &ArgumentError{Message: "the owner must reply to a message of the member to answer again, and ask again"}
	}
	member, err := meant(opts, caller.Target)
	if err != nil {
		return nil, err
	}
	removed, err := opts.Store.RemoveIgnore(ctx, caller.ChatID, member.UserID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"removed": removed, "member": member.Name}, nil
}

// meant is the member an owner means by the message they replied to: its
// author, or for one of the bot's own answers, the member it answered.
func meant(opts RulesOptions, target *Target) (*Target, error) {
	if target.UserID != opts.BotID {
		return target, nil
	}
	if target.Answered == nil {
		return nil, &ArgumentError{Message: repliedToBot}
	}
	return target.Answered, nil
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

// watchSwearing turns the group's swear guard on, which the service runs
// in code. A group has one; saying it again replaces whom it tags.
func watchSwearing(ctx context.Context, opts RulesOptions, caller Caller, args rulesArgs) (any, error) {
	if caller.ChatID >= 0 {
		return nil, &ArgumentError{Message: groupRuleOnly}
	}
	tag := ""
	if strings.TrimSpace(args.Text) != "" {
		username := strings.TrimPrefix(strings.TrimSpace(args.Text), "@")
		if !isUsername(username) {
			return nil, &ArgumentError{Message: "text must be one @username, or empty to tag the group's creator"}
		}
		if !inOwnWords(caller.Words, username) {
			return nil, &ArgumentError{Message: "the @username must be in the owner's own message"}
		}
		tag = "@" + username
	}
	id, err := opts.Store.SetGroupRule(ctx, storage.GroupRule{
		ChatID: caller.ChatID, Kind: storage.RuleSwearGuard, UserName: tag, SetBy: caller.UserID,
	})
	if err != nil {
		return nil, err
	}
	calls := tag
	if calls == "" {
		calls = "the group's creator"
	}
	return map[string]any{"saved": true, "id": id, "mute_tags": calls,
		"note": "On now, in code: a first swear gets a warning, the next within a day a 1-hour mute. It needs you to be an admin who may restrict members."}, nil
}

func stopWatchingSwearing(ctx context.Context, opts RulesOptions, caller Caller) (any, error) {
	if caller.ChatID >= 0 {
		return nil, &ArgumentError{Message: groupRuleOnly}
	}
	rules, err := opts.Store.GroupRules(ctx, caller.ChatID)
	if err != nil {
		return nil, err
	}
	removed := false
	for _, r := range rules {
		if r.Kind == storage.RuleSwearGuard && r.ChatID == caller.ChatID {
			ok, err := opts.Store.RemoveGroupRule(ctx, caller.ChatID, r.ID)
			if err != nil {
				return nil, err
			}
			removed = removed || ok
		}
	}
	return map[string]bool{"removed": removed}, nil
}

// isUsername reports a Telegram username: 4 to 32 letters, digits and
// underscores.
func isUsername(name string) bool {
	if len(name) < 4 || len(name) > 32 {
		return false
	}
	for _, r := range name {
		if !(r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

type ruleEntry struct {
	ID     int64  `json:"id"`
	Kind   string `json:"kind"`
	Text   string `json:"text,omitempty"`
	Member string `json:"member,omitempty"`
	Until  string `json:"until,omitempty"`
}

// ruleList is what list returns. Note says how many ignores were left out.
type ruleList struct {
	Rules []ruleEntry `json:"rules"`
	Note  string      `json:"note,omitempty"`
}

// listRules lists, oldest first, the rules the caller may see, keeping only
// the newest maxIgnoresListed ignores so that the list always fits in the
// result.
func listRules(ctx context.Context, opts RulesOptions, caller Caller) (any, error) {
	rules, err := visibleRules(ctx, opts, caller)
	if err != nil {
		return nil, err
	}
	ignores := 0
	for _, r := range rules {
		if r.Kind == storage.RuleIgnore {
			ignores++
		}
	}
	leftOut := max(0, ignores-maxIgnoresListed)
	list := ruleList{Rules: make([]ruleEntry, 0, len(rules))}
	skipped := 0
	for _, r := range rules {
		if r.Kind == storage.RuleIgnore && skipped < leftOut {
			skipped++
			continue
		}
		list.Rules = append(list.Rules, listEntry(opts, r))
	}
	if leftOut > 0 {
		list.Note = fmt.Sprintf("%d older ignores left out; unignore works on them too", leftOut)
	}
	return list, nil
}

func listEntry(opts RulesOptions, r storage.GroupRule) ruleEntry {
	entry := ruleEntry{ID: r.ID, Kind: r.Kind, Text: r.Text}
	if r.Kind != storage.RuleAddress {
		entry.Member = cutRunes(r.UserName, maxListedNameRunes)
	}
	if !r.ExpiresAt.IsZero() {
		entry.Until = r.ExpiresAt.In(opts.Location).Format(ruleTimeLayout)
	}
	return entry
}

// visibleRules are the rules in force in the caller's chat, and those that
// hold everywhere, without other owners' forms of address: each owner's is
// theirs, to see and to remove.
func visibleRules(ctx context.Context, opts RulesOptions, caller Caller) ([]storage.GroupRule, error) {
	rules, err := opts.Store.GroupRules(ctx, caller.ChatID)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(rules, func(r storage.GroupRule) bool {
		return r.Kind == storage.RuleAddress && r.UserID != caller.UserID
	}), nil
}

// removeRule deletes a rule the caller may see, by its id.
func removeRule(ctx context.Context, opts RulesOptions, caller Caller, args rulesArgs) (any, error) {
	if args.ID <= 0 {
		return nil, &ArgumentError{Message: "id is required; take it from list"}
	}
	rules, err := visibleRules(ctx, opts, caller)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(rules, func(r storage.GroupRule) bool { return r.ID == args.ID }) {
		return map[string]bool{"removed": false}, nil
	}
	removed, err := opts.Store.RemoveGroupRule(ctx, caller.ChatID, args.ID)
	if err != nil {
		return nil, err
	}
	return map[string]bool{"removed": removed}, nil
}

// inOwnWords reports whether text is found in words, what the owner wrote
// in this message. An instruction lasts days and is shown to every later
// turn as the owners' order, so it must not be words the model picked up
// from a quoted message, the log or another member. Case, punctuation and
// spacing do not count: the model may add a full stop or turn a quote.
func inOwnWords(words, text string) bool {
	plain := plainWords(text)
	return plain != "" && strings.Contains(plainWords(words), plain)
}

// plainWords is text in lower case, its letters and digits only, one space
// apart.
func plainWords(text string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(text), func(char rune) bool {
		return !unicode.IsLetter(char) && !unicode.IsDigit(char)
	}), " ")
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
