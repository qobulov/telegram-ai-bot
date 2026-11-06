package cards

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/storage"
)

// Store keeps Kartalar for the Mini App.
type Store interface {
	CatalogStore
	CardCatalog(ctx context.Context) ([]storage.Card, error)
	OwnedCards(ctx context.Context, userID int64, rarity int) ([]storage.OwnedCard, error)
	CardPlayerOf(ctx context.Context, userID int64) (storage.CardPlayer, time.Time, error)
	DrawCard(ctx context.Context, userID int64, name string, rarity int, every time.Duration, now time.Time) (storage.CardDraw, error)
	CardTop(ctx context.Context, limit int) ([]storage.CardPlayer, error)
	CardImage(ctx context.Context, id int64) (string, error)
}

// Options configures the Mini App's API.
type Options struct {
	BotToken string
	Store    Store
	// HTTPClient, AniList and PokeWallet fill the catalog the first time;
	// nil and "" mean a short-timeout client and the services' own
	// addresses. Without PokeWalletKey there are no trading cards.
	HTTPClient    *http.Client
	AniList       string
	PokeWallet    string
	PokeWalletKey string
	Logger        *slog.Logger
	Now           func() time.Time
	// Roll draws a rarity; nil draws by the odds.
	Roll func() Rarity
}

const (
	// initDataAge is how long the Mini App's init data stays good: a page
	// left open keeps drawing for a day.
	initDataAge  = 24 * time.Hour
	maxBodyBytes = 16 << 10
	topSize      = 20
)

type handler struct {
	Options
	catalog *Catalog
}

