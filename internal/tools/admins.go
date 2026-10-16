package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"telegram-ai-bot/internal/telegram"
)

const (
	// maxAdminsListed bounds the list. Telegram allows a supergroup 50
	// admins, so it is cut only if that changes.
	maxAdminsListed = 50
	// Telegram's limits for a first name, a username with its @, and an
	// admin's title. Longer ones are cut, so that the list always fits.
	maxAdminNameRunes     = 64
	maxAdminUsernameRunes = 33
	maxAdminTitleRunes    = 16
	// adminEntryBytes is the most a listed admin takes besides the texts:
	// the keys, the role and the flags.
	adminEntryBytes = 150
	// adminsOutputBytes fits maxAdminsListed admins, each text at its
	// longest and cut with "…", and the notes.
	adminsOutputBytes = maxAdminsListed*((maxAdminNameRunes+maxAdminUsernameRunes+maxAdminTitleRunes+3)*maxEncodedRuneBytes+adminEntryBytes) + 1000
)

const (
	adminsNotInAGroup = "this works only in a group"
	adminsUnavailable = "Telegram did not give the admin list just now; try again later"
	adminsNote        = "These are the group's admins, as Telegram reports them now. You cannot see the full member list: anyone else who wrote in the messages you were given is a regular member. Never guess about members who did not write."
	anonymousNote     = "An anonymous admin hides who they are: never guess or say who it is."
	adminsCutNote     = "%d more admins were left out of this list, so someone not listed may be an admin too."
)

// AdminLister asks Telegram for a group's admins.
type AdminLister interface {
	GetChatAdministrators(ctx context.Context, chatID int64) ([]telegram.ChatMember, error)
}

// AdminsOptions configures the group_admins tool. Telegram is required.
type AdminsOptions struct {
	Telegram AdminLister
	// BotID is the bot's own user ID, marked "you" in the list.
	BotID int64
	// Owners are the bot's owners by Telegram ID. Only an ID marks an admin
	// as an owner: a username can be given up and taken by someone else.
	Owners map[int64]bool
}

type adminEntry struct {
	Name      string `json:"name,omitempty"`
	Username  string `json:"username,omitempty"`
	Role      string `json:"role"`
	Title     string `json:"title,omitempty"`
	Anonymous bool   `json:"anonymous,omitempty"`
	You       bool   `json:"you,omitempty"`
	Owner     bool   `json:"owner,omitempty"`
	Bot       bool   `json:"bot,omitempty"`
}

type adminsResult struct {
	Admins []adminEntry `json:"admins"`
	Note   string       `json:"note"`
}

// AdminsTool lets anyone in a group ask who its admins are. The model
// cannot see the member list, and without this it guessed. The group is the
// one the turn is in, never one the model names.
func AdminsTool(opts AdminsOptions) Tool {
	return Tool{
		Name: "group_admins",
		Description: "List this group's admins, as Telegram reports them now. Use it when someone asks who the admins are, who may moderate, or who the regular members are. " +
			"Nobody can see the full member list: anyone not listed who wrote in the chat is a regular member.",
		Schema:         json.RawMessage(`{"type": "object", "properties": {}, "additionalProperties": false}`),
		GroupOnly:      true,
		MaxOutputBytes: adminsOutputBytes,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return listAdmins(ctx, opts, raw)
		},
	}
}

func listAdmins(ctx context.Context, opts AdminsOptions, raw json.RawMessage) (any, error) {
	var args struct{}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, ok := CallerFrom(ctx)
	if !ok || caller.ChatID >= 0 {
		return nil, &DeniedError{Message: adminsNotInAGroup}
	}
	members, err := opts.Telegram.GetChatAdministrators(ctx, caller.ChatID)
	if err != nil {
		return nil, errors.New(adminsUnavailable)
	}
	return newAdminsResult(opts, members), nil
}

func newAdminsResult(opts AdminsOptions, members []telegram.ChatMember) adminsResult {
	listed := members[:min(len(members), maxAdminsListed)]
	result := adminsResult{Admins: make([]adminEntry, 0, len(listed))}
	anonymous := false
	for _, member := range listed {
		entry := newAdminEntry(opts, member)
		anonymous = anonymous || entry.Anonymous
		result.Admins = append(result.Admins, entry)
	}
	notes := []string{adminsNote}
	if anonymous {
		notes = append(notes, anonymousNote)
	}
	if leftOut := len(members) - len(listed); leftOut > 0 {
		notes = append(notes, fmt.Sprintf(adminsCutNote, leftOut))
	}
	result.Note = strings.Join(notes, " ")
	return result
}

// newAdminEntry describes one admin. An anonymous admin chose to hide who
// they are from the group, so only their role is given: no name, username,
// title or owner mark.
func newAdminEntry(opts AdminsOptions, member telegram.ChatMember) adminEntry {
	entry := adminEntry{Role: member.Status}
	if member.IsAnonymous {
		entry.Anonymous = true
		return entry
	}
	entry.Title = cutRunes(strings.TrimSpace(member.CustomTitle), maxAdminTitleRunes)
	user := member.User
	if user == nil {
		return entry
	}
	entry.Name = cutRunes(strings.TrimSpace(user.FirstName), maxAdminNameRunes)
	if user.Username != "" {
		entry.Username = cutRunes("@"+user.Username, maxAdminUsernameRunes)
	}
	entry.You = user.ID == opts.BotID
	entry.Owner = opts.Owners[user.ID]
	entry.Bot = user.IsBot
	return entry
}
