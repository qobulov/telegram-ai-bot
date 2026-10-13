// Package webhook receives Telegram updates over HTTP, for deployments where
// the bot cannot keep a long-polling connection open (serverless hosting).
package webhook

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"telegram-ai-bot/internal/telegram"
)

// SecretHeader is where Telegram repeats the secret given to setWebhook.
const SecretHeader = "X-Telegram-Bot-Api-Secret-Token"

// maxBodyBytes bounds an update. Telegram's are a few kilobytes.
const maxBodyBytes = 1 << 20

// Options configures the handler. Secret and Handle are required.
type Options struct {
	// Secret must match the secret_token registered with setWebhook. The URL
	// of a webhook is not a secret; this header is the only proof that a
	// request comes from Telegram and not from someone forging updates.
	Secret string
	// Handle processes one update and returns when it is done. On serverless
	// hosting nothing runs after the response is sent, so the reply must be
	// delivered before Handle returns.
	Handle func(telegram.Update)
	Logger *slog.Logger
}

type handler struct {
	opts Options
}

// NewHandler returns the HTTP handler for the webhook URL.
func NewHandler(opts Options) (http.Handler, error) {
	if opts.Secret == "" {
		return nil, errors.New("webhook: a secret is required")
	}
	if opts.Handle == nil {
		return nil, errors.New("webhook: a Handle function is required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &handler{opts: opts}, nil
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		// Lets a person or a monitor see that the function is deployed.
		_, _ = io.WriteString(w, "ok\n")
	case http.MethodPost:
		h.receive(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *handler) receive(w http.ResponseWriter, r *http.Request) {
	if !h.authentic(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	update, err := decode(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	h.handle(update)
	// Always 200 once the update was accepted: any other status makes
	// Telegram deliver the same update again. Failures are reported to the
	// user in the chat, not to Telegram.
	w.WriteHeader(http.StatusOK)
}

// authentic compares in constant time, so the secret cannot be guessed one
// character at a time from response timing.
func (h *handler) authentic(r *http.Request) bool {
	given := []byte(r.Header.Get(SecretHeader))
	return subtle.ConstantTimeCompare(given, []byte(h.opts.Secret)) == 1
}

func decode(body io.Reader) (telegram.Update, error) {
	raw, err := io.ReadAll(io.LimitReader(body, maxBodyBytes+1))
	if err != nil {
		return telegram.Update{}, err
	}
	if len(raw) > maxBodyBytes {
		return telegram.Update{}, errors.New("webhook: update too large")
	}
	var update telegram.Update
	if err := json.Unmarshal(raw, &update); err != nil {
		return telegram.Update{}, err
	}
	return update, nil
}

func (h *handler) handle(update telegram.Update) {
	defer func() {
		if recovered := recover(); recovered != nil {
			h.opts.Logger.Error("handling an update panicked", "update_id", update.UpdateID, "panic", recovered)
		}
	}()
	h.opts.Handle(update)
}
