package webapi

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/tools"
)

// The games tool answers about Dota 2 heroes, from OpenDota, as the mlbb
// tool does for Mobile Legends, and about any game on Steam, at the prices
// Steam asks in Uzbekistan.

const (
	gamesTTL = 6 * time.Hour
	// playersTTL keeps a game's player count: it is "right now".
	playersTTL    = 10 * time.Minute
	maxGameQuery  = 40
	maxMetaHeroes = 8
)

type gamesArgs struct {
	Action string `json:"action"`
	Name   string `json:"name"`
}

func (a *apis) gamesTool() tools.Tool {
	return tools.Tool{
		Name: "games",
		Description: "steam_game: a PC game's Steam price in Uzbekistan, discount, genres, release, Metacritic score, minimum PC requirements and how many play it on Steam right now. " +
			"dota_hero: a Dota 2 hero's attribute, roles and public and pro win, pick and ban rates; dota_meta: the heroes winning most and the pros' most picked and banned. Mobile Legends is the mlbb tool's.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["steam_game", "dota_hero", "dota_meta"]},
				"name": {"type": "string", "description": "The game's or hero's English name, as \"Elden Ring\" or \"Pudge\"."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		Timeout: patientTimeout,
		Run:     a.games,
	}
}

func (a *apis) games(ctx context.Context, raw json.RawMessage) (any, error) {
	var args gamesArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	name := strings.Join(strings.Fields(args.Name), " ")
	if utf8.RuneCountInString(name) > maxGameQuery {
		return nil, argumentError("name must be at most %d characters", maxGameQuery)
	}
	switch args.Action {
	case "steam_game":
		if name == "" {
			return nil, argumentError("name the game")
		}
		return a.steamGame(ctx, name)
	case "dota_hero":
		if name == "" {
			return nil, argumentError("name the hero")
		}
		return a.dotaHero(ctx, name)
	case "dota_meta":
		return a.dotaMeta(ctx)
	}
	return nil, argumentError("action must be steam_game, dota_hero or dota_meta")
}

// --- Steam ---

// shownRequirements bounds a game's minimum PC requirements.
const shownRequirements = 400

// steamGame finds a game on Steam and gives its page, priced for
// Uzbekistan.
func (a *apis) steamGame(ctx context.Context, name string) (any, error) {
	search := a.urls.SteamStore + "/api/storesearch/?cc=uz&l=english&term=" + url.QueryEscape(name)
	var found struct {
		Items []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := a.cachedJSON(ctx, "Steam", search, gamesTTL, &found); err != nil {
		return nil, err
	}
	if len(found.Items) == 0 {
		return nil, argumentError("Steam has no game called %q; give its English name", name)
	}
	id := found.Items[0].ID
	details := fmt.Sprintf("%s/api/appdetails?appids=%d&cc=uz&l=english&filters=basic,price_overview,genres,release_date,metacritic", a.urls.SteamStore, id)
	var answer map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			Name        string `json:"name"`
			Free        bool   `json:"is_free"`
			About       string `json:"short_description"`
			Requirement struct {
				Minimum string `json:"minimum"`
			} `json:"pc_requirements"`
			Price *struct {
				Currency string `json:"currency"`
				Initial  int    `json:"initial"`
				Final    int    `json:"final"`
				Discount int    `json:"discount_percent"`
			} `json:"price_overview"`
			Genres []struct {
				Name string `json:"description"`
			} `json:"genres"`
			Release struct {
				Soon bool   `json:"coming_soon"`
				Date string `json:"date"`
			} `json:"release_date"`
			Metacritic *struct {
				Score int `json:"score"`
			} `json:"metacritic"`
		} `json:"data"`
	}
	if err := a.cachedJSON(ctx, "Steam", details, gamesTTL, &answer); err != nil {
		return nil, err
	}
	page, ok := answer[strconv.Itoa(id)]
	if !ok || !page.Success {
		return nil, fmt.Errorf("Steam sent no page for %q", found.Items[0].Name)
	}
	g := page.Data
	out := map[string]any{
		"game": cleanText(g.Name, 80), "about": cleanText(g.About, 300), "released": g.Release.Date,
		"link": fmt.Sprintf("https://store.steampowered.com/app/%d", id), "source": "Steam, prices for Uzbekistan",
	}
	var genres []string
	for _, genre := range g.Genres {
		genres = append(genres, genre.Name)
	}
	out["genres"] = genres
	switch {
	case g.Free:
		out["price"] = "free to play"
	case g.Price != nil:
		out["price"] = money(g.Price.Final, g.Price.Currency)
		if g.Price.Discount > 0 {
			out["normal_price"], out["discount"] = money(g.Price.Initial, g.Price.Currency), fmt.Sprintf("%d%%", g.Price.Discount)
		}
	case g.Release.Soon:
		out["price"] = "not out yet"
	}
	if g.Metacritic != nil {
		out["metacritic"] = g.Metacritic.Score
	}
	if minimum := cleanText(g.Requirement.Minimum, shownRequirements); minimum != "" {
		out["minimum_pc"] = minimum
	}
	if players, ok := a.playersNow(ctx, id); ok {
		out["playing_now_on_steam"] = players
	}
	return out, nil
}

