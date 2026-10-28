package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
)

const (
	// statsListed is how many top writers and swearers are named.
	statsListed       = 10
	statsBusiestHours = 3
	statsWeek         = 7 * 24 * time.Hour
	statsOutputBytes  = 6000
)

const (
	statsNotInAGroup = "this works only in a group"
	statsUnavailable = "the group's statistics could not be read just now; try again later"
	statsNote        = "Counted from the messages you logged here: those written while you were muted, before you joined or older than your log are not counted, and your own are left out. " +
		"The MVP is the top writer. Give the numbers as they are, briefly and in the asker's language; name swearers only when asked about swearing."
)

// StatsReader counts a group's logged messages.
type StatsReader interface {
	GroupStats(ctx context.Context, chatID, botID int64, since time.Time, timeZone string, limit int) (storage.GroupStats, error)
}

// GroupStatsOptions configures the group_stats tool. Store is required.
type GroupStatsOptions struct {
	Store StatsReader
	// BotID is the bot's own user ID, whose messages are not counted.
	BotID int64
	// Location is the time zone of "today" and of the hours.
	Location *time.Location
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

type groupStatsArgs struct {
	Period string `json:"period"`
}

type statsEntry struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type groupStatsResult struct {
	Period   string       `json:"period"`
	Since    string       `json:"since"`
	Messages int          `json:"messages"`
	Members  int          `json:"members_who_wrote"`
	Top      []statsEntry `json:"top_writers"`
	Busiest  []statsEntry `json:"busiest_hours,omitempty"`
	Swearers []statsEntry `json:"most_caught_swearing,omitempty"`
	Note     string       `json:"note"`
}

// GroupStatsTool tells anyone in a group how it talked today or this week:
// who wrote the most, the busiest hours and whom the swear guard caught.
// The group is the one the turn is in, never one the model names.
func GroupStatsTool(opts GroupStatsOptions) Tool {
	return Tool{
		Name: "group_stats",
		Description: "This group's statistics for today or the last 7 days: messages, how many members wrote, the top writers (the MVP is the first), the busiest hours and whom the swear guard caught the most. " +
			"Use it when someone asks who is the most active, the group's MVP, top or rating, or how much the group wrote (\"eng faol kim\", \"guruh MVP si\", \"кто больше всех пишет\").",
		Schema:         json.RawMessage(`{"type": "object", "properties": {"period": {"type": "string", "enum": ["today", "week"], "description": "today, the default, or the last 7 days."}}, "additionalProperties": false}`),
		GroupOnly:      true,
		MaxOutputBytes: statsOutputBytes,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return groupStats(ctx, opts, raw)
		},
	}
}

func groupStats(ctx context.Context, opts GroupStatsOptions, raw json.RawMessage) (any, error) {
	var args groupStatsArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, ok := CallerFrom(ctx)
	if !ok || caller.ChatID >= 0 {
		return nil, &DeniedError{Message: statsNotInAGroup}
	}
	location := opts.location()
	since, err := statsSince(args.Period, opts.now().In(location))
	if err != nil {
		return nil, err
	}
	stats, err := opts.Store.GroupStats(ctx, caller.ChatID, opts.BotID, since, location.String(), statsListed)
	if err != nil {
		return nil, &InternalError{Err: err, Message: statsUnavailable}
	}
	return groupStatsResult{
		Period:   cmp.Or(args.Period, "today"),
		Since:    since.Format("2 Jan 15:04"),
		Messages: stats.Messages,
		Members:  stats.Members,
		Top:      statsEntries(stats.Top),
		Busiest:  busiestHours(stats.Hours),
		Swearers: statsEntries(stats.Swearers),
		Note:     statsNote,
	}, nil
}

// statsSince is when a period starts: today's midnight, or a week ago.
func statsSince(period string, now time.Time) (time.Time, error) {
	switch period {
	case "", "today":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()), nil
	case "week":
		return now.Add(-statsWeek), nil
	}
	return time.Time{}, &ArgumentError{Message: "period must be today or week"}
}

// statsEntries names each member by first name only: the log's "Ann @ann"
// written back would tag the whole top ten.
func statsEntries(counts []storage.AuthorCount) []statsEntry {
	entries := make([]statsEntry, 0, len(counts))
	for _, c := range counts {
		name, _, _ := strings.Cut(c.Name, " @")
		entries = append(entries, statsEntry{Name: cutRunes(strings.TrimPrefix(name, "@"), maxAdminNameRunes), Count: c.Count})
	}
	return entries
}

// busiestHours are the hours with the most messages, as "13:00", the
// busiest first.
func busiestHours(hours [24]int) []statsEntry {
	var busy []statsEntry
	for hour, count := range hours {
		if count > 0 {
			busy = append(busy, statsEntry{Name: fmt.Sprintf("%02d:00", hour), Count: count})
		}
	}
	slices.SortStableFunc(busy, func(a, b statsEntry) int { return b.Count - a.Count })
	return busy[:min(len(busy), statsBusiestHours)]
}

func (opts GroupStatsOptions) now() time.Time {
	if opts.Now != nil {
		return opts.Now()
	}
	return time.Now()
}

func (opts GroupStatsOptions) location() *time.Location {
	if opts.Location == nil {
		return time.UTC
	}
	return opts.Location
}
