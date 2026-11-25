package service

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

type fakeWeekBook struct {
	groups []storage.GroupWithSetting
	off    map[int64]bool
	weeks  map[int64]storage.WeekAwards
	posted map[int64]string
	asked  time.Time
}

func (f *fakeWeekBook) GroupsWithSetting(_ context.Context, key, value string) ([]storage.GroupWithSetting, error) {
	if key != storage.SettingAI || value != "on" {
		return nil, nil
	}
	return f.groups, nil
}

func (f *fakeWeekBook) GroupSetting(_ context.Context, chatID int64, key string) (string, error) {
	if key == storage.SettingWeekAwardsOff && f.off[chatID] {
		return "on", nil
	}
	return "", nil
}

func (f *fakeWeekBook) WeekAwards(_ context.Context, chatID int64, since, _ time.Time, _ *time.Location) (storage.WeekAwards, error) {
	f.asked = since
	return f.weeks[chatID], nil
}

func (f *fakeWeekBook) MarkWeekAwardsPosted(_ context.Context, chatID int64, week string) (bool, error) {
	if f.posted[chatID] == week {
		return false, nil
	}
	f.posted[chatID] = week
	return true, nil
}

func TestPostWeekAwards(t *testing.T) {
	tashkent := time.FixedZone("Tashkent", 5*3600)
	monday, _ := time.ParseInLocation("2006-01-02 15:04", "2026-11-30 08:00", tashkent)
	full := storage.WeekAwards{
		Talker:  storage.WeekNominee{UserID: 1, Name: "Ali @ali", Count: 196},
		Owl:     storage.WeekNominee{UserID: 2, Name: "Vali", Count: 47},
		Laugher: storage.WeekNominee{UserID: 3, Name: "Gani", Count: 2},
		Muted:   storage.WeekNominee{UserID: 4, Name: "Soli", Count: 6},
		Lucky:   storage.WeekLucky{UserID: 5, Name: "Mubina", Card: "Gojo Satoru", Anime: "Jujutsu Kaisen", Rarity: 4},
	}
	book := &fakeWeekBook{
		groups: []storage.GroupWithSetting{{ChatID: -1}, {ChatID: -2}, {ChatID: -3}},
		off:    map[int64]bool{-2: true},
		weeks: map[int64]storage.WeekAwards{-1: full, -2: full,
			-3: {Talker: storage.WeekNominee{UserID: 7, Name: "Jim", Count: 25}, Owl: storage.WeekNominee{UserID: 8, Name: "Kam", Count: 1}}},
		posted: map[int64]string{},
	}
	poster := &fakePoster{}
	opts := WeekAwardOptions{Book: book, Telegram: poster, Location: tashkent, Now: monday, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	posted, err := PostWeekAwards(context.Background(), opts)

	if err != nil || posted != 1 || len(poster.posts) != 1 || poster.posts[0].ChatID != -1 {
		t.Fatalf("posted %d, %v: %+v", posted, err, poster.posts)
	}
	text := poster.posts[0].Text
	for _, want := range []string{"Haftaning nominatsiyalari", "23.11 – 29.11", "[Ali](tg://user?id=1): 196", "[Vali](tg://user?id=2)", "[Soli](tg://user?id=4): 6", "Gojo Satoru, Jujutsu Kaisen (Mythic)"} {
		if !strings.Contains(text, want) {
			t.Errorf("post lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Gani") {
		t.Errorf("two laughs earned a nomination:\n%s", text)
	}
	if book.asked.Weekday() != time.Monday || book.asked.Day() != 23 {
		t.Errorf("asked from %v, want last Monday", book.asked)
	}
	if again, _ := PostWeekAwards(context.Background(), opts); again != 0 || len(poster.posts) != 1 {
		t.Errorf("posted again: %d", again)
	}
	opts.NotLucky = map[int64]bool{5: true}
	if text, _ := weekAwardText(full, book.asked, monday, opts.NotLucky); strings.Contains(text, "Gojo") {
		t.Errorf("an owner's given card won:\n%s", text)
	}
	opts.Now = monday.AddDate(0, 0, 4)
	book.posted = map[int64]string{}
	if late, _ := PostWeekAwards(context.Background(), opts); late != 0 {
		t.Errorf("a week four days old was posted: %d", late)
	}
}
