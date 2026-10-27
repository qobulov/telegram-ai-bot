package webapi

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // icons may come as JPEG
	_ "image/png"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"telegram-ai-bot/internal/tools"
)

const (
	buildNote = "The builds top players of this rank use on this hero in this lane, from the game's academy, most used first: " +
		"spell, emblem with its three talents and core items."
	// buildCardNote: the card shows the first build, with its icons.
	buildCardNote = " The first build is sent as one picture with your answer, which becomes its caption: keep the answer short."
	// iconLimit is well above an icon's 25 KB.
	iconLimit = 256 << 10
	// The academy's builds name three core items; the boots and the last two
	// of a full build of six are the ones most often added in the hottest
	// guides players write on the academy, when enough of them have six.
	guidesRead     = 30
	minGuides      = 5
	minGuideItems  = 5
	finishingItems = 2
	finishNote     = " finish_with: the boots and last items players most often add to this hero, from guides_counted guides on the academy, not from the top players' games."
)

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
	Hero       string      `json:"hero"`
	Lane       string      `json:"lane"`
	Rank       string      `json:"rank"`
	Builds     []mlbbBuild `json:"builds"`
	FinishWith []string    `json:"finish_with,omitempty"`
	Guides     int         `json:"guides_counted,omitempty"`
	Source     string      `json:"source"`
	Note       string      `json:"note"`
}

// arenaGuide is a guide a player wrote on the academy; its first list of
// items is its build.
type arenaGuide struct {
	Data struct {
		Equips []struct {
			IDs []int `json:"equip_ids"`
		} `json:"equips"`
	} `json:"data"`
}

type arenaBuilds struct {
	Builds []arenaBuild `json:"build"`
}

type arenaBuild struct {
	Spell struct {
		Data struct {
			Detail struct {
				Name string `json:"skillname"`
				Icon string `json:"skillicon"`
			} `json:"__data"`
		} `json:"data"`
	} `json:"battleskill"`
	Emblem struct {
		Data struct {
			Name string `json:"emblemname"`
			Icon string `json:"attriicon"`
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
	Icon string `json:"equipicon"`
}

type arenaTalent struct {
	ID    int `json:"giftid"`
	Skill struct {
		Name string `json:"skillname"`
		Icon string `json:"skillicon"`
	} `json:"emblemskill"`
}

// arenaEntry is one of an academy list's entries: an item or a talent.
type arenaEntry struct {
	Name string
	Icon string
}

// mlbbBuild gives the builds for a hero in lane, or in its main lane when
// lane is empty, and sends the first as a picture.
func (a *apis) mlbbBuild(ctx context.Context, name, lane, rank string) (mlbbBuildOutput, error) {
	if lane != "" && !slices.Contains(mlbbLanes, lane) {
		return mlbbBuildOutput{}, argumentError("lane must be one of %v", mlbbLanes)
	}
	hero, err := a.buildHero(ctx, name, lane)
	if err != nil {
		return mlbbBuildOutput{}, err
	}
	// The builds, the items and the talents at once: the lists are cached
	// for a day, but Rone Arena takes up to 10 s an answer when it is busy.
	var builds arenaBuilds
	var items, talents map[int]arenaEntry
	var guides []arenaGuide
	var buildsErr, itemsErr, talentsErr, guidesErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		guides, guidesErr = remember(a.cache, "mlbb:guides:"+hero.path, mlbbHeroesTTL, func() ([]arenaGuide, error) {
			query := url.Values{"order": {"desc"}, "size": {strconv.Itoa(guidesRead)}}
			return arenaList[arenaGuide](ctx, a, "/api/academy/heroes/"+hero.path+"/recommended?"+query.Encode())
		})
	})
	wg.Go(func() {
		builds, buildsErr = remember(a.cache, "mlbb:builds:"+hero.path+":"+hero.lane+":"+rank, mlbbTTL, func() (arenaBuilds, error) {
			query := url.Values{"lane": {hero.lane}, "rank": {rank}}
			list, err := arenaList[arenaBuilds](ctx, a, "/api/academy/heroes/"+hero.path+"/builds?"+query.Encode())
			if err != nil || len(list) == 0 {
				return arenaBuilds{}, err
			}
			return list[0], nil
		})
	})
	wg.Go(func() {
		items, itemsErr = arenaEntries(ctx, a, "mlbb:items", "/api/academy/equipment", func(i arenaItem) (int, arenaEntry) {
			return i.ID, arenaEntry{Name: i.Name, Icon: i.Icon}
		})
	})
	wg.Go(func() {
		talents, talentsErr = arenaEntries(ctx, a, "mlbb:talents", "/api/academy/emblems", func(t arenaTalent) (int, arenaEntry) {
			return t.ID, arenaEntry{Name: t.Skill.Name, Icon: t.Skill.Icon}
		})
	})
	wg.Wait()
	if buildsErr != nil {
		return mlbbBuildOutput{}, buildsErr
	}
	if len(builds.Builds) == 0 {
		return mlbbBuildOutput{}, fmt.Errorf("%s has no builds for %s in the %s lane", arenaService, hero.name, hero.lane)
	}
	if err := cmp.Or(itemsErr, talentsErr); err != nil {
		return mlbbBuildOutput{}, err
	}
	out := mlbbBuildOutput{Hero: hero.name, Lane: hero.lane, Rank: rank, Source: arenaService + ", the game's academy", Note: buildNote}
	// The academy does not always list the most used build first. The
	// list is the cache's, so it is sorted as a copy.
	sorted := slices.Clone(builds.Builds)
	slices.SortStableFunc(sorted, func(x, y arenaBuild) int { return cmp.Compare(y.PickRate, x.PickRate) })
	for _, b := range sorted {
		out.Builds = append(out.Builds, mlbbBuild{
			Spell:    cleanText(b.Spell.Data.Detail.Name, maxHeroName),
			Emblem:   cleanText(b.Emblem.Data.Name, maxHeroName),
			Talents:  entryNames(talents, b.Talents),
			Items:    entryNames(items, b.Items),
			WinRate:  percent(b.WinRate),
			PickRate: percent(b.PickRate),
		})
	}
	// Guides that do not load leave the three core items.
	var finish []int
	if guidesErr == nil {
		finish, out.Guides = finishing(sorted[0].Items, guides, items)
	}
	if len(finish) > 0 {
		out.FinishWith = entryNames(items, finish)
		out.Note += finishNote
	}
	out.Note += a.sendBuildCard(ctx, out, sorted[0], finish, items, talents)
	return out, nil
}

