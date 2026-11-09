// Package reminders sends the reminders that are due: members ask Maria for
// them with the reminders tool, and an endpoint called every minute (or, in
// long polling, a ticker) sends them here.
package reminders

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

const (
	// batch is how many reminders one claim takes.
	batch = 25
	// budget is how long a call keeps sending, so that with one send
	// after it the call ends inside the function's maxDuration of 15
	// seconds; what is left waits for the next minute.
	budget = 8 * time.Second
	// sendTimeout bounds one message to Telegram.
	sendTimeout = 5 * time.Second
	// maxAttempts is how often a send Telegram turned away for now (its
	// rate limit, its own failure) is tried before it is given up.
	maxAttempts = 3
	retryDelay  = time.Minute
	// cardsStart is the /start payload that opens Kartalar's Mini App in a
	// private chat, as a group's message cannot carry its button.
	cardsStart = "kartalar"
)

// Store claims and records reminders.
type Store interface {
	ClaimDueReminders(ctx context.Context, now time.Time, limit int) ([]storage.Reminder, error)
	FinishReminder(ctx context.Context, id int64, outcome string) error
	RetryReminder(ctx context.Context, id int64, at time.Time) error
}

// Sender sends messages on Telegram.
type Sender interface {
	SendMessage(ctx context.Context, msg telegram.Outgoing) (int64, error)
}

// Options configures Fire. Store and Sender are required.
type Options struct {
	Store  Store
	Sender Sender
	// CardsURL is Kartalar's Mini App, "" for none; BotUsername links a
	// group to it through the private chat.
	CardsURL    string
	BotUsername string
	// CardEvery is how often a card may be drawn: a card reminder whose
	// player drew less than that ago is not sent.
	CardEvery time.Duration
	Logger    *slog.Logger
	Now       func() time.Time
}

// Summary is what one call did, as the endpoint answers it.
type Summary struct {
	Sent    int `json:"sent"`
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
	Retried int `json:"retried"`
}

// Fire sends the reminders due now. Each is claimed before it is sent and
// never claimed again, so overlapping calls send it at most once; a send
// that fails is recorded and does not stop the others.
func Fire(ctx context.Context, opts Options) (Summary, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	var summary Summary
	started := opts.Now()
	for opts.Now().Sub(started) < budget && ctx.Err() == nil {
		due, err := opts.Store.ClaimDueReminders(ctx, opts.Now(), batch)
		if err != nil {
			return summary, err
		}
		for i, r := range due {
			if ctx.Err() != nil || opts.Now().Sub(started) >= budget {
				// Claimed but not sent: give them back to the next call.
				giveBack(opts, due[i:], &summary)
				return summary, nil
			}
			fire(ctx, opts, r, &summary)
		}
		if len(due) < batch {
			break
		}
	}
	return summary, nil
}

func giveBack(opts Options, left []storage.Reminder, summary *Summary) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	for _, r := range left {
		if err := opts.Store.RetryReminder(ctx, r.ID, opts.Now()); err != nil {
			opts.Logger.Warn("could not give a reminder back", "reminder", r.ID, "error", err)
			continue
		}
		summary.Retried++
	}
}

