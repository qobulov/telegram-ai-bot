package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/tools"
)

const (
	// dexLingCounters: Ling beats Miya and a hero the list does not know,
	// and loses to Franco; at mythic, to Tigreal.
	dexLingCounters = `{"data":{
		"all":{"strong":[{"slug":"miya","advantage":3.7},{"slug":"unknown","advantage":2}],"weak":[{"slug":"franco","advantage":-4.9}],"winRate":50},
		"mythic":{"strong":[],"weak":[{"slug":"tigreal","advantage":-2}],"winRate":51}}}`
	// dexLingBuilds keeps the more played build first, as MLBBDex does.
	dexLingBuilds = `{"data":{"Jungle":{
		"all":[{"items":["Hunter Strike","War Axe","Endless Battle"],"emblem":"Assassin","talents":["Fatal","Seasoned Hunter"],"spell":"Retribution","winRate":58.31,"pickRate":36.04},
			{"items":["Blade of Despair"],"emblem":"Fighter","talents":[],"spell":"Retribution","winRate":50,"pickRate":5}],
		"mythic":[{"items":["Corrosion Scythe"],"emblem":"Assassin","talents":["Fatal"],"spell":"Retribution","winRate":55,"pickRate":20}]}}}`
)

func TestMLBBRefusesBadArguments(t *testing.T) {
	u := newUpstream(t, nil)
	for args, word := range map[string]string{
		`{"action":"tierlist"}`:                         "action",
		`{"action":"build","hero":"Ling","lane":"top"}`: "lane",
		`{"action":"meta","rank":"grandmaster"}`:        "rank",
		`{"action":"meta","sort":"kda"}`:                "sort",
	} {
		wantError(t, run(t, u.options(), "mlbb", args), "invalid_arguments", word)
	}
	if u.total() != 0 {
		t.Errorf("bad arguments reached the API %d times", u.total())
	}
}

// MLBBDex failing is an error the model can tell, not an empty answer.
func TestMLBBUpstreamTrouble(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/dex/api/v1/heroes": status(503), "/dex/api/v1/rankings": status(503)})
	if env := run(t, u.options(), "mlbb", `{"action":"hero","hero":"Ling"}`); env.OK {
		t.Errorf("a 503 was taken for a hero: %s", env.raw)
	}
	if env := run(t, u.options(), "mlbb", `{"action":"meta"}`); env.OK {
		t.Errorf("a 503 was taken for an answer: %s", env.raw)
	}
}

func TestMLBBPlayerGivesNicknameAndCountry(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/isan/nickname/ml": ok(`{"success":true,"game":"Mobile Legends: Bang Bang","id":1407157025,"server":15832,"name":"ᴀᴢɪᴢʙᴇᴋ‮","country":"Uzbekistan"}`),
	})

	out := result[mlbbPlayerOutput](t, run(t, u.options(), "mlbb", `{"action":"player","player_id":"1407157025","zone":"15832"}`))

	if out.Nickname != "ᴀᴢɪᴢʙᴇᴋ" || out.Country != "Uzbekistan" || out.PlayerID != "1407157025" || out.Zone != "15832" || out.Source == "" {
		t.Errorf("player = %+v, want the nickname without its invisible override", out)
	}
	if q := u.requests("/isan/nickname/ml")[0].URL.Query(); q.Get("id") != "1407157025" || q.Get("zone") != "15832" {
		t.Errorf("query = %v", q)
	}
}

// The service answers a wrong ID with a 500, as it does its own failure;
// the model is told it cannot tell them apart.
func TestMLBBPlayerNotFound(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/isan/nickname/ml":        status(500),
		"/isan/nickname/ml?zone=2": ok(`{"success":false}`),
	})
	if env := run(t, u.options(), "mlbb", `{"action":"player","player_id":"12345","zone":"2"}`); env.OK || !strings.Contains(env.raw, "no account found") || !strings.Contains(env.raw, "check both") {
		t.Errorf("not found: %s", env.raw)
	}
	// The lookup being down is not the account missing.
	if env := run(t, u.options(), "mlbb", `{"action":"player","player_id":"12345","zone":"1"}`); env.OK || strings.Contains(env.raw, "no account found") || !strings.Contains(env.raw, "not working right now") {
		t.Errorf("lookup down: %s", env.raw)
	}
}

func TestMLBBPlayerRefusesBadIDs(t *testing.T) {
	u := newUpstream(t, nil)
	for _, args := range []string{
		`{"action":"player","player_id":"1407157025"}`,
		`{"action":"player","player_id":"14071 57025","zone":"15832"}`,
		`{"action":"player","player_id":"1407157025","zone":"(15832)"}`,
		`{"action":"player","player_id":"../admin","zone":"1"}`,
	} {
		wantError(t, run(t, u.options(), "mlbb", args), "invalid_arguments", "player_id")
	}
	if u.total() != 0 {
		t.Errorf("bad IDs reached the service %d times", u.total())
	}
}

