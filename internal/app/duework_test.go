package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"telegram-ai-bot/internal/reminders"
)

func TestDueWorkRunsBesideUpdatesOncePerInterval(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range []struct {
		name  string
		every time.Duration
		want  int32
	}{
		{"within the interval", time.Hour, 1},
		{"after the interval", 0, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var runs, served atomic.Int32
			job := func(ctx context.Context) (reminders.Summary, error) {
				if _, ok := ctx.Deadline(); !ok {
					t.Error("the due work has no deadline")
				}
				runs.Add(1)
				return reminders.Summary{}, nil
			}
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { served.Add(1) })
			handler := WithDueWork(next, job, tc.every, logger)
			for range 3 {
				handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/webhook", nil))
			}
			if served.Load() != 3 {
				t.Errorf("served %d updates, want 3", served.Load())
			}
			if runs.Load() != tc.want {
				t.Errorf("ran the due work %d times, want %d", runs.Load(), tc.want)
			}
		})
	}
}
