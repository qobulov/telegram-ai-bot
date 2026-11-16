package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"telegram-ai-bot/internal/reminders"
)

// /api/reminders is public, so a call without CRON_SECRET is refused
// before anything is sent; one with it runs the job and answers its
// summary.
func TestRemindersEndpointNeedsTheSecret(t *testing.T) {
	const secret = "a-long-enough-cron-secret"
	runs := 0
	handler, err := RemindersHandler(secret, func(context.Context) (reminders.Summary, error) {
		runs++
		return reminders.Summary{Sent: 2, Failed: 1}, nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	for _, auth := range []string{"", "Bearer wrong", secret, "Bearer " + secret + "x"} {
		req := httptest.NewRequest(http.MethodGet, "/api/reminders", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Authorization %q: status %d, want 401", auth, rec.Code)
		}
	}
	if runs != 0 {
		t.Fatalf("the job ran %d times without the secret", runs)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/reminders", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if body := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || runs != 1 || body != `{"sent":2,"skipped":0,"failed":1,"retried":0}` {
		t.Errorf("with the secret: status %d, runs %d, body %s", rec.Code, runs, body)
	}

	if _, err := RemindersHandler("", func(context.Context) (reminders.Summary, error) { return reminders.Summary{}, nil }, nil); err == nil {
		t.Error("a handler without a secret was built")
	}
}
