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
)

// Store keeps Kartalar for the Mini App.
type Store interface {
	CatalogStore
	CardCatalog(ctx context.Context) ([]storage.Card, error)
	OwnedCards(ctx context.Context, userID int64, rarity int) ([]storage.OwnedCard, error)
	CardPlayerOf(ctx context.Context, userID int64) (storage.CardPlayer, time.Time, error)
	DrawCard(ctx context.Context, userID int64, name string, roll storage.CardRoll, every time.Duration, now time.Time) (storage.CardDraw, error)
	CardTop(ctx context.Context, limit int) ([]storage.CardPlayer, error)
	CardWallet(ctx context.Context, userID int64) (shards int, spares map[int]int, err error)
	MeltCards(ctx context.Context, userID int64, rates []int) (melted, gained, shards int, err error)
	ExtraCard(ctx context.Context, userID int64, name, collection string, roll storage.CardRoll, cost int) (storage.CardDraw, error)
	SetCardAuto(ctx context.Context, userID int64, name string, on bool, language string) error
	CardPlayersNamed(ctx context.Context, name string, except int64, limit int) ([]storage.CardPlayer, error)
	GiveCard(ctx context.Context, chatID, from, to, cardID int64, rules storage.CardTradeRules) error
	OfferCard(ctx context.Context, from, give, want int64, rules storage.CardMarketRules) (int64, error)
	CardMarket(ctx context.Context, viewer int64, rules storage.CardMarketRules, limit int) ([]storage.CardOffer, error)
	TakeCardOffer(ctx context.Context, id, by int64, rules storage.CardTradeRules, keep time.Duration) (storage.CardTrade, error)
	CallOffCardTrade(ctx context.Context, id, by int64) (bool, error)
	CatalogCardsNamed(ctx context.Context, name string, limit int) ([]storage.Card, error)
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
}

const (
	// initDataAge is how long the Mini App's init data stays good: a page
	// left open keeps drawing for a day.
	initDataAge  = 24 * time.Hour
	maxBodyBytes = 16 << 10
	topSize      = 20
	// playersShown is the most players a search for whom to give a card
	// lists.
	playersShown = 10
	// GiveLimit is how many cards a player may give a day, so that a
	// second account cannot feed a first one its cards; GiveDay is that day.
	GiveLimit = 5
	GiveDay   = 24 * time.Hour
	// OffersOpen is how many offers a player may have on the market, and
	// OfferKeep how long one stays there.
	OffersOpen = 5
	OfferKeep  = 7 * 24 * time.Hour
	// marketShown is the most offers the market lists, and catalogShown the
	// most cards a search of the catalog finds.
	marketShown  = 60
	catalogShown = 12
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
	return &handler{opts, &Catalog{Store: opts.Store, Client: opts.HTTPClient, AniList: opts.AniList, Logger: opts.Logger}}
}

type request struct {
	InitData string `json:"init_data"`
	Action   string `json:"action"`
	// On is the auto opener's switch, for "auto".
	On bool `json:"on"`
	// Query is the name searched for, for "players"; Card and To are the
	// card given and the player it is given to, for "gift".
	Query string `json:"query"`
	Card  int64  `json:"card"`
	To    int64  `json:"to"`
	// Want is the card wanted for Card, for "offer"; Offer is the offer
	// taken or called off, for "take" and "unoffer".
	Want  int64 `json:"want"`
	Offer int64 `json:"offer"`
	// Set is the collection a card is bought from, for "extra"; "" is any.
	Set string `json:"set"`
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
	case "draw":
		return h.draw(ctx, opened.User)
	case "top":
		return h.top(ctx, opened.User)
	case "melt":
		return h.melt(ctx, opened.User)
	case "extra":
		return h.extra(ctx, opened.User, req.Set)
	case "auto":
		return h.auto(ctx, opened.User, req.On)
	case "players":
		return h.players(ctx, opened.User, req.Query)
	case "gift":
		return h.gift(ctx, opened.User, req.Card, req.To)
	case "market":
		return h.market(ctx, opened.User)
	case "catalog":
		return h.catalogSearch(ctx, req.Query)
	case "offer":
		return h.offer(ctx, opened.User, req.Card, req.Want)
	case "take":
		return h.take(ctx, opened.User, req.Offer)
	case "unoffer":
		return h.unoffer(ctx, opened.User, req.Offer)
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
	Image  string `json:"image,omitempty"`
	Attack int    `json:"attack,omitempty"`
	Health int    `json:"health,omitempty"`
	Value  int    `json:"value,omitempty"`
	Count  int    `json:"count"`
	// Spare is the copies held but one that are not melted, which may be
	// given.
	Spare int `json:"spare,omitempty"`
}

