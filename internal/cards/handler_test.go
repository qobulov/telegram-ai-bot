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
	last   time.Time
	draws  int
	images map[int64]string
}

var (
	heldCard   = storage.Card{ID: 1, Name: "Gojo", Anime: "JJK", ImageURL: "https://img/1.png", Rarity: 4, Attack: 5000, Health: 18000, Value: 5000}
	lockedCard = storage.Card{ID: 2, Name: "Levi", Anime: "AoT", ImageURL: "https://img/2.png", Rarity: 0, Value: 100}
)

func (f *fakeStore) CardCollections(context.Context) (map[string]int, error) {
	return map[string]int{"anime": CatalogSize}, nil
}
func (f *fakeStore) CardImage(_ context.Context, id int64) (string, error) { return f.images[id], nil }
func (f *fakeStore) SaveCardCatalog(context.Context, []storage.Card) error { return nil }
func (f *fakeStore) CardCatalog(context.Context) ([]storage.Card, error) {
	return []storage.Card{heldCard, lockedCard}, nil
}
func (f *fakeStore) OwnedCards(context.Context, int64, int) ([]storage.OwnedCard, error) {
	return []storage.OwnedCard{{Card: heldCard, Count: 2}}, nil
}
func (f *fakeStore) CardPlayerOf(context.Context, int64) (storage.CardPlayer, time.Time, error) {
	return storage.CardPlayer{Points: 10000, Cards: 1}, f.last, nil
}
func (f *fakeStore) CardTop(context.Context, int) ([]storage.CardPlayer, error) {
	return []storage.CardPlayer{{UserID: 7, Name: "Bek", Points: 10000, Cards: 1}}, nil
}
func (f *fakeStore) DrawCard(_ context.Context, _ int64, _ string, _ int, every time.Duration, now time.Time) (storage.CardDraw, error) {
	if !f.last.IsZero() && now.Sub(f.last) < every {
		return storage.CardDraw{}, &storage.CardWaitError{Wait: every - now.Sub(f.last)}
	}
	f.last, f.draws = now, f.draws+1
	return storage.CardDraw{Card: heldCard, Count: 3, Points: 15000}, nil
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

// A picture that wants the key is fetched by the server, which alone holds
// it, and kept by the CDN; any other, or a card there is not, is not served.
func TestCardsAppServesPicturesThatWantTheKey(t *testing.T) {
	var key string
	wallet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("JPEG"))
	}))
	defer wallet.Close()
	store := &fakeStore{images: map[int64]string{-5: wallet.URL + "/images/pk_1?size=low", 1: "https://s4.anilist.co/x.png"}}
	h := NewHandler(Options{BotToken: testToken, Store: store, PokeWallet: wallet.URL, PokeWalletKey: "secret"})
	get := func(id string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/cards?image="+id, nil))
		return rec
	}
	rec := get("-5")
	if rec.Code != http.StatusOK || rec.Body.String() != "JPEG" || key != "secret" || !strings.Contains(rec.Header().Get("Cache-Control"), "s-maxage") {
		t.Errorf("picture = %d %q, key %q, cache %q", rec.Code, rec.Body, key, rec.Header().Get("Cache-Control"))
	}
	for _, id := range []string{"1", "99"} {
		if rec := get(id); rec.Code != http.StatusNotFound {
			t.Errorf("card %s = %d", id, rec.Code)
		}
	}
}

// A set gives the cards of known rarities up to its quota, One Piece's flat
// shape too, without their numbers in their names; anything else is left out.
func TestFetchSet(t *testing.T) {
	var key string
	wallet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("X-API-Key")
		if strings.HasPrefix(r.URL.Path, "/op/") {
			_, _ = w.Write([]byte(`{"set":{"name":"Romance Dawn"},"data":[{"id":"op_1","name":"Luffy","rarity":"L","product_type":"card"},{"id":"op_2","name":"DON!!","rarity":"DON!!","product_type":"card"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"set":{"name":"151"},"cards":[{"id":"pk_1","card_info":{"name":"Blastoise ex - 200/165","rarity":"Special Illustration Rare","product_type":"card"}},{"id":"pk_2","card_info":{"name":"Box","rarity":"Rare","product_type":"sealed"}}]}`))
	}))
	defer wallet.Close()

	cards, err := fetchSet(context.Background(), wallet.Client(), wallet.URL, "secret", tradingSet{"pokemon", "Pokémon", "", "", "1"})
	if err != nil || len(cards) != 1 || key != "secret" {
		t.Fatalf("cards = %+v, %v, key %q", cards, err, key)
	}
	c := cards[0]
	if c.Name != "Blastoise ex" || c.Rarity != int(Mythic) || c.Collection != "pokemon" || c.Anime != "Pokémon · 151" || c.ID >= 0 || c.ImageURL != wallet.URL+"/images/pk_1?size=low" {
		t.Errorf("card = %+v", c)
	}
	cards, err = fetchSet(context.Background(), wallet.Client(), wallet.URL, "secret", tradingSet{"onepiece", "One Piece", "/op", "", "2"})
	if err != nil || len(cards) != 1 || cards[0].Name != "Luffy" || cards[0].Rarity != int(Legendary) || cards[0].ImageURL != wallet.URL+"/images/op_1?size=low" {
		t.Errorf("One Piece = %+v, %v", cards, err)
	}
}

// Trading cards that cannot be had leave the anime cards to play with, and
// are not asked for again at each draw.
func TestCatalogGoesOnWithoutTradingCards(t *testing.T) {
	var asked int
	wallet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer wallet.Close()
	catalog := &Catalog{Store: &fakeStore{}, Client: wallet.Client(), PokeWallet: wallet.URL, Key: "secret"}
	for range 3 {
		if err := catalog.Ensure(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if asked != len(tradingSets) {
		t.Errorf("asked %d times; want each set once", asked)
	}
}

// A card's number is dropped wherever it stands in its name.
func TestTradingName(t *testing.T) {
	for name, want := range map[string]string{
		"Blastoise ex - 200/165":                     "Blastoise ex",
		"Trunks : Future - FB01-036 (Alternate Art)": "Trunks : Future (Alternate Art)",
		"Roronoa Zoro (001)":                         "Roronoa Zoro (001)",
		"Mew - the Psychic":                          "Mew - the Psychic",
	} {
		if got := tradingName.ReplaceAllString(name, ""); got != want {
			t.Errorf("%q = %q; want %q", name, got, want)
		}
	}
}
