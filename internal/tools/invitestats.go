package tools

import (
	"context"
	"encoding/json"
	"time"

	"telegram-ai-bot/internal/storage"
)

const (
	invitesListed      = 10
	invitesUnavailable = "the invite count could not be read just now; try again later"
	invitesNote        = "Counted are the people a member added by hand or who came in by that member's own invite link (/mylink); one who left is taken off and shown as left. " +
		"Someone who came in by the group's public link or an admin's link is not counted for whoever shared it: tell them to share their own /mylink. " +
		"Give the numbers as they are, briefly, in the asker's language; /invites, /invites_week and /invites_month show the table, /invites_auto posts it on its own."
)

// InviteCounter reads who brought how many people into a group.
type InviteCounter interface {
	InviteTop(ctx context.Context, chatID int64, q storage.InviteQuery) ([]storage.InviterCount, error)
}

// InviteStatsOptions configures the invite_stats tool. Store is required.
type InviteStatsOptions struct {
	Store InviteCounter
	// Location is the time zone of today, the week and the month.
	Location *time.Location
	Now      func() time.Time
}

type inviteStatsArgs struct {
	Period string `json:"period"`
	Whose  string `json:"whose"`
}

type inviterEntry struct {
	Name   string `json:"name"`
	Stayed int    `json:"brought_still_here"`
	Left   int    `json:"brought_who_left,omitempty"`
}

// InviteStatsTool tells anyone in a group who brought how many people in:
// the top, the asker's own count, or the member they replied to.
func InviteStatsTool(opts InviteStatsOptions) Tool {
	return Tool{
		Name: "invite_stats",
		Description: "Who brought how many people into this group, for today, this week (since Monday, the default), this month or all time: the top, the asker's own count (whose: mine) or the member they replied to (whose: replied). " +
			"Use it whenever someone asks who added the most people, how many they or someone added, or for the invite rating (\"kim ko'p odam qo'shgan\", \"men nechta odam qo'shdim\", \"кто больше всех добавил\"); never guess the numbers.",
		Schema: json.RawMessage(`{"type": "object", "properties": {
			"period": {"type": "string", "enum": ["today", "week", "month", "all"], "description": "week, since Monday, is the default."},
			"whose": {"type": "string", "enum": ["top", "mine", "replied"], "description": "top, the default; mine for the asker; replied for the member their message replies to."}
		}, "additionalProperties": false}`),
		GroupOnly: true,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return inviteStats(ctx, opts, raw)
		},
	}
}

func inviteStats(ctx context.Context, opts InviteStatsOptions, raw json.RawMessage) (any, error) {
	var args inviteStatsArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, ok := CallerFrom(ctx)
	if !ok || caller.ChatID >= 0 {
		return nil, &DeniedError{Message: statsNotInAGroup}
	}
	location := opts.Location
	if location == nil {
		location = time.UTC
	}
	now := time.Now()
	if opts.Now != nil {
		now = opts.Now()
	}
	period, since := invitePeriod(args.Period, now.In(location))
	q := storage.InviteQuery{Since: since, Limit: invitesListed}
	switch args.Whose {
	case "mine":
		q.Inviter = caller.UserID
	case "replied":
		if caller.Target == nil || caller.Target.UserID == 0 {
			return nil, &ArgumentError{Message: "their message replies to no member; use whose top or mine"}
		}
		q.Inviter = caller.Target.UserID
	}
	top, err := opts.Store.InviteTop(ctx, caller.ChatID, q)
	if err != nil {
		return nil, &InternalError{Err: err, Message: invitesUnavailable}
	}
	entries := make([]inviterEntry, len(top))
	for i, c := range top {
		entries[i] = inviterEntry{Name: c.Name, Stayed: c.Stayed, Left: c.Added - c.Stayed}
	}
	return map[string]any{"period": period, "since": since.Format("2 Jan"), "inviters": entries, "note": invitesNote}, nil
}

// invitePeriod is when a period starts: today, the week from Monday, the
// month, or all time.
func invitePeriod(period string, now time.Time) (string, time.Time) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch period {
	case "today":
		return "today", today
	case "month":
		return "month", time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	case "all":
		return "all", time.Time{}
	}
	return "week", today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
}
