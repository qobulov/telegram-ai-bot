package webapi

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/image/webp"

	"telegram-ai-bot/internal/tools"
)

// MLBBDex is the first source for Mobile Legends: heroes, their profiles,
// skills, skins and the rates of all ranks; Rone Arena gives only what it
// lacks. Its data comes from the Mobile Legends Wiki, which lags new skins
// by weeks; the newest often appear only among its artwork names. Its
// rankings are measured about once a week.
const (
	dexSource = "MLBBDex (mlbbdex.com), from the Mobile Legends Wiki"
	dexNote   = "From the wiki, which adds new skins weeks late: the newest may be missing, or only in also_in_artwork, " +
		"names from the hero's artwork without details, which may also be an older name of a listed skin. " +
		"Supreme is the tier players call Legend. For a skin's tier or date that is not here, or upcoming skins and events, use web_search."
	dexService = "MLBBDex"
	// maxSkins keeps the answer within mlbbOutputBytes; the newest are kept.
	maxSkins     = 20
	shownPrice   = 100
	maxBaseStats = 15
	// artworkTypos is how many letters an artwork name may differ from a
	// listed skin and still be taken for it: "Galatic Vanquisher".
	artworkTypos = 2
	// skinPictureLimit is well above a skin artwork's 110 KB.
	skinPictureLimit = 2 << 20
	skinJPEGQuality  = 90
	// shownPoint caps each strength or weakness in a hero's analysis.
	shownPoint = 120
	maxPoints  = 4
	// analysisNote: MLBBDex's editor writes in French.
	analysisNote = "analysis is MLBBDex's editor's view, in French: give it in the asker's language"
)

// errNotAsked stands for an answer that was not asked for, such as the
// rankings of all ranks when one rank is asked about.
var errNotAsked = errors.New("not asked")

// outfitVariant is a second outfit of one skin, such as "Chun-Li (Outfit 2)".
var outfitVariant = regexp.MustCompile(`(?i)\(\s*outfit\s*\d+\s*\)\s*$`)

// The wiki's skill texts keep some of its markup: "|e" after a label such
// as "Passive:", and a link's target joined to its text, "Prey Mark|mark".
var (
	wikiLabelEnd = regexp.MustCompile(`\|e\}?`)
	wikiLink     = regexp.MustCompile(`(\S)\|(\S)`)
)

func skillText(text string) string {
	return cleanText(wikiLink.ReplaceAllString(wikiLabelEnd.ReplaceAllString(text, ""), "$1 $2"), shownSkill)
}

type mlbbSkin struct {
	Name         string `json:"name"`
	Tier         string `json:"tier,omitempty"`
	Released     string `json:"released,omitempty"`
	Availability string `json:"availability,omitempty"`
	Series       string `json:"series,omitempty"`
	Price        string `json:"price,omitempty"`
}

type mlbbSkinsOutput struct {
	Hero          string     `json:"hero"`
	Skins         []mlbbSkin `json:"skins"`
	AlsoInArtwork []string   `json:"also_in_artwork,omitempty"`
	Source        string     `json:"source"`
	Note          string     `json:"note"`
}

// mlbbAnalysis is MLBBDex's editor's view of a hero, which only some have.
type mlbbAnalysis struct {
	Summary    string   `json:"summary,omitempty"`
	Strengths  []string `json:"strengths,omitempty"`
	Weaknesses []string `json:"weaknesses,omitempty"`
}

type mlbbHeroesOutput struct {
	Role   string   `json:"role,omitempty"`
	Lane   string   `json:"lane,omitempty"`
	Count  int      `json:"count"`
	Heroes []string `json:"heroes"`
	Source string   `json:"source"`
}

type dexListHero struct {
	Slug  string   `json:"slug"`
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
	Lanes []string `json:"lanes"`
}

