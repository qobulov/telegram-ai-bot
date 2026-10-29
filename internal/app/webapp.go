package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/game"
	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// The pages WEBAPP_URL serves, from public/: the group dashboard Mini App
// and the game.
const (
	statsPath = "/stats/"
	gamePath  = "/game/"
)

// menuAppText labels the private chats' menu button.
const menuAppText = "📊 Statistika"

// statsPage is the dashboard's page, "" when WEBAPP_URL is not set.
func statsPage(cfg config.Config) string {
	if cfg.WebAppURL == "" {
		return ""
	}
	return cfg.WebAppURL + statsPath
}

// newGame is the game whose page WEBAPP_URL serves, nil when it is not set.
func newGame(cfg config.Config) *game.Game {
	if cfg.WebAppURL == "" {
		return nil
	}
	return game.New(cfg.TelegramToken, cfg.WebAppURL+gamePath, nil)
}

// MenuAppSetter sets what the private chats' menu button opens.
type MenuAppSetter interface {
	SetMenuWebApp(ctx context.Context, text, url string) error
}

// SetMenuApp makes the private chats' menu button open the dashboard while
// WEBAPP_URL is set, logging a failure: the /stats button opens it too.
// Without it the button is left as it is, so that one set in @BotFather
// stays.
func SetMenuApp(ctx context.Context, setter MenuAppSetter, cfg config.Config, logger *slog.Logger) {
	page := statsPage(cfg)
	if page == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, commandsTimeout)
	defer cancel()
	if err := setter.SetMenuWebApp(ctx, menuAppText, page); err != nil {
		logger.Warn("could not set the menu button to the dashboard", "error", err)
	}
}

// Endpoint is one of the pages' API functions.
type Endpoint struct {
	Handler http.Handler
	// Close releases what the endpoint holds.
	Close func()
}

// errNoPages is why the pages' functions do not start without WEBAPP_URL:
// nothing would open them.
var errNoPages = errors.New("WEBAPP_URL is required for the dashboard and the game")

// NewStats builds the dashboard's API: it checks the schema and asks
// Telegram who the bot is, whose own messages are not counted.
func NewStats(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Endpoint, error) {
	if cfg.WebAppURL == "" {
		return nil, errNoPages
	}
	store, err := storage.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	handler, err := newStats(ctx, cfg, store, logger)
	if err != nil {
		store.Close()
		return nil, err
	}
	return &Endpoint{Handler: handler, Close: store.Close}, nil
}

func newStats(ctx context.Context, cfg config.Config, store *storage.Store, logger *slog.Logger) (http.Handler, error) {
	if err := store.CheckSchema(ctx); err != nil {
		return nil, err
	}
	bot, err := Connect(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}
	return miniapp.NewHandler(miniapp.Options{
		BotToken: cfg.TelegramToken,
		BotID:    bot.User.ID,
		Store:    store,
		Telegram: bot.Client,
		Location: cfg.Location,
		Logger:   logger,
	})
}

// NewGame builds the game's API. Telegram keeps the scores, so it needs no
// database.
func NewGame(cfg config.Config, logger *slog.Logger) (*Endpoint, error) {
	g := newGame(cfg)
	if g == nil {
		return nil, errNoPages
	}
	client := telegram.NewClient(telegram.Options{Token: cfg.TelegramToken, BaseURL: cfg.TelegramAPIURL, Logger: logger})
	return &Endpoint{Handler: g.Handler(client, logger), Close: func() {}}, nil
}
