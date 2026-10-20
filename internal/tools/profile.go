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
	profileNoMember    = "for_member needs an owner who replied to that member's message, or to your answer to them; ask the owner to reply to their message with it"
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
	// ForMember is an owner setting the profile of the member they replied to.
	ForMember bool `json:"for_member"`
}

type profileView struct {
	CallName string `json:"call_name,omitempty"`
	Siz      bool   `json:"siz"`
}

type profileResult struct {
	Member  string      `json:"member,omitempty"`
	Profile profileView `json:"profile"`
	Note    string      `json:"note"`
}

// ProfileTool lets a person have the bot remember how to talk to them in
// every chat: a name and "siz". It acts on the person writing, or, for an
// owner, on the member they replied to, so nobody else can set or read
// another's; it keeps nothing they said.
func ProfileTool(opts ProfileOptions) Tool {
	return Tool{
		Name: "my_profile",
		Description: "Remember how to talk to the person writing, in every group and chat: when they ask to be called by a name from now on, or to be spoken to with \"siz\" (or \"sen\" again). " +
			"set_name: name, copied from their own message. siz or sen: the form they want. clear: forget it. show: what you remember. " +
			"For the person themselves; only an owner may set it for someone else, with for_member true, for the member whose message they replied to or whom your replied-to answer answered (\"uni Ustoz deb chaqir\").",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["set_name", "siz", "sen", "clear", "show"]},
				"name": {"type": "string", "description": "For set_name: how they asked to be called."},
				"for_member": {"type": "boolean", "description": "Owners only: true to change the profile of the member the owner replied to."}
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
	whose, member, err := profileOwner(caller, args.ForMember)
	if err != nil {
		return nil, err
	}
	p, err := opts.Store.Profile(ctx, whose)
	if err != nil {
		return nil, internal(err)
	}
	note := profileSavedNote
	switch args.Action {
	case "show":
		return profileResult{Member: member, Profile: profileView{CallName: p.CallName, Siz: p.Siz}, Note: profileShownNote}, nil
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
		p = storage.UserProfile{UserID: whose}
		note = profileClearedNote
	default:
		return nil, &ArgumentError{Message: profileActions}
	}
	if err := opts.Store.SetProfile(ctx, p); err != nil {
		return nil, internal(err)
	}
	return profileResult{Member: member, Profile: profileView{CallName: p.CallName, Siz: p.Siz}, Note: note}, nil
}

// profileOwner is whose profile a call changes, and that member's name when
// it is not the caller's own: an owner's for_member means the member they
// replied to, or the one the bot's replied-to answer answered.
func profileOwner(caller Caller, forMember bool) (int64, string, error) {
	if !forMember {
		return caller.UserID, "", nil
	}
	if !caller.Owner {
		return 0, "", &DeniedError{Message: "only the bot's owners may set someone else's profile"}
	}
	member := caller.Target
	if member != nil && member.Answered != nil {
		member = member.Answered
	}
	if member == nil || member.IsBot || member.UserID == caller.UserID || isTelegramAccount(member.UserID) {
		return 0, "", &ArgumentError{Message: profileNoMember}
	}
	return member.UserID, member.Name, nil
}
