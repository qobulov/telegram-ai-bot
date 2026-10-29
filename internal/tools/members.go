package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
)

const (
	// maxMembersListed bounds the list, the most recently seen first: a
	// long list costs the model more than it helps.
	maxMembersListed = 200
	// memberEntryBytes is the most a listed member takes besides the texts.
	memberEntryBytes = 150
	// membersOutputBytes fits maxMembersListed members, each name at its
	// longest and cut with "…", and the notes.
	membersOutputBytes = maxMembersListed*((maxAdminNameRunes+maxAdminUsernameRunes+2)*maxEncodedRuneBytes+memberEntryBytes) + 1500
	memberTimeLayout   = "2 Jan 15:04"
)

const (
	membersNotInAGroup  = "this works only in a group"
	membersUnavailable  = "the list of members I have seen could not be read just now; try again later"
	membersNote         = "These are the members I have seen in this group, most recently seen first: by their messages, by joining, or by Telegram telling me. It is not everyone: members who have not written or joined since I started keeping the list are missing. known is how many I have seen; total, when given, is how many members the group has. role marks admins; a member without a role is a regular member, except your owners, who are never regular members. Never guess about anyone not listed."
	membersNoAdminsNote = "The admins could not be checked just now, so roles are missing."
	membersCutNote      = "%d more members I have seen are left out of this list."
)

// MemberLister reads the members seen in a group.
type MemberLister interface {
	GroupMembers(ctx context.Context, chatID int64, limit int) ([]storage.GroupMember, int, error)
}

// GroupCounter asks Telegram for a group's size and its admins.
type GroupCounter interface {
	GetChatMemberCount(ctx context.Context, chatID int64) (int, error)
	AdminLister
}

// MembersOptions configures the group_members tool. Store and Telegram are
// required.
type MembersOptions struct {
	Store    MemberLister
	Telegram GroupCounter
	// Owners are the bot's owners by Telegram ID.
	Owners map[int64]bool
	// Location is the time zone of the times reported.
	Location *time.Location
}

type memberEntry struct {
	Name     string `json:"name,omitempty"`
	Username string `json:"username,omitempty"`
	Role     string `json:"role,omitempty"`
	Owner    bool   `json:"owner,omitempty"`
	Bot      bool   `json:"bot,omitempty"`
	LastSeen string `json:"last_seen,omitempty"`
}

type membersResult struct {
	Known   int           `json:"known"`
	Total   int           `json:"total,omitempty"`
	Members []memberEntry `json:"members"`
	Note    string        `json:"note"`
}

// MembersTool lets anyone in a group ask who is in it. The Bot API cannot
// list a group's members, so the bot keeps the list of those it has seen;
// the group is the one the turn is in, never one the model names.
func MembersTool(opts MembersOptions) Tool {
	return Tool{
		Name: "group_members",
		Description: "List the members you have seen in this group, the admins among them marked, with how many members the group has. " +
			"Use it when someone asks who is in the group, who the regular members are or how many members there are; group_admins lists only the admins. " +
			"Use it too when asked to call, wake or tag members (\"uyg'ot\", \"chaqir\", \"teg qil\") whose @username is not in front of you: tag them by the @username listed here; a member listed without one cannot be tagged, so say so.",
		Schema:         json.RawMessage(`{"type": "object", "properties": {}, "additionalProperties": false}`),
		GroupOnly:      true,
		MaxOutputBytes: membersOutputBytes,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return listMembers(ctx, opts, raw)
		},
	}
}

func listMembers(ctx context.Context, opts MembersOptions, raw json.RawMessage) (any, error) {
	var args struct{}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, ok := CallerFrom(ctx)
	if !ok || caller.ChatID >= 0 {
		return nil, &DeniedError{Message: membersNotInAGroup}
	}
	members, known, err := opts.Store.GroupMembers(ctx, caller.ChatID, maxMembersListed)
	if err != nil {
		return nil, &InternalError{Err: err, Message: membersUnavailable}
	}
	// Telegram's answers only add to the list: without them it still stands.
	total, _ := opts.Telegram.GetChatMemberCount(ctx, caller.ChatID)
	admins, adminErr := opts.Telegram.GetChatAdministrators(ctx, caller.ChatID)
	roles := map[int64]string{}
	unseen := map[int64]bool{}
	for _, admin := range admins {
		if admin.User != nil && !admin.IsAnonymous {
			roles[admin.User.ID] = admin.Status
			unseen[admin.User.ID] = true
		}
	}
	location := opts.Location
	if location == nil {
		location = time.UTC
	}
	result := membersResult{Known: known, Total: total, Members: make([]memberEntry, 0, len(members))}
	for _, m := range members {
		entry := memberEntry{
			Name:     cutRunes(strings.TrimSpace(m.FirstName), maxAdminNameRunes),
			Role:     roles[m.UserID],
			Owner:    opts.Owners[m.UserID],
			Bot:      m.IsBot,
			LastSeen: m.LastSeen.In(location).Format(memberTimeLayout),
		}
		if m.Username != "" {
			entry.Username = cutRunes("@"+m.Username, maxAdminUsernameRunes)
		}
		result.Members = append(result.Members, entry)
		delete(unseen, m.UserID)
	}
	// Admins who never wrote are members all the same, bots such as this one
	// aside. When the list is cut, one missing from it may be among those
	// left out, so none is added.
	for _, admin := range admins {
		if admin.User == nil || admin.IsAnonymous || admin.User.IsBot || !unseen[admin.User.ID] || known > len(members) || len(result.Members) >= maxMembersListed {
			continue
		}
		entry := memberEntry{Name: cutRunes(strings.TrimSpace(admin.User.FirstName), maxAdminNameRunes), Role: admin.Status,
			Owner: opts.Owners[admin.User.ID], Bot: admin.User.IsBot}
		if admin.User.Username != "" {
			entry.Username = cutRunes("@"+admin.User.Username, maxAdminUsernameRunes)
		}
		result.Members = append(result.Members, entry)
		result.Known++
	}
	notes := []string{membersNote}
	if adminErr != nil {
		notes = append(notes, membersNoAdminsNote)
	}
	if leftOut := known - len(members); leftOut > 0 {
		notes = append(notes, fmt.Sprintf(membersCutNote, leftOut))
	}
	result.Note = strings.Join(notes, " ")
	return result, nil
}
