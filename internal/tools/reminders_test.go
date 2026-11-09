package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

// reminderNow is 14:30 in Tashkent.
var reminderNow = time.Date(2026, 11, 9, 14, 30, 0, 0, tashkent)

type fakeReminders struct {
	saved     []storage.Reminder
	card      *storage.Reminder
	autoChat  int64
	autoSet   bool
	lastDraw  time.Time
	limitFull bool
}

func (f *fakeReminders) AddReminder(_ context.Context, r storage.Reminder, perUser, perChat int) (storage.Reminder, error) {
	if f.limitFull || perUser != ReminderPerUser || perChat != ReminderPerChat {
		return storage.Reminder{}, storage.ErrReminderLimit
	}
	r.ID, r.Kind = int64(len(f.saved)+1), storage.ReminderText
	f.saved = append(f.saved, r)
	return r, nil
}

func (f *fakeReminders) SetCardReminder(_ context.Context, r storage.Reminder) (storage.Reminder, error) {
	r.ID, r.Kind = 99, storage.ReminderCard
	f.card = &r
	return r, nil
}

func (f *fakeReminders) SetCardAutoRemind(_ context.Context, _ int64, _ string, chatID int64, _ string) error {
	f.autoChat, f.autoSet = chatID, true
	return nil
}

func (f *fakeReminders) CardAutoRemind(context.Context, int64) (int64, error) { return f.autoChat, nil }

func (f *fakeReminders) WaitingReminders(_ context.Context, userID, chatID int64) ([]storage.Reminder, error) {
	var mine []storage.Reminder
	for _, r := range f.saved {
		if r.UserID == userID && r.ChatID == chatID {
			mine = append(mine, r)
		}
	}
	return mine, nil
}

