package webapi

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

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
	// shownSkill and shownTip cap the game's own texts, so that a hero
	// with five skills still fits mlbbOutputBytes.
	shownSkill      = 240
	shownTip        = 200
	mlbbOutputBytes = 6000
	// mlbbTimeout covers the slowest answers, a cold hero or build, when
	// Rone Arena is busy and takes up to 10 s a request.
	mlbbTimeout = 20 * time.Second
	// heroPictureLimit is well above a portrait's 40 KB.
	heroPictureLimit = 1 << 20
)

const (
	playerSource = "api.isan.eu.org, an unofficial nickname lookup"
	playerTTL    = time.Hour
	// playerNote: the service answers a wrong ID and its own failure alike.
	playerNote = "Not found does not tell a wrong ID or zone from the service being down: ask them to check both, and say the lookup may be down."
)

var (
	playerID   = regexp.MustCompile(`^[0-9]{5,12}$`)
	playerZone = regexp.MustCompile(`^[0-9]{1,6}$`)
	mlbbRanks  = []string{"all", "epic", "legend", "mythic", "honor", "glory"}
	mlbbSorts  = []string{"win_rate", "ban_rate", "pick_rate"}
	mlbbRoles  = []string{"Tank", "Fighter", "Assassin", "Mage", "Marksman", "Support"}
)

type mlbbArgs struct {
	Action   string   `json:"action"`
	Hero     string   `json:"hero"`
	Picture  bool     `json:"picture"`
	Skin     string   `json:"skin"`
	Lane     string   `json:"lane"`
	Role     string   `json:"role"`
	Item     string   `json:"item"`
	Enemies  []string `json:"enemies"`
	Allies   []string `json:"allies"`
	Matches  int      `json:"matches"`
	WinRate  float64  `json:"win_rate"`
	Target   float64  `json:"target_win_rate"`
	PlayerID string   `json:"player_id"`
	Zone     string   `json:"zone"`
	Rank     string   `json:"rank"`
	Sort     string   `json:"sort"`
}

type mlbbRates struct {
	Hero     string `json:"hero"`
	WinRate  string `json:"win_rate"`
	PickRate string `json:"pick_rate"`
	BanRate  string `json:"ban_rate"`
	// Tier is MLBBDex's own, from its rates.
	Tier string `json:"tier,omitempty"`
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
	Measured string      `json:"measured,omitempty"`
	Heroes   []mlbbRates `json:"heroes"`
	Source   string      `json:"source"`
	Note     string      `json:"note"`
}

// mlbbSkill is one of a hero's skills as the game describes it.
type mlbbSkill struct {
	Name        string   `json:"name"`
	Tags        []string `json:"tags,omitempty"`
	Description string   `json:"description"`
}

// mlbbTips are the game's own advice on a hero's partners and matchups.
type mlbbTips struct {
	WorksWith     string `json:"works_with,omitempty"`
	StrongAgainst string `json:"strong_against,omitempty"`
	WeakAgainst   string `json:"weak_against,omitempty"`
}

// mlbbHeroOutput is a hero's profile from MLBBDex, and from Rone Arena what
// MLBBDex lacks: counters, partners, the game's tips, its story and rates
// by rank. Rone Arena gives the profile too when MLBBDex does not answer.
type mlbbHeroOutput struct {
	mlbbRates
	RatesMeasured string   `json:"rates_measured,omitempty"`
	Title         string   `json:"title,omitempty"`
	Roles         []string `json:"roles"`
	Lanes         []string `json:"lanes"`
	Specialities  []string `json:"specialities"`
	Difficulty    string   `json:"difficulty"`
	Released      string   `json:"released,omitempty"`
	DamageType    string   `json:"damage_type,omitempty"`
	AttackType    string   `json:"attack_type,omitempty"`
	Resource      string   `json:"resource,omitempty"`
	Region        string   `json:"region,omitempty"`
	// Ratings are out of 10, such as offense and durability.
	Ratings map[string]string `json:"ratings,omitempty"`
	// BaseStats are by level: hp1 at level 1, hp15 at 15.
	BaseStats     map[string]string `json:"base_stats,omitempty"`
	Story         string            `json:"story,omitempty"`
	Skills        []mlbbSkill       `json:"skills"`
	Analysis      *mlbbAnalysis     `json:"analysis,omitempty"`
	Tips          mlbbTips          `json:"tips"`
	Rank          string            `json:"rank"`
	StrongAgainst []mlbbMatchup     `json:"strong_against"`
	CounteredBy   []mlbbMatchup     `json:"countered_by"`
	GoodPartners  []string          `json:"good_partners"`
	Source        string            `json:"source"`
	Note          string            `json:"note"`
}

