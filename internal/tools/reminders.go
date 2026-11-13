package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/reminders"
	"telegram-ai-bot/internal/storage"
)

const (
	// MaxReminderAhead is how far ahead a reminder may be set.
	MaxReminderAhead = 30 * 24 * time.Hour
	// ReminderPerUser and ReminderPerChat are how many text reminders a
	// member, and a group, may have waiting; a card reminder is one more.
	ReminderPerUser  = 20
	ReminderPerChat  = 50
	maxReminderRunes = 500

	reminderNotInATurn = "reminders are set by a person writing to me"
	reminderNoText     = "text is required: what to remind them of, in their words and language"
	reminderTooLong    = "text is too long; keep it under 500 characters"
	reminderWhen       = "give exactly one of in_minutes or at"
	reminderMinutes    = "in_minutes must be from 1 to 43200 (30 days)"
	reminderBadAt      = `at must be a local time "HH:MM", or a date and time "YYYY-MM-DD HH:MM"`
	reminderPast       = "that time has already passed; ask them for a time from now on"
	reminderTooFar     = "a reminder can be set at most 30 days ahead"
	reminderLimit      = "they already have as many reminders waiting as allowed (20 each, 50 in a group); cancel one first"
	reminderNoID       = "id is required: the reminder's id, from list"
	reminderNotFound   = "there is no reminder with that id of theirs waiting in this chat; reminders can be cancelled only by whoever set them, in the chat they were set in"
	reminderNoCards    = "the card reminders are not available here"
	reminderBadAction  = "action must be one of set, list, cancel, card, card_off"
	reminderSetNote    = "Confirm in one short line, with the local time, that you will remind them here then. Never say it is set unless this tool says so."
	reminderCardNow    = "They may open a card right now: say so, and that /card or Kartalar opens it."
	reminderCardNote   = "Confirm in one short line that you will remind them here when their next card may be opened, at the local time due."
	reminderAutoNote   = " From now on every card they open schedules the next reminder, until they ask you to stop."
	reminderOffNote    = "Card reminders are off and none is waiting. Confirm in one short line."
	timeLayout         = "2006-01-02 15:04"
)

// ReminderStore keeps reminders, and the card draws card reminders follow.
type ReminderStore interface {
	AddReminder(ctx context.Context, r storage.Reminder, perUser, perChat int) (storage.Reminder, error)
	SetCardReminder(ctx context.Context, r storage.Reminder) (storage.Reminder, error)
	SetCardAutoRemind(ctx context.Context, userID int64, name string, chatID int64, language string) error
	CardAutoRemind(ctx context.Context, userID int64) (int64, error)
	WaitingReminders(ctx context.Context, userID, chatID int64) ([]storage.Reminder, error)
	CancelReminder(ctx context.Context, id, userID, chatID int64) (bool, error)
	CardPlayerOf(ctx context.Context, userID int64) (storage.CardPlayer, time.Time, error)
}

// RemindersOptions configures the reminders tool. Store is required.
type RemindersOptions struct {
	Store ReminderStore
	// CardEvery is how often a Kartalar card may be drawn; zero leaves the
	// card reminders out.
	CardEvery time.Duration
	// Location is where "18:00" is read: the groups' time zone.
	Location *time.Location
	Now      func() time.Time
}

func (o RemindersOptions) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o RemindersOptions) location() *time.Location {
	if o.Location != nil {
		return o.Location
	}
	return time.UTC
}

type reminderArgs struct {
	Action    string `json:"action"`
	Text      string `json:"text"`
	InMinutes *int   `json:"in_minutes"`
	At        string `json:"at"`
	ID        int64  `json:"id"`
	Always    bool   `json:"always"`
	Language  string `json:"language"`
}

type reminderView struct {
	ID   int64  `json:"id"`
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
	Due  string `json:"due"`
}

type reminderSet struct {
	ID        int64  `json:"id,omitempty"`
	Due       string `json:"due,omitempty"`
	InMinutes int    `json:"in_minutes,omitempty"`
	Timezone  string `json:"timezone"`
	CanDraw   bool   `json:"can_open_card_now,omitempty"`
	Always    bool   `json:"always,omitempty"`
	Note      string `json:"note"`
}

type reminderList struct {
	Reminders []reminderView `json:"reminders"`
	CardAuto  bool           `json:"card_reminders_always,omitempty"`
	Timezone  string         `json:"timezone"`
}