// CancelReminder deletes only the user's own, in that chat, as the store
// does.
func (f *fakeReminders) CancelReminder(_ context.Context, id, userID, chatID int64) (bool, error) {
	for i, r := range f.saved {
		if r.ID == id && r.UserID == userID && r.ChatID == chatID {
			f.saved = append(f.saved[:i], f.saved[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeReminders) CardPlayerOf(context.Context, int64) (storage.CardPlayer, time.Time, error) {
	return storage.CardPlayer{}, f.lastDraw, nil
}

func remindersRegistry(t *testing.T, store *fakeReminders) *Registry {
	t.Helper()
	return mustRegistry(t, RemindersTool(RemindersOptions{Store: store, CardEvery: time.Hour, Location: tashkent, Now: func() time.Time { return reminderNow }}))
}

// remindIn runs the tool as a person's message in chatID.
func remindIn(t *testing.T, registry *Registry, chatID, userID int64, args string) envelope {
	t.Helper()
	ctx := WithTurn(context.Background(), Caller{UserID: userID, ChatID: chatID, Name: "Ali @ali", Human: true, MessageID: 55}, nil)
	return decodeEnvelope(t, registry.Execute(ctx, userID, "reminders", args))
}

func intp(n int) *int { return &n }

func TestReminderDue(t *testing.T) {
	for _, c := range []struct {
		name      string
		inMinutes *int
		at        string
		want      time.Time // zero: refused
	}{
		{"in ten minutes", intp(10), "", reminderNow.Add(10 * time.Minute)},
		{"in thirty days", intp(43200), "", reminderNow.Add(MaxReminderAhead)},
		{"in no minutes", intp(0), "", time.Time{}},
		{"in the past", intp(-5), "", time.Time{}},
		{"beyond thirty days", intp(43201), "", time.Time{}},
		{"neither", nil, "", time.Time{}},
		{"both", intp(5), "18:00", time.Time{}},
		{"later today", nil, "18:00", time.Date(2026, 11, 9, 18, 0, 0, 0, tashkent)},
		{"a clock time passed is tomorrow's", nil, "09:00", time.Date(2026, 11, 10, 9, 0, 0, 0, tashkent)},
		{"this very minute is tomorrow's", nil, "14:30", time.Date(2026, 11, 10, 14, 30, 0, 0, tashkent)},
		{"a date", nil, "2026-11-10 09:00", time.Date(2026, 11, 10, 9, 0, 0, 0, tashkent)},
		{"a date with T", nil, "2026-11-10T09:00", time.Date(2026, 11, 10, 9, 0, 0, 0, tashkent)},
		{"a date passed", nil, "2026-11-09 10:00", time.Time{}},
		{"a date too far", nil, "2026-12-25 10:00", time.Time{}},
		{"not a time", nil, "ertaga", time.Time{}},
		{"not a clock", nil, "25:00", time.Time{}},
	} {
		got, err := ReminderDue(c.inMinutes, c.at, reminderNow, tashkent)
		switch {
		case c.want.IsZero() && err == nil:
			t.Errorf("%s: due %v, want refused", c.name, got)
		case !c.want.IsZero() && (err != nil || !got.Equal(c.want)):
			t.Errorf("%s: due %v, %v; want %v", c.name, got, err, c.want)
		}
	}
}

// A reminder is saved for the chat it was asked in, by whom and for which
// message, with its local time; the model's words carry no tags.
func TestSetReminder(t *testing.T) {
	store := &fakeReminders{}
	registry := remindersRegistry(t, store)
	env := remindIn(t, registry, -100, 7, `{"action":"set","text":"[Vali](tg://user?id=3) ga qo'ng'iroq qil","at":"18:00"}`)
	if !env.OK {
		t.Fatalf("envelope = %+v", env)
	}
	var result struct {
		ID        int64  `json:"id"`
		Due       string `json:"due"`
		InMinutes int    `json:"in_minutes"`
		Timezone  string `json:"timezone"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.Due != "2026-11-09 18:00" || result.InMinutes != 210 || result.Timezone != "Asia/Tashkent" || result.ID != 1 {
		t.Errorf("result = %+v", result)
	}
	saved := store.saved[0]
	if saved.ChatID != -100 || saved.UserID != 7 || saved.MessageID != 55 || saved.Text != "Vali ga qo'ng'iroq qil" || saved.UserName != "Ali @ali" {
		t.Errorf("saved = %+v", saved)
	}
}

func TestSetReminderRefusals(t *testing.T) {
	store := &fakeReminders{}
	registry := remindersRegistry(t, store)
	for _, c := range []struct{ args, code string }{
		{`{"action":"set","in_minutes":10}`, "invalid_arguments"},
		{`{"action":"set","text":"x"}`, "invalid_arguments"},
		{`{"action":"set","text":"x","in_minutes":0}`, "invalid_arguments"},
		{`{"action":"set","text":"x","at":"2026-11-08 10:00"}`, "invalid_arguments"},
		{`{"action":"set","text":"x","at":"2027-01-01 10:00"}`, "invalid_arguments"},
		{`{"action":"set","text":"x","in_minutes":5,"chat_id":1}`, "invalid_arguments"},
		{`{"action":"snooze"}`, "invalid_arguments"},
	} {
		if env := remindIn(t, registry, 7, 7, c.args); env.OK || env.Error.Code != c.code {
			t.Errorf("%s: envelope = %+v, want %s", c.args, env, c.code)
		}
	}
	if len(store.saved) != 0 {
		t.Errorf("refused calls saved %+v", store.saved)
	}

	store.limitFull = true
	if env := remindIn(t, registry, 7, 7, `{"action":"set","text":"x","in_minutes":5}`); env.OK || env.Error.Code != "forbidden" {
		t.Errorf("past the limit: %+v", env)
	}

	// Not a person's turn: a bot, or no turn at all.
	ctx := WithTurn(context.Background(), Caller{UserID: 7, ChatID: 7, Human: false}, nil)
	if env := decodeEnvelope(t, registry.Execute(ctx, 7, "reminders", `{"action":"list"}`)); env.OK {
		t.Errorf("a bot listed reminders: %+v", env)
	}
	if env := decodeEnvelope(t, registry.Execute(context.Background(), 7, "reminders", `{"action":"list"}`)); env.OK {
		t.Errorf("no turn listed reminders: %+v", env)
	}
	// A business chat's customer is never offered it.
	ctx = WithTurn(context.Background(), Caller{UserID: 7, ChatID: 7, Human: true, Business: true}, nil)
	if env := decodeEnvelope(t, registry.Execute(ctx, 7, "reminders", `{"action":"list"}`)); env.OK || env.Error.Code != "forbidden" {
		t.Errorf("a business chat: %+v", env)
	}
}

// A member lists and cancels only their own reminders, in the chat they
// set them in.
func TestListAndCancelOnlyOwnReminders(t *testing.T) {
	store := &fakeReminders{}
	registry := remindersRegistry(t, store)
	remindIn(t, registry, -100, 7, `{"action":"set","text":"Ali's","in_minutes":5}`)
	remindIn(t, registry, -100, 8, `{"action":"set","text":"Bobur's","in_minutes":5}`)

	env := remindIn(t, registry, -100, 8, `{"action":"list"}`)
	var list struct {
		Reminders []struct {
			ID   int64  `json:"id"`
			Text string `json:"text"`
			Due  string `json:"due"`
		} `json:"reminders"`
	}
	if err := json.Unmarshal(env.Result, &list); err != nil || len(list.Reminders) != 1 || list.Reminders[0].Text != "Bobur's" || list.Reminders[0].Due != "2026-11-09 14:35" {
		t.Fatalf("Bobur's list = %s, %v", env.Result, err)
	}

	if env := remindIn(t, registry, -100, 8, `{"action":"cancel","id":1}`); env.OK || env.Error.Code != "forbidden" {
		t.Errorf("Bobur cancelling Ali's: %+v", env)
	}
	if env := remindIn(t, registry, 7, 7, `{"action":"cancel","id":1}`); env.OK {
		t.Errorf("Ali cancelling it from his private chat: %+v", env)
	}
	if env := remindIn(t, registry, -100, 7, `{"action":"cancel"}`); env.OK || env.Error.Code != "invalid_arguments" {
		t.Errorf("no id: %+v", env)
	}
	if env := remindIn(t, registry, -100, 7, `{"action":"cancel","id":1}`); !env.OK {
		t.Errorf("Ali cancelling his own: %+v", env)
	}
	if len(store.saved) != 1 || store.saved[0].UserID != 8 {
		t.Errorf("left = %+v", store.saved)
	}
}

// A card reminder falls at the last draw and an hour after; one who may
// open a card now is told so and nothing is scheduled; always turns on the
// reminder after every draw, and card_off turns it off.
func TestCardReminder(t *testing.T) {
	store := &fakeReminders{lastDraw: reminderNow.Add(-20 * time.Minute)}
	registry := remindersRegistry(t, store)
	env := remindIn(t, registry, 7, 7, `{"action":"card","language":"ru"}`)
	var result struct {
		Due       string `json:"due"`
		InMinutes int    `json:"in_minutes"`
		CanOpen   bool   `json:"can_open_card_now"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil || !env.OK || result.Due != "2026-11-09 15:10" || result.InMinutes != 40 || result.CanOpen {
		t.Fatalf("card = %+v %s", env, env.Result)
	}
	if store.card == nil || !store.card.DueAt.Equal(reminderNow.Add(40*time.Minute)) || store.card.Language != "ru" || store.card.ChatID != 7 || store.autoSet {
		t.Errorf("card reminder = %+v, auto %v", store.card, store.autoSet)
	}

	store = &fakeReminders{lastDraw: reminderNow.Add(-2 * time.Hour)}
	registry = remindersRegistry(t, store)
	env = remindIn(t, registry, -100, 7, `{"action":"card","always":true}`)
	if err := json.Unmarshal(env.Result, &result); err != nil || !result.CanOpen || store.card != nil || store.autoChat != -100 {
		t.Errorf("may open now: %s, card %+v, auto chat %d", env.Result, store.card, store.autoChat)
	}

	env = remindIn(t, registry, -100, 7, `{"action":"list"}`)
	var list struct {
		Always bool `json:"card_reminders_always"`
	}
	if err := json.Unmarshal(env.Result, &list); err != nil || !list.Always {
		t.Errorf("list = %s", env.Result)
	}

	if env := remindIn(t, registry, -100, 7, `{"action":"card_off"}`); !env.OK || store.autoChat != 0 {
		t.Errorf("card_off = %+v, auto chat %d", env, store.autoChat)
	}

	// Without the cards there are no card reminders, and the tool says so.
	registry = mustRegistry(t, RemindersTool(RemindersOptions{Store: store, Location: tashkent}))
	if env := remindIn(t, registry, 7, 7, `{"action":"card"}`); env.OK || env.Error.Code != "forbidden" {
		t.Errorf("no cards: %+v", env)
	}
}
