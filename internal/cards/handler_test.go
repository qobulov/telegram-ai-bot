package cards

import (
	"cmp"
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
	last     time.Time
	draws    int
	shards   int
	auto     bool
	autoLang string
	gifts    []int64 // the card, then the player, of each gift
	giftErr  error
	offers   []storage.CardOffer
	takeErr  error
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
	return storage.CardPlayer{Points: 10000, Cards: 1, Auto: f.auto}, f.last, nil
}
func (f *fakeStore) CardTop(context.Context, int) ([]storage.CardPlayer, error) {
	return []storage.CardPlayer{{UserID: 7, Name: "Bek", Points: 10000, Cards: 1}}, nil
}
func (f *fakeStore) DrawCard(_ context.Context, _ int64, _ string, _ storage.CardRoll, every time.Duration, now time.Time) (storage.CardDraw, error) {
	if !f.last.IsZero() && now.Sub(f.last) < every {
		return storage.CardDraw{}, &storage.CardWaitError{Wait: every - now.Sub(f.last)}
	}
	f.last, f.draws = now, f.draws+1
	return storage.CardDraw{Card: heldCard, Count: 3, Points: 15000}, nil
}

func (f *fakeStore) CardWallet(context.Context, int64) (int, map[int]int, error) {
	return f.shards, map[int]int{0: 2, 4: 1}, nil
}
func (f *fakeStore) MeltCards(_ context.Context, _ int64, rates []int) (int, int, int, error) {
	gained := 2*rates[0] + rates[4]
	f.shards += gained
	return 3, gained, f.shards, nil
}
func (f *fakeStore) ExtraCard(_ context.Context, _ int64, _, collection string, _ storage.CardRoll, cost int) (storage.CardDraw, error) {
	if f.shards < cost {
		return storage.CardDraw{}, &storage.ShardsError{Have: f.shards, Need: cost}
	}
	f.shards -= cost
	card := heldCard
	card.Collection = cmp.Or(collection, card.Collection)
	return storage.CardDraw{Card: card, Count: 4, Points: 20000, Shards: f.shards}, nil
}

func (f *fakeStore) SetCardAuto(_ context.Context, _ int64, _ string, on bool, language string) error {
	f.auto, f.autoLang = on, language
	return nil
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
	store := &fakeStore{last: now.Add(-20 * time.Minute)}
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }})
	code, out := post(t, h, signedInit(7, now), "state")
	views, _ := out["cards"].([]any)
	if code != http.StatusOK || len(views) != 2 || out["held"] != 1.0 || out["points"] != 10000.0 || out["next_in"] != float64((40*time.Minute).Milliseconds()) {
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

// A player draws once an hour, an owner like anyone.
func TestCardsAppDrawsOnceAnHour(t *testing.T) {
	now := time.Now()
	store := &fakeStore{}
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }, Roll: func() Rarity { return Mythic }})
	code, out := post(t, h, signedInit(7, now), "draw")
	card, _ := out["card"].(map[string]any)
	if code != http.StatusOK || card["name"] != "Gojo" || out["points"] != 15000.0 || out["next_in"] != float64(Every.Milliseconds()) {
		t.Fatalf("draw = %d %v", code, out)
	}
	if _, out := post(t, h, signedInit(7, now), "draw"); out["wait"] != float64(Every.Milliseconds()) {
		t.Errorf("a second draw = %v", out)
	}
	if store.draws != 1 {
		t.Errorf("draws = %d", store.draws)
	}
	if _, out := post(t, h, signedInit(7, now), "top"); len(out["top"].([]any)) != 1 || out["top"].([]any)[0].(map[string]any)["me"] != true {
		t.Errorf("top = %v", out)
	}
}

// The page shows the player's shards and what their spares melt into, melts
// them, and draws a card out of turn for shards, saying how few when it
// cannot.
func TestShardsInTheMiniApp(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store := &fakeStore{}
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }})
	initData := signedInit(7, now)
	if code, out := post(t, h, initData, "state"); code != 200 || out["spare"] != 3.0 || out["spare_worth"] != 52.0 || out["extra_cost"] != float64(ExtraCost) {
		t.Fatalf("state = %d %v", code, out)
	}
	if code, out := post(t, h, initData, "extra"); code != 200 || out["need"] != float64(ExtraCost) || out["have"] != 0.0 {
		t.Fatalf("an extra card for no shards = %d %v", code, out)
	}
	if code, out := post(t, h, initData, "melt"); code != 200 || out["gained"] != 52.0 || out["shards"] != 52.0 {
		t.Fatalf("melt = %d %v", code, out)
	}
	if code, out := post(t, h, initData, "extra"); code != 200 || out["shards"] != float64(52-ExtraCost) || out["card"] == nil {
		t.Errorf("an extra card = %d %v", code, out)
	}
	buy := func(set string) (int, map[string]any) {
		body, _ := json.Marshal(map[string]any{"init_data": initData, "action": "extra", "set": set})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(string(body))))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	code, out := buy("anime")
	card, _ := out["card"].(map[string]any)
	if code != 200 || out["shards"] != float64(52-ExtraCost-SetCost) || card["set"] != "anime" {
		t.Errorf("an anime card from the shop = %d %v", code, out)
	}
	if code, _ := buy("pokemon"); code != http.StatusBadRequest {
		t.Errorf("a collection there is none of = %d", code)
	}
}

// The Mini App's switch turns the auto opener on and off, in the player's
// language, and the state shows where it is.
func TestTheAutoOpenerSwitch(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store := &fakeStore{}
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }})
	initData := signedInit(7, now)
	turn := func(on bool) (int, map[string]any) {
		body, _ := json.Marshal(map[string]any{"init_data": initData, "action": "auto", "on": on})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(string(body))))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if _, out := post(t, h, initData, "state"); out["auto"] != false {
		t.Fatalf("state = %v", out)
	}
	if code, out := turn(true); code != 200 || out["auto"] != true || !store.auto || store.autoLang != "uz" {
		t.Fatalf("on = %d %v, stored %v %q", code, out, store.auto, store.autoLang)
	}
	if _, out := post(t, h, initData, "state"); out["auto"] != true {
		t.Errorf("state after on = %v", out)
	}
	if code, out := turn(false); code != 200 || out["auto"] != false || store.auto {
		t.Errorf("off = %d %v", code, out)
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
	store.giftErr = &storage.CardGiveLimitError{UserID: 7}
	if _, out := call(map[string]any{"action": "gift", "card": 1, "to": 8}); out["refused"] != "limit" {
		t.Errorf("at the limit = %v", out)
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
