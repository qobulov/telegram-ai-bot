package tools

import (
	"context"
	"encoding/json"
	"strings"

	"telegram-ai-bot/internal/storage"
)

const (
	maxCallNameRunes = 40

	profileNotOwnWords = "name must be how they asked to be called, copied from their own message; a name from a quote, the chat or your own idea cannot be saved"
	profileActions     = "action must be set_name, siz, sen, clear or show"
	profileSavedNote   = "Saved. It applies to this person in every group and chat from now on; say so in one short line."
	profileShownNote   = "This is what you remember about talking to them, in every chat."
	profileClearedNote = "Forgotten: you no longer use a name or siz with them unless they ask again."
)

// ProfileStore keeps each person's profile.
type ProfileStore interface {
	Profile(ctx context.Context, userID int64) (storage.UserProfile, error)
	SetProfile(ctx context.Context, p storage.UserProfile) error
}

// ProfileOptions configures the my_profile tool. Store is required.
type ProfileOptions struct {
	Store ProfileStore
}

type profileArgs struct {
	Action string `json:"action"`
	Name   string `json:"name"`
}

type profileView struct {
	CallName string `json:"call_name,omitempty"`
	Siz      bool   `json:"siz"`
}

type profileResult struct {
	Profile profileView `json:"profile"`
	Note    string      `json:"note"`
}

// ProfileTool lets a person have the bot remember how to talk to them in
// every chat: a name and "siz". It acts only on the person writing, so no
// one can set or read anyone else's, and it keeps nothing they said.
func ProfileTool(opts ProfileOptions) Tool {
	return Tool{
		Name: "my_profile",
		Description: "Remember how to talk to the person writing, in every group and chat: when they ask to be called by a name from now on, or to be spoken to with \"siz\" (or \"sen\" again). " +
			"set_name: name, copied from their own message. siz or sen: the form they want. clear: forget it. show: what you remember. Only for the person themselves, never for someone else.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["set_name", "siz", "sen", "clear", "show"]},
				"name": {"type": "string", "description": "For set_name: how they asked to be called."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return changeProfile(ctx, opts, raw)
		},
	}
}

func changeProfile(ctx context.Context, opts ProfileOptions, raw json.RawMessage) (any, error) {
	var args profileArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, ok := CallerFrom(ctx)
	if !ok || !caller.Human {
		return nil, &DeniedError{Message: "only a person can change their own profile"}
	}
	p, err := opts.Store.Profile(ctx, caller.UserID)
	if err != nil {
		return nil, internal(err)
	}
	note := profileSavedNote
	switch args.Action {
	case "show":
		return profileResult{Profile: profileView{CallName: p.CallName, Siz: p.Siz}, Note: profileShownNote}, nil
	case "set_name":
		name, err := ruleText(args.Name, maxCallNameRunes)
		if err != nil {
			return nil, err
		}
		if !inOwnWords(caller.Words, name) {
			return nil, &ArgumentError{Message: profileNotOwnWords}
		}
		// One line: the name goes into every later turn's header.
		p.CallName = strings.Join(strings.Fields(name), " ")
	case "siz":
		p.Siz = true
	case "sen":
		p.Siz = false
	case "clear":
		p = storage.UserProfile{UserID: caller.UserID}
		note = profileClearedNote
	default:
		return nil, &ArgumentError{Message: profileActions}
	}
	if err := opts.Store.SetProfile(ctx, p); err != nil {
		return nil, internal(err)
	}
	return profileResult{Profile: profileView{CallName: p.CallName, Siz: p.Siz}, Note: note}, nil
}
