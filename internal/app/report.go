package app

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// ReportSource reads what happened over a period.
type ReportSource interface {
	Report(ctx context.Context, since, until time.Time) (storage.DayReport, error)
}

// ReportSender sends the report to an owner.
type ReportSender interface {
	SendMessage(ctx context.Context, msg telegram.Outgoing) (int64, error)
}

// sendDailyReport tells the owners known by ID what the bot did yesterday,
// in Location, before the old history is deleted. A failure is logged: the
// cleanup still runs.
func sendDailyReport(ctx context.Context, cfg config.Config, source ReportSource, sender ReportSender, now time.Time, logger *slog.Logger) {
	if len(cfg.OwnerIDs) == 0 {
		return
	}
	local := now.In(cfg.Location)
	until := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, cfg.Location)
	since := until.AddDate(0, 0, -1)
	report, err := source.Report(ctx, since, until)
	if err != nil {
		logger.Warn("could not build the daily report", "error", err)
		return
	}
	text := formatReport(report, since)
	for id := range cfg.OwnerIDs {
		if _, err := sender.SendMessage(ctx, telegram.Outgoing{ChatID: id, Text: text}); err != nil {
			logger.Warn("could not send the daily report", "owner_id", id, "error", err)
		}
	}
}

// formatReport writes the report for the owners, in Uzbek, busiest groups
// first.
func formatReport(r storage.DayReport, day time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📊 Kunlik hisobot: %s\n", day.Format("02.01.2006"))
	groups := slices.Clone(r.Groups)
	slices.SortFunc(groups, func(a, b storage.GroupDay) int {
		return cmp.Or(b.Replies-a.Replies, b.Messages-a.Messages, strings.Compare(a.Title, b.Title))
	})
	if len(groups) == 0 {
		b.WriteString("\nGuruhlarda faollik bo'lmadi.\n")
	} else {
		messages, replies := 0, 0
		b.WriteString("\nGuruhlar:\n")
		for _, g := range groups {
			fmt.Fprintf(&b, "• %s — %d xabar, bot %d javob\n", g.Title, g.Messages, g.Replies)
			messages, replies = messages+g.Messages, replies+g.Replies
		}
		fmt.Fprintf(&b, "Jami: %d xabar, bot %d javob\n", messages, replies)
	}
	fmt.Fprintf(&b, "\nShaxsiy chat: %d kishi, %d javob", r.PrivateUsers, r.PrivateAnswers)
	if r.PrivateRefused > 0 {
		fmt.Fprintf(&b, ", limitdan keyin %d rad", r.PrivateRefused)
	}
	b.WriteString("\n")
	if len(r.Moderation) > 0 {
		actions := make([]string, 0, len(r.Moderation))
		for action := range r.Moderation {
			actions = append(actions, action)
		}
		slices.Sort(actions)
		parts := make([]string, len(actions))
		for i, a := range actions {
			parts[i] = fmt.Sprintf("%s %d", a, r.Moderation[a])
		}
		fmt.Fprintf(&b, "Moderatsiya: %s\n", strings.Join(parts, ", "))
	}
	if len(r.NewGroups) > 0 {
		fmt.Fprintf(&b, "Yangi guruhlar: %s\n", strings.Join(r.NewGroups, ", "))
	}
	return strings.TrimSpace(b.String())
}