// Asked for, the hero's portrait goes into the turn's sink to be sent as a
// photo; one that cannot be fetched leaves the answer and says so.
func TestMLBBHeroPicture(t *testing.T) {
	for name, picture := range map[string]stub{"sent": ok("JPEG"), "missing": status(404)} {
		t.Run(name, func(t *testing.T) {
			u := newUpstream(t, map[string]stub{
				"/dex/api/v1/heroes":      ok(dexLingList),
				"/dex/api/v1/heroes/ling": ok(dexLing),
				"/dex/visuels/ling.webp":  picture,
			})
			sink := &tools.Sink{}
			ctx := tools.WithTurn(context.Background(), tools.Caller{UserID: 1, ChatID: -100, Human: true}, sink)
			var env envelope
			if err := json.Unmarshal([]byte(newRegistry(t, u.options()).Execute(ctx, 1, "mlbb", `{"action":"hero","hero":"Ling","picture":true}`)), &env); err != nil {
				t.Fatal(err)
			}
			env.raw = string(env.Result)
			out := result[mlbbHeroOutput](t, env)
			drawings := sink.Take()
			if name == "sent" && (len(drawings) != 1 || string(drawings[0].Data) != "JPEG" || !strings.Contains(out.Note, "picture is sent")) {
				t.Errorf("drawings = %d, note = %q", len(drawings), out.Note)
			}
			if name == "missing" && (len(drawings) != 0 || !strings.Contains(out.Note, "could not be fetched") || out.Hero != "Ling") {
				t.Errorf("drawings = %d, output = %+v", len(drawings), out)
			}
		})
	}
}

const (
	dexHeroList = `{"data":[{"slug":"ling","name":"Ling"},{"slug":"guinevere","name":"Guinevere"},{"slug":"yi-sun-shin","name":"Yi Sun-shin"}],"total":3}`
	// dexGuinevere has a Supreme skin, one priced in diamonds and battle
	// points, and artwork with a second outfit, a misspelt skin and a new
	// skin the wiki has not listed yet.
	dexGuinevere = `{"data":{"slug":"guinevere","name":"Guinevere","release":"2019",
		"stats":{"hp1":"2468","hp15":"5210","movement_spd":"255","magic_def1":""},
		"skins":[
			{"id":"801","name":"Ms. Violet","release":"2019","availability":"Available","rarity":null,"label":null,"price":{"bp":"32000","dm":"599"}},
			{"id":"8010","name":"Psion of Tomorrow","release":"2022-04-25","availability":"Limited","rarity":"Supreme","label":"Legend","price":{"other":"Obtained via the [[Galactic Starhawk]] event"}},
			{"id":"8011","name":"Chun-Li","release":"2025-05-24","availability":"Limited","rarity":"Grand","label":"Street Fighter","price":{}}],
		"illustrations":{"Ms. Violet":"/a.webp","Psion of Tomorow":"/b.webp","Chun-Li":"/c.webp","Chun-Li (Outfit 2)":"/d.webp","Daybreak Halo":"/e.webp"}},
		"source":{"name":"Mobile Legends Wiki"}}`
)

func TestMLBBSkins(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/dex/api/v1/heroes":           ok(dexHeroList),
		"/dex/api/v1/heroes/guinevere": ok(dexGuinevere),
	})

	out := result[mlbbSkinsOutput](t, run(t, u.options(), "mlbb", `{"action":"skins","hero":"GUINEVERE"}`))

	want := []mlbbSkin{
		{Name: "Chun-Li", Tier: "Grand", Released: "2025-05-24", Availability: "Limited", Series: "Street Fighter"},
		{Name: "Psion of Tomorrow", Tier: "Legend (Supreme)", Released: "2022-04-25", Availability: "Limited", Series: "Legend", Price: "Obtained via the Galactic Starhawk event"},
		{Name: "Ms. Violet", Released: "2019", Availability: "Available", Price: "599 diamonds or 32000 battle points"},
	}
	if !slices.Equal(out.Skins, want) || out.Hero != "Guinevere" {
		t.Errorf("skins = %+v, want newest first, Supreme named Legend", out.Skins)
	}
	if !slices.Equal(out.AlsoInArtwork, []string{"Daybreak Halo"}) {
		t.Errorf("also in artwork = %q, want the unlisted skin alone, not a second outfit or a typo", out.AlsoInArtwork)
	}
	if !strings.Contains(out.Note, "weeks late") || out.Source == "" {
		t.Errorf("note = %q, source = %q", out.Note, out.Source)
	}
}

