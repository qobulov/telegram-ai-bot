package cards

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

const testToken = "123:test-token"

// signedInit is init data for user signed as Telegram signs it.
func signedInit(user int64, at time.Time) string {
	values := url.Values{"user": {fmt.Sprintf(`{"id":%d,"first_name":"Bek"}`, user)}, "auth_date": {fmt.Sprint(at.Unix())}}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + values.Get(k)
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(testToken))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	values.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return values.Encode()
}

// fakeStore has two cards; the player holds the first.
type fakeStore struct {
	day      time.Time
	draws    int
	shards   int
	tokens   int
	gifts    []int64 // the card, then the player, of each gift
	giftErr  error
	offers   []storage.CardOffer
	takeErr  error
	offerErr error
	pages    []storage.CatalogPage // the catalog pages asked for
}

var (
	heldCard   = storage.Card{ID: 1, Name: "Gojo", Anime: "JJK", ImageURL: "https://img/1.png", Rarity: 4, Attack: 5000, Health: 18000, Value: 5000}
	lockedCard = storage.Card{ID: 2, Name: "Levi", Anime: "AoT", ImageURL: "https://img/2.png", Rarity: 0, Value: 100}
)

func (f *fakeStore) CardCollections(context.Context) (map[string]int, error) {
	return map[string]int{"anime": CatalogSize}, nil
}
func (f *fakeStore) SaveCardCatalog(context.Context, []storage.Card) error { return nil }
func (f *fakeStore) CardCatalog(context.Context) ([]storage.Card, error) {
	return []storage.Card{heldCard, lockedCard}, nil
}
func (f *fakeStore) OwnedCards(context.Context, int64, int) ([]storage.OwnedCard, error) {
	return []storage.OwnedCard{{Card: heldCard, Count: 2}}, nil
}
func (f *fakeStore) CardPlayerOf(context.Context, int64) (storage.CardPlayer, time.Time, error) {
	return storage.CardPlayer{Points: 10000, Cards: 1, FreeDay: f.day, Tokens: f.tokens}, time.Time{}, nil
}
func (f *fakeStore) CardTop(context.Context, int) ([]storage.CardPlayer, error) {
	return []storage.CardPlayer{{UserID: 7, Name: "Bek", Points: 10000, Cards: 1}}, nil
}
func (f *fakeStore) GiveDayTokens(_ context.Context, _ int64, _ string, day storage.CardDay) (int, error) {
	if f.day.Equal(day.Start) {
		return 0, nil
	}
	f.day, f.tokens = day.Start, f.tokens+day.PerDay
	return day.PerDay, nil
}
func (f *fakeStore) OpenCards(ctx context.Context, userID int64, name string, n int, _ storage.CardRoll, cost int, day storage.CardDay, _ time.Time) (storage.CardOpening, error) {
	given, _ := f.GiveDayTokens(ctx, userID, name, day)
	tokens := min(f.tokens, n)
	if shards := (n - tokens) * cost; shards > f.shards {
		return storage.CardOpening{}, &storage.CardFundsError{Tokens: f.tokens, Shards: f.shards, Need: shards}
	} else {
		f.tokens, f.shards = f.tokens-tokens, f.shards-shards
	}
	f.draws += n
	open := storage.CardOpening{Tokens: f.tokens, Shards: f.shards, Points: 15000, Given: given}
	for range n {
		open.Draws = append(open.Draws, storage.CardDraw{Card: heldCard, Count: 3, Points: 15000})
	}
	return open, nil
}

