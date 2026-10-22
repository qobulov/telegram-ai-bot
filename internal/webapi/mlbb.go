package webapi

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"time"

	"telegram-ai-bot/internal/tools"
)

const (
	mlbbSource = "Rone Arena API, a fan-made mirror of Moonton's MLBB Academy statistics"
	mlbbNote   = "ranked games of the last 7 days; rates change with every patch"
	// mlbbTTL keeps statistics that Moonton refreshes a few times a day;
	// the list of heroes changes only when a hero is released.
	mlbbTTL        = 3 * time.Hour
	mlbbHeroesTTL  = 24 * time.Hour
	mlbbDays       = "7"
	mlbbMetaHeroes = 10
	// mlbbAllHeroes is more than the game has, so one page holds them all.
	mlbbAllHeroes = 300
	maxHeroName   = 40
)

var (
	mlbbRanks = []string{"all", "epic", "legend", "mythic", "honor", "glory"}
	mlbbSorts = []string{"win_rate", "ban_rate", "pick_rate"}
)

type mlbbArgs struct {
	Action string `json:"action"`
	Hero   string `json:"hero"`
	Rank   string `json:"rank"`
	Sort   string `json:"sort"`
}

type mlbbRates struct {
	Hero     string `json:"hero"`
	WinRate  string `json:"win_rate"`
	PickRate string `json:"pick_rate"`
	BanRate  string `json:"ban_rate"`
}

// mlbbMatchup is another hero and how the asked hero's win rate changes
// when that hero is an enemy or a teammate.
type mlbbMatchup struct {
	Hero          string `json:"hero"`
	WinRateChange string `json:"win_rate_change"`
}

type mlbbMetaOutput struct {
	Rank     string      `json:"rank"`
	SortedBy string      `json:"sorted_by"`
	Heroes   []mlbbRates `json:"heroes"`
	Source   string      `json:"source"`
	Note     string      `json:"note"`
}

type mlbbHeroOutput struct {
	mlbbRates
	Rank           string        `json:"rank"`
	StrongAgainst  []mlbbMatchup `json:"strong_against"`
	CounteredBy    []mlbbMatchup `json:"countered_by"`
	BestTeammates  []mlbbMatchup `json:"best_teammates"`
	WorstTeammates []mlbbMatchup `json:"worst_teammates"`
	Source         string        `json:"source"`
	Note           string        `json:"note"`
}

