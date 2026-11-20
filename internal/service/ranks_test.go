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

// The creator gives ranks with /rank N replying to the member, a moderator
// only below their own, the staff and the log list them, "/rank 0" takes
// one away, and only rank 5 or the creator sets a command's rank.
func TestRanksAreGivenWithSlashRank(t *testing.T) {
	const creator, moder, member = int64(10), int64(11), int64(12)
	f := newFixture()
	f.opts.WakeWords = []string{"мари", "mari"}
	f.members.creator = creator
	book := &fakeRankBook{ranks: map[int64]int{}, overrides: map[string]int{}}
	f.opts.Ranks = book
	sent := f.run(from(creator, "/rank 4", moder))
	if len(sent) == 0 {
		t.Fatal("nothing sent")
	}
	if last := sent[len(sent)-1].text; !strings.Contains(last, "4-darajali") || book.ranks[moder] != 4 {
		t.Fatalf("the creator's /rank 4 = %q, ranks %v", last, book.ranks)
	}
	sent = f.run(from(moder, "/rank 4", member))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "past darajani") || book.ranks[member] != 0 {
		t.Errorf("a rank 4 giving 4 = %q", last)
	}
	sent = f.run(from(moder, "/rank 2", member))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "2-darajali") || book.ranks[member] != 2 {
		t.Errorf("a rank 4 giving 2 = %q", last)
	}
	sent = f.run(from(member, "/staff", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "4-daraja") || !strings.Contains(last, "2-daraja") {
		t.Errorf("staff = %q", last)
	}
	sent = f.run(from(member, "/rank 1", moder))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "kamida 4") || book.ranks[moder] != 4 {
		t.Errorf("a rank 2 giving a rank = %q, ranks %v", last, book.ranks)
	}
	sent = f.run(from(moder, "/rank 0", member))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "moderator emas") || book.ranks[member] != 0 {
		t.Errorf("/rank 0 = %q", last)
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
	if last := sent[len(sent)-1].text; !strings.Contains(last, "⭐⭐ 2-daraja\n   • Ban va bandan chiqarish  ·  ban") {
		t.Errorf("access list = %q", last)
	}
	sent = f.run(from(creator, "/rank", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "/rank 0") {
		t.Errorf("/rank alone = %q", last)
	}
	sent = f.run(from(creator, "/rank 3", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "Kimga?") {
		t.Errorf("/rank with no reply = %q", last)
	}
}

// A moderator cannot take the rank of one at their own rank or above; the
// creator and the bot's owners stand above every rank and take any.
func TestTakingARankFollowsTheRanks(t *testing.T) {
	const creator, owner, moder, other = int64(10), int64(20), int64(11), int64(13)
	f := newFixture()
	f.members.creator = creator
	f.opts.OwnerIDs = map[int64]bool{owner: true}
	book := &fakeRankBook{ranks: map[int64]int{moder: 4, other: 5}, overrides: map[string]int{}}
	f.opts.Ranks = book
	sent := f.run(from(moder, "/rank 0", other))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "⛔") || book.ranks[other] != 5 {
		t.Errorf("a rank 4 taking a rank 5 = %q, ranks %v", last, book.ranks)
	}
	f.run(from(owner, "/rank 0", other))
	if book.ranks[other] != 0 {
		t.Errorf("the owner's /rank 0 left rank %d", book.ranks[other])
	}
	f.run(from(creator, "/rank 5", moder))
	if book.ranks[moder] != 5 {
		t.Errorf("the creator's /rank 5 gave %d", book.ranks[moder])
	}
}

// Words are no rank order any more, whoever writes them: "+3 admin mari"
// and "-admin mari" replying to a member change no rank; "darajam qaysi"
// still tells the asker theirs in code.
func TestRankWordsAreNoOrder(t *testing.T) {
	const creator, member = int64(10), int64(12)
	f := newFixture()
	f.opts.WakeWords = []string{"мари", "mari"}
	f.members.creator = creator
	book := &fakeRankBook{ranks: map[int64]int{member: 2}, overrides: map[string]int{}}
	f.opts.Ranks = book
	service := New(f.opts)
	for _, text := range []string{"+3 admin mari", "-admin mari", "+3 даражали мари", "Мари 2 даража бен", "даража 3 мари",
		"- админ Мари", "-1 admin mari", "darajani ol", "сними ранг", "3 admin", "+1"} {
		f.run(from(creator, text, member))
		if book.ranks[member] != 2 || len(book.log) != 0 {
			t.Errorf("%q changed a rank: %v, log %v", text, book.ranks, book.log)
		}
		if _, ok := service.rankRequest(incoming{group: true, text: text, msg: groupMessage(text)}); ok {
			t.Errorf("%q read as a rank order", text)
		}
	}
	sent := f.run(from(member, "mari darajam qaysi", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "darajangiz: 2") {
		t.Errorf("one's own = %q", last)
	}
	sent = f.run(from(member, "Мени даражам неччи мари", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "darajangiz: 2") {
		t.Errorf("даражам неччи = %q", last)
	}
	sent = f.run(from(14, "mari darajam qaysi", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "/rank 2") {
		t.Errorf("no rank = %q", last)
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

// /rank alone replying to a member, the bot's name with it or not, says
// their rank to anyone who asks and changes nothing; the creator and the
// bot's owners stand above the ranks.
func TestSlashRankAloneShowsTheirRank(t *testing.T) {
	const creator, owner, moder, member = int64(10), int64(20), int64(11), int64(12)
	f := newFixture()
	f.members.creator = creator
	f.opts.OwnerIDs = map[int64]bool{owner: true}
	book := &fakeRankBook{ranks: map[int64]int{moder: 3}, overrides: map[string]int{}}
	f.opts.Ranks = book
	for _, c := range []struct {
		text string
		to   int64
		want string
	}{
		{"/rank@" + f.opts.Bot.Username, moder, "Sam: ⭐⭐⭐ 3-daraja"},
		{"/rank", moder, "Sam: ⭐⭐⭐ 3-daraja"},
		{"/rank", 14, "Sam: darajasi yo'q (0)."},
		{"/rank", creator, "hamma darajadan yuqori"},
		{"/rank", owner, "hamma darajadan yuqori"},
	} {
		sent := f.run(from(member, c.text, c.to))
		if len(sent) == 0 || !strings.Contains(sent[len(sent)-1].text, c.want) {
			t.Errorf("%q replying to %d = %v, want %q", c.text, c.to, sent, c.want)
		}
	}
	if book.ranks[moder] != 3 || len(book.log) != 0 {
		t.Errorf("asking changed the ranks: %v, log %v", book.ranks, book.log)
	}
}

// A moderator given a rank gets the menu of that rank in the group, and
// loses it with /rank 0; a Telegram admin and the bot's owners are given
// none, as their own menus hold more.
func TestRankGivesAndTakesTheMenu(t *testing.T) {
	const creator, owner, admin, member = int64(10), int64(20), int64(15), int64(12)
	f := newFixture()
	f.members.creator = creator
	f.members.admins = map[int64]bool{creator: true, admin: true}
	f.opts.OwnerIDs = map[int64]bool{owner: true}
	f.opts.Ranks = &fakeRankBook{ranks: map[int64]int{}, overrides: map[string]int{}}
	menus := &fakeMenuSetter{}
	var asked []int
	f.opts.MenuSetter = menus
	f.opts.RankMenu = func(rank int, _ map[string]int) []telegram.BotCommand {
		asked = append(asked, rank)
		return []telegram.BotCommand{{Command: "ban", Description: "x"}}
	}

	f.run(from(creator, "/rank 3", member))
	if len(menus.calls) != 1 || menus.calls[0] != [2]int64{groupID, member} || len(asked) != 1 || asked[0] != 3 {
		t.Fatalf("after /rank 3: menus %v for ranks %v, want one for the member, of rank 3", menus.calls, asked)
	}
	f.run(from(creator, "/rank 0", member))
	if len(menus.deleted) != 1 || menus.deleted[0] != [2]int64{groupID, member} {
		t.Errorf("after /rank 0: deleted %v, want the member's menu", menus.deleted)
	}
	f.run(from(creator, "/rank 2", admin), from(creator, "/rank 2", owner))
	if len(menus.calls) != 1 {
		t.Errorf("an admin's and an owner's ranks set menus %v", menus.calls)
	}
}

// A ranked member who writes is given the menu of their rank once; plain
// members, admins and owners are not.
func TestRankedMemberGetsTheMenuOnce(t *testing.T) {
	const owner, admin, moder, member = int64(20), int64(15), int64(11), int64(12)
	f := newFixture()
	f.members.admins = map[int64]bool{admin: true}
	f.opts.OwnerIDs = map[int64]bool{owner: true}
	f.opts.Ranks = &fakeRankBook{ranks: map[int64]int{moder: 1, admin: 2, owner: 3}, overrides: map[string]int{}}
	menus := &fakeMenuSetter{}
	f.opts.MenuSetter = menus
	f.opts.RankMenu = func(int, map[string]int) []telegram.BotCommand {
		return []telegram.BotCommand{{Command: "ban", Description: "x"}}
	}
	second := from(moder, "yana", 0)
	second.Message.MessageID = 31

	f.run(from(moder, "salom", 0), second, from(member, "men a'zoman", 0), from(admin, "salom", 0), from(owner, "salom", 0))

	if len(menus.calls) != 1 || menus.calls[0] != [2]int64{groupID, moder} {
		t.Errorf("menus set = %v, want once, for the moderator", menus.calls)
	}
	// A ranked admin's menu from before they were made admin is taken away,
	// as it would hide the admins' one.
	if len(menus.deleted) != 1 || menus.deleted[0] != [2]int64{groupID, admin} {
		t.Errorf("menus taken away = %v, want the ranked admin's", menus.deleted)
	}
}

// A rank given to a message written as a channel, as an anonymous admin or
// by a bot is refused saying why, as a member replied /rank 5 to a channel's
// message in a group and was told only that it could not be done.
func TestRankRefusesWhoCannotHoldOne(t *testing.T) {
	const creator = int64(10)
	for name, c := range map[string]struct {
		reply func(*telegram.Message)
		want  string
	}{
		"channel": {func(m *telegram.Message) {
			m.From = &telegram.User{ID: 136817688, IsBot: true, FirstName: "Channel"}
			m.SenderChat = &telegram.Chat{ID: -1001111, Title: "."}
		}, "kanal nomidan"},
		"anonymous admin": {func(m *telegram.Message) {
			m.From = &telegram.User{ID: 1087968824, IsBot: true, FirstName: "Group"}
			m.SenderChat = &telegram.Chat{ID: groupID}
		}, "anonim admin"},
		"bot": {func(m *telegram.Message) { m.From.IsBot = true }, "Botga"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			f.members.creator = creator
			book := &fakeRankBook{ranks: map[int64]int{}, overrides: map[string]int{}}
			f.opts.Ranks = book
			update := from(creator, "/rank 5", 14)
			c.reply(update.Message.ReplyToMessage)
			sent := f.run(update)
			if len(sent) == 0 || !strings.Contains(sent[len(sent)-1].text, c.want) || len(book.log) != 0 {
				t.Errorf("sent %v, log %v; want %q and no rank", sent, book.log, c.want)
			}
			bare := from(creator, "/rank", 14)
			c.reply(bare.Message.ReplyToMessage)
			if sent := f.run(bare); len(sent) == 0 || !strings.Contains(sent[len(sent)-1].text, c.want) {
				t.Errorf("bare /rank = %v, want %q", sent, c.want)
			}
		})
	}
}
