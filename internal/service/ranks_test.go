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

// Cyrillic rank orders are read too, and "darajam qaysi" tells the asker
// their rank in code.
func TestRanksInCyrillicAndOnesOwn(t *testing.T) {
	const creator, member = int64(10), int64(12)
	f := newFixture()
	f.opts.WakeWords = []string{"мари", "mari"}
	f.members.creator = creator
	book := &fakeRankBook{ranks: map[int64]int{}, overrides: map[string]int{}}
	f.opts.Ranks = book
	sent := f.run(from(creator, "даража 3 мари", member))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "3-darajali") || book.ranks[member] != 3 {
		t.Fatalf("даража 3 = %q, ranks %v", last, book.ranks)
	}
	sent = f.run(from(member, "mari darajam qaysi", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "darajangiz: 3") {
		t.Errorf("one's own = %q", last)
	}
	sent = f.run(from(creator, "даражани ол мари", member))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "moderator emas") || book.ranks[member] != 0 {
		t.Errorf("даражани ол = %q", last)
	}
	// The wordings seen in a group that the code once left to the model.
	for _, c := range []struct {
		text string
		rank int
	}{{"+3 даражали мари", 3}, {"Мари 2 даража бен", 2}, {"мари 4 darajali qil", 4}} {
		f.run(from(creator, c.text, member))
		if book.ranks[member] != c.rank {
			t.Errorf("%q: rank %d, want %d", c.text, book.ranks[member], c.rank)
		}
	}
	sent = f.run(from(member, "Мени даражам неччи мари", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "darajangiz: 4") {
		t.Errorf("даражам неччи = %q", last)
	}
}

// /staff names the owner and the Telegram admins, with their titles, and
// says when no one holds a rank; bots are left out.
func TestStaffShowsAdmins(t *testing.T) {
	f := newFixture()
	f.opts.Ranks = &fakeRankBook{ranks: map[int64]int{}, overrides: map[string]int{}}
	f.opts.AdminList = fakeAdminList{admins: []telegram.ChatMember{
		{Status: "creator", User: &telegram.User{ID: 1, FirstName: "Aziz", Username: "boss"}},
		{Status: "administrator", CustomTitle: "Moderator", User: &telegram.User{ID: 2, FirstName: "Abu"}},
		{Status: "administrator", User: &telegram.User{ID: 99, IsBot: true, Username: "somebot"}},
	}}
	sent := f.run(from(20, "/staff", 0))
	last := sent[len(sent)-1].text
	for _, want := range []string{"Egasi: Aziz (@boss)", "Abu — Moderator", "hech kimda moderator darajasi yo'q"} {
		if !strings.Contains(last, want) {
			t.Errorf("staff = %q, want %q", last, want)
		}
	}
	if strings.Contains(last, "somebot") {
		t.Errorf("staff lists a bot: %q", last)
	}
}
