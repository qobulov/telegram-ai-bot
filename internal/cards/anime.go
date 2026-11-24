package cards

import (
	"context"
	"regexp"
	"strings"

	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/storage"
)

// An anime collection, held whole, gives AnimeRewardTokens once, for an
// anime of at least AnimeRewardLeast cards: smaller ones would pay tokens
// for a single draw (as the owners chose on 2026-11-19).
const (
	AnimeRewardTokens = 10
	AnimeRewardLeast  = 5
)

// franchises are the animes whose seasons and sequels are one anime here,
// as players know them; the app groups them the same way.
var (
	franchises = []string{"Attack on Titan", "My Hero Academia", "JUJUTSU KAISEN", "HAIKYU!!", "Demon Slayer", "Naruto",
		"JoJo's Bizarre Adventure", "The Seven Deadly Sins", "Re:ZERO", "Sword Art Online", "Assassination Classroom", "Danganronpa"}
	monogatari = regexp.MustCompile(`^(Bakemonogatari|Nisemonogatari|Monogatari)`)
)

// Franchise is the anime a card's anime counts as.
func Franchise(anime string) string {
	switch {
	case monogatari.MatchString(anime):
		return "Monogatari"
	case strings.HasPrefix(anime, "Fate/"):
		return "Fate"
	}
	for _, f := range franchises {
		if strings.HasPrefix(anime, f) {
			return f
		}
	}
	return anime
}

// AnimeCards is the IDs of anime's cards that its collection needs: the
// catalog's cards of that franchise, those only an event gives (limited or
// not) left out, as no draw gives them.
func AnimeCards(catalog []storage.Card, anime string) []int64 {
	var ids []int64
	for _, c := range catalog {
		if !storage.CardLimited(c.ID) && !storage.CardEventOnly(c.ID) && Franchise(c.Anime) == anime {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// animeClaim gives the player an anime collection's tokens, once, when
// they hold all its cards; a refusal says why: "small" (fewer than
// AnimeRewardLeast cards), "missing" (a card not held) or "claimed".
func (h *handler) animeClaim(ctx context.Context, user miniapp.User, anime string) (any, failure) {
	if err := h.catalog.Ensure(ctx); err != nil {
		h.Logger.Error("could not fill the card catalog", "error", err)
		return nil, errUnavailable
	}
	catalog, err := h.cardCatalog(ctx)
	if err != nil {
		h.Logger.Error("could not read the card catalog", "error", err)
		return nil, errUnavailable
	}
	need := AnimeCards(catalog, anime)
	if len(need) < AnimeRewardLeast {
		return map[string]any{"refused": "small"}, failure{}
	}
	held, err := h.Store.OwnedCounts(ctx, user.ID, need)
	if err != nil {
		h.Logger.Error("could not read a player's cards", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	for _, id := range need {
		if held[id] <= 0 {
			return map[string]any{"refused": "missing"}, failure{}
		}
	}
	tokens, claimed, err := h.Store.ClaimAnimeReward(ctx, user.ID, name(user), anime, AnimeRewardTokens)
	if err != nil {
		h.Logger.Error("could not give an anime reward", "user_id", user.ID, "anime", anime, "error", err)
		return nil, errUnavailable
	}
	if !claimed {
		return map[string]any{"refused": "claimed", "tokens": tokens}, failure{}
	}
	return map[string]any{"given": AnimeRewardTokens, "tokens": tokens}, failure{}
}
