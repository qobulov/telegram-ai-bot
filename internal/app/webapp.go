package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/game"
	"telegram-ai-bot/internal/mafia"
	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/raid"
	"telegram-ai-bot/internal/shashkaapp"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// The pages WEBAPP_URL serves, from public/: the group dashboard Mini App,
// the game and the draughts Mini App.
const (
	statsPath   = "/stats/"
	gamePath    = "/game/"
	shashkaPath = "/shashka/"
)

// ShashkaPage is the draughts Mini App's page, "" when WEBAPP_URL is not
// set.
func ShashkaPage(cfg config.Config) string {
	if cfg.WebAppURL == "" {
		return ""
	}
	return cfg.WebAppURL + shashkaPath
}

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
	SetMenuWebApp(ctx context.Context, chatID int64, text, url string) error
	SetMenuCommands(ctx context.Context) error
}

// SetMenuApp makes the menu button of the owners' private chats open the
// dashboard while WEBAPP_URL is set, and puts everyone else's back to the
// commands, as an earlier version opened it for all. It logs a failure:
// /stats opens the dashboard too. Without WEBAPP_URL the buttons are left
// as they are, so that one set in @BotFather stays.
func SetMenuApp(ctx context.Context, setter MenuAppSetter, cfg config.Config, logger *slog.Logger) {
	page := statsPage(cfg)
	if page == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, commandsTimeout)
	defer cancel()
	if err := setter.SetMenuCommands(ctx); err != nil {
		logger.Warn("could not put the menu button back to the commands", "error", err)
	}
	for ownerID := range cfg.OwnerIDs {
		if err := setter.SetMenuWebApp(ctx, ownerID, menuAppText, page); err != nil {
			logger.Warn("could not set an owner's menu button to the dashboard", "user_id", ownerID, "error", err)
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

// NewShashka builds the draughts Mini App's API: it keeps the games and
// ratings in the database, and asks Telegram who the bot is, for the
// invites' links.
func NewShashka(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Endpoint, error) {
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
	bot, err := Connect(ctx, cfg, logger)
	if err != nil {
		store.Close()
		return nil, err
	}
	handler := shashkaapp.NewHandler(shashkaapp.Options{
		BotToken:    cfg.TelegramToken,
		BotUsername: bot.User.Username,
		PageURL:     ShashkaPage(cfg),
		Store:       store,
		Messenger:   bot.Client,
		Logger:      logger,
	})
	return &Endpoint{Handler: handler, Close: store.Close}, nil
}
