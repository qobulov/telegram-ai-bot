package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

type fakeReports struct {
	mu      sync.Mutex
	reports []storage.Report
}

func (f *fakeReports) AddReport(_ context.Context, r storage.Report) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r.ID, r.Status, r.At = int64(len(f.reports)+1), "open", time.Now()
	f.reports = append(f.reports, r)
	return r.ID, nil
}

func (f *fakeReports) OpenReports(_ context.Context, chatID int64, _ time.Time, _ int) ([]storage.Report, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var open []storage.Report
	for _, r := range f.reports {
		if r.ChatID == chatID && r.Status == "open" {
			open = append(open, r)
		}
	}
	return open, nil
}

func (f *fakeReports) SettleReport(_ context.Context, id int64, status string, by int64) (storage.Report, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := &f.reports[id-1]
	if r.Status != "open" {
		return *r, false, nil
	}
	r.Status, r.HandledBy = status, by
	return *r, true, nil
}

type fakeWarnList struct{ warns []storage.MemberWarn }

func (f fakeWarnList) Warns(_ context.Context, _, userID int64, _ int) ([]storage.MemberWarn, error) {
	var out []storage.MemberWarn
	for _, w := range f.warns {
		if userID == 0 || w.UserID == userID {
			out = append(out, w)
		}
	}
	return out, nil
}

// A report is kept with buttons: a member's press is refused, a
// moderator's ban bans the reported member once; /reports lists the open
// ones to moderators alone; /warns shows warnings with their reason.
func TestReportsAndWarns(t *testing.T) {
	groupCalls = callTimes{}
	f := newFixture()
	f.opts.AdminList = fakeAdminList{admins: []telegram.ChatMember{{Status: "creator", User: &telegram.User{ID: 1, Username: "boss"}}}}
	f.opts.Ranks = &fakeRankBook{ranks: map[int64]int{40: 3}, overrides: map[string]int{}}
	moderator, buttons, reports := &fakeModerator{}, &fakeButtons{}, &fakeReports{}
	f.opts.GroupModerator, f.opts.Buttons, f.opts.Reports = moderator, buttons, reports
	f.opts.WarnList = fakeWarnList{warns: []storage.MemberWarn{{UserID: 30, Name: "Ali", Count: 2, Reason: "spam", At: time.Now()}}}

	sent := f.run(from(20, "/report", 30))
	if last := sent[len(sent)-1]; !strings.Contains(last.text, "Shikoyat") || len(last.buttons) != 1 || len(last.buttons[0]) != 3 {
		t.Fatalf("report = %+v", last)
	}
	sent = f.run(from(20, "/reports", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "⛔") {
		t.Errorf("member's /reports = %q", last)
	}
	sent = f.run(from(40, "/reports", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "(1)") || !strings.Contains(last, "salom") {
		t.Errorf("moderator's /reports = %q", last)
	}

	chat := telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup}
	press := func(id int64, data string) telegram.Update {
		return doozPlayer(int(id), &telegram.User{ID: id, FirstName: "M"}, chat, "", data)
	}
	f.run(press(21, "rp:b:1"))
	if len(moderator.banned) != 0 || !buttons.answers[len(buttons.answers)-1].alert {
		t.Fatalf("a member's press banned: %v", moderator.banned)
	}
	f.run(press(40, "rp:b:1"), press(40, "rp:b:1"))
	if len(moderator.banned) != 1 || moderator.banned[0] != 30 {
		t.Fatalf("banned = %v, want the reported member once", moderator.banned)
	}
	if last := buttons.edits[len(buttons.edits)-1]; !strings.Contains(last, "Bloklandi") {
		t.Errorf("settled report = %q", last)
	}
	sent = f.run(from(40, "/reports", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "📭") {
		t.Errorf("after settling = %q", last)
	}

	sent = f.run(from(40, "/warns", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "2/3") || !strings.Contains(last, "spam") {
		t.Errorf("/warns = %q", last)
	}
}

// The one who started a vote, or a moderator, can cancel it; anyone else
// cannot, and a press after the time ran out closes it as not banned.
func TestBanVoteCancelAndExpiry(t *testing.T) {
	f := newFixture()
	f.opts.Ranks = &fakeRankBook{ranks: map[int64]int{}, overrides: map[string]int{}}
	moderator, votes, buttons := &fakeModerator{}, &memoryVotes{}, &fakeButtons{}
	f.opts.GroupModerator, f.opts.BanVotes, f.opts.Buttons = moderator, votes, buttons
	chat := telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup}
	press := func(id int64, side string) telegram.Update {
		var vote banVote
		_ = json.Unmarshal(votes.states[groupID], &vote)
		return doozPlayer(int(id), &telegram.User{ID: id, FirstName: "V"}, chat, "", voteBanData+side+":"+vote.Token)
	}

	f.run(from(20, "/voteban", 30))
	f.run(press(21, "c"))
	if !buttons.answers[len(buttons.answers)-1].alert {
		t.Fatalf("another member cancelled: %+v", buttons.answers)
	}
	f.run(press(20, "c"))
	if last := buttons.edits[len(buttons.edits)-1]; !strings.Contains(last, "bekor") {
		t.Fatalf("cancelled = %q", last)
	}

	f.run(from(20, "/voteban", 30))
	var vote banVote
	_ = json.Unmarshal(votes.states[groupID], &vote)
	vote.Until = time.Now().Add(-time.Minute).Unix()
	votes.states[groupID], _ = json.Marshal(vote)
	f.run(press(22, "y"))
	if last := buttons.edits[len(buttons.edits)-1]; !strings.Contains(last, "Vaqt tugadi") || len(moderator.banned) != 0 {
		t.Fatalf("expired = %q, banned %v", last, moderator.banned)
	}
}

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
	if last := sent[len(sent)-1]; !strings.Contains(last.text, "bloklaymizmi") || len(last.buttons) != 2 {
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
