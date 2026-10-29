package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"telegram-ai-bot/internal/storage"
)

const (
	// A punishment lasts as long as a rule may; a mute it orders, a day.
	defaultPunishmentHours = maxRuleHours
	defaultMuteHours       = 24
	notPunisher            = "only the bot's owners and this group's creator may set punishments; say so"
)

// CreatorChecker asks Telegram whether a member created a group.
type CreatorChecker interface {
	IsGroupCreator(ctx context.Context, chatID, userID int64) (bool, error)
}

// PunishmentsOptions configures the punishments tool. Store and Creators
// are required.
type PunishmentsOptions struct {
	Store    RuleStore
	Creators CreatorChecker
	// BotID is the bot's own user ID: it is never punished.
	BotID    int64
	Location *time.Location
	Now      func() time.Time
}

type punishmentArgs struct {
	Action      string `json:"action"`
	PunishFor   string `json:"punish_for"`
	Punishment  string `json:"punishment"`
	MuteHours   int    `json:"mute_hours"`
	Hours       int    `json:"hours"`
	AboutMember bool   `json:"about_member"`
	ID          int64  `json:"id"`
}

// punishmentKinds are the rule kinds of punish_for.
var punishmentKinds = map[string]string{"swearing": storage.RuleSwearPunishment, "pictures": storage.RulePicturePunishment}

// PunishmentsTool lets the bot's owners and a group's creator order what
// the swear and picture guards do the next time, in code. The member is the
// one the asker replied to, never one the model names.
func PunishmentsTool(opts PunishmentsOptions) Tool {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	return Tool{
		Name: "punishments",
		Description: "The bot's owners and this group's creator only. What the guards do the next time, in code, without you (\"yana so'kinsa ban qil\", \"18+ tashasa ban ber\"). " +
			"set: punish_for swearing or pictures (a nude picture, which the images guard deletes), punishment ban or mute (mute_hours, default 24), for the member the asker replied to (about_member true) or anyone in the group; hours, default 720. For one member it holds for any swearing; for anyone, for insults only. " +
			"list: the punishments here; remove: one by its id from list. Never say one is set unless this tool reported it.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["set", "list", "remove"]},
				"punish_for": {"type": "string", "enum": ["swearing", "pictures"], "description": "For set: what it is for."},
				"punishment": {"type": "string", "enum": ["ban", "mute"], "description": "For set."},
				"mute_hours": {"type": "integer", "description": "For set with mute: how long, 1 to 720 hours."},
				"hours": {"type": "integer", "description": "For set: how long the order lasts, 1 to 720 hours."},
				"about_member": {"type": "boolean", "description": "For set: true when it is for the member the asker replied to."},
				"id": {"type": "integer", "description": "For remove: its id from list."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		GroupOnly: true,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return runPunishments(ctx, opts, raw)
		},
	}
}

func runPunishments(ctx context.Context, opts PunishmentsOptions, raw json.RawMessage) (any, error) {
	var args punishmentArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, ok := CallerFrom(ctx)
	if !ok || caller.ChatID >= 0 {
		return nil, &DeniedError{Message: groupRuleOnly}
	}
	if !caller.Owner {
		creator, err := opts.Creators.IsGroupCreator(ctx, caller.ChatID, caller.UserID)
		if err != nil {
			return nil, errors.New("could not learn from Telegram who created this group; try again later")
		}
		if !creator {
			return nil, &DeniedError{Message: notPunisher}
		}
	}
	switch args.Action {
	case "set":
		return setPunishment(ctx, opts, caller, args)
	case "list":
		return listPunishments(ctx, opts, caller)
	case "remove":
		return removePunishment(ctx, opts, caller, args.ID)
	}
	return nil, &ArgumentError{Message: "action must be set, list or remove"}
}

