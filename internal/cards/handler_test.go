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
	"slices"
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
	offers   []storage.CardOffer
	takeErr  error
	offerErr error
	pages    []storage.CatalogPage // the catalog pages asked for
	stars    int                   // the held card's stars; 0 is ★1
	fuseErr  error
	resetErr error
	// weeksPaid are the arena weeks paid, by their start.
	weeksPaid     []time.Time
	arena         fakeArena
	tower         fakeTower
	boss          []fakeBossFight
	bossWeeksPaid []time.Time
	quests        fakeQuests
	catalog       []storage.Card  // cards beyond heldCard and lockedCard
	animes        map[string]bool // the anime rewards claimed
	boxes         map[string]int
	mail          []storage.CardMail
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
	return append([]storage.Card{heldCard, lockedCard}, f.catalog...), nil
}
func (f *fakeStore) CardCatalogStamp(context.Context) (string, error) {
	return fmt.Sprint(len(f.catalog)), nil
}
func (f *fakeStore) CardInbox(context.Context, int64) (int, int, error) {
	boxes, unclaimed := 0, 0
	for _, n := range f.boxes {
		boxes += n
	}
	for _, m := range f.mail {
		if m.ClaimedAt == nil {
			unclaimed++
		}
	}
	return boxes, unclaimed, nil
}
func (f *fakeStore) CardBoxes(context.Context, int64) (map[string]int, error) { return f.boxes, nil }
func (f *fakeStore) CardMailbox(context.Context, int64, int) ([]storage.CardMail, error) {
	return f.mail, nil
}
func (f *fakeStore) ClaimCardMail(_ context.Context, userID int64, _ string, id int64) (storage.CardMail, storage.CardOpening, bool, error) {
	for i, m := range f.mail {
		if m.ID != id || m.UserID != userID || m.ClaimedAt != nil {
			continue
		}
		now := time.Now()
		f.mail[i].ClaimedAt = &now
		if f.boxes == nil {
			f.boxes = map[string]int{}
		}
		f.boxes[m.Box] += m.Boxes
		f.tokens += m.Tokens
		f.shards += m.Crystals
		return m, storage.CardOpening{Tokens: f.tokens, Shards: f.shards}, true, nil
	}
	return storage.CardMail{}, storage.CardOpening{}, false, nil
}
func (f *fakeStore) OpenCardBox(_ context.Context, _ int64, _, box string, crystals int) (int, int, error) {
	if f.boxes[box] <= 0 {
		return 0, 0, storage.ErrNoCardBox
	}
	f.boxes[box]--
	f.shards += crystals
	return f.shards, f.boxes[box], nil
}
func (f *fakeStore) AnimeRewardsClaimed(context.Context, int64) ([]string, error) {
	var claimed []string
	for anime := range f.animes {
		claimed = append(claimed, anime)
	}
	return claimed, nil
}
func (f *fakeStore) ClaimAnimeReward(_ context.Context, _ int64, _, anime string, tokens int) (int, bool, error) {
	if f.animes[anime] {
		return f.tokens, false, nil
	}
	if f.animes == nil {
		f.animes = map[string]bool{}
	}
	f.animes[anime], f.tokens = true, f.tokens+tokens
	return f.tokens, true, nil
}
func (f *fakeStore) OwnedCopies(ctx context.Context, userID int64) ([]storage.OwnedCopy, error) {
	all, err := f.OwnedCards(ctx, userID, -1)
	copies := make([]storage.OwnedCopy, len(all))
	for i, c := range all {
		copies[i] = storage.OwnedCopy{CardID: c.ID, Count: c.Count, Melted: c.Melted, Stars: c.Stars, Serial: c.Serial}
	}
	return copies, err
}
func (f *fakeStore) OwnedCardsOf(ctx context.Context, userID int64, ids []int64) ([]storage.OwnedCard, error) {
	all, err := f.OwnedCards(ctx, userID, -1)
	var some []storage.OwnedCard
	for _, c := range all {
		if slices.Contains(ids, c.ID) {
			some = append(some, c)
		}
	}
	return some, err
}
func (f *fakeStore) OwnedCards(_ context.Context, userID int64, _ int) ([]storage.OwnedCard, error) {
	if held, ok := f.arena.held[userID]; ok {
		return held, nil
	}
	return []storage.OwnedCard{{Card: heldCard, Count: 2, Stars: max(f.stars, 1)}}, nil
}
func (f *fakeStore) FuseCard(_ context.Context, userID, cardID int64, cost []int, quest storage.CardQuest) (int, error) {
	if f.fuseErr != nil {
		return 0, f.fuseErr
	}
	if cardID != heldCard.ID {
		return 0, storage.ErrCardNotHeld
	}
	f.stars = max(f.stars, 1) + 1
	f.count(userID, quest, 1)
	return f.stars, nil
}
func (f *fakeStore) PayArenaWeek(_ context.Context, since, _ time.Time, _ []storage.ArenaPrize, _ func(int) string) (int, bool, error) {
	if slices.ContainsFunc(f.weeksPaid, since.Equal) {
		return 0, false, nil
	}
	f.weeksPaid = append(f.weeksPaid, since)
	return 1, true, nil
}
func (f *fakeStore) ResetCardStars(_ context.Context, _, cardID int64, _ []int) (int, error) {
	if f.resetErr != nil {
		return 0, f.resetErr
	}
	if cardID != heldCard.ID {
		return 0, storage.ErrCardNotHeld
	}
	f.stars = 1
	return 2, nil
}
func (f *fakeStore) CardPlayerOf(context.Context, int64) (storage.CardPlayer, time.Time, error) {
	return storage.CardPlayer{Points: 10000, Cards: 1, FreeDay: f.day, Tokens: f.tokens}, time.Time{}, nil
}
func (f *fakeStore) CardPlace(_ context.Context, userID int64) (storage.CardPlayer, int, int, error) {
	return storage.CardPlayer{UserID: userID, Name: "Bek", Points: 100, Cards: 2}, 150, 400, nil
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
func (f *fakeStore) OpenCards(ctx context.Context, userID int64, name string, n int, _ storage.CardRoll, cost int, day storage.CardDay, _ time.Time, quest storage.CardQuest) (storage.CardOpening, error) {
	given, _ := f.GiveDayTokens(ctx, userID, name, day)
	tokens := min(f.tokens, n)
	if shards := (n - tokens) * cost; shards > f.shards {
		return storage.CardOpening{}, &storage.CardFundsError{Tokens: f.tokens, Shards: f.shards, Need: shards}
	} else {
		f.tokens, f.shards = f.tokens-tokens, f.shards-shards
	}
	f.draws += n
	f.count(userID, quest, n)
	open := storage.CardOpening{Tokens: f.tokens, Shards: f.shards, Points: 15000, Given: given}
	for range n {
		open.Draws = append(open.Draws, storage.CardDraw{Card: heldCard, Count: 3, Points: 15000, Serial: 123})
	}
	return open, nil
}

func (f *fakeStore) CardWallet(context.Context, int64) (int, error) { return f.shards, nil }
func (f *fakeStore) OfferCard(_ context.Context, from, give, want int64, rules storage.CardMarketRules) (int64, error) {
	if f.offerErr != nil {
		return 0, f.offerErr
	}
	if rules.Open > 0 && len(f.offers) >= rules.Open {
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
func (f *fakeStore) TakeCardOffer(_ context.Context, id, by int64, _ storage.CardTradeRules, _ time.Duration, quest storage.CardQuest) (storage.CardTrade, error) {
	if f.takeErr != nil {
		return storage.CardTrade{}, f.takeErr
	}
	o := f.offers[id-1]
	f.offers = append(f.offers[:id-1], f.offers[id:]...)
	f.count(by, quest, 1)
	f.count(o.From, quest, 1)
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
	if code, out := post(t, h, initData, "state"); code != 200 || out["tokens"] != 2.0 || out["given"] != 1.0 || out["next_token_in"] == nil || out["card_cost"] != float64(CardCost) || out["batch"] != float64(Batch) || out["big_batch"] != float64(BigBatch) {
		t.Fatalf("state = %d %v", code, out)
	}
	if out := open(1); out["tokens"] != 1.0 || len(out["drawn"].([]any)) != 1 || out["drawn"].([]any)[0].(map[string]any)["card"].(map[string]any)["name"] != "Gojo" || out["drawn"].([]any)[0].(map[string]any)["serial"] != 123.0 {
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
	store.shards = BigBatch * CardCost
	if out := open(BigBatch); out["shards"] != 0.0 || len(out["drawn"].([]any)) != BigBatch {
		t.Fatalf("fifty with crystals for fifty = %v", out)
	}
	if store.draws != 1+Batch+BigBatch {
		t.Errorf("draws = %d", store.draws)
	}
	if _, out := post(t, h, initData, "top"); len(out["top"].([]any)) != 1 || out["top"].([]any)[0].(map[string]any)["me"] != true {
		t.Errorf("top = %v", out)
	}
	if _, out := post(t, h, initData, "top"); out["me"].(map[string]any)["place"] != 150.0 || out["me"].(map[string]any)["total"] != 400.0 {
		t.Errorf("my place = %v", out["me"])
	}
}

// The page shows the player's crystals, and melting spare copies into them
// is gone: copies are used up by fusion alone.
func TestMeltIsGone(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	store := &fakeStore{shards: 5}
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }})
	initData := signedInit(7, now)
	if code, out := post(t, h, initData, "state"); code != 200 || out["shards"] != 5.0 {
		t.Fatalf("state = %d %v", code, out)
	}
	if code, _ := post(t, h, initData, "melt"); code != http.StatusBadRequest {
		t.Errorf("melt = %d, want 400", code)
	}
	if store.shards != 5 {
		t.Errorf("shards after a melt = %d, want 5", store.shards)
	}
}

func TestAppLanguage(t *testing.T) {
	for code, want := range map[string]string{"": "uz", "uz": "uz", "ru": "ru", "uk-UA": "ru", "en": "en", "de": "en"} {
		if got := appLanguage(code); got != want {
			t.Errorf("appLanguage(%q) = %q, want %q", code, got, want)
		}
	}
}

// Cards change hands on the market alone: gifting and the list of players to
// give to are gone.
func TestGiftIsGone(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	h := NewHandler(Options{BotToken: testToken, Store: &fakeStore{}, Now: func() time.Time { return now }})
	initData := signedInit(7, now)
	for _, action := range []string{"gift", "players"} {
		body, _ := json.Marshal(map[string]any{"init_data": initData, "action": action, "card": 1, "to": 8})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(string(body))))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("action %q = %d, want 400", action, rec.Code)
		}
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

