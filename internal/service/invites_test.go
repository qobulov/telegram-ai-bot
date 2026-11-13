package service

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// fakeInviteBook keeps the invites noted and lists them for /qoshganlar.
type fakeInviteBook struct {
	mu    sync.Mutex
	noted []storage.Invite
	since time.Time
}

func (f *fakeInviteBook) RecordInvite(_ context.Context, inv storage.Invite) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.noted = append(f.noted, inv)
	return nil
}

func (f *fakeInviteBook) InviteTop(_ context.Context, _ int64, since time.Time, _ int64, _ int) ([]storage.InviterCount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.since = since
	return []storage.InviterCount{{UserID: 1, Name: "Ali", Added: 17, Stayed: 15}, {UserID: 2, Name: "Vali", Added: 3, Stayed: 3}}, nil
}

// A member added by another counts for them, and one by another's link for
// the link's maker; one who came alone, or a bot, counts for no one.
func TestInvitesAreNotedAndListed(t *testing.T) {
	f := newFixture()
	book := &fakeInviteBook{}
	f.opts.Invites = book
	ali := &telegram.User{ID: userID, FirstName: "Ali"}
	added := groupMessage("")
	added.From = ali
	added.NewChatMembers = []telegram.User{{ID: 501, FirstName: "Sam"}, {ID: 502, FirstName: "Bot", IsBot: true}}
	alone := groupMessage("")
	alone.From = &telegram.User{ID: 503, FirstName: "Kim"}
	alone.NewChatMembers = []telegram.User{{ID: 503, FirstName: "Kim"}}
	nextUpdateID++
	byLink := telegram.Update{UpdateID: nextUpdateID, ChatMember: &telegram.ChatMemberUpdated{
		Chat:          telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup},
		From:          &telegram.User{ID: 504},
		OldChatMember: telegram.ChatMember{Status: "left", User: &telegram.User{ID: 504}},
		NewChatMember: telegram.ChatMember{Status: "member", User: &telegram.User{ID: 504, FirstName: "Lin"}},
		InviteLink:    &telegram.ChatInviteLink{InviteLink: "https://t.me/+x", Creator: &telegram.User{ID: 77, FirstName: "Vali"}},
	}}
	f.run(inGroup(added), inGroup(alone), byLink)
	if len(book.noted) != 2 || book.noted[0].UserID != 501 || book.noted[0].InviterID != userID || book.noted[0].Via != "add" ||
		book.noted[1].UserID != 504 || book.noted[1].InviterID != 77 || book.noted[1].Via != "link" {
		t.Fatalf("noted = %+v", book.noted)
	}

	sent := f.run(inGroup(groupMessage("/qoshganlar")))
	got := sent[len(sent)-1].text
	if !strings.Contains(got, "1. Ali — 15 (yana 2 tasi chiqib ketgan)") || !strings.Contains(got, "2. Vali — 3") || !strings.Contains(got, "bu hafta") {
		t.Fatalf("table = %q", got)
	}
	if wd := book.since.In(f.opts.Location).Weekday(); wd != time.Monday {
		t.Errorf("the week starts on %v", wd)
	}
	if sent = f.run(textFrom(userID, "/qoshganlar")); !strings.Contains(sent[len(sent)-1].text, "guruhda") {
		t.Errorf("in private = %+v", sent[len(sent)-1])
	}
}
