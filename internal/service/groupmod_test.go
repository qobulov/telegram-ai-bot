package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
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

type unbanningModerator struct {
	fakeModerator
	unbanned []int64
}

func (f *unbanningModerator) UnbanChatMember(_ context.Context, _, userID int64) error {
	f.unbanned = append(f.unbanned, userID)
	return nil
}

// /banlist offers an unban button for each ban; a member's press is
// refused, a moderator's lifts the ban and notes it.
func TestBanListUnbanButton(t *testing.T) {
	const moder, member = int64(11), int64(12)
	f := newFixture()
	f.opts.Ranks = &fakeRankBook{ranks: map[int64]int{moder: 1}, overrides: map[string]int{}}
	f.opts.BanList = &fakeBanList{banned: []storage.BannedMember{{UserID: 70, Name: "Spammer", At: time.Now()}}}
	moderator, buttons, strikes := &unbanningModerator{}, &fakeButtons{}, &fakeStrikes{}
	f.opts.GroupModerator, f.opts.Buttons, f.opts.Strikes = moderator, buttons, strikes

	sent := f.run(from(moder, "/banlist", 0))
	last := sent[len(sent)-1]
	if len(last.buttons) != 1 || last.buttons[0][0].Data != unbanData+"70" {
		t.Fatalf("banlist buttons = %+v", last.buttons)
	}
	chat := telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup}
	press := func(id int64) telegram.Update {
		return pressUpdate(int(id), &telegram.User{ID: id, FirstName: "M"}, chat, "", unbanData+"70")
	}
	f.run(press(member))
	if len(moderator.unbanned) != 0 {
		t.Fatalf("a member unbanned: %v", moderator.unbanned)
	}
	f.run(press(moder))
	if len(moderator.unbanned) != 1 || moderator.unbanned[0] != 70 {
		t.Fatalf("unbanned = %v", moderator.unbanned)
	}
	if len(strikes.actions) != 1 || strikes.actions[0].Action != "unban" {
		t.Errorf("noted = %+v", strikes.actions)
	}
}