// playersNow is how many play a game on Steam right now. The rest of the
// game's page stands without it, so a failure only leaves it out.
func (a *apis) playersNow(ctx context.Context, id int) (int, bool) {
	link := fmt.Sprintf("%s/ISteamUserStats/GetNumberOfCurrentPlayers/v1/?appid=%d", a.urls.SteamAPI, id)
	players, err := remember(a.cache, "steam-players:"+strconv.Itoa(id), playersTTL, func() (int, error) {
		var answer struct {
			Response struct {
				Players int `json:"player_count"`
				Result  int `json:"result"`
			} `json:"response"`
		}
		if err := a.client.getJSON(ctx, call{service: "Steam", url: link}, &answer); err != nil {
			return 0, err
		}
		if answer.Response.Result != 1 {
			return 0, fmt.Errorf("Steam has no player count for app %d", id)
		}
		return answer.Response.Players, nil
	})
	return players, err == nil
}

// --- Dota 2 (OpenDota) ---

type dotaHero struct {
	Name       string   `json:"localized_name"`
	Attribute  string   `json:"primary_attr"`
	AttackType string   `json:"attack_type"`
	Roles      []string `json:"roles"`
	PubPick    int      `json:"pub_pick"`
	PubWin     int      `json:"pub_win"`
	ProPick    int      `json:"pro_pick"`
	ProWin     int      `json:"pro_win"`
	ProBan     int      `json:"pro_ban"`
}

var dotaAttributes = map[string]string{"str": "strength", "agi": "agility", "int": "intelligence", "all": "universal"}

func (a *apis) dotaHeroes(ctx context.Context) ([]dotaHero, error) {
	link := a.urls.OpenDota + "/api/heroStats"
	return remember(a.cache, "dota:"+link, gamesTTL, func() ([]dotaHero, error) {
		var heroes []dotaHero
		err := a.patient.getJSON(ctx, call{service: "OpenDota", url: link, limit: 1 << 20}, &heroes)
		return heroes, err
	})
}

func (a *apis) dotaHero(ctx context.Context, name string) (any, error) {
	heroes, err := a.dotaHeroes(ctx)
	if err != nil {
		return nil, err
	}
	h, ok := findByName(heroes, name, func(h dotaHero) string { return h.Name })
	if !ok {
		return nil, argumentError("no Dota 2 hero is called %q; give its English name", name)
	}
	return map[string]any{
		"hero": h.Name, "attribute": cmp.Or(dotaAttributes[h.Attribute], h.Attribute), "attack": h.AttackType, "roles": h.Roles,
		"public": map[string]any{"win_rate": winRate(h.PubWin, h.PubPick), "matches": h.PubPick},
		"pro":    map[string]any{"win_rate": winRate(h.ProWin, h.ProPick), "picks": h.ProPick, "bans": h.ProBan},
		"source": "OpenDota, recent matches",
	}, nil
}

func (a *apis) dotaMeta(ctx context.Context) (any, error) {
	heroes, err := a.dotaHeroes(ctx)
	if err != nil {
		return nil, err
	}
	// A hero few people play wins or loses by chance: only those played at
	// least half as often as the average are ranked.
	total := 0
	for _, h := range heroes {
		total += h.PubPick
	}
	var played []dotaHero
	for _, h := range heroes {
		if h.PubPick > 0 && h.PubPick*2*len(heroes) >= total {
			played = append(played, h)
		}
	}
	slices.SortFunc(played, func(x, y dotaHero) int {
		return cmp.Compare(float64(y.PubWin)/float64(y.PubPick), float64(x.PubWin)/float64(x.PubPick))
	})
	var winning []map[string]any
	for _, h := range played[:min(len(played), maxMetaHeroes)] {
		winning = append(winning, map[string]any{"hero": h.Name, "win_rate": winRate(h.PubWin, h.PubPick)})
	}
	pro := slices.Clone(heroes)
	slices.SortFunc(pro, func(x, y dotaHero) int { return cmp.Compare(y.ProPick+y.ProBan, x.ProPick+x.ProBan) })
	var contested []map[string]any
	for _, h := range pro[:min(len(pro), maxMetaHeroes)] {
		contested = append(contested, map[string]any{"hero": h.Name, "picks": h.ProPick, "bans": h.ProBan})
	}
	return map[string]any{"winning_public": winning, "most_contested_pro": contested, "source": "OpenDota, recent matches"}, nil
}

// winRate is wins out of games as a percentage, "" for no games.
func winRate(wins, games int) string {
	if games == 0 {
		return ""
	}
	return fmt.Sprintf("%.1f%%", float64(wins)*100/float64(games))
}

// findByName finds the item whose name is name, ignoring case and what is
// not a letter or digit, or else the first whose name begins with it.
func findByName[T any](items []T, name string, nameOf func(T) string) (T, bool) {
	want := lettersOf(name)
	var found T
	ok := false
	for _, item := range items {
		have := lettersOf(nameOf(item))
		switch {
		case have == want:
			return item, true
		case !ok && want != "" && strings.HasPrefix(have, want):
			found, ok = item, true
		}
	}
	return found, ok
}

func lettersOf(text string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, strings.ToLower(text))
}
