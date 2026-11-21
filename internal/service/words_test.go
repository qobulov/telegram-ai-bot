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
	if user != 0 {
		return []storage.WordCount{{UserID: user, Name: "Vali", Words: 1234, Messages: 56}}, nil
	}
	return []storage.WordCount{{UserID: 1, Name: "Ali", Words: 12345, Messages: 600}, {UserID: 2, Name: "Vali", Words: 1234, Messages: 56}}, nil
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

// /words lists the last 7 days, with buttons for 30 days and the presser's
// own count; in private it says it works in a group.
func TestWordTable(t *testing.T) {
	f := newFixture()
	book, buttons := &fakeWordBook{}, &fakeButtons{}
	f.opts.Words, f.opts.Buttons = book, buttons
	now := time.Date(2026, 11, 21, 15, 0, 0, 0, f.opts.Location)
	f.opts.Now = func() time.Time { return now }

	sent := f.run(inGroup(groupMessage("/words")))
	last := sent[len(sent)-1]
	if !strings.Contains(last.text, "1. Ali — 12 345 so'z (600 xabar)") || !strings.Contains(last.text, "oxirgi 7 kun") {
		t.Fatalf("table = %q", last.text)
	}
	today := startOfDay(f.opts.Now(), f.opts.Location)
	if days := int(today.Sub(book.since).Hours()/24 + 0.5); days != 6 {
		t.Errorf("7 days start %d days back", days)
	}
	menu := last.buttons
	if len(menu) != 1 || len(menu[0]) != 3 || menu[0][0].Text != "✅ 7 kun" || menu[0][1].Data != "wrd:month" || menu[0][2].Data != "wrd:me" {
		t.Fatalf("buttons = %+v", menu)
	}
	press := func(id int64, data string, from int64) telegram.Update {
		return telegram.Update{UpdateID: 990 + id, CallbackQuery: &telegram.CallbackQuery{
			ID: fmt.Sprint(id), From: &telegram.User{ID: from, FirstName: "Vali"}, Data: data,
			Message: &telegram.Message{MessageID: 77, Chat: telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup}},
		}}
	}
	f.run(press(1, "wrd:month", 9))
	if len(buttons.edits) != 1 || !strings.Contains(buttons.edits[0], "oxirgi 30 kun") || buttons.menus[0][0][1].Text != "✅ 30 kun" {
		t.Fatalf("month press: edits %q, menus %+v", buttons.edits, buttons.menus)
	}
	if days := int(today.Sub(book.since).Hours()/24 + 0.5); days != 29 {
		t.Errorf("30 days start %d days back", days)
	}
	f.run(press(2, "wrd:me", 2))
	answer := buttons.answers[len(buttons.answers)-1]
	if !answer.alert || !strings.Contains(answer.text, "7 kun: 1 234 so'z, 56 xabar") || book.user != 2 || len(buttons.edits) != 1 {
		t.Errorf("own press = %+v (user %d), edits %d", answer, book.user, len(buttons.edits))
	}
	if sent := f.run(textFrom(userID, "/words")); !strings.Contains(sent[len(sent)-1].text, "guruhda") {
		t.Errorf("in private = %+v", sent[len(sent)-1])
	}
}