func fire(ctx context.Context, opts Options, r storage.Reminder, summary *Summary) {
	finish := func(outcome string) {
		if err := opts.Store.FinishReminder(context.WithoutCancel(ctx), r.ID, outcome); err != nil {
			opts.Logger.Warn("could not record a reminder", "reminder", r.ID, "error", err)
		}
	}
	if r.Kind == storage.ReminderCard && tooSoon(opts, r) {
		finish(storage.ReminderSkipped)
		summary.Skipped++
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	_, err := opts.Sender.SendMessage(sendCtx, Message(opts, r))
	switch {
	case err == nil:
		finish(storage.ReminderSent)
		summary.Sent++
	case retryable(err) && r.Kind == storage.ReminderText && r.Attempts < maxAttempts:
		if err := opts.Store.RetryReminder(context.WithoutCancel(ctx), r.ID, opts.Now().Add(retryDelay)); err != nil {
			opts.Logger.Warn("could not retry a reminder", "reminder", r.ID, "error", err)
			finish(storage.ReminderFailed)
			summary.Failed++
			return
		}
		summary.Retried++
	default:
		// The bot blocked, removed from the group, the chat gone: it will
		// not get better, and a send that may have gone out is not sent
		// twice.
		opts.Logger.Info("could not send a reminder", "reminder", r.ID, "chat_id", r.ChatID, "error", err)
		finish(storage.ReminderFailed)
		summary.Failed++
	}
}

// tooSoon is true when the player drew a card since the reminder was set,
// so that no new card may be opened yet.
func tooSoon(opts Options, r storage.Reminder) bool {
	return !r.LastDraw.IsZero() && opts.Now().Before(r.LastDraw.Add(opts.CardEvery))
}

// retryable is a send Telegram answered it did not do for now: its rate
// limit, or its own failure. Without an answer the message may have gone
// out, so it is not sent again.
func retryable(err error) bool {
	var apiErr *telegram.APIError
	return errors.As(err, &apiErr) && (apiErr.Code == http.StatusTooManyRequests || apiErr.Code >= http.StatusInternalServerError)
}

// Message is the reminder as it is sent: in a group it tags the member it
// is for, and a card reminder carries the Mini App's button.
func Message(opts Options, r storage.Reminder) telegram.Outgoing {
	group := r.ChatID < 0
	msg := telegram.Outgoing{ChatID: r.ChatID, ReplyToMessageID: r.MessageID}
	var lines []string
	if group {
		lines = append(lines, "⏰ "+Mention(r.UserID, r.UserName))
	}
	switch r.Kind {
	case storage.ReminderCard:
		command := "/karta"
		if group && opts.BotUsername != "" {
			command += "@" + opts.BotUsername
		}
		text := cardText[language(r.Language)]
		if !group {
			text = "⏰ " + text
		}
		lines = append(lines, strings.Replace(text, "%s", command, 1))
		msg.Buttons = cardsButton(opts, group, r.Language)
	default:
		if group {
			lines = append(lines, r.Text)
		} else {
			lines = append(lines, "⏰ "+r.Text)
		}
	}
	msg.Text = strings.Join(lines, "\n")
	return msg
}

var cardText = map[string]string{
	"uz": "🃏 Yangi karta ochish vaqti keldi! Kartalarni oching yoki %s yozing.",
	"ru": "🃏 Пора открыть новую карту! Откройте «Kartalar» или напишите %s.",
	"en": "🃏 Time to open a new card! Open Kartalar or send %s.",
}

var cardButton = map[string]string{
	"uz": "🃏 Kartalar",
	"ru": "🃏 Карты",
	"en": "🃏 Cards",
}

func language(l string) string {
	if _, ok := cardText[l]; ok {
		return l
	}
	return "uz"
}

// cardsButton opens the Mini App: itself in a private chat, and in a group
// a link to the private chat, where Telegram lets it open.
func cardsButton(opts Options, group bool, lang string) [][]telegram.Button {
	label := cardButton[language(lang)]
	switch {
	case opts.CardsURL == "":
		return nil
	case !group:
		return [][]telegram.Button{{{Text: label, WebApp: &telegram.WebAppInfo{URL: opts.CardsURL}}}}
	case opts.BotUsername != "":
		return [][]telegram.Button{{{Text: label, URL: "https://t.me/" + opts.BotUsername + "?start=" + cardsStart}}}
	}
	return nil
}

// labelMarks are what would break the tag's [label](link).
var labelMarks = strings.NewReplacer("[", "", "]", "", "(", "", ")", "", "\n", " ", "*", "", "`", "")

// Mention tags a member by ID, which reaches them with or without a
// @username: their first name, without the @username the log adds.
func Mention(userID int64, name string) string {
	if at := strings.Index(name, " @"); at >= 0 {
		name = name[:at]
	}
	name = strings.TrimSpace(labelMarks.Replace(name))
	if utf8.RuneCountInString(name) > 32 {
		name = string([]rune(name)[:32])
	}
	if name == "" {
		name = "👤"
	}
	return "[" + name + "](tg://user?id=" + strconv.FormatInt(userID, 10) + ")"
}

// tags are tags of members, which a reminder's words may not carry: the
// bot tags only the member it reminds.
var tags = regexp.MustCompile(`\[([^\]\n]*)\]\(tg://user\?id=[0-9]*\)`)

// CleanText is a reminder's words as they are kept: tags of members become
// their bare names.
func CleanText(text string) string {
	return strings.TrimSpace(tags.ReplaceAllString(text, "$1"))
}
