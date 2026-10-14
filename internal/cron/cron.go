// Package cron exposes a scheduled job as an HTTP endpoint for Vercel Cron.
package cron

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// Options configures the handler. Secret and Run are required.
type Options struct {
	// Secret is the value of CRON_SECRET. Vercel Cron sends it as
	// "Authorization: Bearer <secret>"; any request without it is refused,
	// because the endpoint's URL is public.
	Secret string
	// Run does the job and returns a summary for the response.
	Run    func(ctx context.Context) (any, error)
	Logger *slog.Logger
}

type handler struct {
	opts Options
}

// NewHandler returns the HTTP handler for the job.
func NewHandler(opts Options) (http.Handler, error) {
	if opts.Secret == "" {
		return nil, errors.New("cron: a secret is required")
	}
	if opts.Run == nil {
		return nil, errors.New("cron: a Run function is required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &handler{opts: opts}, nil
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.authentic(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	result, err := h.opts.Run(r.Context())
	if err != nil {
		h.opts.Logger.Error("scheduled job failed", "error", err)
		http.Error(w, "the job failed; see the function logs", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (h *handler) authentic(r *http.Request) bool {
	given := []byte(r.Header.Get("Authorization"))
	want := []byte("Bearer " + h.opts.Secret)
	return subtle.ConstantTimeCompare(given, want) == 1
}
