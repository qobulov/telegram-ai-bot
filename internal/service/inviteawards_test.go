package service

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// Only a group's admin turns the announcement on, with a period and how
// many; "off" turns it off, and nothing says how to.
func TestInviteAutoCommand(t *testing.T) {
	f := newFixture()
	book := &fakeInviteBook{}
	f.opts.Invites = book
	asAdmin := func(text string) string {
		msg := groupMessage(text)
		msg.From = &telegram.User{ID: groupAdminID, FirstName: "Admin"}
		sent := f.run(inGroup(msg))
		return sent[len(sent)-1].text
	}
	if sent := f.run(inGroup(groupMessage("/invites_auto week 3"))); len(book.awards) != 0 || !strings.Contains(sent[len(sent)-1].text, textAdminsOnly) {
		t.Fatalf("a member turned it on: %+v %+v", book.awards, sent)
	}
	if got := asAdmin("/invites_auto half 5"); len(book.awards) != 1 || book.awards[0].Period != "half" || book.awards[0].Top != 5 || !strings.Contains(got, "16") {
		t.Fatalf("half 5 = %+v, %q", book.awards, got)
	}
	if asAdmin("/invites_auto hafta"); book.awards[0].Period != "week" || book.awards[0].Top != awardTop {
		t.Errorf("hafta = %+v", book.awards)
	}
	if got := asAdmin("/invites_auto week 50"); book.awards[0].Top != awardTop || !strings.Contains(got, "/invites_auto week 3") {
		t.Errorf("week 50 = %+v, %q", book.awards, got)
	}
	if got := asAdmin("/invites_auto"); !strings.Contains(got, "dushanba") {
		t.Errorf("status = %q", got)
	}
	if got := asAdmin("/invites_auto off"); len(book.awards) != 0 || !strings.Contains(got, "o'chirildi") {
		t.Errorf("off = %+v, %q", book.awards, got)
	}
}

// A week runs from Monday, a half month to the 15th or from the 16th, a
// month from the 1st: the last of each to have ended.
func TestAwardPeriod(t *testing.T) {
	tashkent := time.FixedZone("Tashkent", 5*3600)
	day := func(d string) time.Time { at, _ := time.ParseInLocation("2006-01-02 15:04", d, tashkent); return at }
	for _, c := range []struct{ period, now, since, until string }{
		{"week", "2026-11-16 08:00", "2026-11-09", "2026-11-16"},
		{"week", "2026-11-13 08:00", "2026-11-02", "2026-11-09"},
		{"half", "2026-11-16 08:00", "2026-11-01", "2026-11-16"},
		{"half", "2026-12-01 08:00", "2026-11-16", "2026-12-01"},
		{"month", "2026-12-01 08:00", "2026-11-01", "2026-12-01"},
	} {
		since, until, key := awardPeriod(c.period, day(c.now), tashkent)
		if since.Format("2006-01-02") != c.since || until.Format("2006-01-02") != c.until || key != c.period+":"+c.since {
			t.Errorf("%s at %s = %v – %v (%s)", c.period, c.now, since, until, key)
		}
	}
}

// fakeAwards keeps announcements for PostInviteAwards.
type fakeAwards struct {
	fakeInviteBook
	asked  storage.InviteQuery
	posted map[int64]string
}

func (f *fakeAwards) InviteTop(_ context.Context, _ int64, q storage.InviteQuery) ([]storage.InviterCount, error) {
	f.asked = q
	return []storage.InviterCount{{UserID: 1, Name: "Ali", Stayed: 12}, {UserID: 2, Name: "Vali", Stayed: 9}, {UserID: 3, Name: "Gani", Stayed: 4}, {UserID: 4, Name: "Soli", Stayed: 1}}, nil
}

func (f *fakeAwards) MarkInviteAwardPosted(_ context.Context, chatID int64, period string) (bool, error) {
	if f.posted[chatID] == period {
		return false, nil
	}
	f.posted[chatID] = period
	for i := range f.awards {
		if f.awards[i].ChatID == chatID {
			f.awards[i].Posted = period
		}
	}
	return true, nil
}

// fakePoster records the posts and pins.
type fakePoster struct {
	mu     sync.Mutex
	posts  []telegram.Outgoing
	pinned []int64
}

func (f *fakePoster) SendMessage(_ context.Context, msg telegram.Outgoing) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts = append(f.posts, msg)
	return int64(100 + len(f.posts)), nil
}

func (f *fakePoster) PinChatMessage(_ context.Context, _, messageID int64) error {
	f.pinned = append(f.pinned, messageID)
	return nil
}

func (f *fakePoster) GetChatAdministrators(context.Context, int64) ([]telegram.ChatMember, error) {
	return []telegram.ChatMember{{Status: "administrator", User: &telegram.User{ID: 8}}, {Status: "creator", User: &telegram.User{ID: 9, FirstName: "Boss"}}}, nil
}

// When a period ends, its winners are posted with medals, pinned, and the
// group's owner tagged, once; a period that ended before the announcement
// was turned on is not posted.
func TestPostInviteAwards(t *testing.T) {
	tashkent := time.FixedZone("Tashkent", 5*3600)
	monday, _ := time.ParseInLocation("2006-01-02 15:04", "2026-11-16 08:00", tashkent)
	book := &fakeAwards{posted: map[int64]string{}}
	book.awards = []storage.InviteAward{
		{ChatID: -1, Period: "week", Top: 4, Language: "uz", SetAt: monday.AddDate(0, 0, -3)},
		{ChatID: -2, Period: "week", Top: 3, Language: "uz", SetAt: monday.Add(time.Hour)},
	}
	poster := &fakePoster{}
	opts := InviteAwardOptions{Book: book, Telegram: poster, Location: tashkent, Now: monday, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	posted, err := PostInviteAwards(context.Background(), opts)
	if err != nil || posted != 1 || len(poster.posts) != 1 || poster.posts[0].ChatID != -1 {
		t.Fatalf("posted %d, %v: %+v", posted, err, poster.posts)
	}
	text := poster.posts[0].Text
	for _, want := range []string{"Haftaning", "09.11 – 15.11", "🥇 [Ali](tg://user?id=1) — 12 ta", "🥉 [Gani]", "4. [Soli]", "👑 Guruh egasi: [Boss](tg://user?id=9)"} {
		if !strings.Contains(text, want) {
			t.Errorf("post lacks %q:\n%s", want, text)
		}
	}
	if len(poster.pinned) != 1 || poster.pinned[0] != 101 || book.asked.Limit != 4 || book.asked.Until.Weekday() != time.Monday {
		t.Errorf("pinned %v, asked %+v", poster.pinned, book.asked)
	}
	if again, _ := PostInviteAwards(context.Background(), opts); again != 0 || len(poster.posts) != 1 {
		t.Errorf("posted again: %d", again)
	}
}