func TestMLBBSkinsUnknownHero(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/dex/api/v1/heroes": ok(dexHeroList)})
	wantError(t, run(t, u.options(), "mlbb", `{"action":"skins","hero":"Gatotkaca"}`), "invalid_arguments", "closest")
	wantError(t, run(t, u.options(), "mlbb", `{"action":"skins"}`), "invalid_arguments", "hero")
}

// The hero answer gives MLBBDex's release date and base stats.
func TestMLBBHeroAddsReleaseAndBaseStats(t *testing.T) {
	routes := map[string]stub{
		"/dex/api/v1/heroes":             ok(dexHeroList),
		"/dex/api/v1/heroes/yi-sun-shin": ok(strings.Replace(dexGuinevere, `"name":"Guinevere"`, `"name":"Yi Sun-shin"`, 1)),
	}
	out := result[mlbbHeroOutput](t, run(t, newUpstream(t, routes).options(), "mlbb", `{"action":"hero","hero":"yi sun shin"}`))
	if out.Released != "2019" || out.BaseStats["hp15"] != "5210" || len(out.BaseStats) != 3 || out.Hero != "Yi Sun-shin" {
		t.Errorf("released = %q, base stats = %v, want the empty stat left out", out.Released, out.BaseStats)
	}
}

func TestEditDistance(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"", "", 0}, {"galaticvanquisher", "galacticvanquisher", 1}, {"arrowofspring", "arrowofsprings", 1}, {"kitten", "sitting", 3}} {
		if got := editDistance(c.a, c.b); got != c.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

const (
	// arenaLingBuilds names a spell, an emblem, talents and items by ID,
	// the less used build first; 9999 is an item the list does not know.
	arenaLingBuilds = `{"code":0,"data":{"records":[{"data":{"heroid":84,"build":[
		{"battleskill":{"data":{"__data":{"skillname":"Flicker"}}},"emblem":{"data":{"emblemname":"Fighter"}},
		 "new_rune_skill":[],"equipid":[3002],"build_win_rate":0.5,"build_pick_rate":0.0501},
		{"battleskill":{"data":{"__data":{"skillname":"Retribution"}}},"emblem":{"data":{"emblemname":"Assassin"}},
		 "new_rune_skill":[1211,122],"equipid":[3003,3002,9999],"build_win_rate":0.5831,"build_pick_rate":0.3604}]}}]}}`
	arenaItems   = `{"code":0,"data":{"records":[{"data":{"equipid":3003,"equipname":"Berserker's Fury"}},{"data":{"equipid":3002,"equipname":"Haas' Claws"}}]}}`
	arenaEmblems = `{"code":0,"data":{"records":[{"data":{"giftid":1211,"emblemskill":{"skillname":"Fatal"}}},{"data":{"giftid":122,"emblemskill":{"skillname":"Seasoned Hunter"}}}]}}`
)

// The build is the academy's, for the hero MLBBDex names and its main
// lane, asked by name; an item the academy's list does not know is left out.
func TestMLBBBuild(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/dex/api/v1/heroes":                    ok(dexLingList),
		"/arena/api/academy/heroes/Ling/builds": ok(arenaLingBuilds),
		"/arena/api/academy/equipment":          ok(arenaItems),
		"/arena/api/academy/emblems":            ok(arenaEmblems),
	})

	out := result[mlbbBuildOutput](t, run(t, u.options(), "mlbb", `{"action":"build","hero":"ling","rank":"mythic"}`))

	want := mlbbBuild{Spell: "Retribution", Emblem: "Assassin", Talents: []string{"Fatal", "Seasoned Hunter"}, Items: []string{"Berserker's Fury", "Haas' Claws"}, WinRate: "58.3%", PickRate: "36.0%"}
	if len(out.Builds) != 2 || !reflect.DeepEqual(out.Builds[0], want) || out.Builds[1].Spell != "Flicker" || out.Hero != "Ling" || out.Lane != "jungle" || out.Rank != "mythic" {
		t.Errorf("build = %+v, want the hero's main lane and the unknown item left out", out)
	}
	if q := u.requests("/arena/api/academy/heroes/Ling/builds")[0].URL.Query(); q.Get("lane") != "jungle" || q.Get("rank") != "mythic" {
		t.Errorf("query = %v", q)
	}
}

