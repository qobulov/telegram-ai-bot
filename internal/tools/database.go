package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"telegram-ai-bot/internal/storage"
)

const (
	maxGroupsListed     = 50
	defaultMessageLimit = 100
	maxMessageLimit     = 200
	maxMessageLineRunes = 300
	// messagesBudget keeps the newest messages that fit in the result.
	messagesBudget       = 20000
	databaseOutputBytes  = 24000
	databaseTimeout      = 20 * time.Second
	memberCountsAtOnce   = 5
	databaseTimeLayout   = "2006-01-02 15:04"
	messageTimeLayout    = "15:04"
	actionOverview       = "overview"
	actionGroups         = "groups"
	actionGroupMessages  = "messages"
	otherGroupNotAllowed = "another group's messages can be read only in a private chat with the owner; ask them to write to you privately"
)

// ErrNotInGroup means the bot is no longer a member of the group.
var ErrNotInGroup = errors.New("the bot is not in this group")

// DatabaseReader is what the bot_database tool reads.
type DatabaseReader interface {
	Overview(ctx context.Context, since time.Time) (storage.Overview, error)
	Groups(ctx context.Context, since time.Time, limit int) ([]storage.GroupSummary, error)
	GroupMessagesSince(ctx context.Context, chatID int64, since time.Time, limit int) ([]storage.GroupMessage, error)
}

// Membership is the bot's standing in a group, as Telegram reports it now.
type Membership struct {
	// Status is the bot's: "member", "administrator", "left", "kicked"...
	Status  string
	Members int
	Title   string
}

func (m Membership) in() bool {
	return m.Status != "left" && m.Status != "kicked"
}

// GroupMembers asks Telegram about the bot in a group. It returns
// ErrNotInGroup when the group cannot be reached by the bot at all.
type GroupMembers interface {
	Membership(ctx context.Context, chatID int64) (Membership, error)
}

// DatabaseOptions configures the bot_database tool.
type DatabaseOptions struct {
	Store DatabaseReader
	// Members is optional; without it groups are listed without live checks.
	Members GroupMembers
	// Location decides when "today" starts.
	Location *time.Location
	Now      func() time.Time
}

type databaseArgs struct {
	Action string `json:"action"`
	ChatID int64  `json:"chat_id"`
	Limit  int    `json:"limit"`
}

// DatabaseTool lets the bot's owners ask about its own data: how many groups
// it is in, who uses it, and what was said in a group today.
func DatabaseTool(opts DatabaseOptions) Tool {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	return Tool{
		Name: "bot_database",
		Description: "Owners only. Look up the bot's own data. " +
			"overview: how many groups the bot has been in, users, and today's messages and pictures. " +
			"groups: every group seen, with its chat_id, whether the bot is still there, whether it is an admin there, and the member count, checked live. " +
			"messages: today's messages of one group by chat_id (from the groups list); another group's messages only in a private chat. " +
			"Messages are kept only for today. Answer only from what this returns; never guess groups or admin rights.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["overview", "groups", "messages"]},
				"chat_id": {"type": "integer", "description": "The group, for messages. Group IDs are negative."},
				"limit": {"type": "integer", "description": "For messages: how many of the newest, 1 to 200. Default 100."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		OwnerOnly:      true,
		Timeout:        databaseTimeout,
		MaxOutputBytes: databaseOutputBytes,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return queryDatabase(ctx, opts, raw)
		},
	}
}

func queryDatabase(ctx context.Context, opts DatabaseOptions, raw json.RawMessage) (any, error) {
	var args databaseArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, ok := CallerFrom(ctx)
	if !ok || !caller.Owner {
		return nil, errors.New("only the bot's owners may read its data")
	}
	today := startOfDay(opts.Now(), opts.Location)
	switch args.Action {
	case actionOverview:
		return opts.Store.Overview(ctx, today)
	case actionGroups:
		return listGroups(ctx, opts, today)
	case actionGroupMessages:
		return groupMessages(ctx, opts, caller, args, today)
	default:
		return nil, &ArgumentError{Message: "action must be overview, groups or messages"}
	}
}

func startOfDay(now time.Time, location *time.Location) time.Time {
	local := now.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
}

type groupEntry struct {
	ChatID        int64  `json:"chat_id"`
	Title         string `json:"title"`
	Username      string `json:"username,omitempty"`
	BotInGroup    *bool  `json:"bot_in_group,omitempty"`
	BotStatus     string `json:"bot_status,omitempty"`
	BotIsAdmin    bool   `json:"bot_is_admin"`
	Members       *int   `json:"members,omitempty"`
	Muted         bool   `json:"muted,omitempty"`
	MessagesToday int    `json:"messages_today"`
	FirstSeen     string `json:"first_seen"`
	LastSeen      string `json:"last_seen"`
}

