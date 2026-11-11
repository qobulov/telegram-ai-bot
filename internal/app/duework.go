package app

import (
	"context"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"telegram-ai-bot/internal/reminders"
)

// dueWorkTimeout bounds one run of the due work beside an update: the auto
// opener's and the reminders' budgets of 8 seconds each, and a send after
// each, inside the webhook's maxDuration of 60 seconds.
const dueWorkTimeout = 25 * time.Second

// WithDueWork runs job beside the updates next handles, at most once every
// interval on this instance, so that the cards and reminders due go out
// while updates keep arriving, whether or not an outside scheduler calls
// the reminders endpoint. The request waits for it, as a function's work
// stops once it has answered. Runs on several instances may overlap: a
// reminder is claimed once and a card opened under its player's lock, so
// each goes out once.
func WithDueWork(next http.Handler, job func(ctx context.Context) (reminders.Summary, error), every time.Duration, logger *slog.Logger) http.Handler {
	var last atomic.Int64
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !takeTurn(&last, every) {
			next.ServeHTTP(w, r)
			return
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), dueWorkTimeout)
			defer cancel()
			if _, err := job(ctx); err != nil {
				logger.Warn("could not do the due work beside an update", "error", err)
			}
		}()
		next.ServeHTTP(w, r)
		<-done
	})
}

// takeTurn reports whether every has passed since the last turn, and takes
// this one if so; of requests arriving together only one takes it.
func takeTurn(last *atomic.Int64, every time.Duration) bool {
	now := time.Now().UnixNano()
	prev := last.Load()
	if prev != 0 && now-prev < int64(every) {
		return false
	}
	return last.CompareAndSwap(prev, now)
}