// finishing returns the boots and finishingItems other items most often
// in the guides, besides the core items, and how many guides were counted:
// those that list at least minGuideItems items. It returns nothing when
// fewer than minGuides do.
func finishing(core []int, guides []arenaGuide, items map[int]arenaEntry) ([]int, int) {
	counts := map[int]int{}
	counted := 0
	for _, g := range guides {
		if len(g.Data.Equips) == 0 || len(g.Data.Equips[0].IDs) < minGuideItems {
			continue
		}
		counted++
		seen := map[int]bool{}
		for _, id := range g.Data.Equips[0].IDs {
			if !seen[id] && items[id].Name != "" && !slices.Contains(core, id) {
				seen[id] = true
				counts[id]++
			}
		}
	}
	if counted < minGuides {
		return nil, 0
	}
	ranked := slices.Collect(maps.Keys(counts))
	// Most often first; the lower ID breaks a tie, so the answer is stable.
	slices.SortFunc(ranked, func(x, y int) int { return cmp.Or(cmp.Compare(counts[y], counts[x]), cmp.Compare(x, y)) })
	var boots, others []int
	for _, id := range ranked {
		if isBoots(items[id].Name) {
			boots = append(boots, id)
		} else {
			others = append(others, id)
		}
	}
	return append(boots[:min(len(boots), 1)], others[:min(len(others), finishingItems)]...), counted
}

func isBoots(item string) bool {
	return strings.Contains(strings.ToLower(item), "boots")
}

// buildTarget is the hero a build is asked for: its name, how the
// academy's builds address it and the lane.
type buildTarget struct {
	name, path, lane string
}

// buildHero finds the hero, and its main lane when lane is empty, in
// MLBBDex's list, whose names the academy takes in place of its IDs; when
// MLBBDex does not answer or know the hero, in Rone Arena's.
func (a *apis) buildHero(ctx context.Context, name, lane string) (buildTarget, error) {
	if name == "" || len([]rune(name)) > maxHeroName {
		return buildTarget{}, argumentError("hero must be a name of at most %d characters", maxHeroName)
	}
	if heroes, err := a.dexHeroes(ctx); err == nil {
		if found, err := matchHero(dexNames(heroes), name); err == nil {
			h := heroes[slices.IndexFunc(heroes, func(h dexListHero) bool { return h.Name == found })]
			return buildTarget{name: h.Name, path: url.PathEscape(h.Name), lane: cmp.Or(lane, mainLane(h.Lanes))}, nil
		}
	}
	names, err := a.mlbbHeroes(ctx)
	if err != nil {
		return buildTarget{}, err
	}
	id, err := findHero(names, name)
	if err != nil {
		return buildTarget{}, err
	}
	if lane == "" {
		detail, err := a.arenaDetail(ctx, id)
		if err != nil {
			return buildTarget{}, err
		}
		lane = mainLane(detail.Hero.Data.Lanes)
	}
	return buildTarget{name: names[id], path: strconv.Itoa(id), lane: lane}, nil
}

