package cards

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

func TestFranchiseJoinsSeasons(t *testing.T) {
	for anime, want := range map[string]string{
		"Attack on Titan Season 3": "Attack on Titan", "Fate/Zero": "Fate", "Nisemonogatari": "Monogatari",
		"Naruto: Shippuuden": "Naruto", "ONE PIECE": "ONE PIECE",
	} {
		if got := Franchise(anime); got != want {
			t.Errorf("Franchise(%q) = %q, want %q", anime, got, want)
		}
	}
}

func postAnime(t *testing.T, h http.Handler, initData, action, anime string) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"init_data": initData, "action": action, "query": anime})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(string(body))))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

// An anime of five cards or more, held whole, gives its tokens once;
// seasons count as one anime and a limited card is not needed.
func TestAnimeCollectionReward(t *testing.T) {
	now := time.Now()
	store := &fakeStore{arena: fakeArena{held: map[int64][]storage.OwnedCard{}}}
	var owned []storage.OwnedCard
	for i := range 5 {
		// Haikyu's seasons, one anime here.
		c := storage.Card{ID: int64(500 + i), Name: "H", Anime: "HAIKYU!!" + strings.Repeat(" 2nd Season", i%2), Rarity: 0}
		store.catalog = append(store.catalog, c)
		owned = append(owned, storage.OwnedCard{Card: c, Count: 1})
	}
	store.catalog = append(store.catalog, storage.Card{ID: 298761, Name: "Satoko", Anime: "HAIKYU!!", Rarity: 4}) // limited
	h := NewHandler(Options{BotToken: testToken, Store: store, Now: func() time.Time { return now }})
	init := signedInit(7, now)

	store.arena.held[7] = owned[:4]
	if out := postAnime(t, h, init, "anime_claim", "HAIKYU!!"); out["refused"] != "missing" {
		t.Fatalf("a card missing: %v", out)
	}
	store.arena.held[7] = owned
	out := postAnime(t, h, init, "anime_claim", "HAIKYU!!")
	if out["given"] != float64(AnimeRewardTokens) || out["tokens"] != float64(AnimeRewardTokens) {
		t.Fatalf("held whole, the limited card aside: %v", out)
	}
	if out := postAnime(t, h, init, "anime_claim", "HAIKYU!!"); out["refused"] != "claimed" || store.tokens != AnimeRewardTokens {
		t.Fatalf("a second claim: %v, tokens %d", out, store.tokens)
	}
	if out := postAnime(t, h, init, "anime_claim", "JJK"); out["refused"] != "small" {
		t.Fatalf("an anime of one card: %v", out)
	}

	list := postAnime(t, h, init, "animes", "")
	claimed, _ := list["claimed"].([]any)
	reward, _ := list["reward"].(map[string]any)
	if len(claimed) != 1 || claimed[0] != "HAIKYU!!" || reward["tokens"] != float64(AnimeRewardTokens) || reward["least"] != float64(AnimeRewardLeast) {
		t.Fatalf("animes = claimed %v, reward %v", claimed, reward)
	}
	first := list["cards"].([]any)[2].(map[string]any)
	if first["franchise"] != "HAIKYU!!" {
		t.Errorf("a card's franchise = %v", first["franchise"])
	}
}