// RemindersTool lets a member have Maria remind them later: of their own
// words at a time, or when their next Kartalar card may be opened. The
// reminder is sent where it was asked, tagging them in a group. Times are
// checked here, not left to the model: none past, none more than 30 days
// ahead, and only so many waiting. Each member sees and cancels only their
// own, in the chat they set them in.
func RemindersTool(opts RemindersOptions) Tool {
	card := ""
	if opts.CardEvery > 0 {
		card = " card: remind them when their next Kartalar card may be opened (\"karta ochish vaqti kelganda eslat\"); always true when they want that after every card (\"har safar eslat\"). card_off: stop the card reminders."
	}
	return Tool{
		Name: "reminders",
		Description: "Remind the person later, in this chat. set: \"10 daqiqadan keyin eslat\", \"soat 18:00 da ayt\", \"ertaga 9 da ...\": text is what to remind them of, short, in their language; give in_minutes for a time from now, or at for a local clock time (\"HH:MM\" is the next such time, \"YYYY-MM-DD HH:MM\" a date). list: their reminders waiting here. cancel: one of them, by id from list." +
			card + " Never say a reminder is set unless this tool says so.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["set", "list", "cancel", "card", "card_off"]},
				"text": {"type": "string", "description": "set: what to remind them of, in their words and language."},
				"in_minutes": {"type": "integer", "description": "set: minutes from now, 1 to 43200. Hours are minutes times 60."},
				"at": {"type": "string", "description": "set: local time \"HH:MM\" or \"YYYY-MM-DD HH:MM\", in the group's time zone."},
				"id": {"type": "integer", "description": "cancel: the reminder's id."},
				"always": {"type": "boolean", "description": "card: remind them after every card they open, until card_off."},
				"language": {"type": "string", "enum": ["uz", "ru", "en"], "description": "card: the language they write in."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		NoBusiness: true,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return runReminders(ctx, opts, raw)
		},
	}
}

func runReminders(ctx context.Context, opts RemindersOptions, raw json.RawMessage) (any, error) {
	var args reminderArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, ok := CallerFrom(ctx)
	if !ok || !caller.Human || caller.UserID == 0 || caller.ChatID == 0 {
		return nil, &DeniedError{Message: reminderNotInATurn}
	}
	switch args.Action {
	case "set":
		return setReminder(ctx, opts, caller, args)
	case "list":
		return listReminders(ctx, opts, caller)
	case "cancel":
		return cancelReminder(ctx, opts, caller, args)
	case "card", "card_off":
		if opts.CardEvery <= 0 {
			return nil, &DeniedError{Message: reminderNoCards}
		}
		if args.Action == "card_off" {
			return cardRemindersOff(ctx, opts, caller, args)
		}
		return cardReminder(ctx, opts, caller, args)
	}
	return nil, &ArgumentError{Message: reminderBadAction}
}

func setReminder(ctx context.Context, opts RemindersOptions, caller Caller, args reminderArgs) (any, error) {
	text := reminders.CleanText(args.Text)
	switch {
	case text == "":
		return nil, &ArgumentError{Message: reminderNoText}
	case utf8.RuneCountInString(text) > maxReminderRunes:
		return nil, &ArgumentError{Message: reminderTooLong}
	}
	now := opts.now()
	due, err := ReminderDue(args.InMinutes, args.At, now, opts.location())
	if err != nil {
		return nil, err
	}
	saved, err := opts.Store.AddReminder(ctx, storage.Reminder{
		ChatID: caller.ChatID, UserID: caller.UserID, UserName: caller.Name, MessageID: caller.MessageID,
		Text: text, Language: pickLanguage(args.Language), DueAt: due,
	}, ReminderPerUser, ReminderPerChat)
	if errors.Is(err, storage.ErrReminderLimit) {
		return nil, &DeniedError{Message: reminderLimit}
	}
	if err != nil {
		return nil, internal(err)
	}
	return reminderSet{
		ID: saved.ID, Due: due.In(opts.location()).Format(timeLayout), InMinutes: minutesUntil(now, due),
		Timezone: opts.location().String(), Note: reminderSetNote,
	}, nil
}

// ReminderDue is when a reminder asked for in minutes from now, or at a
// local time, falls: "HH:MM" is the next such time, a date and time must
// be ahead. Exactly one of the two is given, and the time is at most
// MaxReminderAhead away.
func ReminderDue(inMinutes *int, at string, now time.Time, location *time.Location) (time.Time, error) {
	at = strings.TrimSpace(at)
	if (inMinutes == nil) == (at == "") {
		return time.Time{}, &ArgumentError{Message: reminderWhen}
	}
	if inMinutes != nil {
		if *inMinutes < 1 || time.Duration(*inMinutes)*time.Minute > MaxReminderAhead {
			return time.Time{}, &ArgumentError{Message: reminderMinutes}
		}
		return now.Add(time.Duration(*inMinutes) * time.Minute).Truncate(time.Second), nil
	}
	local := now.In(location)
	var due time.Time
	if clock, err := time.ParseInLocation("15:04", at, location); err == nil {
		due = time.Date(local.Year(), local.Month(), local.Day(), clock.Hour(), clock.Minute(), 0, 0, location)
		if !due.After(now) {
			due = due.AddDate(0, 0, 1)
		}
	} else {
		parsed, err := parseLocalTime(at, location)
		if err != nil {
			return time.Time{}, err
		}
		if !parsed.After(now) {
			return time.Time{}, &ArgumentError{Message: reminderPast}
		}
		due = parsed
	}
	if due.Sub(now) > MaxReminderAhead {
		return time.Time{}, &ArgumentError{Message: reminderTooFar}
	}
	return due, nil
}

