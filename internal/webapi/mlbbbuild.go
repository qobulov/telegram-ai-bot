package webapi

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const buildNote = "The builds top players of this rank use on this hero in this lane, from the game's academy, most used first: " +
	"spell, emblem with its three talents and core items. To show the items, send their cards with mlbb_item_card."

// mlbbLanes are the lanes the academy keeps builds for.
var mlbbLanes = []string{"gold", "exp", "mid", "jungle", "roam"}

// laneWords find a lane in the academy's lane names, such as "Jungling",
// "Gold Lane" or "Roaming".
var laneWords = []struct{ word, lane string }{
	{"gold", "gold"}, {"exp", "exp"}, {"mid", "mid"}, {"jungl", "jungle"}, {"roam", "roam"},
}

type mlbbBuild struct {
	Spell    string   `json:"spell"`
	Emblem   string   `json:"emblem"`
	Talents  []string `json:"talents"`
	Items    []string `json:"core_items"`
	WinRate  string   `json:"win_rate"`
	PickRate string   `json:"pick_rate"`
}

type mlbbBuildOutput struct {
	Hero   string      `json:"hero"`
	Lane   string      `json:"lane"`
	Rank   string      `json:"rank"`
	Builds []mlbbBuild `json:"builds"`
	Source string      `json:"source"`
	Note   string      `json:"note"`
}

type arenaBuilds struct {
	Builds []arenaBuild `json:"build"`
}

type arenaBuild struct {
	Spell struct {
		Data struct {
			Detail struct {
				Name string `json:"skillname"`
			} `json:"__data"`
		} `json:"data"`
	} `json:"battleskill"`
	Emblem struct {
		Data struct {
			Name string `json:"emblemname"`
		} `json:"data"`
	} `json:"emblem"`
	Talents  []int   `json:"new_rune_skill"`
	Items    []int   `json:"equipid"`
	WinRate  float64 `json:"build_win_rate"`
	PickRate float64 `json:"build_pick_rate"`
}

type arenaItem struct {
	ID   int    `json:"equipid"`
	Name string `json:"equipname"`
}

type arenaTalent struct {
	ID    int `json:"giftid"`
	Skill struct {
		Name string `json:"skillname"`
	} `json:"emblemskill"`
}

// mlbbBuild gives the builds for a hero in lane, or in its main lane when
// lane is empty.
func (a *apis) mlbbBuild(ctx context.Context, name, lane, rank string) (mlbbBuildOutput, error) {
	if lane != "" && !slices.Contains(mlbbLanes, lane) {
		return mlbbBuildOutput{}, argumentError("lane must be one of %v", mlbbLanes)
	}
	names, err := a.mlbbHeroes(ctx)
	if err != nil {
		return mlbbBuildOutput{}, err
	}
	id, err := findHero(names, name)
	if err != nil {
		return mlbbBuildOutput{}, err
	}
	if lane == "" {
		detail, err := a.arenaDetail(ctx, id)
		if err != nil {
			return mlbbBuildOutput{}, err
		}
		lane = mainLane(detail.Hero.Data.Lanes)
	}
	builds, err := remember(a.cache, fmt.Sprintf("mlbb:builds:%d:%s:%s", id, lane, rank), mlbbTTL, func() (arenaBuilds, error) {
		query := url.Values{"lane": {lane}, "rank": {rank}}
		list, err := arenaList[arenaBuilds](ctx, a, fmt.Sprintf("/api/academy/heroes/%d/builds?%s", id, query.Encode()))
		if err != nil || len(list) == 0 {
			return arenaBuilds{}, err
		}
		return list[0], nil
	})
	if err != nil {
		return mlbbBuildOutput{}, err
	}
	if len(builds.Builds) == 0 {
		return mlbbBuildOutput{}, fmt.Errorf("%s has no builds for %s in the %s lane", arenaService, names[id], lane)
	}
	items, err := arenaNames(ctx, a, "mlbb:items", "/api/academy/equipment", func(i arenaItem) (int, string) { return i.ID, i.Name })
	if err != nil {
		return mlbbBuildOutput{}, err
	}
	talents, err := arenaNames(ctx, a, "mlbb:talents", "/api/academy/emblems", func(t arenaTalent) (int, string) { return t.ID, t.Skill.Name })
	if err != nil {
		return mlbbBuildOutput{}, err
	}
	out := mlbbBuildOutput{Hero: names[id], Lane: lane, Rank: rank, Source: arenaService + ", the game's academy", Note: buildNote}
	// The academy does not always list the most used build first. The
	// list is the cache's, so it is sorted as a copy.
	sorted := slices.Clone(builds.Builds)
	slices.SortStableFunc(sorted, func(x, y arenaBuild) int { return cmp.Compare(y.PickRate, x.PickRate) })
	for _, b := range sorted {
		out.Builds = append(out.Builds, mlbbBuild{
			Spell:    cleanText(b.Spell.Data.Detail.Name, maxHeroName),
			Emblem:   cleanText(b.Emblem.Data.Name, maxHeroName),
			Talents:  lookUp(talents, b.Talents),
			Items:    lookUp(items, b.Items),
			WinRate:  percent(b.WinRate),
			PickRate: percent(b.PickRate),
		})
	}
	return out, nil
}

// mainLane is the lane of the first of a hero's academy lane names, or
// jungle when none is known.
func mainLane(labels []string) string {
	for _, label := range labels {
		label = strings.ToLower(label)
		for _, w := range laneWords {
			if strings.Contains(label, w.word) {
				return w.lane
			}
		}
	}
	return "jungle"
}

// arenaNames maps the IDs of one of the academy's lists, such as the
// items, to their names. The lists change only with a patch.
func arenaNames[T any](ctx context.Context, a *apis, key, path string, entry func(T) (int, string)) (map[int]string, error) {
	return remember(a.cache, key, mlbbHeroesTTL, func() (map[int]string, error) {
		list, err := arenaList[T](ctx, a, path+"?size="+strconv.Itoa(mlbbAllHeroes))
		if err != nil {
			return nil, err
		}
		names := make(map[int]string, len(list))
		for _, e := range list {
			id, name := entry(e)
			names[id] = cleanText(name, maxHeroName)
		}
		return names, nil
	})
}

// lookUp names ids, leaving out any the list does not know.
func lookUp(names map[int]string, ids []int) []string {
	out := []string{}
	for _, id := range ids {
		if name := names[id]; name != "" {
			out = append(out, name)
		}
	}
	return out
}
