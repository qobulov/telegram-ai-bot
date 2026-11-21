package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// fakeWordBook keeps the words counted and lists a fixed table for /words.
type fakeWordBook struct {
	mu      sync.Mutex
	counted []string
	since   time.Time
	user    int64
}

func (f *fakeWordBook) AddMemberWords(_ context.Context, _, userID int64, name string, _ time.Time, words int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counted = append(f.counted, fmt.Sprintf("%d:%s:%d", userID, name, words))
	return nil
}

func (f *fakeWordBook) WordTop(_ context.Context, _ int64, since time.Time, user int64, _ int) ([]storage.WordCount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.since, f.user = since, user
	return []storage.WordCount{{UserID: 1, Name: "Ali", Words: 12345, Messages: 600}, {UserID: 2, Name: "Vali", Words: 1234, Messages: 56}}, nil
}

func (f *fakeWordBook) WordPlace(_ context.Context, _ int64, _ time.Time, user int64) (int, storage.WordCount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.user = user
	if user == 2 {
		return 2, storage.WordCount{UserID: 2, Name: "Vali", Words: 1234}, nil
	}
	return 76, storage.WordCount{UserID: user, Name: "Sam", Words: 692, Messages: 31}, nil
}

// A member's message is counted by its words; a bot's is not.
func TestWordsAreCounted(t *testing.T) {
	f := newFixture()
	book := &fakeWordBook{}
	f.opts.Words = book
	said := groupMessage("salom hammaga  qalaysizlar")
	said.From = &telegram.User{ID: 501, FirstName: "Sam"}
	bot := groupMessage("men botman")
	bot.From = &telegram.User{ID: 502, FirstName: "Bot", IsBot: true}
	f.run(inGroup(said), inGroup(bot))
	if strings.Join(book.counted, " ") != "501:Sam:3" {
		t.Fatalf("counted = %v", book.counted)
	}
}

// /words lists the top 10 of the last 7 days, the asker's own place under
// it when they are not in it, with buttons for 7 and 30 days;
// in private it says it works in a group.
func TestWordTable(t *testing.T) {
	f := newFixture()
	book, buttons := &fakeWordBook{}, &fakeButtons{}
	f.opts.Words, f.opts.Buttons = book, buttons
	now := time.Date(2026, 11, 21, 15, 0, 0, 0, f.opts.Location)
	f.opts.Now = func() time.Time { return now }

	asked := groupMessage("/words")
	asked.Chat.Username = "tovbotn"
	asked.From = &telegram.User{ID: 501, FirstName: "Sam"}
	sent := f.run(inGroup(asked))
	last := sent[len(sent)-1]
	for _, want := range []string{"Top-10", "oxirgi 7 kun", "1. Ali — 12 345 so'z, 600 xabar", "2. Vali — 1 234 so'z, 56 xabar", "---------------------\n76. Sam — 692 so'z, 31 xabar"} {
		if !strings.Contains(last.text, want) {
			t.Fatalf("table = %q, want %q in it", last.text, want)
		}
	}
	today := startOfDay(f.opts.Now(), f.opts.Location)
	if days := int(today.Sub(book.since).Hours()/24 + 0.5); days != 6 {
		t.Errorf("7 days start %d days back", days)
	}
	menu := last.buttons
	if len(menu) != 1 || len(menu[0]) != 2 || menu[0][0].Text != "✅ 7 kun" || menu[0][1].Data != "wrd:month" {
		t.Fatalf("buttons = %+v", menu)
	}
	// Vali, in the top, gets no line of their own under it.
	f.run(telegram.Update{UpdateID: 991, CallbackQuery: &telegram.CallbackQuery{
		ID: "1", From: &telegram.User{ID: 2, FirstName: "Vali"}, Data: "wrd:month",
		Message: &telegram.Message{MessageID: 77, Chat: telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup, Title: "Guruh"}},
	}})
	if len(buttons.edits) != 1 || !strings.Contains(buttons.edits[0], "oxirgi 30 kun") || strings.Contains(buttons.edits[0], "-----") ||
		strings.Contains(buttons.edits[0], "Guruh") || buttons.menus[0][0][1].Text != "✅ 30 kun" {
		t.Fatalf("month press: edits %q, menus %+v", buttons.edits, buttons.menus)
	}
	if days := int(today.Sub(book.since).Hours()/24 + 0.5); days != 29 {
		t.Errorf("30 days start %d days back", days)
	}
	if sent := f.run(textFrom(userID, "/words")); !strings.Contains(sent[len(sent)-1].text, "guruhda") {
		t.Errorf("in private = %+v", sent[len(sent)-1])
	}
}