func (a *apis) mlbbTool() tools.Tool {
	return tools.Tool{
		Name:        "mlbb",
		Description: "Mobile Legends: Bang Bang hero statistics from ranked games of the last 7 days. action meta: the top heroes by win, ban or pick rate. action hero: one hero's win, pick and ban rates, the heroes it beats, the heroes that counter it, and its best and worst teammates. Use it for questions about the MLBB meta, counters, picks and teammates; it has no player profiles, builds or items.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["meta", "hero"]},
				"hero": {"type": "string", "description": "For hero: the hero's English name, such as Ling, Fanny or Yi Sun-shin."},
				"rank": {"type": "string", "enum": ["all", "epic", "legend", "mythic", "honor", "glory"], "description": "Rank of the games counted. Default all; honor and glory are Mythical Honor and Mythical Glory."},
				"sort": {"type": "string", "enum": ["win_rate", "ban_rate", "pick_rate"], "description": "For meta: what the heroes are ranked by. Default win_rate."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		Run: a.mlbb,
	}
}

func (a *apis) mlbb(ctx context.Context, raw json.RawMessage) (any, error) {
	var args mlbbArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	rank := cmp.Or(args.Rank, "all")
	if !slices.Contains(mlbbRanks, rank) {
		return nil, argumentError("rank must be one of %v", mlbbRanks)
	}
	switch args.Action {
	case "meta":
		sort := cmp.Or(args.Sort, "win_rate")
		if !slices.Contains(mlbbSorts, sort) {
			return nil, argumentError("sort must be one of %v", mlbbSorts)
		}
		return a.mlbbMeta(ctx, rank, sort)
	case "hero":
		return a.mlbbHero(ctx, args.Hero, rank)
	default:
		return nil, argumentError("action must be meta or hero")
	}
}

// arenaRecords is how the API wraps every list.
type arenaRecords[T any] struct {
	Data struct {
		Records []struct {
			Data T `json:"data"`
		} `json:"records"`
	} `json:"data"`
}

type arenaName struct {
	Data struct {
		Name string `json:"name"`
	} `json:"data"`
}

type arenaHero struct {
	ID   int       `json:"hero_id"`
	Hero arenaName `json:"hero"`
}

// arenaStats is a hero's rates, and for counters and compatibility the
// heroes that raise (sub_hero) and lower (sub_hero_last) its win rate most.
type arenaStats struct {
	Hero     arenaName  `json:"main_hero"`
	WinRate  float64    `json:"main_hero_win_rate"`
	PickRate float64    `json:"main_hero_appearance_rate"`
	BanRate  float64    `json:"main_hero_ban_rate"`
	Raising  []arenaSub `json:"sub_hero"`
	Lowering []arenaSub `json:"sub_hero_last"`
}

type arenaSub struct {
	ID     int     `json:"heroid"`
	Change float64 `json:"increase_win_rate"`
}

func (s arenaStats) rates() mlbbRates {
	return mlbbRates{Hero: s.Hero.Data.Name, WinRate: percent(s.WinRate), PickRate: percent(s.PickRate), BanRate: percent(s.BanRate)}
}

func (a *apis) mlbbMeta(ctx context.Context, rank, sort string) (mlbbMetaOutput, error) {
	stats, err := remember(a.cache, "mlbb:meta:"+rank+":"+sort, mlbbTTL, func() ([]arenaStats, error) {
		query := url.Values{"days": {mlbbDays}, "rank": {rank}, "sort_field": {sort}, "sort_order": {"desc"}, "size": {strconv.Itoa(mlbbMetaHeroes)}}
		return arenaList[arenaStats](ctx, a, "/api/heroes/rank?"+query.Encode())
	})
	if err != nil {
		return mlbbMetaOutput{}, err
	}
	out := mlbbMetaOutput{Rank: rank, SortedBy: sort, Source: mlbbSource, Note: mlbbNote}
	for _, s := range stats {
		out.Heroes = append(out.Heroes, s.rates())
	}
	return out, nil
}

func (a *apis) mlbbHero(ctx context.Context, name, rank string) (mlbbHeroOutput, error) {
	names, err := a.mlbbHeroes(ctx)
	if err != nil {
		return mlbbHeroOutput{}, err
	}
	id, err := findHero(names, name)
	if err != nil {
		return mlbbHeroOutput{}, err
	}
	var enemies, allies arenaStats
	var enemiesErr, alliesErr error
	var wg sync.WaitGroup
	wg.Go(func() { enemies, enemiesErr = a.arenaMatchups(ctx, id, "counters", rank) })
	wg.Go(func() { allies, alliesErr = a.arenaMatchups(ctx, id, "compatibility", rank) })
	wg.Wait()
	if err := cmp.Or(enemiesErr, alliesErr); err != nil {
		return mlbbHeroOutput{}, err
	}
	return mlbbHeroOutput{
		mlbbRates:      enemies.rates(),
		Rank:           rank,
		StrongAgainst:  matchups(names, enemies.Raising),
		CounteredBy:    matchups(names, enemies.Lowering),
		BestTeammates:  matchups(names, allies.Raising),
		WorstTeammates: matchups(names, allies.Lowering),
		Source:         mlbbSource,
		Note:           mlbbNote,
	}, nil
}

// mlbbHeroes maps every hero's ID to its English name.
func (a *apis) mlbbHeroes(ctx context.Context) (map[int]string, error) {
	return remember(a.cache, "mlbb:heroes", mlbbHeroesTTL, func() (map[int]string, error) {
		heroes, err := arenaList[arenaHero](ctx, a, "/api/heroes?size="+strconv.Itoa(mlbbAllHeroes))
		if err != nil {
			return nil, err
		}
		names := make(map[int]string, len(heroes))
		for _, h := range heroes {
			names[h.ID] = h.Hero.Data.Name
		}
		return names, nil
	})
}

// findHero returns the ID of the hero called name, however it is spaced,
// hyphenated or capitalised: "yi sun shin" finds Yi Sun-shin.
func findHero(names map[int]string, name string) (int, error) {
	if name == "" || len([]rune(name)) > maxHeroName {
		return 0, argumentError("hero must be a name of at most %d characters", maxHeroName)
	}
	wanted := normalizeName(name)
	for id, hero := range names {
		if normalizeName(hero) == wanted {
			return id, nil
		}
	}
	return 0, argumentError("no MLBB hero is called %q; give the hero's English name, such as Ling or Yi Sun-shin", name)
}

func (a *apis) arenaMatchups(ctx context.Context, id int, kind, rank string) (arenaStats, error) {
	key := fmt.Sprintf("mlbb:%s:%d:%s", kind, id, rank)
	return remember(a.cache, key, mlbbTTL, func() (arenaStats, error) {
		query := url.Values{"days": {mlbbDays}, "rank": {rank}}
		stats, err := arenaList[arenaStats](ctx, a, fmt.Sprintf("/api/heroes/%d/%s?%s", id, kind, query.Encode()))
		if err != nil {
			return arenaStats{}, err
		}
		if len(stats) == 0 {
			return arenaStats{}, fmt.Errorf("%s has no statistics for this hero yet", arenaService)
		}
		return stats[0], nil
	})
}

const arenaService = "Rone Arena API"

// arenaList fetches one of the API's lists. It is a function, not a
// method, because Go methods cannot take type parameters.
func arenaList[T any](ctx context.Context, a *apis, path string) ([]T, error) {
	var answer arenaRecords[T]
	if err := a.client.getJSON(ctx, call{service: arenaService, url: a.urls.RoneArena + path, limit: largeBodyLimit}, &answer); err != nil {
		return nil, err
	}
	list := make([]T, 0, len(answer.Data.Records))
	for _, r := range answer.Data.Records {
		list = append(list, r.Data)
	}
	return list, nil
}

func matchups(names map[int]string, subs []arenaSub) []mlbbMatchup {
	var out []mlbbMatchup
	for _, s := range subs {
		if name, ok := names[s.ID]; ok {
			out = append(out, mlbbMatchup{Hero: name, WinRateChange: fmt.Sprintf("%+.1f%%", s.Change*100)})
		}
	}
	return out
}

func percent(rate float64) string {
	return fmt.Sprintf("%.1f%%", rate*100)
}
