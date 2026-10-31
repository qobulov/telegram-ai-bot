// Package serverless holds the Vercel functions' handlers.
//
// The files in api/ are compiled by Vercel as a separate module, so they may
// not import this module's internal packages. They call this package, which
// is outside internal/ and may.
package serverless

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
	_ "time/tzdata" // time zones for the group log and get_current_time; the host has none

	"telegram-ai-bot/internal/app"
	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/storage"
)

const (
	setupTimeout = 20 * time.Second
	// settingsCheck is how often an instance asks whether the owners changed
	// the bot's settings, and retireAfter how long a handler built before a
	// change keeps the requests it already has before it is closed.
	settingsCheck = 30 * time.Second
	retireAfter   = time.Minute
)

// lazy builds a handler on first use, with the owners' saved settings over
// the environment's, and keeps it for later requests of the same instance
// until those settings change. A failed setup is tried again on the next
// request rather than remembered, so a briefly unreachable database does
// not break the instance for good.
type lazy struct {
	name    string
	build   func(ctx context.Context, cfg config.Config, logger *slog.Logger) (http.Handler, func(), error)
	mu      sync.Mutex
	handler http.Handler
	close   func()
	// settings reads the saved settings; version is theirs the handler was
	// built with, last asked about at checked.
	settings *storage.Store
	version  string
	checked  time.Time
}

func (l *lazy) serve(w http.ResponseWriter, r *http.Request) {
	handler, err := l.ready()
	if err != nil {
		// The error names missing settings, never their values.
		slog.Error("function is not ready", "function", l.name, "error", err)
		// 503 makes Telegram deliver the update again later.
		http.Error(w, l.name+" is not ready; see the function logs", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(w, r)
}

func (l *lazy) ready() (http.Handler, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.handler != nil && !l.settingsChanged() {
		return l.handler, nil
	}
	env, err := config.Load(os.Getenv)
	if err != nil {
		return nil, err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: env.LogLevel}))
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()
	if l.settings == nil {
		store, err := storage.Open(ctx, env.DatabaseURL)
		if err != nil {
			return nil, err
		}
		l.settings = store
	}
	cfg, version := app.LoadSettings(ctx, env, os.Getenv, l.settings, logger)
	handler, closeHandler, err := l.build(ctx, cfg, logger)
	if err != nil {
		if l.handler != nil {
			// The settings changed but the new build failed: the old one
			// still works, and the next check tries again.
			logger.Error("could not rebuild with the new settings; keeping the old", "function", l.name, "error", err)
			return l.handler, nil
		}
		return nil, err
	}
	if old := l.close; old != nil {
		time.AfterFunc(retireAfter, old)
		logger.Info("rebuilt with the new settings", "function", l.name)
	}
	l.handler, l.close, l.version, l.checked = handler, closeHandler, version, time.Now()
	return handler, nil
}

// settingsChanged asks, at most every settingsCheck, whether the saved
// settings are no longer the ones the handler was built with. When it
// cannot tell, they did not.
func (l *lazy) settingsChanged() bool {
	if l.settings == nil || time.Since(l.checked) < settingsCheck {
		return false
	}
	l.checked = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	version, err := l.settings.GlobalSettingsVersion(ctx)
	return err == nil && version != l.version
}

var webhook = &lazy{name: "webhook", build: func(ctx context.Context, cfg config.Config, logger *slog.Logger) (http.Handler, func(), error) {
	hook, err := app.NewWebhook(ctx, cfg, logger)
	if err != nil {
		return nil, nil, err
	}
	logger.Info("webhook ready", "username", hook.Bot.User.Username, "name", cfg.BotDisplayName, "model", cfg.Model)
	return hook.Handler, hook.Close, nil
}}

var cleanup = &lazy{name: "cleanup", build: func(ctx context.Context, cfg config.Config, logger *slog.Logger) (http.Handler, func(), error) {
	job, err := app.NewCleanup(ctx, cfg, logger)
	if err != nil {
		return nil, nil, err
	}
	return job.Handler, job.Close, nil
}}

var stats = &lazy{name: "stats", build: func(ctx context.Context, cfg config.Config, logger *slog.Logger) (http.Handler, func(), error) {
	endpoint, err := app.NewStats(ctx, cfg, logger)
	if err != nil {
		return nil, nil, err
	}
	return endpoint.Handler, endpoint.Close, nil
}}

var games = &lazy{name: "game", build: func(ctx context.Context, cfg config.Config, logger *slog.Logger) (http.Handler, func(), error) {
	endpoint, err := app.NewGame(ctx, cfg, logger)
	if err != nil {
		return nil, nil, err
	}
	return endpoint.Handler, endpoint.Close, nil
}}

var shashkas = &lazy{name: "shashka", build: func(ctx context.Context, cfg config.Config, logger *slog.Logger) (http.Handler, func(), error) {
	endpoint, err := app.NewShashka(ctx, cfg, logger)
	if err != nil {
		return nil, nil, err
	}
	return endpoint.Handler, endpoint.Close, nil
}}

var raids = &lazy{name: "raid", build: func(ctx context.Context, cfg config.Config, logger *slog.Logger) (http.Handler, func(), error) {
	endpoint, err := app.NewRaid(ctx, cfg, logger)
	if err != nil {
		return nil, nil, err
	}
	return endpoint.Handler, endpoint.Close, nil
}}

var mafias = &lazy{name: "mafia", build: func(ctx context.Context, cfg config.Config, logger *slog.Logger) (http.Handler, func(), error) {
	endpoint, err := app.NewMafia(ctx, cfg, logger)
	if err != nil {
		return nil, nil, err
	}
	return endpoint.Handler, endpoint.Close, nil
}}

// Mafia answers the Mafia game.
func Mafia(w http.ResponseWriter, r *http.Request) { mafias.serve(w, r) }

// Raid answers Dev jangi, the boss raid game.
func Raid(w http.ResponseWriter, r *http.Request) { raids.serve(w, r) }

// Shashka answers the draughts Mini App.
func Shashka(w http.ResponseWriter, r *http.Request) { shashkas.serve(w, r) }

// Stats answers the group dashboard Mini App.
func Stats(w http.ResponseWriter, r *http.Request) { stats.serve(w, r) }

// Game deals and scores plays of the game.
func Game(w http.ResponseWriter, r *http.Request) { games.serve(w, r) }

// Webhook receives Telegram updates.
func Webhook(w http.ResponseWriter, r *http.Request) { webhook.serve(w, r) }

// Cleanup deletes old chat history when Vercel Cron calls it.
func Cleanup(w http.ResponseWriter, r *http.Request) { cleanup.serve(w, r) }