type dexHero struct {
	Data struct {
		Name          string              `json:"name"`
		Title         string              `json:"title"`
		Roles         []string            `json:"roles"`
		Lanes         []string            `json:"lanes"`
		Specialties   []string            `json:"specialties"`
		Release       string              `json:"release"`
		Resource      string              `json:"resource"`
		DamageType    string              `json:"damageType"`
		AttackType    string              `json:"attackType"`
		Region        string              `json:"region"`
		Ratings       map[string]*float64 `json:"ratings"`
		Stats         map[string]string   `json:"stats"`
		Skills        []dexSkill          `json:"skills"`
		Skins         []dexSkin           `json:"skins"`
		Illustrations map[string]string   `json:"illustrations"`
		Images        struct {
			Portrait string            `json:"portrait"`
			Skins    map[string]string `json:"skins"`
		} `json:"images"`
		Analysis *struct {
			Summary    string   `json:"summary"`
			Strengths  []string `json:"strengths"`
			Weaknesses []string `json:"weaknesses"`
		} `json:"analysis"`
	} `json:"data"`
}

type dexSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type dexRankings struct {
	MeasuredAt string    `json:"measuredAt"`
	Heroes     []dexRank `json:"heroes"`
}

// dexRank is a hero's rates in percent, such as 52.3, and MLBBDex's tier
// from them. LowSample marks a hero with too few games to go by.
type dexRank struct {
	Name      string  `json:"name"`
	Tier      string  `json:"tier"`
	WinRate   float64 `json:"winRate"`
	BanRate   float64 `json:"banRate"`
	PickRate  float64 `json:"pickRate"`
	LowSample bool    `json:"lowSample"`
}

type dexSkin struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Release      string            `json:"release"`
	Availability string            `json:"availability"`
	Rarity       string            `json:"rarity"`
	Label        string            `json:"label"`
	Price        map[string]string `json:"price"`
}

// mlbbSkins lists a hero's skins and, when skin names one, sends its
// picture.
func (a *apis) mlbbSkins(ctx context.Context, name, skin string) (mlbbSkinsOutput, error) {
	hero, err := a.dexHeroByName(ctx, name)
	if err != nil {
		return mlbbSkinsOutput{}, err
	}
	note := dexNote
	if skin != "" {
		note = a.sendSkinPicture(ctx, hero, skin)
	}
	skins := slices.Clone(hero.Data.Skins)
	// Newest first, so a long list loses the oldest: "2016" sorts before
	// "2022-01-22" as text.
	slices.SortStableFunc(skins, func(x, y dexSkin) int { return strings.Compare(y.Release, x.Release) })
	out := mlbbSkinsOutput{Hero: hero.Data.Name, Skins: []mlbbSkin{}, Source: dexSource, Note: note}
	for _, s := range skins[:min(len(skins), maxSkins)] {
		out.Skins = append(out.Skins, mlbbSkin{
			Name:         cleanText(s.Name, maxHeroName),
			Tier:         skinTier(s.Rarity),
			Released:     cleanText(s.Release, maxHeroName),
			Availability: cleanText(s.Availability, maxHeroName),
			Series:       cleanText(s.Label, maxHeroName),
			Price:        skinPrice(s.Price),
		})
	}
	out.AlsoInArtwork = artworkOnly(hero)
	return out, nil
}

// sendSkinPicture puts the named skin's artwork, or its smaller in-game
// picture, into the turn's sink as a JPEG, Telegram showing WebP photos
// poorly, and returns the note that says whether it will be sent.
func (a *apis) sendSkinPicture(ctx context.Context, hero dexHero, skin string) string {
	sink, ok := tools.SinkFrom(ctx)
	path := skinPicturePath(hero, skin)
	if !ok || path == "" {
		return dexNote + " No picture of a skin called " + cleanText(skin, maxHeroName) + " was found, so none is sent: say so plainly, never as if one were."
	}
	data, err := a.patient.get(ctx, call{service: dexService, url: a.urls.MLBBDex + path, limit: skinPictureLimit})
	if err == nil {
		data, err = asJPEG(data)
	}
	if err != nil {
		return dexNote + " The skin's picture could not be fetched, so none is sent: say so plainly, never as if one were."
	}
	sink.Add(tools.Drawing{Data: data, MimeType: "image/jpeg"})
	return dexNote + " The skin's picture is sent with your answer, which becomes its caption: keep the answer short."
}