// sendBuildCard draws the first build as one picture into the turn's sink
// and returns what the note should add about it. An icon that will not load
// is left blank; a card that cannot be drawn leaves the answer in words.
func (a *apis) sendBuildCard(ctx context.Context, out mlbbBuildOutput, b arenaBuild, finish []int, items, talents map[int]arenaEntry) string {
	sink, ok := tools.SinkFrom(ctx)
	if !ok {
		return ""
	}
	first := out.Builds[0]
	setup := []arenaEntry{{Name: first.Spell, Icon: b.Spell.Data.Detail.Icon}, {Name: first.Emblem + " emblem", Icon: b.Emblem.Data.Icon}}
	setup = append(setup, knownEntries(talents, b.Talents)...)
	core := knownEntries(items, b.Items)
	finished := knownEntries(items, finish)
	icons := a.fetchIcons(ctx, slices.Concat(setup, core, finished))
	card := buildCard{
		Title:    fmt.Sprintf("%s · %s · %s", out.Hero, laneTitle(out.Lane), rankTitle(out.Rank)),
		Subtitle: fmt.Sprintf("Win rate %s · used in %s of builds · %s", first.WinRate, first.PickRate, "MLBB Academy"),
		Rows: []cardRow{
			{Heading: "Spell, emblem and talents", Icons: cardIcons(setup, icons[:len(setup)])},
			{Heading: "Core items · top players", Icons: cardIcons(core, icons[len(setup):len(setup)+len(core)])},
		},
	}
	if len(finished) > 0 {
		card.Rows = append(card.Rows, cardRow{
			Heading: fmt.Sprintf("Boots and last items · %d player guides", out.Guides),
			Icons:   cardIcons(finished, icons[len(setup)+len(core):]),
		})
	}
	picture, err := drawBuildCard(card)
	if err != nil {
		return ""
	}
	sink.Add(tools.Drawing{Data: picture, MimeType: "image/png"})
	return buildCardNote
}

// fetchIcons loads the entries' icons at once, a nil for any that will not
// load or is not an https link.
func (a *apis) fetchIcons(ctx context.Context, entries []arenaEntry) []image.Image {
	icons := make([]image.Image, len(entries))
	var wg sync.WaitGroup
	for i, e := range entries {
		link, err := url.Parse(e.Icon)
		if err != nil || link.Scheme != "https" || link.Host == "" {
			continue
		}
		wg.Go(func() {
			data, err := remember(a.cache, "mlbb:icon:"+e.Icon, mlbbHeroesTTL, func() ([]byte, error) {
				return a.client.get(ctx, call{service: "the icon server", url: e.Icon, limit: iconLimit})
			})
			if err != nil {
				return
			}
			if icon, _, err := image.Decode(bytes.NewReader(data)); err == nil {
				icons[i] = icon
			}
		})
	}
	wg.Wait()
	return icons
}

func cardIcons(entries []arenaEntry, pictures []image.Image) []cardIcon {
	out := make([]cardIcon, len(entries))
	for i, e := range entries {
		out[i] = cardIcon{Name: e.Name, Picture: pictures[i]}
	}
	return out
}

// laneTitle and rankTitle name a lane and a rank on the card.
func laneTitle(lane string) string {
	return map[string]string{"gold": "Gold Lane", "exp": "EXP Lane", "mid": "Mid Lane", "jungle": "Jungle", "roam": "Roam"}[lane]
}

func rankTitle(rank string) string {
	return map[string]string{"all": "All ranks", "epic": "Epic", "legend": "Legend", "mythic": "Mythic", "honor": "Mythical Honor", "glory": "Mythical Glory"}[rank]
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

// arenaEntries maps the IDs of one of the academy's lists, such as the
// items, to their names and icons. The lists change only with a patch.
func arenaEntries[T any](ctx context.Context, a *apis, key, path string, entry func(T) (int, arenaEntry)) (map[int]arenaEntry, error) {
	return remember(a.cache, key, mlbbHeroesTTL, func() (map[int]arenaEntry, error) {
		list, err := arenaList[T](ctx, a, path+"?size="+strconv.Itoa(mlbbAllHeroes))
		if err != nil {
			return nil, err
		}
		entries := make(map[int]arenaEntry, len(list))
		for _, e := range list {
			id, found := entry(e)
			found.Name = cleanText(found.Name, maxHeroName)
			entries[id] = found
		}
		return entries, nil
	})
}

// knownEntries are the entries of ids, leaving out any the list does not
// know.
func knownEntries(entries map[int]arenaEntry, ids []int) []arenaEntry {
	var out []arenaEntry
	for _, id := range ids {
		if e := entries[id]; e.Name != "" {
			out = append(out, e)
		}
	}
	return out
}

func entryNames(entries map[int]arenaEntry, ids []int) []string {
	out := []string{}
	for _, e := range knownEntries(entries, ids) {
		out = append(out, e.Name)
	}
	return out
}