// With the academy down, the builds are MLBBDex's, as text, for the hero's
// main lane and the rank, the more played first; a lane with none is an
// error.
func TestMLBBBuildFromMLBBDexWhenTheAcademyFails(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/dex/api/v1/heroes": ok(dexLingList), "/dex/api/v1/heroes/ling/builds": ok(dexLingBuilds)})

	out := result[mlbbBuildOutput](t, run(t, u.options(), "mlbb", `{"action":"build","hero":"ling"}`))

	want := mlbbBuild{Spell: "Retribution", Emblem: "Assassin", Talents: []string{"Fatal", "Seasoned Hunter"}, Items: []string{"Hunter Strike", "War Axe", "Endless Battle"}, WinRate: "58.3%", PickRate: "36.0%"}
	if len(out.Builds) != 2 || !reflect.DeepEqual(out.Builds[0], want) || out.Hero != "Ling" || out.Lane != "jungle" || out.Rank != "all" {
		t.Errorf("build = %+v, want the hero's main lane", out)
	}
	if out := result[mlbbBuildOutput](t, run(t, u.options(), "mlbb", `{"action":"build","hero":"Ling","rank":"mythic"}`)); len(out.Builds) != 1 || out.Builds[0].Items[0] != "Corrosion Scythe" {
		t.Errorf("mythic build = %+v", out)
	}
	if env := run(t, u.options(), "mlbb", `{"action":"build","hero":"Ling","lane":"gold"}`); env.OK || !strings.Contains(env.raw, "no builds") {
		t.Errorf("no builds: %s", env.raw)
	}
}

// The first build goes as one PNG, even when its icons will not load; the
// test server's icons are not https, so they are left blank.
func TestMLBBBuildSendsOnePicture(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/dex/api/v1/heroes":                    ok(dexLingList),
		"/arena/api/academy/heroes/Ling/builds": ok(arenaLingBuilds),
		"/arena/api/academy/equipment":          ok(arenaItems),
		"/arena/api/academy/emblems":            ok(arenaEmblems),
	})
	sink := &tools.Sink{}
	ctx := tools.WithTurn(context.Background(), tools.Caller{UserID: 1, ChatID: -100, Human: true}, sink)
	var env envelope
	if err := json.Unmarshal([]byte(newRegistry(t, u.options()).Execute(ctx, 1, "mlbb", `{"action":"build","hero":"Ling","lane":"jungle"}`)), &env); err != nil {
		t.Fatal(err)
	}
	env.raw = string(env.Result)
	out := result[mlbbBuildOutput](t, env)
	drawings := sink.Take()
	if len(drawings) != 1 || drawings[0].MimeType != "image/png" || !strings.Contains(out.Note, "one picture") {
		t.Fatalf("drawings = %d, note = %q", len(drawings), out.Note)
	}
	card, err := png.Decode(bytes.NewReader(drawings[0].Data))
	// Spell, emblem and two talents make the widest row.
	if err != nil || card.Bounds().Dx() != 2*cardPadding+4*cardCell {
		t.Errorf("card = %v, %v", card.Bounds(), err)
	}
}

