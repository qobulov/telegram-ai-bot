package cards

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// Store keeps Kartalar for the Mini App.
type Store interface {
	CatalogStore
	CardCatalog(ctx context.Context) ([]storage.Card, error)
	OwnedCards(ctx context.Context, userID int64, rarity int) ([]storage.OwnedCard, error)
	CardPlayerOf(ctx context.Context, userID int64) (storage.CardPlayer, time.Time, error)
	OpenCards(ctx context.Context, userID int64, name string, n int, roll storage.CardRoll, cost int, day storage.CardDay, now time.Time, quest storage.CardQuest) (storage.CardOpening, error)
	GiveDayTokens(ctx context.Context, userID int64, name string, day storage.CardDay) (int, error)
	CardTop(ctx context.Context, limit int) ([]storage.CardPlayer, error)
	CardWallet(ctx context.Context, userID int64) (shards int, err error)
	OfferCard(ctx context.Context, from, give, want int64, rules storage.CardMarketRules) (int64, error)
	CardMarket(ctx context.Context, viewer int64, rules storage.CardMarketRules, limit int) ([]storage.CardOffer, error)
	TakeCardOffer(ctx context.Context, id, by int64, rules storage.CardTradeRules, keep time.Duration, quest storage.CardQuest) (storage.CardTrade, error)
	CallOffCardTrade(ctx context.Context, id, by int64) (bool, error)
	CatalogCardsNamed(ctx context.Context, name string, limit int) ([]storage.Card, error)
	CatalogCards(ctx context.Context, page storage.CatalogPage) ([]storage.Card, bool, error)
	BuyCardTokens(ctx context.Context, userID int64, name string, tokens, price int) (storage.CardOpening, error)
	OpenCardPack(ctx context.Context, userID int64, name string, n int, roll storage.CardRoll, price int, now time.Time, quest storage.CardQuest) (storage.CardOpening, error)
	FuseCard(ctx context.Context, userID, cardID int64, cost []int, quest storage.CardQuest) (stars int, err error)
	OpenEventCards(ctx context.Context, userID int64, name string, n int, ev storage.CardEvent, day storage.CardDay, now time.Time, quest storage.CardQuest) (storage.EventOpening, error)
	CardEventPulls(ctx context.Context, userID, card int64) (int, storage.Card, error)
	SaveArenaTeam(ctx context.Context, userID int64, front [2]int64, back [3]int64, power int) error
	ArenaTeam(ctx context.Context, userID int64) (front [2]int64, back [3]int64, ok bool, err error)
	ArenaPlayerOf(ctx context.Context, userID int64, rules storage.ArenaRules) (storage.ArenaPlayer, error)
	ArenaOpponents(ctx context.Context, userID int64, n int) ([]storage.ArenaPlayer, error)
	RecordArenaBattle(ctx context.Context, attacker, defender, seed int64, attackerWon bool, rules storage.ArenaRules, quest storage.CardQuest) (storage.ArenaOutcome, error)
	ArenaBattles(ctx context.Context, userID int64, limit int) ([]storage.ArenaBattle, error)
	ArenaTop(ctx context.Context, limit int) ([]storage.ArenaPlayer, error)
	CardQuests(ctx context.Context, userID int64, day time.Time) (map[string]storage.QuestProgress, error)
	ClaimCardQuest(ctx context.Context, userID int64, quest storage.CardQuest, reward int, now time.Time) error
	AnimeRewardsClaimed(ctx context.Context, userID int64) ([]string, error)
	ClaimAnimeReward(ctx context.Context, userID int64, name, anime string, tokens int) (after int, claimed bool, err error)
}

// Invoices makes links to invoices in Stars, for crystals bought in the app.
type Invoices interface {
	CreateInvoiceLink(ctx context.Context, inv telegram.Invoice) (string, error)
}