func parseLocalTime(at string, location *time.Location) (time.Time, error) {
	for _, layout := range []string{timeLayout, "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if parsed, err := time.ParseInLocation(layout, at, location); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, &ArgumentError{Message: reminderBadAt}
}

func minutesUntil(now, due time.Time) int {
	return int((due.Sub(now) + 30*time.Second) / time.Minute)
}

func pickLanguage(language string) string {
	switch language {
	case "ru", "en":
		return language
	}
	return "uz"
}

func listReminders(ctx context.Context, opts RemindersOptions, caller Caller) (any, error) {
	waiting, err := opts.Store.WaitingReminders(ctx, caller.UserID, caller.ChatID)
	if err != nil {
		return nil, internal(err)
	}
	list := reminderList{Reminders: []reminderView{}, Timezone: opts.location().String()}
	for _, r := range waiting {
		list.Reminders = append(list.Reminders, reminderView{ID: r.ID, Kind: r.Kind, Text: r.Text, Due: r.DueAt.In(opts.location()).Format(timeLayout)})
	}
	if opts.CardEvery > 0 {
		chatID, err := opts.Store.CardAutoRemind(ctx, caller.UserID)
		if err != nil {
			return nil, internal(err)
		}
		list.CardAuto = chatID == caller.ChatID
	}
	return list, nil
}

func cancelReminder(ctx context.Context, opts RemindersOptions, caller Caller, args reminderArgs) (any, error) {
	if args.ID <= 0 {
		return nil, &ArgumentError{Message: reminderNoID}
	}
	cancelled, err := opts.Store.CancelReminder(ctx, args.ID, caller.UserID, caller.ChatID)
	if err != nil {
		return nil, internal(err)
	}
	if !cancelled {
		return nil, &DeniedError{Message: reminderNotFound}
	}
	return map[string]any{"cancelled": args.ID, "note": "Confirm in one short line."}, nil
}

// cardReminder schedules a reminder for when the caller may next draw a
// card: their last draw and CardEvery after. One who may draw now is told
// so instead. always turns on the reminder after every draw, too.
func cardReminder(ctx context.Context, opts RemindersOptions, caller Caller, args reminderArgs) (any, error) {
	language := pickLanguage(args.Language)
	if args.Always {
		if err := opts.Store.SetCardAutoRemind(ctx, caller.UserID, playerName(caller.Name), caller.ChatID, language); err != nil {
			return nil, internal(err)
		}
	}
	now := opts.now()
	_, last, err := opts.Store.CardPlayerOf(ctx, caller.UserID)
	if err != nil {
		return nil, internal(err)
	}
	result := reminderSet{Timezone: opts.location().String(), Always: args.Always}
	next := last.Add(opts.CardEvery)
	if last.IsZero() || !next.After(now) {
		result.CanDraw, result.Note = true, reminderCardNow
		if args.Always {
			result.Note += reminderAutoNote
		}
		return result, nil
	}
	saved, err := opts.Store.SetCardReminder(ctx, storage.Reminder{
		ChatID: caller.ChatID, UserID: caller.UserID, UserName: caller.Name, MessageID: caller.MessageID,
		Language: language, DueAt: next,
	})
	if err != nil {
		return nil, internal(err)
	}
	result.ID, result.Due, result.InMinutes = saved.ID, next.In(opts.location()).Format(timeLayout), max(1, minutesUntil(now, next))
	result.Note = reminderCardNote
	if args.Always {
		result.Note += reminderAutoNote
	}
	return result, nil
}

func cardRemindersOff(ctx context.Context, opts RemindersOptions, caller Caller, args reminderArgs) (any, error) {
	if err := opts.Store.SetCardAutoRemind(ctx, caller.UserID, playerName(caller.Name), 0, pickLanguage(args.Language)); err != nil {
		return nil, internal(err)
	}
	return map[string]any{"card_reminders": "off", "note": reminderOffNote}, nil
}

// playerName is the caller's first name, as the card table shows it,
// without the @username the log adds.
func playerName(name string) string {
	if at := strings.Index(name, " @"); at >= 0 {
		return name[:at]
	}
	return name
}
