package webapi

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
)

const buildNote = "The builds players of this rank win with on this hero in this lane, from MLBBDex's ranked statistics, most played first: " +
	"spell, emblem with its three talents and three core items."

// mlbbLanes are the lanes builds are kept for.
var mlbbLanes = []string{"gold", "exp", "mid", "jungle", "roam"}

// laneWords find a lane in a lane's name, such as "Jungle", "Gold" or
// "Roam".
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

// mlbbBuild gives the builds played most with a hero in lane, or in its
// main lane when lane is empty, at rank.
func (a *apis) mlbbBuild(ctx context.Context, name, lane, rank string) (mlbbBuildOutput, error) {
	if lane != "" && !slices.Contains(mlbbLanes, lane) {
		return mlbbBuildOutput{}, argumentError("lane must be one of %v", mlbbLanes)
	}
	if name == "" || len([]rune(name)) > maxHeroName {
		return mlbbBuildOutput{}, argumentError("hero must be a name of at most %d characters", maxHeroName)
	}
	heroes, err := a.dexHeroes(ctx)
	if err != nil {
		return mlbbBuildOutput{}, err
	}
	found, err := matchHero(dexNames(heroes), name)
	if err != nil {
		return mlbbBuildOutput{}, err
	}
	hero := heroes[slices.IndexFunc(heroes, func(h dexListHero) bool { return h.Name == found })]
	lane = cmp.Or(lane, mainLane(hero.Lanes))
	builds := a.dexBuildsFor(ctx, hero.Name, lane, rank)
	if len(builds) == 0 {
		return mlbbBuildOutput{}, fmt.Errorf("%s has no builds for %s in the %s lane", dexService, hero.Name, lane)
	}
	return mlbbBuildOutput{Hero: hero.Name, Lane: lane, Rank: rank, Builds: builds[:min(len(builds), shownDexBuilds)], Source: statsSource, Note: buildNote}, nil
}

// mainLane is the lane of the first of a hero's lane names, or jungle when
// none is known.
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
