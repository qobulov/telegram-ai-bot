package service

import (
	"context"
	"strings"
	"sync"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

type fakeMemberLinks struct {
	mu    sync.Mutex
	links map[int64]string
}

func (f *fakeMemberLinks) MemberLink(_ context.Context, _, userID int64) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	link, ok := f.links[userID]
	return link, ok, nil
}
func (f *fakeMemberLinks) SaveMemberLink(_ context.Context, _, userID int64, link string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.links[userID] = link
	return nil
}

type fakeLinkMaker struct{ made []telegram.InviteLink }

func (f *fakeLinkMaker) CreateChatInviteLink(_ context.Context, l telegram.InviteLink) (string, error) {
	f.made = append(f.made, l)
	return "https://t.me/+own" + l.Name[1:4], nil
}

// /mylink makes a member's own link once; whoever joins by it counts for the
// member, not for the bot that made it.
func TestMyLinkCountsForItsMember(t *testing.T) {
	f := newFixture()
	book := &fakeInviteBook{}
	links := &fakeMemberLinks{links: map[int64]string{}}
	maker := &fakeLinkMaker{}
	f.opts.Invites, f.opts.MemberLinks, f.opts.LinkMaker = book, links, maker
	sent := f.run(from(321, "/mylink", 0), from(321, "/mylink", 0))
	if len(maker.made) != 1 || !strings.HasPrefix(maker.made[0].Name, "m321 ") || !strings.Contains(sent[len(sent)-1].text, "https://t.me/+own") {
		t.Fatalf("made %+v, sent %+v", maker.made, sent)
	}
	nextUpdateID++
	join := telegram.Update{UpdateID: nextUpdateID, ChatMember: &telegram.ChatMemberUpdated{
		Chat:          telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup},
		From:          &telegram.User{ID: 600},
		OldChatMember: telegram.ChatMember{Status: "left", User: &telegram.User{ID: 600}},
		NewChatMember: telegram.ChatMember{Status: "member", User: &telegram.User{ID: 600, FirstName: "New"}},
		InviteLink:    &telegram.ChatInviteLink{InviteLink: links.links[321], Name: maker.made[0].Name, Creator: &telegram.User{ID: botID, IsBot: true}},
	}}
	f.run(join)
	if len(book.noted) != 1 || book.noted[0].InviterID != 321 || book.noted[0].Via != "link" || book.noted[0].UserID != 600 {
		t.Fatalf("noted = %+v, want the link's member", book.noted)
	}
}