func TestDrawBuildCard(t *testing.T) {
	red := image.NewRGBA(image.Rect(0, 0, 100, 100))
	draw.Draw(red, red.Bounds(), image.NewUniform(color.RGBA{R: 0xff, A: 0xff}), image.Point{}, draw.Src)
	data, err := drawBuildCard(buildCard{Title: "Ling · Jungle", Subtitle: "Win rate 58.3%", Rows: []cardRow{
		{Heading: "Core items", Icons: []cardIcon{{Name: "Berserker's Fury", Picture: red}, {Name: "A name long enough to need two lines and more"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	card, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	top := cardPadding + cardTitle + cardHeading
	centre := func(cell int) color.Color {
		return card.At(cardPadding+cell*cardCell+cardCell/2, top+cardIconSize/2)
	}
	if r, g, b, _ := centre(0).RGBA(); r>>8 != 0xff || g != 0 || b != 0 {
		t.Errorf("the icon's centre is %v, want red", centre(0))
	}
	if r, g, b, _ := centre(1).RGBA(); uint8(r>>8) != cardMissing.R || uint8(g>>8) != cardMissing.G || uint8(b>>8) != cardMissing.B {
		t.Errorf("the missing icon's centre is %v, want the blank colour", centre(1))
	}
}

// The boots and the last two items are the ones most guides add besides
// the core; guides with too few items do not count, and too few guides
// give nothing.
func TestFinishing(t *testing.T) {
	items := map[int]arenaEntry{1: {Name: "Core"}, 2: {Name: "Swift Boots"}, 3: {Name: "Tough Boots"}, 4: {Name: "Malefic Gun"}, 5: {Name: "Sea Halberd"}, 6: {Name: "Rose Gold Meteor"}, 7: {Name: "Immortality"}}
	guide := func(ids ...int) arenaGuide {
		var g arenaGuide
		g.Data.Equips = append(g.Data.Equips, struct {
			IDs []int `json:"equip_ids"`
		}{IDs: ids})
		return g
	}
	guides := []arenaGuide{
		guide(1, 2, 4, 5, 6, 7), guide(1, 2, 4, 5, 6, 7), guide(1, 2, 4, 5, 7, 7),
		guide(1, 3, 4, 6, 7, 5), guide(1, 2, 4, 6, 5, 3), guide(2, 4),
	}
	finish, counted := finishing([]int{1}, guides, items)
	if !slices.Equal(finish, []int{2, 4, 5}) || counted != 5 {
		t.Errorf("finishing = %v from %d guides, want Swift Boots, Malefic Gun, Sea Halberd from 5", finish, counted)
	}
	if finish, counted := finishing([]int{1}, guides[:4], items); finish != nil || counted != 0 {
		t.Errorf("four guides gave %v", finish)
	}
}

func TestMainLane(t *testing.T) {
	for labels, want := range map[string]string{"Jungle": "jungle", "Gold": "gold", "Exp": "exp", "Mid": "mid", "Roam": "roam", "": "jungle"} {
		if got := mainLane([]string{labels, ""}); got != want {
			t.Errorf("mainLane(%q) = %q, want %q", labels, got, want)
		}
	}
}

// A skin asked for by name is sent as a JPEG, from its artwork; one with
// no picture leaves the list and says so.
func TestMLBBSkinPicture(t *testing.T) {
	var webpPicture bytes.Buffer
	// The smallest lossless WebP: one white pixel.
	webpPicture.Write([]byte{0x52, 0x49, 0x46, 0x46, 0x1a, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50, 0x56, 0x50, 0x38, 0x4c,
		0x0d, 0x00, 0x00, 0x00, 0x2f, 0x00, 0x00, 0x00, 0x10, 0x07, 0x10, 0x11, 0x11, 0x88, 0x88, 0xfe, 0x07, 0x00})
	for skin, sent := range map[string]bool{"daybreak halo": true, "Lady Crane": false} {
		u := newUpstream(t, map[string]stub{
			"/dex/api/v1/heroes":           ok(dexHeroList),
			"/dex/api/v1/heroes/guinevere": ok(dexGuinevere),
			"/dex/e.webp":                  ok(webpPicture.String()),
		})
		sink := &tools.Sink{}
		ctx := tools.WithTurn(context.Background(), tools.Caller{UserID: 1, ChatID: -100, Human: true}, sink)
		var env envelope
		if err := json.Unmarshal([]byte(newRegistry(t, u.options()).Execute(ctx, 1, "mlbb", `{"action":"skins","hero":"Guinevere","skin":"`+skin+`"}`)), &env); err != nil {
			t.Fatal(err)
		}
		env.raw = string(env.Result)
		out := result[mlbbSkinsOutput](t, env)
		drawings := sink.Take()
		if sent && (len(drawings) != 1 || drawings[0].MimeType != "image/jpeg" || http.DetectContentType(drawings[0].Data) != "image/jpeg" || !strings.Contains(out.Note, "picture is sent")) {
			t.Errorf("%s: drawings = %d, note = %q; want one JPEG", skin, len(drawings), out.Note)
		}
		if !sent && (len(drawings) != 0 || !strings.Contains(out.Note, "No picture") || len(out.Skins) != 3) {
			t.Errorf("%s: drawings = %d, output = %+v", skin, len(drawings), out)
		}
	}
}

const (
	dexLingList = `{"data":[{"slug":"ling","name":"Ling","roles":["Assassin"],"lanes":["Jungle"]},
		{"slug":"tigreal","name":"Tigreal","roles":["Tank"],"lanes":["Roam"]},
		{"slug":"franco","name":"Franco","roles":["Tank"],"lanes":["Roam"]},
		{"slug":"miya","name":"Miya","roles":["Marksman"],"lanes":["Gold"]}]}`
	dexLing = `{"data":{"slug":"ling","name":"Ling","title":"Cloudwalker","roles":["Assassin"],"lanes":["Jungle"],"specialties":["Chase","Burst"],
		"release":"2019","resource":"Energy","damageType":"Physical","attackType":"Melee","region":"Cadia Riverlands",
		"ratings":{"offense":8,"durability":4,"abilityEffects":null,"difficulty":9},"stats":{"hp1":"2500"},
		"skills":[{"name":"Cloud Walker","description":"Ling leaps onto walls."}],
		"images":{"portrait":"/visuels/ling.webp","skins":{}},
		"analysis":{"summary":"Assassin des murs.","strengths":["Mobile"],"weaknesses":["Energie"]}}}`
	// dexRankingsAll has Marcel on too few games to count.
	dexRankingsAll = `{"data":{"measuredAt":"2026-10-19T09:45:29.833Z","heroes":[
		{"slug":"ling","name":"Ling","tier":"A","winRate":51.2,"banRate":20.5,"pickRate":1.1,"lowSample":false},
		{"slug":"marcel","name":"Marcel","tier":"S+","winRate":58.1,"banRate":33.8,"pickRate":0.3,"lowSample":true},
		{"slug":"miya","name":"Miya","tier":"S","winRate":53.0,"banRate":5.0,"pickRate":2.0,"lowSample":false}]}}`
)

// A hero is MLBBDex's alone: its profile, its rates of all ranks and its
// counters for the rank asked.
func TestMLBBHeroFromMLBBDex(t *testing.T) {
	routes := map[string]stub{
		"/dex/api/v1/heroes":               ok(dexLingList),
		"/dex/api/v1/heroes/ling":          ok(dexLing),
		"/dex/api/v1/heroes/ling/counters": ok(dexLingCounters),
		"/dex/api/v1/rankings":             ok(dexRankingsAll),
	}
	u := newUpstream(t, routes)
	reg := newRegistry(t, u.options())
	out := result[mlbbHeroOutput](t, execute(t, reg, "mlbb", `{"action":"hero","hero":"LING"}`))

	if out.Title != "Cloudwalker" || out.DamageType != "Physical" || out.Difficulty != "9/10" || out.Ratings["offense"] != "8/10" || len(out.Ratings) != 2 {
		t.Errorf("profile = %+v, want the null rating left out", out)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "Cloud Walker" || out.Analysis == nil || out.Analysis.Summary != "Assassin des murs." || !strings.Contains(out.Note, "French") {
		t.Errorf("skills = %+v, analysis = %+v, note = %q", out.Skills, out.Analysis, out.Note)
	}
	if out.mlbbRates != (mlbbRates{Hero: "Ling", WinRate: "51.2%", PickRate: "1.1%", BanRate: "20.5%", Tier: "A"}) || out.RatesMeasured != "2026-10-19" {
		t.Errorf("rates = %+v measured %q", out.mlbbRates, out.RatesMeasured)
	}
	if !slices.Equal(out.StrongAgainst, []mlbbMatchup{{Hero: "Miya", WinRateChange: "+3.7%"}}) || !slices.Equal(out.CounteredBy, []mlbbMatchup{{Hero: "Franco", WinRateChange: "-4.9%"}}) {
		t.Errorf("counters = %+v / %+v, want the unknown hero left out", out.StrongAgainst, out.CounteredBy)
	}
	if out := result[mlbbHeroOutput](t, execute(t, reg, "mlbb", `{"action":"hero","hero":"Ling","rank":"mythic"}`)); out.Rank != "mythic" || len(out.CounteredBy) != 1 || out.CounteredBy[0].Hero != "Tigreal" || out.WinRate != "51.2%" {
		t.Errorf("mythic = %+v, want mythic counters and rates of all ranks", out)
	}
	if n := len(u.requests("/dex/api/v1/heroes/ling/counters")); n != 1 {
		t.Errorf("the counters were asked for %d times, want once, kept for the second rank", n)
	}

	// No counters: the profile stands without them.
	routes["/dex/api/v1/heroes/ling/counters"] = status(503)
	out = result[mlbbHeroOutput](t, run(t, newUpstream(t, routes).options(), "mlbb", `{"action":"hero","hero":"Ling"}`))
	if out.Title != "Cloudwalker" || len(out.CounteredBy) != 0 {
		t.Errorf("without counters = %+v", out)
	}
}

func TestMLBBMetaFromMLBBDex(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/dex/api/v1/rankings": ok(dexRankingsAll)})

	out := result[mlbbMetaOutput](t, run(t, u.options(), "mlbb", `{"action":"meta","sort":"win_rate"}`))
	if len(out.Heroes) != 2 || out.Heroes[0] != (mlbbRates{Hero: "Miya", WinRate: "53.0%", PickRate: "2.0%", BanRate: "5.0%", Tier: "S"}) || out.Measured != "2026-10-19" {
		t.Errorf("meta = %+v, want Miya first and Marcel, on too few games, left out", out)
	}
}

func TestMLBBHeroList(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/dex/api/v1/heroes": ok(dexLingList)})

	out := result[mlbbHeroesOutput](t, run(t, u.options(), "mlbb", `{"action":"heroes","role":"tank","lane":"roam"}`))
	if !slices.Equal(out.Heroes, []string{"Franco", "Tigreal"}) || out.Count != 2 {
		t.Errorf("tanks of the roam = %+v", out)
	}
	if out := result[mlbbHeroesOutput](t, run(t, u.options(), "mlbb", `{"action":"heroes"}`)); out.Count != 4 {
		t.Errorf("all heroes = %+v", out)
	}
	wantError(t, run(t, u.options(), "mlbb", `{"action":"heroes","role":"Healer"}`), "invalid_arguments", "role")
}

func TestSkillTextDropsWikiMarkup(t *testing.T) {
	for in, want := range map[string]string{
		"Passive: |e Ling's critical chance.": "Passive: Ling's critical chance.",
		"Use again|e}: Gusion moves back.":    "Use again: Gusion moves back.",
		"a stack of Prey Mark|mark each time": "a stack of Prey Mark mark each time",
		"pets and clone | summoned unit":      "pets and clone | summoned unit",
	} {
		if got := skillText(in); got != want {
			t.Errorf("skillText(%q) = %q, want %q", in, got, want)
		}
	}
}

// A hero is found in Cyrillic and with a typo, when it alone is that close;
// a name near two heroes, or near none, finds nothing.
func TestMatchHero(t *testing.T) {
	heroes := []string{"Hirara", "Harith", "Ling", "Lylia", "Yi Sun-shin", "Chang'e"}
	for name, want := range map[string]string{
		"Hirara": "Hirara", "hirara": "Hirara", "Xirara": "Hirara", "Хирара": "Hirara", "Ҳирара": "Hirara",
		"Lingg": "Ling", "Линг": "Ling", "yi sun shin": "Yi Sun-shin", "change": "Chang'e", "Чанъэ": "Chang'e", "Hirraa": "Hirara",
		"Lia": "", "Zask": "",
	} {
		got, err := matchHero(heroes, name)
		if got != want || (want == "" && err == nil) {
			t.Errorf("matchHero(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
}

func TestHeroesInWords(t *testing.T) {
	heroes := []string{"Hirara", "Harith", "Helcurt", "Joy", "Ling", "Yi Sun-shin", "Popol and Kupa", "Sora", "Fanny"}
	for words, want := range map[string]string{
		"hiraraga build ber":      "Hirara",
		"Xirara dedi":             "Hirara",
		"Хирарани расми борми":    "Hirara",
		"lingni counter kim":      "Ling",
		"yi sun shin build":       "Yi Sun-shin",
		"popol and kupa skin":     "Popol and Kupa",
		"Ling yoki Harith kuchli": "Harith,Ling",
		"build ber":               "",
		"so'ramasdan tashlayver":  "",
		"sorani skini":            "Sora",
		"fannyning legasi":        "Fanny",
		"kelinglar o'ynaymiz":     "",
	} {
		if got := strings.Join(heroesInWords(heroes, words), ","); got != want {
			t.Errorf("heroesInWords(%q) = %q, want %q", words, got, want)
		}
	}
}

// The model swapping a hero it does not know for one it does is undone
// when the asker's own words name one hero alone.
func TestMLBBUsesTheHeroTheAskerNamed(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/dex/api/v1/heroes":        ok(`{"data":[{"slug":"hirara","name":"Hirara","release":"17 June 2026"},{"slug":"joy","name":"Joy","release":"18 November 2022"}]}`),
		"/dex/api/v1/heroes/hirara": ok(`{"data":{"name":"Hirara","skins":[{"id":"1","name":"Fallen Scarlet","release":"2026-06-17"}]}}`),
	})
	ctx := tools.WithTurn(context.Background(), tools.Caller{UserID: 1, ChatID: -100, Human: true, Words: "hiraraga skin bormi"}, &tools.Sink{})
	var env envelope
	if err := json.Unmarshal([]byte(newRegistry(t, u.options()).Execute(ctx, 1, "mlbb", `{"action":"skins","hero":"Joy"}`)), &env); err != nil {
		t.Fatal(err)
	}
	env.raw = string(env.Result)
	out := result[mlbbSkinsOutput](t, env)
	if out.Hero != "Hirara" || !strings.Contains(out.Note, "names Hirara") {
		t.Errorf("skins = %+v, want Hirara's and the note saying why", out)
	}
	if len(u.requests("/dex/api/v1/heroes/joy")) != 0 {
		t.Error("Joy was asked for")
	}
}

// A hero the model knows is left as asked: "joyni counteri" may look up
// Hirara.
func TestMLBBKeepsAnotherHeroBesideAKnownOne(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/dex/api/v1/heroes":        ok(`{"data":[{"slug":"hirara","name":"Hirara","release":"17 June 2026"},{"slug":"joy","name":"Joy","release":"18 November 2022"}]}`),
		"/dex/api/v1/heroes/hirara": ok(`{"data":{"name":"Hirara","skins":[{"id":"1","name":"Fallen Scarlet","release":"2026-06-17"}]}}`),
	})
	ctx := tools.WithTurn(context.Background(), tools.Caller{UserID: 1, ChatID: -100, Human: true, Words: "joyni counteri kim"}, &tools.Sink{})
	var env envelope
	if err := json.Unmarshal([]byte(newRegistry(t, u.options()).Execute(ctx, 1, "mlbb", `{"action":"skins","hero":"Hirara"}`)), &env); err != nil {
		t.Fatal(err)
	}
	env.raw = string(env.Result)
	if out := result[mlbbSkinsOutput](t, env); out.Hero != "Hirara" || strings.Contains(out.Note, "asker's message names") {
		t.Errorf("skins = %+v, want Hirara's as asked", out)
	}
}

func TestMLBBWinrate(t *testing.T) {
	for _, c := range []struct {
		matches         int
		winRate, target float64
		want            int
	}{{500, 48, 55, 78}, {100, 50, 60, 25}, {200, 60, 55, 0}, {10, 0, 50, 10}} {
		out, err := mlbbWinrate(c.matches, c.winRate, c.target)
		if err != nil || out.WinsInARow != c.want {
			t.Errorf("mlbbWinrate(%d, %v, %v) = %d, %v; want %d", c.matches, c.winRate, c.target, out.WinsInARow, err, c.want)
		}
	}
	if _, err := mlbbWinrate(100, 50, 100); err == nil {
		t.Error("a 100% target was taken; it is never reached")
	}
}

func TestMLBBItem(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/dex/api/v1/items": ok(`{"data":[
		{"name":"Blade of Despair","summary":"Highest Physical Attack","category":"Attack","price":3010,"bonus":"+160 Physical Attack","unique":null,"passive":"Despair: more attack below 50% HP.","active":null,"recipe":["Legion Sword","Legion Sword"],"bestFor":"All heroes"},
		{"name":"Winter Truncheon","category":"Defense","price":1910}]}`)})
	out := result[mlbbItemOutput](t, run(t, u.options(), "mlbb", `{"action":"item","item":"blade of despar"}`))
	if out.Item != "Blade of Despair" || out.Price != 3010 || out.Unique != "" || len(out.Recipe) != 2 || out.Passive == "" {
		t.Errorf("item = %+v", out)
	}
	wantError(t, run(t, u.options(), "mlbb", `{"action":"item","item":"Sword"}`), "invalid_arguments", "no MLBB item")
}

// A hero the enemy loses to leads, by the points of that matchup; one the
// enemy beats is left out; a lane keeps its own heroes.
func TestMLBBDraft(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/dex/api/v1/heroes": ok(dexLingList), "/dex/api/v1/heroes/ling/counters": ok(dexLingCounters)})

	out := result[mlbbDraftOutput](t, run(t, u.options(), "mlbb", `{"action":"draft","enemies":["Ling","ling"]}`))

	if len(out.Picks) != 1 || out.Picks[0].Hero != "Franco" || out.Picks[0].Score != "+4.9%" || !slices.Equal(out.Picks[0].Why, []string{"counters Ling (+4.9%)"}) || !slices.Equal(out.Enemies, []string{"Ling"}) {
		t.Fatalf("draft = %+v, want Franco alone and Miya, whom Ling beats, left out", out)
	}
	if out := result[mlbbDraftOutput](t, run(t, u.options(), "mlbb", `{"action":"draft","enemies":["Ling"],"lane":"gold"}`)); len(out.Picks) != 0 {
		t.Errorf("gold lane picks = %+v, want none: Franco roams", out.Picks)
	}
	wantError(t, run(t, u.options(), "mlbb", `{"action":"draft"}`), "invalid_arguments", "enemy")
}

// The persona's note names the heroes of the last two years with their
// roles, and nothing when MLBBDex does not answer.
func TestNewHeroesNote(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/dex/api/v1/heroes": ok(`{"data":[
		{"slug":"marcel","name":"Marcel","title":"Soul Photographer","roles":["Support"],"specialties":["Crowd Control"],"skills":["Platinum Snap","Golden Hour","Golden Hour"],"release":"11 March 2026"},
		{"slug":"ling","name":"Ling","roles":["Assassin"],"release":"24 November 2019"},
		{"slug":"future","name":"Future","roles":["Mage"],"release":"1 January 2030"},
		{"slug":"odd","name":"Odd","roles":["Tank"],"release":"soon"}]}`)})
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

	note := NewHeroesNote(context.Background(), u.options(), now)

	if !strings.Contains(note, "Marcel, Soul Photographer (Support; Crowd Control; skills Platinum Snap, Golden Hour; March 2026)") || strings.Contains(note, "Ling") || strings.Contains(note, "Future") || strings.Contains(note, "Odd") {
		t.Errorf("note = %q", note)
	}
	if note := NewHeroesNote(context.Background(), newUpstream(t, nil).options(), now); note != "" {
		t.Errorf("without MLBBDex: %q", note)
	}
}

// The model now and then quotes a boolean; "true" is taken as true, and
// anything else than true or false still fails the call.
func TestLooseBool(t *testing.T) {
	for raw, want := range map[string]bool{`true`: true, `"true"`: true, `false`: false, `"false"`: false} {
		var got looseBool
		if err := json.Unmarshal([]byte(raw), &got); err != nil || bool(got) != want {
			t.Errorf("%s = %v, %v; want %v", raw, got, err, want)
		}
	}
	var b looseBool
	if err := json.Unmarshal([]byte(`"yes please"`), &b); err == nil {
		t.Error(`"yes please" was taken as a boolean`)
	}
}