type groupList struct {
	Groups     []groupEntry `json:"groups"`
	BotIsIn    int          `json:"bot_is_in"`
	BotIsAdmin int          `json:"bot_is_admin_in"`
	Note       string       `json:"note,omitempty"`
}

func listGroups(ctx context.Context, opts DatabaseOptions, today time.Time) (groupList, error) {
	groups, err := opts.Store.Groups(ctx, today, maxGroupsListed)
	if err != nil {
		return groupList{}, err
	}
	entries := make([]groupEntry, len(groups))
	for i, g := range groups {
		entries[i] = groupEntry{
			ChatID:        g.ChatID,
			Title:         g.Title,
			Username:      g.Username,
			BotStatus:     g.BotStatus,
			BotIsAdmin:    isAdminStatus(g.BotStatus),
			Muted:         g.Muted,
			MessagesToday: g.MessagesToday,
			FirstSeen:     g.FirstSeen.In(opts.Location).Format(databaseTimeLayout),
			LastSeen:      g.LastSeen.In(opts.Location).Format(databaseTimeLayout),
		}
	}
	list := groupList{Groups: entries}
	if opts.Members == nil {
		list.Note = "membership was not checked live; bot_status is the last one Telegram reported"
	} else {
		checkMembership(ctx, opts.Members, entries)
	}
	for _, e := range entries {
		if e.BotInGroup != nil && *e.BotInGroup {
			list.BotIsIn++
		}
		if e.BotIsAdmin {
			list.BotIsAdmin++
		}
	}
	return list, nil
}

func isAdminStatus(status string) bool {
	return status == "administrator" || status == "creator"
}

// checkMembership asks Telegram about each group, a few at a time. A group
// that could not be checked keeps what was stored and has no bot_in_group.
func checkMembership(ctx context.Context, members GroupMembers, entries []groupEntry) {
	slots := make(chan struct{}, memberCountsAtOnce)
	var wg sync.WaitGroup
	for i := range entries {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			membership, err := members.Membership(ctx, entries[i].ChatID)
			switch {
			case err == nil:
				applyMembership(&entries[i], membership)
			case errors.Is(err, ErrNotInGroup):
				applyMembership(&entries[i], Membership{Status: "left"})
			}
		})
	}
	wg.Wait()
}

func applyMembership(e *groupEntry, m Membership) {
	in := m.in()
	e.BotInGroup, e.BotStatus, e.BotIsAdmin = &in, m.Status, isAdminStatus(m.Status)
	if in {
		members := m.Members
		e.Members = &members
	}
	if m.Title != "" {
		e.Title = m.Title
	}
}

type messageLog struct {
	ChatID   int64    `json:"chat_id"`
	Messages []string `json:"messages"`
	Note     string   `json:"note,omitempty"`
}

func groupMessages(ctx context.Context, opts DatabaseOptions, caller Caller, args databaseArgs, today time.Time) (messageLog, error) {
	if args.ChatID >= 0 {
		return messageLog{}, &ArgumentError{Message: "chat_id must be a group's ID, which is negative; get it from the groups action"}
	}
	limit := args.Limit
	if limit == 0 {
		limit = defaultMessageLimit
	}
	if limit < 1 || limit > maxMessageLimit {
		return messageLog{}, &ArgumentError{Message: fmt.Sprintf("limit must be 1 to %d", maxMessageLimit)}
	}
	// Posting one group's conversation into another would leak it to people
	// who were never in it.
	if caller.ChatID < 0 && caller.ChatID != args.ChatID {
		return messageLog{}, &ArgumentError{Message: otherGroupNotAllowed}
	}
	messages, err := opts.Store.GroupMessagesSince(ctx, args.ChatID, today, limit)
	if err != nil {
		return messageLog{}, err
	}
	lines, dropped := fitMessages(messages, opts.Location)
	log := messageLog{ChatID: args.ChatID, Messages: lines}
	switch {
	case len(messages) == 0:
		log.Note = "no messages today"
	case dropped > 0:
		log.Note = fmt.Sprintf("%d older messages left out", dropped)
	}
	return log, nil
}

// fitMessages formats messages as "15:04 Name: text", keeping the newest
// that fit in messagesBudget. It reports how many older ones were dropped.
func fitMessages(messages []storage.GroupMessage, location *time.Location) ([]string, int) {
	lines := make([]string, len(messages))
	for i, m := range messages {
		lines[i] = m.SentAt.In(location).Format(messageTimeLayout) + " " + m.AuthorName + ": " + cutRunes(m.Text, maxMessageLineRunes)
	}
	size, first := 0, len(lines)
	for first > 0 && size+len(lines[first-1]) <= messagesBudget {
		size += len(lines[first-1])
		first--
	}
	return lines[first:], first
}

func cutRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}