// skinPicturePath finds a skin's picture by its name however it is
// written: the artwork, or else the in-game picture of a listed skin.
func skinPicturePath(hero dexHero, skin string) string {
	wanted := normalizeName(skin)
	for name, path := range hero.Data.Illustrations {
		if normalizeName(name) == wanted && strings.HasPrefix(path, "/") {
			return path
		}
	}
	for _, s := range hero.Data.Skins {
		if path := hero.Data.Images.Skins[s.ID]; normalizeName(s.Name) == wanted && strings.HasPrefix(path, "/") {
			return path
		}
	}
	return ""
}

// asJPEG turns a WebP picture into a JPEG and leaves any other as it is.
func asJPEG(data []byte) ([]byte, error) {
	if http.DetectContentType(data) != "image/webp" {
		return data, nil
	}
	picture, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, picture, &jpeg.Options{Quality: skinJPEGQuality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// applyDexProfile puts MLBBDex's profile of a hero over Rone Arena's,
// keeping Rone Arena's wherever MLBBDex has nothing.
func applyDexProfile(out *mlbbHeroOutput, hero dexHero) {
	d := hero.Data
	out.Title = cleanText(d.Title, maxHeroName)
	out.Roles = orKept(nonEmpty(d.Roles), out.Roles)
	out.Lanes = orKept(nonEmpty(d.Lanes), out.Lanes)
	out.Specialities = orKept(nonEmpty(d.Specialties), out.Specialities)
	out.Released = cleanText(d.Release, maxHeroName)
	out.DamageType = cleanText(d.DamageType, maxHeroName)
	out.AttackType = cleanText(d.AttackType, maxHeroName)
	out.Resource = cleanText(d.Resource, maxHeroName)
	out.Region = cleanText(d.Region, maxHeroName)
	out.Ratings = map[string]string{}
	for key, value := range d.Ratings {
		if value == nil {
			continue
		}
		rating := fmt.Sprintf("%g/10", *value)
		if key == "difficulty" {
			out.Difficulty = rating
			continue
		}
		out.Ratings[cleanText(key, maxHeroName)] = rating
	}
	out.BaseStats = map[string]string{}
	for key, value := range d.Stats {
		if value = cleanText(value, maxHeroName); value != "" && len(out.BaseStats) < maxBaseStats {
			out.BaseStats[cleanText(key, maxHeroName)] = value
		}
	}
	var skills []mlbbSkill
	for _, skill := range d.Skills {
		if name := cleanText(skill.Name, maxHeroName); name != "" {
			skills = append(skills, mlbbSkill{Name: name, Description: skillText(skill.Description)})
		}
	}
	out.Skills = orKept(skills, out.Skills)
	if d.Analysis != nil {
		out.Analysis = &mlbbAnalysis{
			Summary:    cleanText(d.Analysis.Summary, shownTip),
			Strengths:  points(d.Analysis.Strengths),
			Weaknesses: points(d.Analysis.Weaknesses),
		}
		out.Note += "; " + analysisNote
	}
}

func points(texts []string) []string {
	var out []string
	for _, t := range texts[:min(len(texts), maxPoints)] {
		if t = cleanText(t, shownPoint); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// dexMeta gives the top heroes of all ranks by sort, leaving out those
// with too few games, whose rates mean little.
func (a *apis) dexMeta(ctx context.Context, sort string) (mlbbMetaOutput, error) {
	rankings, err := a.dexRankings(ctx)
	if err != nil {
		return mlbbMetaOutput{}, err
	}
	rate := map[string]func(dexRank) float64{
		"win_rate":  func(r dexRank) float64 { return r.WinRate },
		"ban_rate":  func(r dexRank) float64 { return r.BanRate },
		"pick_rate": func(r dexRank) float64 { return r.PickRate },
	}[sort]
	heroes := slices.DeleteFunc(slices.Clone(rankings.Heroes), func(r dexRank) bool { return r.LowSample })
	if len(heroes) == 0 {
		return mlbbMetaOutput{}, fmt.Errorf("%s has no rankings", dexService)
	}
	slices.SortStableFunc(heroes, func(x, y dexRank) int { return cmp.Compare(rate(y), rate(x)) })
	out := mlbbMetaOutput{Rank: "all", SortedBy: sort, Measured: rankings.measured(), Source: dexSource,
		Note: "ranked games of all ranks, measured on the date given; the tier is MLBBDex's own, from these rates; heroes with too few games are left out"}
	for _, r := range heroes[:min(len(heroes), mlbbMetaHeroes)] {
		out.Heroes = append(out.Heroes, r.rates())
	}
	return out, nil
}

func (a *apis) dexRankings(ctx context.Context) (dexRankings, error) {
	return remember(a.cache, "mlbbdex:rankings", mlbbTTL, func() (dexRankings, error) {
		var answer struct {
			Data dexRankings `json:"data"`
		}
		err := a.patient.getJSON(ctx, call{service: dexService, url: a.urls.MLBBDex + "/api/v1/rankings", limit: largeBodyLimit}, &answer)
		return answer.Data, err
	})
}

// find returns the named hero's rates and the day they were measured.
func (r dexRankings) find(name string) (mlbbRates, string, bool) {
	wanted := normalizeName(name)
	i := slices.IndexFunc(r.Heroes, func(h dexRank) bool { return normalizeName(h.Name) == wanted })
	if i < 0 {
		return mlbbRates{}, "", false
	}
	return r.Heroes[i].rates(), r.measured(), true
}

// measured is the day the rankings were measured, such as 2026-10-19.
func (r dexRankings) measured() string {
	return cleanText(r.MeasuredAt[:min(len(r.MeasuredAt), len("2006-01-02"))], maxHeroName)
}

func (r dexRank) rates() mlbbRates {
	return mlbbRates{
		Hero: cleanText(r.Name, maxHeroName), Tier: cleanText(r.Tier, maxHeroName),
		WinRate: percent(r.WinRate / 100), PickRate: percent(r.PickRate / 100), BanRate: percent(r.BanRate / 100),
	}
}

// mlbbHeroList names the heroes of a role and a lane, either of which may
// be empty for any.
func (a *apis) mlbbHeroList(ctx context.Context, role, lane string) (mlbbHeroesOutput, error) {
	if role != "" && !containsFold(mlbbRoles, role) {
		return mlbbHeroesOutput{}, argumentError("role must be one of %v", mlbbRoles)
	}
	if lane != "" && !slices.Contains(mlbbLanes, lane) {
		return mlbbHeroesOutput{}, argumentError("lane must be one of %v", mlbbLanes)
	}
	heroes, err := a.dexHeroes(ctx)
	if err != nil {
		return mlbbHeroesOutput{}, err
	}
	out := mlbbHeroesOutput{Role: role, Lane: lane, Heroes: []string{}, Source: dexSource}
	for _, h := range heroes {
		if (role == "" || containsFold(h.Roles, role)) && (lane == "" || containsFold(h.Lanes, lane)) {
			out.Heroes = append(out.Heroes, cleanText(h.Name, maxHeroName))
		}
	}
	slices.Sort(out.Heroes)
	out.Count = len(out.Heroes)
	return out, nil
}

func containsFold(list []string, s string) bool {
	return slices.ContainsFunc(list, func(item string) bool { return strings.EqualFold(item, s) })
}

func (a *apis) dexHeroes(ctx context.Context) ([]dexListHero, error) {
	return remember(a.cache, "mlbbdex:heroes", mlbbHeroesTTL, func() ([]dexListHero, error) {
		var answer struct {
			Data []dexListHero `json:"data"`
		}
		err := a.patient.getJSON(ctx, call{service: dexService, url: a.urls.MLBBDex + "/api/v1/heroes", limit: largeBodyLimit}, &answer)
		return answer.Data, err
	})
}

func (a *apis) dexHeroByName(ctx context.Context, name string) (dexHero, error) {
	if name == "" || len([]rune(name)) > maxHeroName {
		return dexHero{}, argumentError("hero must be a name of at most %d characters", maxHeroName)
	}
	heroes, err := a.dexHeroes(ctx)
	if err != nil {
		return dexHero{}, err
	}
	found, err := matchHero(dexNames(heroes), name)
	if err != nil {
		return dexHero{}, err
	}
	i := slices.IndexFunc(heroes, func(h dexListHero) bool { return h.Name == found })
	slug := heroes[i].Slug
	return remember(a.cache, "mlbbdex:hero:"+slug, mlbbHeroesTTL, func() (dexHero, error) {
		var answer dexHero
		err := a.patient.getJSON(ctx, call{service: dexService, url: a.urls.MLBBDex + "/api/v1/heroes/" + slug, limit: largeBodyLimit}, &answer)
		return answer, err
	})
}

// skinTier names the wiki's rarity as players do: its top tier, Supreme,
// is what the game long called Legend.
func skinTier(rarity string) string {
	rarity = cleanText(rarity, maxHeroName)
	if strings.EqualFold(rarity, "Supreme") {
		return "Legend (Supreme)"
	}
	return rarity
}

func skinPrice(price map[string]string) string {
	var parts []string
	if dm := cleanText(price["dm"], maxHeroName); dm != "" {
		parts = append(parts, dm+" diamonds")
	}
	if bp := cleanText(price["bp"], maxHeroName); bp != "" {
		parts = append(parts, bp+" battle points")
	}
	if other := cleanText(strings.NewReplacer("[[", "", "]]", "").Replace(price["other"]), shownPrice); other != "" {
		parts = append(parts, other)
	}
	return strings.Join(parts, " or ")
}

// artworkOnly is the hero's artwork names that no listed skin has, which is
// where the wiki shows a new skin first, leaving out second outfits and
// names a typo away from a listed skin.
func artworkOnly(hero dexHero) []string {
	var listed []string
	for _, s := range hero.Data.Skins {
		listed = append(listed, normalizeName(s.Name))
	}
	var out []string
	for name := range hero.Data.Illustrations {
		if outfitVariant.MatchString(name) {
			continue
		}
		key := normalizeName(name)
		if !slices.ContainsFunc(listed, func(skin string) bool { return editDistance(key, skin) <= artworkTypos }) {
			out = append(out, cleanText(name, maxHeroName))
		}
	}
	slices.Sort(out)
	return out
}

// editDistance is the number of letters to add, remove or change to turn a
// into b.
func editDistance(a, b string) int {
	x, y := []rune(a), []rune(b)
	row := make([]int, len(y)+1)
	for j := range row {
		row[j] = j
	}
	for i := 1; i <= len(x); i++ {
		diagonal := row[0]
		row[0] = i
		for j := 1; j <= len(y); j++ {
			cost := 1
			if x[i-1] == y[j-1] {
				cost = 0
			}
			diagonal, row[j] = row[j], min(row[j]+1, row[j-1]+1, diagonal+cost)
		}
	}
	return row[len(y)]
}

// orKept is list, or kept when list is empty.
func orKept[T any](list, kept []T) []T {
	if len(list) == 0 {
		return kept
	}
	return list
}

func dexNames(heroes []dexListHero) []string {
	names := make([]string, len(heroes))
	for i, h := range heroes {
		names[i] = h.Name
	}
	return names
}