func (f *fakeStore) CardWallet(context.Context, int64) (int, map[int]int, error) {
	return f.shards, map[int]int{0: 2, 4: 1}, nil
}
func (f *fakeStore) MeltCards(_ context.Context, _ int64, rates []int) (int, int, int, error) {
	gained := 2*rates[0] + rates[4]
	f.shards += gained
	return 3, gained, f.shards, nil
}
func (f *fakeStore) CardPlayersNamed(_ context.Context, name string, except int64, _ int) ([]storage.CardPlayer, error) {
	var found []storage.CardPlayer
	for _, p := range []storage.CardPlayer{{UserID: 7, Name: "Bek", Cards: 3}, {UserID: 8, Name: "Vali", Cards: 1}} {
		if p.UserID != except && strings.Contains(strings.ToLower(p.Name), strings.ToLower(name)) {
			found = append(found, p)
		}
	}
	return found, nil
}

func (f *fakeStore) GiveCard(_ context.Context, _, _, to, cardID int64, _ storage.CardTradeRules) error {
	if f.giftErr != nil {
		return f.giftErr
	}
	f.gifts = append(f.gifts, cardID, to)
	return nil
}

func (f *fakeStore) OfferCard(_ context.Context, from, give, want int64, rules storage.CardMarketRules) (int64, error) {
	if f.offerErr != nil {
		return 0, f.offerErr
	}
	if len(f.offers) >= rules.Open {
		return 0, &storage.CardOffersError{Limit: rules.Open}
	}
	id := int64(len(f.offers) + 1)
	f.offers = append(f.offers, storage.CardOffer{ID: id, From: from, FromName: "Bek", Give: storage.Card{ID: give, Name: "Gojo"}, Want: storage.Card{ID: want, Name: "Levi"}})
	return id, nil
}
func (f *fakeStore) CardMarket(_ context.Context, viewer int64, _ storage.CardMarketRules, _ int) ([]storage.CardOffer, error) {
	out := make([]storage.CardOffer, len(f.offers))
	for i, o := range f.offers {
		o.Mine = o.From == viewer
		out[i] = o
	}
	return out, nil
}
func (f *fakeStore) TakeCardOffer(_ context.Context, id, by int64, _ storage.CardTradeRules, _ time.Duration) (storage.CardTrade, error) {
	if f.takeErr != nil {
		return storage.CardTrade{}, f.takeErr
	}
	o := f.offers[id-1]
	f.offers = append(f.offers[:id-1], f.offers[id:]...)
	return storage.CardTrade{ID: id, From: o.From, To: by, Give: o.Give, Take: o.Want}, nil
}
func (f *fakeStore) CallOffCardTrade(_ context.Context, id, by int64) (bool, error) {
	for i, o := range f.offers {
		if o.ID == id && o.From == by {
			f.offers = append(f.offers[:i], f.offers[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}
func (f *fakeStore) CatalogCardsNamed(_ context.Context, name string, _ int) ([]storage.Card, error) {
	if strings.Contains("levi", strings.ToLower(name)) {
		return []storage.Card{lockedCard}, nil
	}
	return nil, nil
}

// CatalogCards pages through 25 Mythic cards, named "Card 1" on.
func (f *fakeStore) CatalogCards(_ context.Context, page storage.CatalogPage) ([]storage.Card, bool, error) {
	f.pages = append(f.pages, page)
	var cards []storage.Card
	for i := page.Offset; i < 25 && len(cards) < page.Limit; i++ {
		cards = append(cards, storage.Card{ID: int64(100 + i), Name: fmt.Sprint("Card ", i+1), Rarity: page.Rarity})
	}
	return cards, page.Offset+len(cards) < 25, nil
}

func post(t *testing.T, h http.Handler, initData, action string) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"init_data": initData, "action": action})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(string(body))))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// Init data not signed with the bot's token, or a day old, opens nothing.
func TestCardsAppWantsSignedInitData(t *testing.T) {
	now := time.Now()
	h := NewHandler(Options{BotToken: testToken, Store: &fakeStore{}, Now: func() time.Time { return now }})
	if code, out := post(t, h, strings.Replace(signedInit(7, now), "Bek", "Bob", 1), "state"); code != http.StatusUnauthorized || out["error"] != "forged" {
		t.Errorf("forged = %d %v", code, out)
	}
	if code, out := post(t, h, signedInit(7, now.Add(-25*time.Hour)), "state"); code != http.StatusUnauthorized || out["error"] != "stale" {
		t.Errorf("stale = %d %v", code, out)
	}
	if code, _ := post(t, h, signedInit(7, now), "steal"); code != http.StatusBadRequest {
		t.Errorf("an unknown action = %d", code)
	}
}