// A card held shows its stars, its free copies, what the next star costs
// and its numbers then; "fuse" raises it and gives the state, or says why
// not.
func TestTheAppFuses(t *testing.T) {
	now := time.Now()
	store := &fakeStore{day: PeriodStart(now)}
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
	gojo := func(out map[string]any) map[string]any {
		views, _ := out["cards"].([]any)
		if len(views) == 0 {
			t.Fatalf("no cards in %v", out)
		}
		return views[0].(map[string]any)
	}
	_, out := call(map[string]any{"action": "state"})
	if g := gojo(out); g["stars"] != 1.0 || g["free"] != 1.0 || g["fuse"] != 1.0 || g["attack"] != 5000.0 || g["next_attack"] != 6000.0 || g["next_health"] != 21600.0 {
		t.Errorf("Gojo at ★1 = %v", g)
	}
	code, out := call(map[string]any{"action": "fuse", "card": heldCard.ID})
	if g := gojo(out); code != http.StatusOK || out["fused"] != 2.0 || g["stars"] != 2.0 || g["attack"] != 6000.0 || g["health"] != 21600.0 {
		t.Errorf("fused = %d %v", code, out)
	}
	if code, _ := call(map[string]any{"action": "fuse"}); code != http.StatusBadRequest {
		t.Errorf("a fusion of no card = %d", code)
	}
	for err, refused := range map[error]string{storage.ErrCardMaxStars: "max", storage.ErrCardFewCopies: "few", storage.ErrCardNotHeld: "not_held"} {
		store.fuseErr = err
		if _, out := call(map[string]any{"action": "fuse", "card": heldCard.ID}); out["refused"] != refused {
			t.Errorf("%v = %v", err, out)
		}
	}
	store.fuseErr = nil
	if code, out := call(map[string]any{"action": "reset_stars", "card": heldCard.ID}); code != http.StatusOK || out["reset"] != 2.0 || gojo(out)["stars"] != 1.0 {
		t.Errorf("reset = %d %v", code, out)
	}
	for err, refused := range map[error]string{storage.ErrCardNotFused: "not_fused", storage.ErrCardNotHeld: "not_held"} {
		store.resetErr = err
		if _, out := call(map[string]any{"action": "reset_stars", "card": heldCard.ID}); out["refused"] != refused {
			t.Errorf("a reset refused by %v = %v", err, out)
		}
	}
	store.resetErr = nil
	store.offerErr, store.takeErr = storage.ErrCardFused, storage.ErrCardFused
	if _, out := call(map[string]any{"action": "offer", "card": 1, "want": 2}); out["refused"] != "fused" {
		t.Errorf("an offer of a fused last copy = %v", out)
	}
	if _, out := call(map[string]any{"action": "take", "offer": 1}); out["refused"] != "fused" {
		t.Errorf("taking a fused last copy = %v", out)
	}
}
