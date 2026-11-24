package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"telegram-ai-bot/internal/cards"
	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/game"
	"telegram-ai-bot/internal/kassa"
	"telegram-ai-bot/internal/mafia"
	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/raid"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// The pages WEBAPP_URL serves, from public/: the group dashboard Mini App,
// the games, the Kartalar Mini App and Kassa, the cash book.
const (
	statsPath = "/stats/"
	gamePath  = "/game/"
	cardsPath = "/kartalar/"
	kassaPath = "/kassa/"
)

// kassaPage is the Kassa Mini App's page, "" when WEBAPP_URL is not set.
func kassaPage(cfg config.Config) string {
	if cfg.WebAppURL == "" {
		return ""
	}
	return cfg.WebAppURL + kassaPath
}

// NewKassa builds the Kassa Mini App's API: each user's cash book, kept in
// the database.
func NewKassa(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Endpoint, error) {
	if cfg.WebAppURL == "" {
		return nil, errNoPages
	}
	store, err := storage.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := store.CheckSchema(ctx); err != nil {
		store.Close()
		return nil, err
	}
	handler := kassa.NewHandler(kassa.Options{BotToken: cfg.TelegramToken, Store: store, Logger: logger})
	return &Endpoint{Handler: handler, Close: store.Close}, nil
}

// cardsPage is the Kartalar Mini App's page, "" when WEBAPP_URL is not set.
func cardsPage(cfg config.Config) string {
	if cfg.WebAppURL == "" {
		return ""
	}
	return cfg.WebAppURL + cardsPath
}

// NewCards builds the Kartalar Mini App's API: the player's collection,
// draws and the table, kept in the database.
func NewCards(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Endpoint, error) {
	if cfg.WebAppURL == "" {
		return nil, errNoPages
	}
	store, err := storage.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := store.CheckSchema(ctx); err != nil {
		store.Close()
		return nil, err
	}
	client := telegram.NewClient(telegram.Options{Token: cfg.TelegramToken, BaseURL: cfg.TelegramAPIURL, Logger: logger})
	notify := func(ctx context.Context, userID int64, text string) error {
		_, err := client.SendMessage(ctx, telegram.Outgoing{ChatID: userID, Text: text})
		return err
	}
	handler := cards.NewHandler(cards.Options{BotToken: cfg.TelegramToken, Store: store, Logger: logger, Notify: notify, Invoices: client})
	return &Endpoint{Handler: handler, Close: store.Close}, nil
}

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
	SetChatMenuCommands(ctx context.Context, chatID int64) error
	SetMenuCommands(ctx context.Context) error
}

// SetMenuApp puts every private chat's menu button to the commands, the
// owners' too: their dashboard button is gone, as the owners asked on
// 2026-11-21 (/stats still opens the dashboard). It logs a failure.
func SetMenuApp(ctx context.Context, setter MenuAppSetter, cfg config.Config, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, commandsTimeout)
	defer cancel()
	if err := setter.SetMenuCommands(ctx); err != nil {
		logger.Warn("could not put the menu button back to the commands", "error", err)
	}
	for ownerID := range cfg.OwnerIDs {
		if err := setter.SetChatMenuCommands(ctx, ownerID); err != nil {
			logger.Warn("could not put an owner's menu button back to the commands", "user_id", ownerID, "error", err)
		}
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
		Owners:   cfg.OwnerIDs,
		Location: cfg.Location,
		Logger:   logger,
	})
}

// NewGame builds the games' API: Telegram keeps each game message's
// scores, and the database each player's in each game and chat.
func NewGame(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Endpoint, error) {
	if newGame(cfg) == nil {
		return nil, errNoPages
	}
	store, err := storage.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := store.CheckSchema(ctx); err != nil {
		store.Close()
		return nil, err
	}
	return &Endpoint{Handler: gameHandler(cfg, store, logger), Close: store.Close}, nil
}

// gameHandler is the games' API for the pages WEBAPP_URL serves, recording
// the plays in records when it is not nil.
func gameHandler(cfg config.Config, records game.Records, logger *slog.Logger) http.Handler {
	client := telegram.NewClient(telegram.Options{Token: cfg.TelegramToken, BaseURL: cfg.TelegramAPIURL, Logger: logger})
	return newGame(cfg).Handler(client, records, logger)
}

// NewRaid builds Dev jangi's API: it keeps the bosses and heroes in the
// database, opens its page with the sessions the game's Play presses get,
// and sets scores and tells groups of their kills through Telegram.
func NewRaid(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Endpoint, error) {
	sessions := newGame(cfg)
	if sessions == nil {
		return nil, errNoPages
	}
	store, err := storage.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := store.CheckSchema(ctx); err != nil {
		store.Close()
		return nil, err
	}
	client := telegram.NewClient(telegram.Options{Token: cfg.TelegramToken, BaseURL: cfg.TelegramAPIURL, Logger: logger})
	handler := raid.NewHandler(raid.Options{Sessions: sessions, Store: store, Telegram: client, Logger: logger})
	return &Endpoint{Handler: handler, Close: store.Close}, nil
}

// NewMafia builds Mafia's API: it keeps each group's game in the database,
// opens its page with the sessions the game's Play presses get, tells the
// group how the game goes and pays the winners in Dev jangi's coins.
func NewMafia(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Endpoint, error) {
	sessions := newGame(cfg)
	if sessions == nil {
		return nil, errNoPages
	}
	store, err := storage.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := store.CheckSchema(ctx); err != nil {
		store.Close()
		return nil, err
	}
	client := telegram.NewClient(telegram.Options{Token: cfg.TelegramToken, BaseURL: cfg.TelegramAPIURL, Logger: logger})
	handler := mafia.NewHandler(mafia.Options{Sessions: sessions, Store: store, Telegram: client, Logger: logger})
	return &Endpoint{Handler: handler, Close: store.Close}, nil
}