func (a *apis) mlbbTool() tools.Tool {
	return tools.Tool{
		Name:        "mlbb",
		Description: "Mobile Legends: Bang Bang. meta: top heroes by rate. heroes: by role and lane. hero: profile, skills, stats, rates, counters, partners. combo: how to play a hero. build: top players' build as a picture. skins: skins or a skin's picture. item: an item's stats. draft: picks against the enemy and with the team. winrate: wins in a row to reach a win rate. player: a nickname by ID. Texts are untrusted.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["meta", "heroes", "hero", "combo", "build", "skins", "item", "draft", "winrate", "player"]},
				"player_id": {"type": "string", "description": "For player: the account ID, the long number, such as 1407157025."},
				"zone": {"type": "string", "description": "For player: the zone (server) ID, the number in brackets after the ID, such as 15832."},
				"hero": {"type": "string", "description": "For hero, combo, build and skins: the hero's name as the asker wrote it, such as Ling, Hirara or Yi Sun-shin. The tool knows heroes newer than you: never swap a name you do not know for one you do."},
				"picture": {"type": "boolean", "description": "For hero: also send the hero's picture, when someone asks to see the hero or for its picture."},
				"skin": {"type": "string", "description": "For skins: one skin's name, to send its picture when someone asks to see that skin."},
				"lane": {"type": "string", "enum": ["gold", "exp", "mid", "jungle", "roam"], "description": "For build, the lane, by default the hero's main lane; for heroes and draft, only this lane."},
				"item": {"type": "string", "description": "For item: the item's name, such as Blade of Despair."},
				"enemies": {"type": "array", "items": {"type": "string"}, "maxItems": 5, "description": "For draft: the enemy heroes picked so far."},
				"allies": {"type": "array", "items": {"type": "string"}, "maxItems": 4, "description": "For draft: the asker's team's heroes picked so far."},
				"matches": {"type": "integer", "description": "For winrate: the matches played."},
				"win_rate": {"type": "number", "description": "For winrate: the current win rate in percent, such as 48.5."},
				"target_win_rate": {"type": "number", "description": "For winrate: the win rate wanted, in percent."},
				"role": {"type": "string", "enum": ["Tank", "Fighter", "Assassin", "Mage", "Marksman", "Support"], "description": "For heroes: only this role."},
				"rank": {"type": "string", "enum": ["all", "epic", "legend", "mythic", "honor", "glory"], "description": "Rank of the games counted. Default all; honor and glory are Mythical Honor and Mythical Glory."},
				"sort": {"type": "string", "enum": ["win_rate", "ban_rate", "pick_rate"], "description": "For meta: what the heroes are ranked by. Default win_rate."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		Run:            a.mlbb,
		MaxOutputBytes: mlbbOutputBytes,
		Timeout:        mlbbTimeout,
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
	note := ""
	if args.Hero != "" && slices.Contains([]string{"hero", "combo", "build", "skins"}, args.Action) {
		args.Hero, note = a.heroAsked(ctx, args.Hero)
	}
	out, err := a.mlbbAction(ctx, args, rank)
	if err != nil || note == "" {
		return out, err
	}
	switch o := out.(type) {
	case mlbbHeroOutput:
		o.Note += note
		return o, nil
	case mlbbBuildOutput:
		o.Note += note
		return o, nil
	case mlbbSkinsOutput:
		o.Note += note
		return o, nil
	}
	return out, nil
}

func (a *apis) mlbbAction(ctx context.Context, args mlbbArgs, rank string) (any, error) {
	switch args.Action {
	case "meta":
		sort := cmp.Or(args.Sort, "win_rate")
		if !slices.Contains(mlbbSorts, sort) {
			return nil, argumentError("sort must be one of %v", mlbbSorts)
		}
		return a.mlbbMeta(ctx, rank, sort)
	case "heroes":
		return a.mlbbHeroList(ctx, args.Role, args.Lane)
	case "hero":
		return a.mlbbHero(ctx, args.Hero, rank, args.Picture)
	case "build":
		return a.mlbbBuild(ctx, args.Hero, args.Lane, rank)
	case "skins":
		return a.mlbbSkins(ctx, args.Hero, args.Skin)
	case "combo":
		return a.mlbbCombos(ctx, args.Hero)
	case "item":
		return a.mlbbItem(ctx, args.Item)
	case "draft":
		return a.mlbbDraft(ctx, args.Enemies, args.Allies, args.Lane, rank)
	case "winrate":
		return mlbbWinrate(args.Matches, args.WinRate, args.Target)
	case "player":
		return a.mlbbPlayer(ctx, args.PlayerID, args.Zone)
	default:
		return nil, argumentError("action must be meta, heroes, hero, combo, build, skins, item, draft, winrate or player")
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

// arenaStats is a hero's rates, and for counters the enemies that raise
// (sub_hero) and lower (sub_hero_last) its win rate most.
type arenaStats struct {
	Hero     arenaName  `json:"main_hero"`
	WinRate  float64    `json:"main_hero_win_rate"`
	PickRate float64    `json:"main_hero_appearance_rate"`
	BanRate  float64    `json:"main_hero_ban_rate"`
	Raising  []arenaSub `json:"sub_hero"`
	Lowering []arenaSub `json:"sub_hero_last"`
}

// arenaDetail is a hero's page in the game's academy.
type arenaDetail struct {
	Hero struct {
		Data struct {
			Difficulty string   `json:"difficulty"`
			Speciality []string `json:"speciality"`
			Roles      []string `json:"sortlabel"`
			Lanes      []string `json:"roadsortlabel"`
			Story      string   `json:"story"`
			// Picture is the hero's 500 by 500 portrait; Card, its 240 by
			// 390 card, which new heroes such as Hirara have before one.
			Picture     string `json:"squareheadbig"`
			Card        string `json:"smallmap"`
			SkillGroups []struct {
				Skills []struct {
					Name string `json:"skillname"`
					Desc string `json:"skilldesc"`
					Tags []struct {
						Name string `json:"tagname"`
					} `json:"skilltag"`
				} `json:"skilllist"`
			} `json:"heroskilllist"`
		} `json:"data"`
	} `json:"hero"`
	Relation struct {
		Assist arenaTip `json:"assist"`
		Strong arenaTip `json:"strong"`
		Weak   arenaTip `json:"weak"`
	} `json:"relation"`
}

type arenaTip struct {
	Desc   string `json:"desc"`
	Heroes []int  `json:"target_hero_id"`
}

type arenaSub struct {
	ID     int     `json:"heroid"`
	Change float64 `json:"increase_win_rate"`
}

func (s arenaStats) rates() mlbbRates {
	return mlbbRates{Hero: s.Hero.Data.Name, WinRate: percent(s.WinRate), PickRate: percent(s.PickRate), BanRate: percent(s.BanRate)}
}

// mlbbMeta gives the top heroes from MLBBDex's rankings, which count every
// rank, and from Rone Arena for one rank, which MLBBDex does not split, or
// when MLBBDex does not answer.
func (a *apis) mlbbMeta(ctx context.Context, rank, sort string) (mlbbMetaOutput, error) {
	if rank == "all" {
		if out, err := a.dexMeta(ctx, sort); err == nil {
			return out, nil
		}
	}
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

func (a *apis) mlbbHero(ctx context.Context, name, rank string, picture bool) (mlbbHeroOutput, error) {
	names, err := a.mlbbHeroes(ctx)
	if err != nil {
		return mlbbHeroOutput{}, err
	}
	id, err := findHero(names, name)
	if err != nil {
		return mlbbHeroOutput{}, err
	}
	// Two requests to Rone Arena, not three: the API answers one
	// connection's requests in turn, and a third would wait past the
	// client's timeout. MLBBDex is another host.
	var enemies arenaStats
	var detail arenaDetail
	var profile dexHero
	var rankings dexRankings
	var enemiesErr, detailErr, profileErr, rankingsErr error
	var wg sync.WaitGroup
	wg.Go(func() { enemies, enemiesErr = a.arenaCounters(ctx, id, rank) })
	wg.Go(func() { detail, detailErr = a.arenaDetail(ctx, id) })
	wg.Go(func() { profile, profileErr = a.dexHeroByName(ctx, names[id]) })
	rankingsErr = errNotAsked
	if rank == "all" {
		wg.Go(func() { rankings, rankingsErr = a.dexRankings(ctx) })
	}
	wg.Wait()
	roneErr := cmp.Or(enemiesErr, detailErr)
	if roneErr != nil && profileErr != nil {
		return mlbbHeroOutput{}, roneErr
	}
	out := mlbbHeroOutput{Rank: rank, Source: mlbbSource, Note: mlbbNote}
	if roneErr == nil {
		out = roneHero(names, enemies, detail, rank)
	} else {
		out.Note += "; Rone Arena did not answer, so the rates by rank, counters, partners and tips are missing: say so if asked"
	}
	if profileErr == nil {
		applyDexProfile(&out, profile)
		out.Source = dexSource + "; " + mlbbSource
	}
	if rates, measured, ok := rankings.find(names[id]); rankingsErr == nil && ok {
		out.mlbbRates, out.RatesMeasured = rates, measured
		out.Note += "; win, pick and ban rates and the tier are MLBBDex's, measured on rates_measured"
	}
	if out.Hero == "" {
		out.Hero = names[id]
	}
	if picture {
		out.Note = a.sendHeroPicture(ctx, out.Note, profile.Data.Images.Portrait, cmp.Or(detail.Hero.Data.Picture, detail.Hero.Data.Card))
	}
	return out, nil
}

// roneHero is the hero as Rone Arena gives it.
func roneHero(names map[int]string, enemies arenaStats, detail arenaDetail, rank string) mlbbHeroOutput {
	hero := detail.Hero.Data
	return mlbbHeroOutput{
		mlbbRates:    enemies.rates(),
		Roles:        nonEmpty(hero.Roles),
		Lanes:        nonEmpty(hero.Lanes),
		Specialities: nonEmpty(hero.Speciality),
		Difficulty:   hero.Difficulty + "/100",
		Story:        cleanText(hero.Story, shownTip),
		Skills:       detail.skills(),
		Tips: mlbbTips{
			WorksWith:     cleanText(detail.Relation.Assist.Desc, shownTip),
			StrongAgainst: cleanText(detail.Relation.Strong.Desc, shownTip),
			WeakAgainst:   cleanText(detail.Relation.Weak.Desc, shownTip),
		},
		Rank:          rank,
		StrongAgainst: matchups(names, enemies.Raising),
		CounteredBy:   matchups(names, enemies.Lowering),
		GoodPartners:  heroNames(names, detail.Relation.Assist.Heroes),
		Source:        mlbbSource,
		Note:          mlbbNote,
	}
}

// sendHeroPicture fetches the hero's picture into the turn's sink, to go
// with the answer as a photo: MLBBDex's portrait, else Rone Arena's
// picture. It returns note with whether it will.
func (a *apis) sendHeroPicture(ctx context.Context, note, dexPath, roneLink string) string {
	sink, ok := tools.SinkFrom(ctx)
	if !ok || (dexPath == "" && roneLink == "") {
		return note + "; there is no picture of this hero, so none is sent: say so plainly, never as if one were"
	}
	var data []byte
	err := errNotAsked
	if strings.HasPrefix(dexPath, "/") {
		data, err = a.patient.get(ctx, call{service: dexService, url: a.urls.MLBBDex + dexPath, limit: heroPictureLimit})
		if err == nil {
			data, err = asJPEG(data)
		}
	}
	if err != nil && roneLink != "" {
		data, err = a.patient.get(ctx, call{service: arenaService, url: roneLink, limit: heroPictureLimit})
	}
	if err != nil {
		return note + "; the picture could not be fetched, so none is sent: say so plainly, never as if one were"
	}
	sink.Add(tools.Drawing{Data: data, MimeType: http.DetectContentType(data)})
	return note + "; the hero's picture is sent with your answer, which becomes its caption: keep the answer short"
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

// findHero returns the ID of the hero called name, found as matchHero
// finds it.
func findHero(names map[int]string, name string) (int, error) {
	hero, err := matchHero(slices.Collect(maps.Values(names)), name)
	if err != nil {
		return 0, err
	}
	for id, n := range names {
		if n == hero {
			return id, nil
		}
	}
	return 0, argumentError("no MLBB hero is called %q", name)
}

// heroTypos is how many letters a name of more than five may be off and
// still find its hero, when no other is as close: "Xirara" finds Hirara.
// A shorter name may be one letter off.
const heroTypos = 2

// matchHero returns the hero of heroes that name means, as matchName
// finds it.
func matchHero(heroes []string, name string) (string, error) {
	return matchName(heroes, name, "hero")
}

// matchName returns the one of names that name means, however it is
// spaced, hyphenated, capitalised or written in Cyrillic ("Хирара"), or with
// a typo when one alone is that close. The error, about an MLBB what, names
// the closest.
func matchName(heroes []string, name, what string) (string, error) {
	if name == "" || len([]rune(name)) > maxHeroName {
		return "", argumentError("%s must be a name of at most %d characters", what, maxHeroName)
	}
	wanted := latinName(normalizeName(name))
	distance := make(map[string]int, len(heroes))
	for _, hero := range heroes {
		distance[hero] = editDistance(wanted, latinName(normalizeName(hero)))
		if distance[hero] == 0 {
			return hero, nil
		}
	}
	ranked := slices.Clone(heroes)
	slices.SortStableFunc(ranked, func(x, y string) int { return cmp.Or(cmp.Compare(distance[x], distance[y]), strings.Compare(x, y)) })
	allowed := 1
	if len([]rune(wanted)) > 5 {
		allowed = heroTypos
	}
	if len(ranked) > 0 && distance[ranked[0]] <= allowed && (len(ranked) == 1 || distance[ranked[1]] > distance[ranked[0]]) {
		return ranked[0], nil
	}
	return "", argumentError("no MLBB %s is called %q; the closest are %s. Give the name as the asker wrote it, never one you guess", what, name, strings.Join(ranked[:min(len(ranked), 3)], ", "))
}

// heroAsked is the hero the asker named, when their own words name one
// hero alone and the model asked for another they never named: the model
// takes heroes newer than it, such as Hirara, for ones it knows. It returns
// the hero to use and what the note should add.
func (a *apis) heroAsked(ctx context.Context, asked string) (string, string) {
	caller, ok := tools.CallerFrom(ctx)
	if !ok || caller.Words == "" {
		return asked, ""
	}
	heroes, err := a.dexHeroes(ctx)
	if err != nil {
		return asked, ""
	}
	named := heroesInWords(dexNames(heroes), caller.Words)
	if len(named) != 1 {
		return asked, ""
	}
	if found, err := matchHero(dexNames(heroes), asked); err == nil && found == named[0] {
		return asked, ""
	}
	return named[0], fmt.Sprintf("; the asker's message names %s, so this is %s, not %s: answer about %s", named[0], named[0], asked, named[0])
}

// heroesInWords are the heroes words name: a word, or two or three in a row
// for a name such as Yi Sun-shin, that is a hero's name, starts with one
// of four letters or more, as Uzbek endings do ("hiraraga", "lingni"), or
// is one typo off a name of six letters or more ("Xirara").
func heroesInWords(heroes []string, words string) []string {
	tokens := strings.FieldsFunc(strings.ToLower(words), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !isApostrophe(r)
	})
	for i, t := range tokens {
		tokens[i] = latinName(normalizeName(t))
	}
	var named []string
	for _, hero := range heroes {
		name := latinName(normalizeName(hero))
		if name != "" && namedIn(tokens, name) {
			named = append(named, hero)
		}
	}
	return named
}

// heroEndings are the Uzbek and Russian endings, in Latin letters, a hero's
// name takes in a message: "hiraraga", "lingni", "fannyning". Any other
// rest makes it another word: "so'ramasdan" is not Sora.
var heroEndings = map[string]bool{
	"ga": true, "ni": true, "ning": true, "da": true, "dan": true, "dagi": true, "niki": true, "lar": true, "larni": true,
	"mi": true, "ku": true, "chi": true, "dek": true, "day": true, "ka": true, "na": true,
	"u": true, "y": true, "a": true, "e": true, "om": true, "oy": true,
}

func namedIn(tokens []string, name string) bool {
	for i := range tokens {
		joined := ""
		for j := i; j < min(len(tokens), i+3); j++ {
			joined += tokens[j]
			switch {
			case joined == name:
				return true
			case j == i && len([]rune(name)) >= 4 && strings.HasPrefix(joined, name) && heroEndings[strings.TrimPrefix(joined, name)]:
				return true
			case j == i && len([]rune(name)) >= 6 && len([]rune(joined)) >= 6 && editDistance(joined, name) == 1:
				return true
			}
		}
	}
	return false
}

// cyrillicLetters spell Russian and Uzbek Cyrillic in Latin letters, so
// that "Хирара" and "Ҳирара" find Hirara.
var cyrillicLetters = strings.NewReplacer(
	"а", "a", "б", "b", "в", "v", "г", "g", "д", "d", "е", "e", "ж", "j", "з", "z", "и", "i", "й", "y",
	"к", "k", "л", "l", "м", "m", "н", "n", "о", "o", "п", "p", "р", "r", "с", "s", "т", "t", "у", "u",
	"ф", "f", "х", "h", "ц", "ts", "ч", "ch", "ш", "sh", "щ", "sh", "ъ", "", "ы", "i", "ь", "", "э", "e",
	"ю", "yu", "я", "ya", "ў", "o", "қ", "q", "ғ", "g", "ҳ", "h",
)

// latinName is a normalised name in Latin letters.
func latinName(name string) string {
	return cyrillicLetters.Replace(name)
}

func (a *apis) arenaCounters(ctx context.Context, id int, rank string) (arenaStats, error) {
	return remember(a.cache, fmt.Sprintf("mlbb:counters:%d:%s", id, rank), mlbbTTL, func() (arenaStats, error) {
		query := url.Values{"days": {mlbbDays}, "rank": {rank}}
		stats, err := arenaList[arenaStats](ctx, a, fmt.Sprintf("/api/heroes/%d/counters?%s", id, query.Encode()))
		if err != nil {
			return arenaStats{}, err
		}
		if len(stats) == 0 {
			return arenaStats{}, fmt.Errorf("%s has no statistics for this hero yet", arenaService)
		}
		return stats[0], nil
	})
}

// arenaDetail fetches a hero's page, which changes only with a patch.
func (a *apis) arenaDetail(ctx context.Context, id int) (arenaDetail, error) {
	return remember(a.cache, fmt.Sprintf("mlbb:detail:%d", id), mlbbHeroesTTL, func() (arenaDetail, error) {
		details, err := arenaList[arenaDetail](ctx, a, "/api/heroes/"+strconv.Itoa(id))
		if err != nil {
			return arenaDetail{}, err
		}
		if len(details) == 0 {
			return arenaDetail{}, fmt.Errorf("%s has no page for this hero yet", arenaService)
		}
		return details[0], nil
	})
}

func (d arenaDetail) skills() []mlbbSkill {
	var out []mlbbSkill
	for _, group := range d.Hero.Data.SkillGroups {
		for _, s := range group.Skills {
			skill := mlbbSkill{Name: cleanText(s.Name, maxHeroName), Description: cleanText(s.Desc, shownSkill)}
			for _, tag := range s.Tags {
				skill.Tags = append(skill.Tags, cleanText(tag.Name, maxHeroName))
			}
			out = append(out, skill)
		}
	}
	return out
}

func nonEmpty(labels []string) []string {
	var out []string
	for _, l := range labels {
		if l = cleanText(l, maxHeroName); l != "" {
			out = append(out, l)
		}
	}
	return out
}

const arenaService = "Rone Arena API"

// arenaList fetches one of the API's lists. It is a function, not a
// method, because Go methods cannot take type parameters.
func arenaList[T any](ctx context.Context, a *apis, path string) ([]T, error) {
	var answer arenaRecords[T]
	if err := a.patient.getJSON(ctx, call{service: arenaService, url: a.urls.RoneArena + path, limit: largeBodyLimit}, &answer); err != nil {
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

type mlbbPlayerOutput struct {
	PlayerID string `json:"player_id"`
	Zone     string `json:"zone"`
	Nickname string `json:"nickname"`
	Country  string `json:"country"`
	Source   string `json:"source"`
}

type isanAnswer struct {
	Success bool   `json:"success"`
	Name    string `json:"name"`
	Country string `json:"country"`
}

// mlbbPlayer looks up an account's nickname and country. The nickname is
// the player's own text, so it is cleaned like any outside text.
func (a *apis) mlbbPlayer(ctx context.Context, id, zone string) (mlbbPlayerOutput, error) {
	if !playerID.MatchString(id) || !playerZone.MatchString(zone) {
		return mlbbPlayerOutput{}, argumentError("player_id must be the account's 5 to 12 digits and zone the 1 to 6 digits in brackets")
	}
	found, err := remember(a.cache, "mlbb:player:"+id+":"+zone, playerTTL, func() (isanAnswer, error) {
		var answer isanAnswer
		query := url.Values{"id": {id}, "zone": {zone}}
		err := a.client.getJSON(ctx, call{service: "the nickname lookup", url: a.urls.Isan + "/nickname/ml?" + query.Encode()}, &answer)
		return answer, err
	})
	if err != nil {
		// The lookup failing is not the account missing: saying "not found"
		// would have the player doubt an ID that is right.
		return mlbbPlayerOutput{}, fmt.Errorf("the nickname lookup is not working right now, so the account could not be checked; say so, and that the ID may well be right")
	}
	if !found.Success || found.Name == "" {
		return mlbbPlayerOutput{}, fmt.Errorf("no account found for %s (%s). %s", id, zone, playerNote)
	}
	return mlbbPlayerOutput{
		PlayerID: id, Zone: zone,
		Nickname: cleanText(found.Name, maxHeroName),
		Country:  cleanText(found.Country, maxHeroName),
		Source:   playerSource,
	}, nil
}

// heroNames names the heroes of ids; 0, which pads the game's lists, and
// heroes not in names are left out.
func heroNames(names map[int]string, ids []int) []string {
	var out []string
	for _, id := range ids {
		if name, ok := names[id]; ok {
			out = append(out, name)
		}
	}
	return out
}

func percent(rate float64) string {
	return fmt.Sprintf("%.1f%%", rate*100)
}