// Options configures the Mini App's API.
type Options struct {
	BotToken string
	Store    Store
	// HTTPClient and AniList fill the catalog the first time; nil and ""
	// mean a short-timeout client and AniList's own address.
	HTTPClient *http.Client
	AniList    string
	Logger     *slog.Logger
	Now        func() time.Time
	// Roll draws a rarity; nil draws by the odds.
	Roll func() Rarity
	// Notify tells a player in their private chat of a card given them;
	// nil tells no one.
	Notify func(ctx context.Context, userID int64, text string) error
	// Invoices sells crystals for Stars; nil sells none.
	Invoices Invoices
	// EventRoll draws an event pull's rarity, and EventPick whether its
	// Mythic is the event's card; nil draws by the event's odds.
	EventRoll func() Rarity
	EventPick func() bool
}

const (
	// initDataAge is how long the Mini App's init data stays good: a page
	// left open keeps drawing for a day.
	initDataAge  = 24 * time.Hour
	maxBodyBytes = 16 << 10
	topSize      = 20
	// OffersOpen is how many offers a player may have on the market, and
	// OfferKeep how long one stays there.
	OffersOpen = 5
	OfferKeep  = 7 * 24 * time.Hour
	// marketShown is the most offers the market lists, and catalogShown the
	// most cards a search of the catalog finds; catalogPage is how many of a
	// rarity each page of the card wanted lists.
	marketShown  = 60
	catalogShown = 12
	catalogPage  = 10
)

type handler struct {
	Options
	catalog *Catalog
}

// NewHandler serves the Kartalar Mini App: "state" is the catalog, with
// the names and pictures of only the cards the player holds, their points
// and their tokens; "open" opens one card or Batch; "top" is the table.
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
	return &handler{opts, &Catalog{Store: opts.Store, Client: opts.HTTPClient, AniList: opts.AniList, Logger: opts.Logger}}
}

type request struct {
	InitData string `json:"init_data"`
	Action   string `json:"action"`
	// Query is the name searched for, for "catalog"; Card is the card
	// offered, for "offer", or raised, for "fuse".
	Query string `json:"query"`
	Card  int64  `json:"card"`
	// Want is the card wanted for Card, for "offer"; Offer is the offer
	// taken or called off, for "take" and "unoffer".
	Want  int64 `json:"want"`
	Offer int64 `json:"offer"`
	// Rarity and Offset page "catalog" through the cards of one rarity, but
	// Card; without Rarity it searches the whole catalog by Query.
	Rarity *int `json:"rarity"`
	Offset int  `json:"offset"`
	// Count is how many cards "open" opens: 1 or Batch.
	Count int `json:"count"`
	// Item is what "buy" buys, of ShopItems, or "invoice" sells, of
	// CrystalPacks.
	Item string `json:"item"`
	// Front and Back are the cards of the team "arena_team" saves, two and
	// three; Opponent is the player "arena_attack" attacks.
	Front    []int64 `json:"front"`
	Back     []int64 `json:"back"`
	Opponent int64   `json:"opponent"`
	// Quest is the daily quest "quest_claim" claims.
	Quest string `json:"quest"`
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
	case "event":
		return h.eventView(ctx, opened.User)
	case "event_open":
		return h.eventOpen(ctx, opened.User, req.Count)
	case "shop":
		return map[string]any{"items": ShopItems, "packs": CrystalPacks, "stars": h.Invoices != nil}, failure{}
	case "buy":
		return h.buy(ctx, opened.User, req.Item)
	case "invoice":
		return h.invoice(ctx, opened.User, req.Item)
	case "open":
		return h.open(ctx, opened.User, req.Count)
	case "top":
		return h.top(ctx, opened.User)
	case "market":
		return h.market(ctx, opened.User)
	case "catalog":
		if req.Rarity != nil {
			return h.catalogPage(ctx, storage.CatalogPage{Rarity: *req.Rarity, Name: req.Query, Except: req.Card, Offset: req.Offset, Limit: catalogPage})
		}
		return h.catalogSearch(ctx, req.Query)
	case "animes":
		return h.animes(ctx, opened.User)
	case "anime_claim":
		return h.animeClaim(ctx, opened.User, req.Query)
	case "offer":
		return h.offer(ctx, opened.User, req.Card, req.Want)
	case "take":
		return h.take(ctx, opened.User, req.Offer)
	case "unoffer":
		return h.unoffer(ctx, opened.User, req.Offer)
	case "fuse":
		return h.fuse(ctx, opened.User, req.Card)
	case "arena":
		return h.arenaView(ctx, opened.User)
	case "arena_team":
		return h.arenaTeam(ctx, opened.User, req.Front, req.Back)
	case "arena_attack":
		return h.arenaAttack(ctx, opened.User, req.Opponent)
	case "quests":
		return h.quests(ctx, opened.User)
	case "quest_claim":
		return h.claimQuest(ctx, opened.User, req.Quest)
	}
	return nil, errBadRequest
}

