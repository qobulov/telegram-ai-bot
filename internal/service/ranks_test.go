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

type fakeRankBook struct {
	mu        sync.Mutex
	ranks     map[int64]int
	log       []storage.RankChange
	overrides map[string]int
}

func (f *fakeRankBook) SetRank(_ context.Context, _, userID int64, rank int, by int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if rank == 0 {
		delete(f.ranks, userID)
	} else {
		f.ranks[userID] = rank
	}
	f.log = append([]storage.RankChange{{UserID: userID, GivenBy: by, Rank: rank, At: time.Now()}}, f.log...)
	return nil
}
func (f *fakeRankBook) RankOf(_ context.Context, _, userID int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ranks[userID], nil
}
func (f *fakeRankBook) Ranks(context.Context, int64) ([]storage.MemberRank, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []storage.MemberRank
	for id, r := range f.ranks {
		out = append(out, storage.MemberRank{UserID: id, Name: "M" + string(rune('0'+id%10)), Rank: r})
	}
	return out, nil
}
func (f *fakeRankBook) RankLog(context.Context, int64, int64, int) ([]storage.RankChange, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]storage.RankChange(nil), f.log...), nil
}
func (f *fakeRankBook) CommandRanks(context.Context, int64) (map[string]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int{}
	for k, v := range f.overrides {
		out[k] = v
	}
	return out, nil
}
func (f *fakeRankBook) SetCommandRank(_ context.Context, _ int64, code string, rank int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.overrides[code] = rank
	return nil
}

// from is a group message by the user, replying to a member when to is not 0.
func from(user int64, text string, to int64) telegram.Update {
	msg := groupMessage(text)
	msg.From = &telegram.User{ID: user, FirstName: "U"}
	if to != 0 {
		msg.ReplyToMessage = &telegram.Message{MessageID: 29, From: &telegram.User{ID: to, FirstName: "Sam"}, Chat: msg.Chat, Text: "salom"}
	}
	return inGroup(msg)
}

// The creator gives ranks in a word, a moderator only below their own, the
// staff and the log list them, "-admin" takes one away, and only rank 5 or
// the creator sets a command's rank.
func TestRanksAreGivenInWords(t *testing.T) {
	const creator, moder, member = int64(10), int64(11), int64(12)
	f := newFixture()
	f.opts.WakeWords = []string{"мари", "mari"}
	f.members.creator = creator
	book := &fakeRankBook{ranks: map[int64]int{}, overrides: map[string]int{}}
	f.opts.Ranks = book
	sent := f.run(from(creator, "+4 админ мари", moder))
	if len(sent) == 0 {
		t.Fatal("nothing sent")
	}
	if last := sent[len(sent)-1].text; !strings.Contains(last, "4-darajali") || book.ranks[moder] != 4 {
		t.Fatalf("the creator's +4 = %q, ranks %v", last, book.ranks)
	}
	sent = f.run(from(moder, "Мари +4 admin", member))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "past darajani") || book.ranks[member] != 0 {
		t.Errorf("a rank 4 giving 4 = %q", last)
	}
	sent = f.run(from(moder, "mari daraja 2", member))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "2-darajali") || book.ranks[member] != 2 {
		t.Errorf("a rank 4 giving 2 = %q", last)
	}
	sent = f.run(from(member, "/staff", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "4-daraja") || !strings.Contains(last, "2-daraja") {
		t.Errorf("staff = %q", last)
	}
	sent = f.run(from(moder, "-admin мари", member))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "moderator emas") || book.ranks[member] != 0 {
		t.Errorf("-admin = %q", last)
	}
	sent = f.run(from(member, "/ranklog", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "darajasi olindi") {
		t.Errorf("log = %q", last)
	}
	sent = f.run(from(moder, "/access ban 2", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "kamida 5") || book.overrides["ban"] != 0 {
		t.Errorf("a rank 4 setting access = %q", last)
	}
	sent = f.run(from(creator, "/access ban 2", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "2-darajadan") || book.overrides["ban"] != 2 {
		t.Errorf("the creator setting access = %q", last)
	}
	sent = f.run(from(member, "/access", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "2 · ban") {
		t.Errorf("access list = %q", last)
	}
	// "+1" alone is agreement, not a rank.
	if _, ok := New(f.opts).rankRequest(incoming{group: true, text: "+1", msg: groupMessage("+1")}); ok {
		t.Error("+1 alone read as a rank")
	}
}