// setPunishment saves what a guard does the next time, for one member or
// anyone in the group. The asker's own words must ask for the ban or the
// mute, as for moderate: the model cannot order one from the chat.
func setPunishment(ctx context.Context, opts PunishmentsOptions, caller Caller, args punishmentArgs) (any, error) {
	kind, ok := punishmentKinds[args.PunishFor]
	if !ok {
		return nil, &ArgumentError{Message: "punish_for must be swearing or pictures"}
	}
	var p storage.Punishment
	switch args.Punishment {
	case "ban":
		if !askedFor(caller.Words, actionBan) {
			return nil, &DeniedError{Message: notAsked}
		}
	case "mute":
		if !askedFor(caller.Words, actionMute) {
			return nil, &DeniedError{Message: notAsked}
		}
		p.MuteHours = cmp.Or(args.MuteHours, defaultMuteHours)
		if p.MuteHours < 1 || p.MuteHours > maxRuleHours {
			return nil, &ArgumentError{Message: fmt.Sprintf("mute_hours must be 1 to %d", maxRuleHours)}
		}
	default:
		return nil, &ArgumentError{Message: "punishment must be ban or mute"}
	}
	hours := cmp.Or(args.Hours, defaultPunishmentHours)
	if hours < 1 || hours > maxRuleHours {
		return nil, &ArgumentError{Message: fmt.Sprintf("hours must be 1 to %d", maxRuleHours)}
	}
	until := opts.Now().Add(time.Duration(hours) * time.Hour)
	rule := storage.GroupRule{ChatID: caller.ChatID, Kind: kind, Text: p.String(), SetBy: caller.UserID, ExpiresAt: until}
	who := "anyone in the group"
	if args.AboutMember {
		member := instructionSubject(opts.BotID, caller)
		if member == nil {
			return nil, &ArgumentError{Message: noSubject}
		}
		if member.Owner {
			return nil, &ArgumentError{Message: "the bot's owners are never punished"}
		}
		rule.UserID, rule.UserName, who = member.UserID, member.Name, member.Name
	}
	id, err := opts.Store.SetGroupRule(ctx, rule)
	if err != nil {
		return nil, err
	}
	note := "On now, in code, without you: the next swearing by them gets this instead of the guard's warning or mute."
	if kind == storage.RulePicturePunishment {
		note = "On now, in code, without you: after the images guard deletes a nude picture from them, this follows. It needs the images guard on here."
	} else if rule.UserID == 0 {
		note = "On now, in code, without you: the next insult at a person or at you gets this; exclamations only the guard's warning."
	}
	return map[string]any{"saved": true, "id": id, "for": args.PunishFor, "punishment": p.String(), "who": who,
		"until": until.In(opts.Location).Format(ruleTimeLayout), "note": note + " An admin cannot be banned or muted by a bot: they are named instead."}, nil
}

func listPunishments(ctx context.Context, opts PunishmentsOptions, caller Caller) (any, error) {
	rules, err := groupPunishments(ctx, opts, caller.ChatID)
	if err != nil {
		return nil, err
	}
	list := make([]ruleEntry, 0, len(rules))
	for _, r := range rules {
		list = append(list, ruleEntry{ID: r.ID, Kind: r.Kind, Text: r.Text, Member: cutRunes(r.UserName, maxListedNameRunes),
			Until: r.ExpiresAt.In(opts.Location).Format(ruleTimeLayout)})
	}
	return map[string]any{"punishments": list}, nil
}

// removePunishment deletes one of the group's punishments by its id; no
// other rule can be removed here.
func removePunishment(ctx context.Context, opts PunishmentsOptions, caller Caller, id int64) (any, error) {
	if id <= 0 {
		return nil, &ArgumentError{Message: "id is required; take it from list"}
	}
	rules, err := groupPunishments(ctx, opts, caller.ChatID)
	if err != nil {
		return nil, err
	}
	for _, r := range rules {
		if r.ID == id {
			removed, err := opts.Store.RemoveGroupRule(ctx, caller.ChatID, id)
			if err != nil {
				return nil, err
			}
			return map[string]bool{"removed": removed}, nil
		}
	}
	return map[string]bool{"removed": false}, nil
}

func groupPunishments(ctx context.Context, opts PunishmentsOptions, chatID int64) ([]storage.GroupRule, error) {
	rules, err := opts.Store.GroupRules(ctx, chatID)
	if err != nil {
		return nil, err
	}
	var punishments []storage.GroupRule
	for _, r := range rules {
		if r.ChatID == chatID && (r.Kind == storage.RuleSwearPunishment || r.Kind == storage.RulePicturePunishment) {
			punishments = append(punishments, r)
		}
	}
	return punishments, nil
}