// The state names only the cards held: one not held is its rarity alone.
func TestCardsAppHidesCardsNotHeld(t *testing.T) {
	now := time.Now()
	store := &fakeStore{day: PeriodStart(now)}
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }})
	code, out := post(t, h, signedInit(7, now), "state")
	views, _ := out["cards"].([]any)
	if code != http.StatusOK || len(views) != 2 || out["held"] != 1.0 || out["points"] != 10000.0 || out["tokens"] != 0.0 || out["given"] != 0.0 {
		t.Fatalf("state = %d %v", code, out)
	}
	held, locked := views[0].(map[string]any), views[1].(map[string]any)
	if held["name"] != "Gojo" || held["image"] != "https://img/1.png" || held["count"] != 2.0 {
		t.Errorf("held = %v", held)
	}
	if _, named := locked["name"]; named || locked["image"] != nil || locked["rarity"] != 0.0 {
		t.Errorf("a card not held shows %v", locked)
	}
}

// Opening the app gives the period's token; a card or five open for
// tokens, then for crystals; with too few of both nothing opens.
func TestCardsAppOpensForTokensThenCrystals(t *testing.T) {
	now := time.Now()
	store := &fakeStore{tokens: 1}
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }, Roll: func() Rarity { return Mythic }})
	initData := signedInit(7, now)
	open := func(n int) map[string]any {
		body, _ := json.Marshal(map[string]any{"init_data": initData, "action": "open", "count": n})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(string(body))))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		out["code"] = float64(rec.Code)
		return out
	}
	if code, out := post(t, h, initData, "state"); code != 200 || out["tokens"] != 2.0 || out["given"] != 1.0 || out["next_token_in"] == nil || out["card_cost"] != float64(CardCost) || out["batch"] != float64(Batch) {
		t.Fatalf("state = %d %v", code, out)
	}
	if out := open(1); out["tokens"] != 1.0 || len(out["drawn"].([]any)) != 1 || out["drawn"].([]any)[0].(map[string]any)["card"].(map[string]any)["name"] != "Gojo" {
		t.Fatalf("one card = %v", out)
	}
	if out := open(Batch); out["short"] != float64((Batch-1)*CardCost) || out["drawn"] != nil {
		t.Fatalf("ten with one token and no crystals = %v", out)
	}
	store.shards = (Batch - 1) * CardCost
	if out := open(Batch); out["tokens"] != 0.0 || out["shards"] != 0.0 || len(out["drawn"].([]any)) != Batch {
		t.Fatalf("ten with one token and crystals for nine = %v", out)
	}
	if out := open(3); out["code"] != float64(http.StatusBadRequest) {
		t.Errorf("three at once = %v", out)
	}
	if store.draws != 1+Batch {
		t.Errorf("draws = %d", store.draws)
	}
	if _, out := post(t, h, initData, "top"); len(out["top"].([]any)) != 1 || out["top"].([]any)[0].(map[string]any)["me"] != true {
		t.Errorf("top = %v", out)
	}
}

// The page shows the player's shards and what their spares melt into, and
// melts them.
func TestShardsInTheMiniApp(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store := &fakeStore{}
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }})
	initData := signedInit(7, now)
	if code, out := post(t, h, initData, "state"); code != 200 || out["spare"] != 3.0 || out["spare_worth"] != 52.0 {
		t.Fatalf("state = %d %v", code, out)
	}
	if code, out := post(t, h, initData, "melt"); code != 200 || out["gained"] != 52.0 || out["shards"] != 52.0 {
		t.Fatalf("melt = %d %v", code, out)
	}
}