// NewHandler serves the Kartalar Mini App: "state" is the catalog, with
// the names and pictures of only the cards the player holds, their points
// and how long until their next card; "draw" draws one; "top" is the table.
// Each request is the player's by the init data Telegram signs.
func NewHandler(opts Options) http.Handler {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Roll == nil {
		opts.Roll = RollRandom
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &handler{opts, &Catalog{Store: opts.Store, Client: opts.HTTPClient, AniList: opts.AniList, PokeWallet: opts.PokeWallet, Key: opts.PokeWalletKey, Logger: opts.Logger}}
}

type request struct {
	InitData string `json:"init_data"`
	Action   string `json:"action"`
}

type failure struct {
	status int
	code   string
}

var (
	errMethod      = failure{http.StatusMethodNotAllowed, "method"}
	errBadRequest  = failure{http.StatusBadRequest, "bad_request"}
	errForged      = failure{http.StatusUnauthorized, "forged"}
	errStale       = failure{http.StatusUnauthorized, "stale"}
	errUnavailable = failure{http.StatusBadGateway, "unavailable"}
)

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Query().Has("image") {
		h.image(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	result, fail := h.serve(r)
	if fail != (failure{}) {
		w.WriteHeader(fail.status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fail.code})
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func (h *handler) serve(r *http.Request) (any, failure) {
	if r.Method != http.MethodPost {
		return nil, errMethod
	}
	var req request
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes)).Decode(&req); err != nil {
		return nil, errBadRequest
	}
	opened, err := miniapp.Validate(req.InitData, h.BotToken, h.Now(), initDataAge)
	switch {
	case errors.Is(err, miniapp.ErrStale):
		return nil, errStale
	case err != nil:
		return nil, errForged
	}
	ctx := r.Context()
	switch req.Action {
	case "state":
		return h.state(ctx, opened.User)
	case "draw":
		return h.draw(ctx, opened.User)
	case "top":
		return h.top(ctx, opened.User)
	}
	return nil, errBadRequest
}

// image serves the picture of a card in the catalog that wants the key, and
// only those: the address carries nothing but the card's ID, and it is kept
// by the CDN, as a picture never changes and PokéWallet counts each request.
func (h *handler) image(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("image"), 10, 64)
	if err != nil {
		http.Error(w, "bad_request", http.StatusBadRequest)
		return
	}
	link, err := h.Store.CardImage(r.Context(), id)
	if err != nil || !h.catalog.Proxied(link) {
		http.NotFound(w, r)
		return
	}
	data, kind, err := h.catalog.FetchPicture(r.Context(), link)
	if err != nil {
		h.Logger.Warn("could not fetch a card's picture", "card", id, "error", err)
		http.Error(w, "unavailable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", "public, max-age=31536000, s-maxage=31536000, immutable")
	_, _ = w.Write(data)
}

// cardView is a card as the page shows it; one not held is only its
// rarity, so that the page holds no secret to peek at.
type cardView struct {
	ID     int64  `json:"id"`
	Rarity int    `json:"rarity"`
	Set    string `json:"set"`
	Name   string `json:"name,omitempty"`
	Anime  string `json:"anime,omitempty"`
	Image  string `json:"image,omitempty"`
	Attack int    `json:"attack,omitempty"`
	Health int    `json:"health,omitempty"`
	Value  int    `json:"value,omitempty"`
	Count  int    `json:"count"`
}

func (h *handler) held(c storage.Card, count int) cardView {
	image := c.ImageURL
	if h.catalog.Proxied(image) {
		image = "/api/cards?image=" + strconv.FormatInt(c.ID, 10)
	}
	return cardView{ID: c.ID, Rarity: c.Rarity, Set: c.Collection, Name: c.Name, Anime: c.Anime, Image: image, Attack: c.Attack, Health: c.Health, Value: c.Value, Count: count}
}

func (h *handler) state(ctx context.Context, user miniapp.User) (any, failure) {
	if err := h.catalog.Ensure(ctx); err != nil {
		h.Logger.Error("could not fill the card catalog", "error", err)
		return nil, errUnavailable
	}
	catalog, err := h.Store.CardCatalog(ctx)
	if err != nil {
		h.Logger.Error("could not read the card catalog", "error", err)
		return nil, errUnavailable
	}
	owned, err := h.Store.OwnedCards(ctx, user.ID, -1)
	if err != nil {
		h.Logger.Error("could not read a player's cards", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	counts := make(map[int64]int, len(owned))
	for _, o := range owned {
		counts[o.ID] = o.Count
	}
	views := make([]cardView, len(catalog))
	for i, c := range catalog {
		if count := counts[c.ID]; count > 0 {
			views[i] = h.held(c, count)
		} else {
			views[i] = cardView{ID: c.ID, Rarity: c.Rarity, Set: c.Collection}
		}
	}
	player, last, err := h.Store.CardPlayerOf(ctx, user.ID)
	if err != nil {
		h.Logger.Error("could not read a card player", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	return map[string]any{
		"cards": views, "points": player.Points, "held": len(owned),
		"next_in": h.wait(last).Milliseconds(), "every": Every.Milliseconds(),
	}, failure{}
}

// wait is how long until the player may draw again.
func (h *handler) wait(last time.Time) time.Duration {
	if last.IsZero() {
		return 0
	}
	return max(0, Every-h.Now().Sub(last))
}

func (h *handler) draw(ctx context.Context, user miniapp.User) (any, failure) {
	if err := h.catalog.Ensure(ctx); err != nil {
		h.Logger.Error("could not fill the card catalog", "error", err)
		return nil, errUnavailable
	}
	draw, err := h.Store.DrawCard(ctx, user.ID, name(user), int(h.Roll()), Every, h.Now())
	var wait *storage.CardWaitError
	switch {
	case errors.As(err, &wait):
		return map[string]any{"wait": wait.Wait.Milliseconds()}, failure{}
	case err != nil:
		h.Logger.Error("could not draw a card", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	return map[string]any{
		"card": h.held(draw.Card, draw.Count), "new": draw.New, "points": draw.Points, "next_in": Every.Milliseconds(),
	}, failure{}
}

func (h *handler) top(ctx context.Context, user miniapp.User) (any, failure) {
	top, err := h.Store.CardTop(ctx, topSize)
	if err != nil {
		h.Logger.Error("could not read the card table", "error", err)
		return nil, errUnavailable
	}
	rows := make([]map[string]any, len(top))
	for i, p := range top {
		rows[i] = map[string]any{"name": p.Name, "points": p.Points, "cards": p.Cards, "me": p.UserID == user.ID}
	}
	return map[string]any{"top": rows}, failure{}
}

// name is how the player is named on the table: their first name, else
// their username.
func name(user miniapp.User) string {
	if first := strings.TrimSpace(user.FirstName); first != "" {
		return first
	}
	return user.Username
}
