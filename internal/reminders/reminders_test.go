package reminders

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

var now = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

type fakeStore struct {
	due      []storage.Reminder
	outcomes map[int64]string
	retried  map[int64]time.Time
}

func (f *fakeStore) ClaimDueReminders(_ context.Context, _ time.Time, limit int) ([]storage.Reminder, error) {
	n := min(limit, len(f.due))
	claimed := f.due[:n]
	f.due = f.due[n:]
	return claimed, nil
}

func (f *fakeStore) FinishReminder(_ context.Context, id int64, outcome string) error {
	f.outcomes[id] = outcome
	return nil
}

func (f *fakeStore) RetryReminder(_ context.Context, id int64, at time.Time) error {
	f.retried[id] = at
	return nil
}

// fakeSender fails the chats in fail with their error, and keeps the rest.
type fakeSender struct {
	fail map[int64]error
	sent []telegram.Outgoing
}

func (f *fakeSender) SendMessage(_ context.Context, msg telegram.Outgoing) (int64, error) {
	if err := f.fail[msg.ChatID]; err != nil {
		return 0, err
	}
	f.sent = append(f.sent, msg)
	return 1, nil
}

func fireAll(t *testing.T, due []storage.Reminder, fail map[int64]error) (Summary, *fakeStore, *fakeSender) {
	t.Helper()
	store := &fakeStore{due: due, outcomes: map[int64]string{}, retried: map[int64]time.Time{}}
	sender := &fakeSender{fail: fail}
	summary, err := Fire(context.Background(), Options{
		Store: store, Sender: sender, CardsURL: "https://bot.example/kartalar/", BotUsername: "maria_bot",
		CardEvery: time.Hour, Now: func() time.Time { return now }, Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	return summary, store, sender
}

// A blocked bot, a chat gone or Telegram's rate limit fail only their own
// reminder: the rest go out, the rate-limited one is tried again a minute
// later, and one tried too often is given up.
func TestFireSendsEachAndRecordsFailures(t *testing.T) {
	blocked, gone, limited, tired := int64(11), int64(-12), int64(13), int64(14)
	due := []storage.Reminder{
		{ID: 1, ChatID: blocked, UserID: blocked, Kind: storage.ReminderText, Text: "a", Attempts: 1},
		{ID: 2, ChatID: 20, UserID: 20, Kind: storage.ReminderText, Text: "suv ich", Attempts: 1},
		{ID: 3, ChatID: gone, UserID: 30, Kind: storage.ReminderText, Text: "c", Attempts: 1},
		{ID: 4, ChatID: limited, UserID: limited, Kind: storage.ReminderText, Text: "d", Attempts: 1},
		{ID: 5, ChatID: tired, UserID: tired, Kind: storage.ReminderText, Text: "e", Attempts: maxAttempts},
		{ID: 6, ChatID: 60, UserID: 60, Kind: storage.ReminderText, Text: "f", Attempts: 1},
	}
	summary, store, sender := fireAll(t, due, map[int64]error{
		blocked: &telegram.APIError{Code: http.StatusForbidden, Description: "bot was blocked by the user"},
		gone:    &telegram.APIError{Code: http.StatusBadRequest, Description: "chat not found"},
		limited: &telegram.APIError{Code: http.StatusTooManyRequests},
		tired:   &telegram.APIError{Code: http.StatusBadGateway},
	})
	if summary != (Summary{Sent: 2, Failed: 3, Retried: 1}) {
		t.Errorf("summary = %+v", summary)
	}
	want := map[int64]string{1: storage.ReminderFailed, 2: storage.ReminderSent, 3: storage.ReminderFailed, 5: storage.ReminderFailed, 6: storage.ReminderSent}
	for id, outcome := range want {
		if store.outcomes[id] != outcome {
			t.Errorf("reminder %d: outcome %q, want %q", id, store.outcomes[id], outcome)
		}
	}
	if at, ok := store.retried[4]; !ok || !at.Equal(now.Add(retryDelay)) || store.outcomes[4] != "" {
		t.Errorf("the rate-limited one: retried %v at %v, outcome %q", ok, at, store.outcomes[4])
	}
	if len(sender.sent) != 2 || sender.sent[0].Text != "⏰ suv ich" {
		t.Errorf("sent = %+v", sender.sent)
	}
}

// A send without Telegram's answer may have gone out, so it is not sent
// again.
func TestFireDoesNotRetryWithoutAnAnswer(t *testing.T) {
	summary, store, _ := fireAll(t, []storage.Reminder{{ID: 1, ChatID: 5, UserID: 5, Kind: storage.ReminderText, Text: "x", Attempts: 1}},
		map[int64]error{5: errors.New("connection reset")})
	if summary != (Summary{Failed: 1}) || store.outcomes[1] != storage.ReminderFailed || len(store.retried) != 0 {
		t.Errorf("summary = %+v, outcomes = %v, retried = %v", summary, store.outcomes, store.retried)
	}
}

// A card reminder is skipped when the player drew since it was set, so
// that no card may be opened yet; otherwise it carries the Mini App:
// itself in a private chat, a link to the private chat in a group.
func TestFireCardReminders(t *testing.T) {
	due := []storage.Reminder{
		{ID: 1, ChatID: 7, UserID: 7, Kind: storage.ReminderCard, Language: "uz", LastDraw: now.Add(-time.Hour)},
		{ID: 2, ChatID: 8, UserID: 8, Kind: storage.ReminderCard, Language: "uz", LastDraw: now.Add(-10 * time.Minute)},
		{ID: 3, ChatID: -100, UserID: 9, UserName: "Ali @ali", MessageID: 42, Kind: storage.ReminderCard, Language: "ru", LastDraw: now.Add(-2 * time.Hour)},
	}
	summary, store, sender := fireAll(t, due, nil)
	if summary != (Summary{Sent: 2, Skipped: 1}) || store.outcomes[2] != storage.ReminderSkipped {
		t.Fatalf("summary = %+v, outcomes = %v", summary, store.outcomes)
	}
	private, group := sender.sent[0], sender.sent[1]
	if !strings.Contains(private.Text, "/card") || strings.Contains(private.Text, "@maria_bot") ||
		len(private.Buttons) != 1 || private.Buttons[0][0].WebApp == nil || private.Buttons[0][0].WebApp.URL != "https://bot.example/kartalar/" {
		t.Errorf("private card reminder = %+v", private)
	}
	if !strings.HasPrefix(group.Text, "⏰ [Ali](tg://user?id=9)\n") || !strings.Contains(group.Text, "/card@maria_bot") || !strings.Contains(group.Text, "карту") ||
		group.ReplyToMessageID != 42 || group.Buttons[0][0].URL != "https://t.me/maria_bot?start=kartalar" || group.Buttons[0][0].WebApp != nil {
		t.Errorf("group card reminder = %+v", group)
	}
}

// In a group the member is tagged by ID, their name stripped of what
// would break the tag; the reminder's own words carry no tags.
func TestMessageTagsTheMemberOnly(t *testing.T) {
	msg := Message(Options{}, storage.Reminder{ChatID: -5, UserID: 9, UserName: "[Bo]b (x) @bob", MessageID: 3, Kind: storage.ReminderText, Text: "uyga bor"})
	if msg.Text != "⏰ [Bob x](tg://user?id=9)\nuyga bor" || msg.ReplyToMessageID != 3 || msg.ChatID != -5 {
		t.Errorf("message = %+v", msg)
	}
	if got := Mention(9, ""); got != "[👤](tg://user?id=9)" {
		t.Errorf("no name: %q", got)
	}
	if got := CleanText("  ayt [Vali](tg://user?id=123) ga  "); got != "ayt Vali ga" {
		t.Errorf("CleanText = %q", got)
	}
}

// What a call claimed but had no time left for is given back, not lost.
func TestFireGivesBackWhatItHasNoTimeFor(t *testing.T) {
	store := &fakeStore{due: []storage.Reminder{{ID: 1, ChatID: 1, Kind: storage.ReminderText, Text: "a"}, {ID: 2, ChatID: 2, Kind: storage.ReminderText, Text: "b"}}, outcomes: map[int64]string{}, retried: map[int64]time.Time{}}
	sender := &fakeSender{}
	clock := now
	summary, err := Fire(context.Background(), Options{Store: store, Sender: sender, Now: func() time.Time {
		clock = clock.Add(2 * time.Second)
		return clock
	}})
	if err != nil || summary.Sent != 1 || summary.Retried != 1 {
		t.Fatalf("summary = %+v, %v", summary, err)
	}
	if _, ok := store.retried[2]; !ok || store.outcomes[1] != storage.ReminderSent {
		t.Errorf("retried = %v, outcomes = %v", store.retried, store.outcomes)
	}
}