func TestAppLanguage(t *testing.T) {
	for code, want := range map[string]string{"": "uz", "uz": "uz", "ru": "ru", "uk-UA": "ru", "en": "en", "de": "en"} {
		if got := appLanguage(code); got != want {
			t.Errorf("appLanguage(%q) = %q, want %q", code, got, want)
		}
	}
}

// The player finds whom to give a card among the others, gives it, and the
// one given it is told; a gift refused says why.
func TestACardIsGiven(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store := &fakeStore{}
	var told []string
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now },
		Notify: func(_ context.Context, userID int64, text string) error {
			told = append(told, fmt.Sprint(userID, ": ", text))
			return nil
		}})
	initData := signedInit(7, now)
	call := func(fields map[string]any) (int, map[string]any) {
		fields["init_data"] = initData
		body, _ := json.Marshal(fields)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(string(body))))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	_, out := call(map[string]any{"action": "players", "query": ""})
	if players, _ := out["players"].([]any); len(players) != 1 || players[0].(map[string]any)["name"] != "Vali" {
		t.Fatalf("players but the giver = %v", out)
	}
	if code, out := call(map[string]any{"action": "gift", "card": 1, "to": 8}); code != 200 || out["cards"] == nil {
		t.Fatalf("gift = %d %v", code, out)
	}
	if len(store.gifts) != 2 || store.gifts[0] != 1 || store.gifts[1] != 8 {
		t.Errorf("gifts = %v", store.gifts)
	}
	if len(told) != 1 || !strings.HasPrefix(told[0], "8: 🎁") || !strings.Contains(told[0], "**Gojo**") {
		t.Errorf("told = %q", told)
	}
	if code, _ := call(map[string]any{"action": "gift", "card": 1, "to": 7}); code != http.StatusBadRequest {
		t.Errorf("a gift to oneself = %d", code)
	}
}

// A player puts a card up for one found in the catalog; another takes the
// offer and its player is told; a player calls off their own offer.
func TestTheCardMarket(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store := &fakeStore{}
	var told []string
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now },
		Notify: func(_ context.Context, userID int64, text string) error {
			told = append(told, fmt.Sprint(userID, ": ", text))
			return nil
		}})
	call := func(user int64, fields map[string]any) map[string]any {
		fields["init_data"] = signedInit(user, now)
		body, _ := json.Marshal(fields)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(string(body))))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	if cards, _ := call(7, map[string]any{"action": "catalog", "query": "lev"})["cards"].([]any); len(cards) != 1 || cards[0].(map[string]any)["name"] != "Levi" {
		t.Fatalf("catalog search = %v", cards)
	}
	if out := call(7, map[string]any{"action": "offer", "card": 1, "want": 1}); out["refused"] != "same" {
		t.Errorf("an offer of a card for itself = %v", out)
	}
	out := call(7, map[string]any{"action": "offer", "card": 1, "want": 2})
	offers, _ := out["offers"].([]any)
	if len(offers) != 1 || offers[0].(map[string]any)["mine"] != true {
		t.Fatalf("market after the offer = %v", out)
	}
	if offers, _ = call(8, map[string]any{"action": "market"})["offers"].([]any); len(offers) != 1 || offers[0].(map[string]any)["mine"] != false {
		t.Fatalf("another's market = %v", offers)
	}
	out = call(8, map[string]any{"action": "take", "offer": 1})
	if got, _ := out["got"].(map[string]any); got["name"] != "Gojo" || out["cards"] == nil {
		t.Fatalf("take = %v", out)
	}
	if len(told) != 1 || !strings.HasPrefix(told[0], "7: 🔄") || !strings.Contains(told[0], "**Levi**") {
		t.Errorf("told = %q", told)
	}
	store.takeErr = storage.ErrCardOfferGone
	if out := call(8, map[string]any{"action": "take", "offer": 1}); out["refused"] != "gone" {
		t.Errorf("an offer gone = %v", out)
	}
	call(7, map[string]any{"action": "offer", "card": 1, "want": 2})
	if out := call(7, map[string]any{"action": "unoffer", "offer": 1}); len(out["offers"].([]any)) != 0 {
		t.Errorf("market after calling off = %v", out)
	}
}