// cardView is a card as the page shows it; one not held is only its
// rarity, so that the page holds no secret to peek at.
type cardView struct {
	ID     int64  `json:"id"`
	Rarity int    `json:"rarity"`
	Set    string `json:"set"`
	Name   string `json:"name,omitempty"`
	Anime  string `json:"anime,omitempty"`
	// Franchise is the anime a card counts as, seasons and sequels as one
	// (the anime list only).
	Franchise string `json:"franchise,omitempty"`
	Image     string `json:"image,omitempty"`
	Attack    int    `json:"attack,omitempty"`
	Health    int    `json:"health,omitempty"`
	Value     int    `json:"value,omitempty"`
	Count     int    `json:"count"`
	// Limited is a card that is never traded.
	Limited bool `json:"limited,omitempty"`
	// Stars are a card held's, which raise its Attack and Health; Free are
	// its copies beyond the one kept and those used up, Fuse the free copies
	// its next star needs (0 at the most stars), and NextAttack and
	// NextHealth its numbers then.
	Stars      int `json:"stars,omitempty"`
	Free       int `json:"free,omitempty"`
	Fuse       int `json:"fuse,omitempty"`
	NextAttack int `json:"next_attack,omitempty"`
	NextHealth int `json:"next_health,omitempty"`
	// Serial is a limited card's number for its holder, which holder of it
	// they are; 0 for others.
	Serial int `json:"serial,omitempty"`
}

func (h *handler) held(c storage.Card, count int) cardView {
	return cardView{ID: c.ID, Rarity: c.Rarity, Set: c.Collection, Name: c.Name, Anime: c.Anime, Image: c.ImageURL, Attack: c.Attack, Health: c.Health, Value: c.Value, Count: count, Limited: storage.CardLimited(c.ID)}
}