func (h *handler) held(c storage.Card, count int) cardView {
	return cardView{ID: c.ID, Rarity: c.Rarity, Set: c.Collection, Name: c.Name, Anime: c.Anime, Image: c.ImageURL, Attack: c.Attack, Health: c.Health, Value: c.Value, Count: count}
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
			views[i] = h.held(c, o.Count)
			views[i].Spare = max(0, o.Count-1-o.Melted)
		} else {
			views[i] = cardView{ID: c.ID, Rarity: c.Rarity, Set: c.Collection}
		}
	}
	player, last, err := h.Store.CardPlayerOf(ctx, user.ID)
	if err != nil {
		h.Logger.Error("could not read a card player", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	shards, spares, err := h.Store.CardWallet(ctx, user.ID)
	if err != nil {
		h.Logger.Error("could not read a player's shards", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	spare, worth := SpareWorth(spares)
	return map[string]any{
		"cards": views, "points": player.Points, "held": len(owned),
		"next_in": h.wait(last).Milliseconds(), "every": Every.Milliseconds(),
		"shards": shards, "spare": spare, "spare_worth": worth, "extra_cost": ExtraCost, "set_cost": SetCost,
		"auto": player.Auto,
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
	draw, err := h.Store.DrawCard(ctx, user.ID, name(user), WithPity(h.Roll), Every, h.Now())
	var wait *storage.CardWaitError
	switch {
	case errors.As(err, &wait):
		return map[string]any{"wait": wait.Wait.Milliseconds()}, failure{}
	case err != nil:
		h.Logger.Error("could not draw a card", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	legendaryIn, mythicIn := PityIn(draw)
	return map[string]any{
		"card": h.held(draw.Card, draw.Count), "new": draw.New, "points": draw.Points, "next_in": Every.Milliseconds(),
		"guaranteed": draw.Guaranteed, "legendary_in": legendaryIn, "mythic_in": mythicIn,
	}, failure{}
}

// auto turns the player's auto opener on or off; its cards go to their
// private chat in the language of their Telegram.
func (h *handler) auto(ctx context.Context, user miniapp.User, on bool) (any, failure) {
	if err := h.Store.SetCardAuto(ctx, user.ID, name(user), on, appLanguage(user.Language)); err != nil {
		h.Logger.Error("could not set a card auto opener", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	return map[string]any{"auto": on}, failure{}
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

// melt melts the player's spare copies into shards.
func (h *handler) melt(ctx context.Context, user miniapp.User) (any, failure) {
	melted, gained, shards, err := h.Store.MeltCards(ctx, user.ID, ShardRates)
	if err != nil {
		h.Logger.Error("could not melt cards", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	return map[string]any{"melted": melted, "gained": gained, "shards": shards}, failure{}
}

// extra draws a card out of turn for shards, of the collection set or of
// any when it is "", or says how few the player has.
func (h *handler) extra(ctx context.Context, user miniapp.User, set string) (any, failure) {
	cost := ExtraCost
	if set != "" {
		if !IsCollection(set) {
			return nil, errBadRequest
		}
		cost = SetCost
	}
	if err := h.catalog.Ensure(ctx); err != nil {
		h.Logger.Error("could not fill the card catalog", "error", err)
		return nil, errUnavailable
	}
	draw, err := h.Store.ExtraCard(ctx, user.ID, name(user), set, WithPity(h.Roll), cost)
	var few *storage.ShardsError
	switch {
	case errors.As(err, &few):
		return map[string]any{"need": few.Need, "have": few.Have}, failure{}
	case err != nil:
		h.Logger.Error("could not draw a card for shards", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	legendaryIn, mythicIn := PityIn(draw)
	return map[string]any{
		"card": h.held(draw.Card, draw.Count), "new": draw.New, "points": draw.Points, "shards": draw.Shards,
		"guaranteed": draw.Guaranteed, "legendary_in": legendaryIn, "mythic_in": mythicIn,
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

// players are those the player may give a card to whose name holds query:
// any who have opened a card but themselves, the most points first.
func (h *handler) players(ctx context.Context, user miniapp.User, query string) (any, failure) {
	found, err := h.Store.CardPlayersNamed(ctx, query, user.ID, playersShown)
	if err != nil {
		h.Logger.Error("could not find card players", "error", err)
		return nil, errUnavailable
	}
	rows := make([]map[string]any, len(found))
	for i, p := range found {
		rows[i] = map[string]any{"id": p.UserID, "name": p.Name, "username": p.Username, "cards": p.Cards, "points": p.Points}
	}
	return map[string]any{"players": rows}, failure{}
}

// gift gives a copy of the player's card to another, with its points, and
// tells them. A gift refused says why: "limit", "no_player" or "not_held".
func (h *handler) gift(ctx context.Context, user miniapp.User, cardID, to int64) (any, failure) {
	if cardID == 0 || to == 0 || to == user.ID {
		return nil, errBadRequest
	}
	err := h.Store.GiveCard(ctx, 0, user.ID, to, cardID, storage.CardTradeRules{Limit: GiveLimit, Day: GiveDay, Now: h.Now()})
	var limit *storage.CardGiveLimitError
	switch {
	case errors.As(err, &limit):
		return map[string]any{"refused": "limit", "limit": GiveLimit}, failure{}
	case errors.Is(err, storage.ErrCardNoPlayer):
		return map[string]any{"refused": "no_player"}, failure{}
	case errors.Is(err, storage.ErrCardNotHeld):
		return map[string]any{"refused": "not_held"}, failure{}
	case errors.Is(err, storage.ErrCardLastCopy):
		return map[string]any{"refused": "no_spare"}, failure{}
	case err != nil:
		h.Logger.Error("could not give a card", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	h.tellGift(ctx, user, cardID, to)
	return h.state(ctx, user)
}

// tellGift tells the player given a card who gave it, in the giver's
// language; a message that does not go out is only logged.
func (h *handler) tellGift(ctx context.Context, user miniapp.User, cardID, to int64) {
	if h.Notify == nil {
		return
	}
	card := ""
	if owned, err := h.Store.OwnedCards(ctx, to, -1); err == nil {
		for _, c := range owned {
			if c.ID == cardID {
				card = c.Name
			}
		}
	}
	text := strings.NewReplacer("{who}", giftMarks.Replace(name(user)), "{card}", giftMarks.Replace(card)).
		Replace(giftTexts[appLanguage(user.Language)])
	if err := h.Notify(ctx, to, text); err != nil {
		h.Logger.Info("could not tell a player of a gift", "user_id", to, "error", err)
	}
}

// giftMarks are what Telegram's formatting would read in a name as a mark.
var giftMarks = strings.NewReplacer("*", "", "`", "", "[", "(", "]", ")")

var giftTexts = map[string]string{
	"uz": "🎁 {who} sizga karta sovg'a qildi: **{card}**!\nKolleksiyangiz: /cards",
	"ru": "🎁 {who} подарил вам карту: **{card}**!\nВаша коллекция: /cards",
	"en": "🎁 {who} gave you a card: **{card}**!\nYour collection: /cards",
}

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

// offer puts the player's card up on the market for the card they want. An
// offer refused says why: "not_held", "limit" or "same".
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
// "gone", "not_held", "limit" or "own".
func (h *handler) take(ctx context.Context, user miniapp.User, id int64) (any, failure) {
	if id == 0 {
		return nil, errBadRequest
	}
	trade, err := h.Store.TakeCardOffer(ctx, id, user.ID, storage.CardTradeRules{Limit: GiveLimit, Day: GiveDay, Now: h.Now()}, OfferKeep)
	var limit *storage.CardGiveLimitError
	switch {
	case errors.Is(err, storage.ErrCardTradeOver):
		return map[string]any{"refused": "over"}, failure{}
	case errors.Is(err, storage.ErrCardOfferGone):
		return map[string]any{"refused": "gone"}, failure{}
	case errors.Is(err, storage.ErrCardOfferOwn):
		return map[string]any{"refused": "own"}, failure{}
	case errors.Is(err, storage.ErrCardNotHeld):
		return map[string]any{"refused": "not_held"}, failure{}
	case errors.As(err, &limit):
		return map[string]any{"refused": "limit", "limit": GiveLimit, "mine": limit.UserID == user.ID}, failure{}
	case err != nil:
		h.Logger.Error("could not take a card offer", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	if h.Notify != nil {
		text := strings.NewReplacer("{who}", giftMarks.Replace(name(user)), "{got}", giftMarks.Replace(trade.Take.Name), "{gave}", giftMarks.Replace(trade.Give.Name)).
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

// unoffer calls off the player's own offer.
func (h *handler) unoffer(ctx context.Context, user miniapp.User, id int64) (any, failure) {
	if _, err := h.Store.CallOffCardTrade(ctx, id, user.ID); err != nil {
		h.Logger.Error("could not call off a card offer", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	return h.market(ctx, user)
}

var takenTexts = map[string]string{
	"uz": "🔄 {who} taklifingizni qabul qildi: siz **{got}** oldingiz, **{gave}** berdingiz.\nKolleksiyangiz: /cards",
	"ru": "🔄 {who} принял ваше предложение: вы получили **{got}** и отдали **{gave}**.\nВаша коллекция: /cards",
	"en": "🔄 {who} took your offer: you got **{got}** and gave **{gave}**.\nYour collection: /cards",
}
