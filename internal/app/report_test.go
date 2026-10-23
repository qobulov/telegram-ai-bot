package app

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

type fakeReportSource struct {
	since, until time.Time
	report       storage.DayReport
}

func (f *fakeReportSource) Report(_ context.Context, since, until time.Time) (storage.DayReport, error) {
	f.since, f.until = since, until
	return f.report, nil
}

type fakeReportSender struct{ sent []telegram.Outgoing }

func (f *fakeReportSender) SendMessage(_ context.Context, msg telegram.Outgoing) (int64, error) {
	f.sent = append(f.sent, msg)
	return 1, nil
}

// Each owner known by ID gets yesterday's report, in Tashkent's day,
// busiest groups first.
func TestDailyReportGoesToTheOwners(t *testing.T) {
	tashkent, _ := time.LoadLocation("Asia/Tashkent")
	cfg := config.Config{Location: tashkent, OwnerIDs: map[int64]bool{1688909825: true, 1047217990: true}}
	source := &fakeReportSource{report: storage.DayReport{
		Groups:       []storage.GroupDay{{Title: "Quiet", Messages: 9, Replies: 1}, {Title: "BS news", Messages: 1038, Replies: 140}},
		NewGroups:    []string{"Махалла камитети"},
		PrivateUsers: 5, PrivateAnswers: 22, PrivateRefused: 3,
		Moderation: map[string]int{"mute": 5, "swear": 9},
	}}
	sender := &fakeReportSender{}
	now := time.Date(2026, 10, 24, 8, 3, 0, 0, tashkent)

	sendDailyReport(context.Background(), cfg, source, sender, now, slog.New(slog.DiscardHandler))

	if !source.since.Equal(time.Date(2026, 10, 23, 0, 0, 0, 0, tashkent)) || !source.until.Equal(time.Date(2026, 10, 24, 0, 0, 0, 0, tashkent)) {
		t.Errorf("period %v – %v, want the 23rd", source.since, source.until)
	}
	if len(sender.sent) != 2 {
		t.Fatalf("sent %d reports, want one per owner", len(sender.sent))
	}
	text := sender.sent[0].Text
	for _, want := range []string{"23.10.2026", "• BS news — 1038 xabar, bot 140 javob\n• Quiet", "Jami: 1047 xabar, bot 141 javob", "5 kishi, 22 javob, limitdan keyin 3 rad", "mute 5, swear 9", "Yangi guruhlar: Махалла камитети"} {
		if !strings.Contains(text, want) {
			t.Errorf("report does not contain %q:\n%s", want, text)
		}
	}

	none := &fakeReportSender{}
	sendDailyReport(context.Background(), config.Config{Location: tashkent}, source, none, now, slog.New(slog.DiscardHandler))
	if len(none.sent) != 0 {
		t.Errorf("sent without owner IDs: %+v", none.sent)
	}
}