// The anime page gets every card with its name and picture, held or not.
func TestTheAnimePageSeesEveryCard(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	h := NewHandler(Options{BotToken: testToken, Store: &fakeStore{}, Now: func() time.Time { return now }})
	code, out := post(t, h, signedInit(7, now), "animes")
	cards, _ := out["cards"].([]any)
	if code != 200 || len(cards) == 0 {
		t.Fatalf("animes = %d %v", code, out)
	}
	for _, c := range cards {
		if card := c.(map[string]any); card["name"] == nil || card["image"] == nil {
			t.Errorf("a card without its name or picture: %v", card)
		}
	}
}

// The card wanted is paged through the catalog by the rarity of the card
// given, ten at a time; a limited card or cards of different rarities are
// refused with a reason the page shows.
func TestTradesAreOfOneRarityAndNoLimitedCard(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store := &fakeStore{}
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }})
	call := func(fields map[string]any) (int, map[string]any) {
		fields["init_data"] = signedInit(7, now)
		body, _ := json.Marshal(fields)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(string(body))))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	_, out := call(map[string]any{"action": "catalog", "rarity": 4, "card": 1})
	cards, _ := out["cards"].([]any)
	if len(cards) != 10 || out["more"] != true || cards[0].(map[string]any)["name"] != "Card 1" {
		t.Fatalf("first page = %v", out)
	}
	_, out = call(map[string]any{"action": "catalog", "rarity": 4, "card": 1, "offset": 20, "query": "ca"})
	if cards, _ = out["cards"].([]any); len(cards) != 5 || out["more"] != false {
		t.Errorf("last page = %v", out)
	}
	if got := store.pages[1]; got != (storage.CatalogPage{Rarity: 4, Name: "ca", Except: 1, Offset: 20, Limit: catalogPage}) {
		t.Errorf("page asked = %+v", got)
	}
	if code, _ := call(map[string]any{"action": "catalog", "rarity": 9}); code != http.StatusBadRequest {
		t.Errorf("an unknown rarity = %d", code)
	}
	store.offerErr = storage.ErrCardTradeUneven
	if _, out := call(map[string]any{"action": "offer", "card": 1, "want": 2}); out["refused"] != "uneven" {
		t.Errorf("an uneven offer = %v", out)
	}
	store.offerErr = storage.ErrCardLimited
	if _, out := call(map[string]any{"action": "offer", "card": 1, "want": 2}); out["refused"] != "limited" {
		t.Errorf("an offer of a limited card = %v", out)
	}
	store.takeErr = storage.ErrCardLimited
	if _, out := call(map[string]any{"action": "take", "offer": 1}); out["refused"] != "limited" {
		t.Errorf("taking a limited offer = %v", out)
	}
	store.giftErr = storage.ErrCardLimited
	if _, out := call(map[string]any{"action": "gift", "card": 1, "to": 8}); out["refused"] != "limited" {
		t.Errorf("a gift of a limited card = %v", out)
	}
}

// The page is told which cards are limited, held or not.
func TestTheAppKnowsLimitedCards(t *testing.T) {
	h := &handler{}
	if v := h.held(storage.Card{ID: 298761, Name: "Satoko Kirigaya"}, 1); !v.Limited {
		t.Errorf("Satoko = %+v", v)
	}
	if v := h.held(heldCard, 1); v.Limited {
		t.Errorf("Gojo = %+v", v)
	}
}