// owned is a card the player holds, with its stars and what fusing it
// next costs and gives.
func (h *handler) owned(o storage.OwnedCard) cardView {
	v := h.held(o.Card, o.Count)
	v.Stars = min(max(o.Stars, 1), MaxStars)
	v.Free = max(o.Count-1-o.Melted, 0)
	v.Serial = o.Serial
	v.Attack, v.Health = Boosted(o.Card, v.Stars)
	if v.Stars < MaxStars {
		v.Fuse = FuseCost[v.Stars]
		v.NextAttack, v.NextHealth = Boosted(o.Card, v.Stars+1)
	}
	return v
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
	byID := make(map[int64]storage.OwnedCard, len(owned))
	for _, o := range owned {
		byID[o.ID] = o
	}
	views := make([]cardView, len(catalog))
	for i, c := range catalog {
		if o, ok := byID[c.ID]; ok && o.Count > 0 {
			o.Card = c
			views[i] = h.owned(o)
		} else {
			views[i] = cardView{ID: c.ID, Rarity: c.Rarity, Set: c.Collection, Limited: storage.CardLimited(c.ID)}
		}
	}
	// Opening the app gives the day's tokens, so that the page shows them.
	now := h.Now()
	given, err := h.Store.GiveDayTokens(ctx, user.ID, name(user), Today(now))
	if err != nil {
		h.Logger.Error("could not give the day's tokens", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	player, _, err := h.Store.CardPlayerOf(ctx, user.ID)
	if err != nil {
		h.Logger.Error("could not read a card player", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	shards, err := h.Store.CardWallet(ctx, user.ID)
	if err != nil {
		h.Logger.Error("could not read a player's shards", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	_, done, ready, err := h.questList(ctx, user.ID, now)
	if err != nil {
		h.Logger.Error("could not read card quests", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	return map[string]any{
		"cards": views, "points": player.Points, "held": len(owned),
		"tokens": Tokens(player, now), "given": given, "per_day": PerDay, "card_cost": CardCost, "batch": Batch,
		"next_token_in": Today(now).Next().Sub(now).Milliseconds(),
		"shards":        shards,
		"quests":        map[string]any{"done": done, "ready": ready, "total": len(Quests(now)), "reset_in": questsResetIn(now)},
	}, failure{}
}

// open opens 1 or Batch cards, each for a token or, with none left, for
// CardCost crystals; with too few of both it says what the player has.
func (h *handler) open(ctx context.Context, user miniapp.User, n int) (any, failure) {
	if n != 1 && n != Batch {
		return nil, errBadRequest
	}
	if err := h.catalog.Ensure(ctx); err != nil {
		h.Logger.Error("could not fill the card catalog", "error", err)
		return nil, errUnavailable
	}
	now := h.Now()
	opened, err := h.Store.OpenCards(ctx, user.ID, name(user), n, ByOdds(h.Roll), CardCost, Today(now), now, questOf(QuestOpen, now))
	var few *storage.CardFundsError
	switch {
	case errors.As(err, &few):
		return map[string]any{"short": few.Need - few.Shards, "tokens": few.Tokens, "shards": few.Shards}, failure{}
	case err != nil:
		h.Logger.Error("could not open cards", "user_id", user.ID, "count", n, "error", err)
		return nil, errUnavailable
	}
	drawn := make([]map[string]any, len(opened.Draws))
	for i, d := range opened.Draws {
		drawn[i] = map[string]any{"card": h.held(d.Card, d.Count), "new": d.New, "serial": d.Serial}
	}
	return map[string]any{
		"drawn": drawn, "points": opened.Points, "tokens": opened.Tokens, "shards": opened.Shards, "given": opened.Given,
	}, failure{}
}

// appLanguage is "uz", "ru" or "en" for a Telegram language code, as the
// bot answers in.
func appLanguage(code string) string {
	code, _, _ = strings.Cut(strings.ToLower(code), "-")
	switch code {
	case "", "uz":
		return "uz"
	case "ru", "uk", "be", "kk", "ky", "tg":
		return "ru"
	}
	return "en"
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

// nameMarks are what Telegram's formatting would read in a name as a mark.
var nameMarks = strings.NewReplacer("*", "", "`", "", "[", "(", "]", ")")

func marketRules(now time.Time) storage.CardMarketRules {
	return storage.CardMarketRules{Open: OffersOpen, Keep: OfferKeep, Now: now}
}

// offerView is an offer on the market as the page shows it.
type offerView struct {
	ID   int64    `json:"id"`
	Who  string   `json:"who"`
	Give cardView `json:"give"`
	Want cardView `json:"want"`
	Can  bool     `json:"can"`
	Mine bool     `json:"mine"`
}

// market is the offers open: the player's own, which they may call off, and
// the others', each saying whether the player holds the card it wants.
func (h *handler) market(ctx context.Context, user miniapp.User) (any, failure) {
	offers, err := h.Store.CardMarket(ctx, user.ID, marketRules(h.Now()), marketShown)
	if err != nil {
		h.Logger.Error("could not read the card market", "error", err)
		return nil, errUnavailable
	}
	views := make([]offerView, len(offers))
	for i, o := range offers {
		views[i] = offerView{ID: o.ID, Who: o.FromName, Give: h.held(o.Give, 0), Want: h.held(o.Want, 0), Can: o.Can, Mine: o.Mine}
	}
	return map[string]any{"offers": views, "open_limit": OffersOpen}, failure{}
}

// animes is the whole catalog with every card's name, picture and anime as
// players know it, so that a player may look through each anime's cards,
// held or not, with the animes whose collection reward they took and the
// reward's rule.
func (h *handler) animes(ctx context.Context, user miniapp.User) (any, failure) {
	if err := h.catalog.Ensure(ctx); err != nil {
		h.Logger.Error("could not fill the card catalog", "error", err)
		return nil, errUnavailable
	}
	catalog, err := h.Store.CardCatalog(ctx)
	if err != nil {
		h.Logger.Error("could not read the card catalog", "error", err)
		return nil, errUnavailable
	}
	views := make([]cardView, len(catalog))
	for i, c := range catalog {
		views[i] = h.held(c, 0)
		views[i].Franchise = Franchise(c.Anime)
	}
	claimed, err := h.Store.AnimeRewardsClaimed(ctx, user.ID)
	if err != nil {
		h.Logger.Error("could not read a player's anime rewards", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	return map[string]any{
		"cards": views, "claimed": claimed, "reward": map[string]int{"tokens": AnimeRewardTokens, "least": AnimeRewardLeast},
	}, failure{}
}

// catalogSearch finds the cards of the catalog by name, for the card a
// player wants: a name searched for is shown, held or not.
func (h *handler) catalogSearch(ctx context.Context, query string) (any, failure) {
	if strings.TrimSpace(query) == "" {
		return map[string]any{"cards": []cardView{}}, failure{}
	}
	found, err := h.Store.CatalogCardsNamed(ctx, query, catalogShown)
	if err != nil {
		h.Logger.Error("could not search the card catalog", "error", err)
		return nil, errUnavailable
	}
	views := make([]cardView, len(found))
	for i, c := range found {
		views[i] = h.held(c, 0)
	}
	return map[string]any{"cards": views}, failure{}
}

// catalogPage is a page of the cards of one rarity a player may want for
// theirs, and whether another page follows.
func (h *handler) catalogPage(ctx context.Context, page storage.CatalogPage) (any, failure) {
	if page.Rarity < 0 || page.Rarity >= len(Rarities) || page.Offset < 0 {
		return nil, errBadRequest
	}
	found, more, err := h.Store.CatalogCards(ctx, page)
	if err != nil {
		h.Logger.Error("could not read a catalog page", "error", err)
		return nil, errUnavailable
	}
	views := make([]cardView, len(found))
	for i, c := range found {
		views[i] = h.held(c, 0)
	}
	return map[string]any{"cards": views, "more": more}, failure{}
}

// offer puts the player's card up on the market for the card they want. An
// offer refused says why: "not_held", "limit", "same", "uneven" or
// "limited" or "fused".
func (h *handler) offer(ctx context.Context, user miniapp.User, give, want int64) (any, failure) {
	switch {
	case give == 0 || want == 0:
		return nil, errBadRequest
	case give == want:
		return map[string]any{"refused": "same"}, failure{}
	}
	_, err := h.Store.OfferCard(ctx, user.ID, give, want, marketRules(h.Now()))
	var many *storage.CardOffersError
	switch {
	case errors.Is(err, storage.ErrCardNotHeld):
		return map[string]any{"refused": "not_held"}, failure{}
	case errors.Is(err, storage.ErrCardTradeUneven):
		return map[string]any{"refused": "uneven"}, failure{}
	case errors.Is(err, storage.ErrCardLimited):
		return map[string]any{"refused": "limited"}, failure{}
	case errors.Is(err, storage.ErrCardFused):
		return map[string]any{"refused": "fused"}, failure{}
	case errors.As(err, &many):
		return map[string]any{"refused": "limit", "limit": many.Limit}, failure{}
	case err != nil:
		h.Logger.Error("could not offer a card", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	return h.market(ctx, user)
}

// take takes an offer: its player and this one each give the other their
// card, and its player is told. An offer refused says why: "over",
// "gone", "not_held", "own", "uneven", "limited" or "fused".
func (h *handler) take(ctx context.Context, user miniapp.User, id int64) (any, failure) {
	if id == 0 {
		return nil, errBadRequest
	}
	now := h.Now()
	trade, err := h.Store.TakeCardOffer(ctx, id, user.ID, storage.CardTradeRules{Now: now}, OfferKeep, questOf(QuestMarket, now))
	switch {
	case errors.Is(err, storage.ErrCardTradeOver):
		return map[string]any{"refused": "over"}, failure{}
	case errors.Is(err, storage.ErrCardOfferGone):
		return map[string]any{"refused": "gone"}, failure{}
	case errors.Is(err, storage.ErrCardOfferOwn):
		return map[string]any{"refused": "own"}, failure{}
	case errors.Is(err, storage.ErrCardNotHeld):
		return map[string]any{"refused": "not_held"}, failure{}
	case errors.Is(err, storage.ErrCardTradeUneven):
		return map[string]any{"refused": "uneven"}, failure{}
	case errors.Is(err, storage.ErrCardLimited):
		return map[string]any{"refused": "limited"}, failure{}
	case errors.Is(err, storage.ErrCardFused):
		return map[string]any{"refused": "fused"}, failure{}
	case err != nil:
		h.Logger.Error("could not take a card offer", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	if h.Notify != nil {
		text := strings.NewReplacer("{who}", nameMarks.Replace(name(user)), "{got}", nameMarks.Replace(trade.Take.Name), "{gave}", nameMarks.Replace(trade.Give.Name)).
			Replace(takenTexts[appLanguage(user.Language)])
		if err := h.Notify(ctx, trade.From, text); err != nil {
			h.Logger.Info("could not tell a player of a trade", "user_id", trade.From, "error", err)
		}
	}
	state, fail := h.state(ctx, user)
	if fail != (failure{}) {
		return nil, fail
	}
	result := state.(map[string]any)
	result["got"] = h.held(trade.Give, 1)
	return result, failure{}
}

// fuse raises the player's card one star with its free copies, and gives
// the state with "fused", its stars now. A fusion refused says why: "max",
// "few" or "not_held".
func (h *handler) fuse(ctx context.Context, user miniapp.User, cardID int64) (any, failure) {
	if cardID == 0 {
		return nil, errBadRequest
	}
	stars, err := h.Store.FuseCard(ctx, user.ID, cardID, FuseCost[:], questOf(QuestFuse, h.Now()))
	switch {
	case errors.Is(err, storage.ErrCardMaxStars):
		return map[string]any{"refused": "max"}, failure{}
	case errors.Is(err, storage.ErrCardFewCopies):
		return map[string]any{"refused": "few"}, failure{}
	case errors.Is(err, storage.ErrCardNotHeld):
		return map[string]any{"refused": "not_held"}, failure{}
	case err != nil:
		h.Logger.Error("could not fuse a card", "user_id", user.ID, "card_id", cardID, "error", err)
		return nil, errUnavailable
	}
	state, fail := h.state(ctx, user)
	if fail != (failure{}) {
		return nil, fail
	}
	result := state.(map[string]any)
	result["fused"] = stars
	return result, failure{}
}

// unoffer calls off the player's own offer.
func (h *handler) unoffer(ctx context.Context, user miniapp.User, id int64) (any, failure) {
	if _, err := h.Store.CallOffCardTrade(ctx, id, user.ID); err != nil {
		h.Logger.Error("could not call off a card offer", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	return h.market(ctx, user)
}

var takenTexts = map[string]string{
	"uz": "🔄 {who} taklifingizni qabul qildi: siz **{got}** oldingiz, **{gave}** berdingiz.\nKolleksiyangiz: /card",
	"ru": "🔄 {who} принял ваше предложение: вы получили **{got}** и отдали **{gave}**.\nВаша коллекция: /card",
	"en": "🔄 {who} took your offer: you got **{got}** and gave **{gave}**.\nYour collection: /card",
}
