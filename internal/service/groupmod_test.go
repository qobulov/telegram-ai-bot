package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

type fakeBanList struct{ banned []storage.BannedMember }

func (f *fakeBanList) BannedMembers(context.Context, int64, int) ([]storage.BannedMember, error) {
	return f.banned, nil
}

// /banlist shows who was banned and why to a moderator, and not to a member.
func TestBanListIsForModerators(t *testing.T) {
	const moder, member = int64(11), int64(12)
	f := newFixture()
	f.opts.Ranks = &fakeRankBook{ranks: map[int64]int{moder: 1}, overrides: map[string]int{}}
	f.opts.BanList = &fakeBanList{banned: []storage.BannedMember{{UserID: 70, Name: "Spammer", Reason: "reklama", ByName: "Ali", At: time.Now()}}}
	sent := f.run(from(moder, "/banlist", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "Spammer") || !strings.Contains(last, "reklama") {
		t.Errorf("a moderator's banlist = %q", last)
	}
	sent = f.run(from(member, "/banlist", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "⛔") {
		t.Errorf("a member's banlist = %q", last)
	}
}
