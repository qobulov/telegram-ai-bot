package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// memoryVotes keeps ban votes as memoryLobbies keeps lobbies.
type memoryVotes struct{ memoryLobbies }

func (m *memoryVotes) ChangeBanVote(ctx context.Context, chatID int64, change func([]byte) ([]byte, error)) error {
	return m.ChangeTeamLobby(ctx, chatID, change)
}

// /admins tags the admins and moderators once in a while; /report quotes
// the message to them; /voteban bans at five votes for, each member voting
// once, the target never.
func TestCallsReportsAndBanVotes(t *testing.T) {
	groupCalls = callTimes{}
	f := newFixture()
	f.opts.AdminList = fakeAdminList{admins: []telegram.ChatMember{
		{Status: "creator", User: &telegram.User{ID: 1, Username: "boss"}},
		{Status: "administrator", User: &telegram.User{ID: 99, IsBot: true, Username: "somebot"}},
	}}
	f.opts.Ranks = &fakeRankBook{ranks: map[int64]int{}, overrides: map[string]int{}}
	moderator := &fakeModerator{}
	votes := &memoryVotes{}
	buttons := &fakeButtons{}
	f.opts.GroupModerator, f.opts.BanVotes, f.opts.Buttons = moderator, votes, buttons

	sent := f.run(from(20, "/admins", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "@boss") || strings.Contains(last, "somebot") {
		t.Errorf("admins = %q", last)
	}
	sent = f.run(from(21, "/admins", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "⏳") {
		t.Errorf("a second call at once = %q", last)
	}
	sent = f.run(from(20, "/report", 30))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "Shikoyat") || !strings.Contains(last, "salom") || !strings.Contains(last, "@boss") {
		t.Errorf("report = %q", last)
	}

	sent = f.run(from(20, "/voteban", 30))
	if last := sent[len(sent)-1]; !strings.Contains(last.text, "bloklaymizmi") || len(last.buttons) != 1 {
		t.Fatalf("vote = %+v", last)
	}
	var vote banVote
	_ = json.Unmarshal(votes.states[groupID], &vote)
	chat := telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup}
	press := func(id int64, side string) telegram.Update {
		return doozPlayer(int(id), &telegram.User{ID: id, FirstName: "V"}, chat, "", voteBanData+side+":"+vote.Token)
	}
	f.run(press(30, "y"), press(21, "y"), press(21, "y"), press(22, "n"), press(23, "y"), press(24, "y"))
	if len(moderator.banned) != 0 {
		t.Fatalf("banned at four votes for: %v", moderator.banned)
	}
	f.run(press(25, "y"))
	if len(moderator.banned) != 1 || moderator.banned[0] != 30 {
		t.Fatalf("banned = %v, want the target at five votes", moderator.banned)
	}
}
