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

	"telegram-ai-bot/internal/tools"
)

const (
	arenaHeroes = `{"code":0,"data":{"records":[
		{"data":{"hero_id":84,"hero":{"data":{"name":"Ling"}}}},
		{"data":{"hero_id":30,"hero":{"data":{"name":"Yi Sun-shin"}}}},
		{"data":{"hero_id":69,"hero":{"data":{"name":"Hanzo"}}}},
		{"data":{"hero_id":24,"hero":{"data":{"name":"Natalia"}}}},
		{"data":{"hero_id":46,"hero":{"data":{"name":"Odette"}}}}]}}`
	// arenaLingCounters raises Ling's win rate against Hanzo and lowers it
	// against Natalia; 999 is a hero the list does not know yet.
	arenaLingCounters = `{"code":0,"data":{"records":[{"data":{
		"main_hero":{"data":{"name":"Ling"}},"main_hero_win_rate":0.527652,"main_hero_appearance_rate":0.006991,"main_hero_ban_rate":0.006833,
		"sub_hero":[{"heroid":69,"increase_win_rate":0.036573},{"heroid":999,"increase_win_rate":0.03}],
		"sub_hero_last":[{"heroid":24,"increase_win_rate":-0.049257}]}}]}}`
	arenaLingPage = `{"code":0,"data":{"records":[{"data":{
		"hero":{"data":{"name":"Ling","difficulty":"60","speciality":["Chase","Burst"],"sortlabel":["Assassin",""],"roadsortlabel":["Jungling",""],
			"story":"A graceful Finch.","heroskilllist":[{"skilllist":[
				{"skillname":"Cloud Walker","skilldesc":"Leaps among walls.","skilltag":[{"tagname":"Buff"}]},
				{"skillname":"Tempest of Blades","skilldesc":"<font color=\"a6aafb\">Ultimate</font>: Ling leaps into the air.","skilltag":[{"tagname":"Burst"},{"tagname":"CC"}]}]}]}},
		"relation":{"assist":{"desc":"Works with CC supports.","target_hero_id":[46,0]},"strong":{"desc":"Strong against squishy heroes."},"weak":{"desc":"Weak against single-target CC."}}}}]}}`
	arenaRank = `{"code":0,"data":{"records":[
		{"data":{"main_hero":{"data":{"name":"Masha"}},"main_hero_win_rate":0.60985,"main_hero_appearance_rate":0.005404,"main_hero_ban_rate":0.35482}},
		{"data":{"main_hero":{"data":{"name":"Marcel"}},"main_hero_win_rate":0.585272,"main_hero_appearance_rate":0.002,"main_hero_ban_rate":0.01}}]}}`
)

func TestMLBBHeroGivesItsPageRatesAndCounters(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/arena/api/heroes":             ok(arenaHeroes),
		"/arena/api/heroes/84/counters": ok(arenaLingCounters),
		"/arena/api/heroes/84":          ok(arenaLingPage),
	})

	out := result[mlbbHeroOutput](t, run(t, u.options(), "mlbb", `{"action":"hero","hero":"LING","rank":"mythic"}`))

	if out.mlbbRates != (mlbbRates{Hero: "Ling", WinRate: "52.8%", PickRate: "0.7%", BanRate: "0.7%"}) || out.Rank != "mythic" || out.Source == "" {
		t.Errorf("rates = %+v", out)
	}
	if len(out.StrongAgainst) != 1 || out.StrongAgainst[0] != (mlbbMatchup{Hero: "Hanzo", WinRateChange: "+3.7%"}) {
		t.Errorf("strong against = %+v, want Hanzo alone: an unknown hero is left out", out.StrongAgainst)
	}
	if len(out.CounteredBy) != 1 || out.CounteredBy[0] != (mlbbMatchup{Hero: "Natalia", WinRateChange: "-4.9%"}) {
		t.Errorf("countered by = %+v", out.CounteredBy)
	}
	if strings.Join(out.GoodPartners, ",") != "Odette" {
		t.Errorf("partners = %q, want Odette and the padding 0 left out", out.GoodPartners)
	}
	if strings.Join(out.Roles, ",") != "Assassin" || strings.Join(out.Lanes, ",") != "Jungling" || strings.Join(out.Specialities, ",") != "Chase,Burst" || out.Difficulty != "60/100" || out.Story != "A graceful Finch." {
		t.Errorf("hero = %+v, want empty labels dropped", out)
	}
	if len(out.Skills) != 2 || out.Skills[1].Name != "Tempest of Blades" || out.Skills[1].Description != "Ultimate : Ling leaps into the air." || strings.Join(out.Skills[1].Tags, ",") != "Burst,CC" {
		t.Errorf("skills = %+v, want the game's markup taken out", out.Skills)
	}
	if out.Tips != (mlbbTips{WorksWith: "Works with CC supports.", StrongAgainst: "Strong against squishy heroes.", WeakAgainst: "Weak against single-target CC."}) {
		t.Errorf("tips = %+v", out.Tips)
	}
	if q := u.requests("/arena/api/heroes/84/counters")[0].URL.Query(); q.Get("rank") != "mythic" || q.Get("days") != "7" {
		t.Errorf("query = %v", q)
	}
}

// A hero's name is found however it is written, and the hero list is asked
// for once.
func TestMLBBHeroNames(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/arena/api/heroes":             ok(arenaHeroes),
		"/arena/api/heroes/30/counters": ok(arenaLingCounters),
		"/arena/api/heroes/30":          ok(arenaLingPage),
	})
	reg := newRegistry(t, u.options())

	if env := execute(t, reg, "mlbb", `{"action":"hero","hero":"yi sun shin"}`); !env.OK {
		t.Errorf("Yi Sun-shin was not found: %s", env.raw)
	}
	wantError(t, execute(t, reg, "mlbb", `{"action":"hero","hero":"Lingg"}`), "invalid_arguments", "no MLBB hero")
	if n := len(u.requests("/arena/api/heroes")); n != 1 {
		t.Errorf("the hero list was asked for %d times, want once", n)
	}
}

func TestMLBBMeta(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/arena/api/heroes/rank": ok(arenaRank)})

	out := result[mlbbMetaOutput](t, run(t, u.options(), "mlbb", `{"action":"meta","sort":"ban_rate"}`))

	if len(out.Heroes) != 2 || out.Heroes[0] != (mlbbRates{Hero: "Masha", WinRate: "61.0%", PickRate: "0.5%", BanRate: "35.5%"}) || out.Rank != "all" || out.SortedBy != "ban_rate" {
		t.Errorf("meta = %+v", out)
	}
	if q := u.requests("/arena/api/heroes/rank")[0].URL.Query(); q.Get("sort_field") != "ban_rate" || q.Get("size") != "10" || q.Get("days") != "7" {
		t.Errorf("query = %v", q)
	}
}

func TestMLBBRefusesBadArguments(t *testing.T) {
	u := newUpstream(t, nil)
	for args, word := range map[string]string{
		`{"action":"draft"}`:                            "action",
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

// A hero with no statistics yet, or an API that fails, is an error the
// model can tell, not an empty answer.
func TestMLBBUpstreamTrouble(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/arena/api/heroes":             ok(arenaHeroes),
		"/arena/api/heroes/84/counters": ok(`{"code":0,"data":{"records":[]}}`),
		"/arena/api/heroes/84":          ok(arenaLingPage),
		"/arena/api/heroes/rank":        status(503),
	})
	if env := run(t, u.options(), "mlbb", `{"action":"hero","hero":"Ling"}`); env.OK || !strings.Contains(env.raw, "no statistics") {
		t.Errorf("an empty answer was taken for statistics: %s", env.raw)
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

// Asked for, the hero's picture goes into the turn's sink to be sent as a
// photo; one that cannot be fetched leaves the answer and says so.
func TestMLBBHeroPicture(t *testing.T) {
	for name, picture := range map[string]stub{"sent": ok("JPEG"), "missing": status(404)} {
		t.Run(name, func(t *testing.T) {
			u := newUpstream(t, map[string]stub{
				"/arena/api/heroes":             ok(arenaHeroes),
				"/arena/api/heroes/84/counters": ok(arenaLingCounters),
				"/img/ling.jpg":                 picture,
			})
			u.routes["/arena/api/heroes/84"] = ok(strings.Replace(arenaLingPage, `"story":`, `"squareheadbig":"`+u.server.URL+`/img/ling.jpg","story":`, 1))
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

// A new hero with no portrait yet, such as Hirara, is sent its card.
func TestMLBBHeroCardStandsInForAMissingPortrait(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/arena/api/heroes":             ok(arenaHeroes),
		"/arena/api/heroes/84/counters": ok(arenaLingCounters),
		"/img/card.png":                 ok("\x89PNG\r\n\x1a\nCARD"),
	})
	u.routes["/arena/api/heroes/84"] = ok(strings.Replace(arenaLingPage, `"story":`, `"smallmap":"`+u.server.URL+`/img/card.png","story":`, 1))
	sink := &tools.Sink{}
	ctx := tools.WithTurn(context.Background(), tools.Caller{UserID: 1, ChatID: -100, Human: true}, sink)
	var env envelope
	if err := json.Unmarshal([]byte(newRegistry(t, u.options()).Execute(ctx, 1, "mlbb", `{"action":"hero","hero":"Ling","picture":true}`)), &env); err != nil {
		t.Fatal(err)
	}
	env.raw = string(env.Result)
	out := result[mlbbHeroOutput](t, env)
	drawings := sink.Take()
	if len(drawings) != 1 || drawings[0].MimeType != "image/png" || !strings.Contains(out.Note, "picture is sent") {
		t.Errorf("drawings = %+v, note = %q; want the card sent as a PNG", drawings, out.Note)
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
	wantError(t, run(t, u.options(), "mlbb", `{"action":"skins","hero":"Guinever"}`), "invalid_arguments", "no MLBB hero")
	wantError(t, run(t, u.options(), "mlbb", `{"action":"skins"}`), "invalid_arguments", "hero")
}

// The hero answer adds MLBBDex's release date and base stats, and stands
// without them when MLBBDex fails.
func TestMLBBHeroAddsReleaseAndBaseStats(t *testing.T) {
	routes := map[string]stub{
		"/arena/api/heroes":              ok(arenaHeroes),
		"/arena/api/heroes/30/counters":  ok(arenaLingCounters),
		"/arena/api/heroes/30":           ok(arenaLingPage),
		"/dex/api/v1/heroes":             ok(dexHeroList),
		"/dex/api/v1/heroes/yi-sun-shin": ok(strings.Replace(dexGuinevere, `"name":"Guinevere"`, `"name":"Yi Sun-shin"`, 1)),
	}
	out := result[mlbbHeroOutput](t, run(t, newUpstream(t, routes).options(), "mlbb", `{"action":"hero","hero":"yi sun shin"}`))
	if out.Released != "2019" || out.BaseStats["hp15"] != "5210" || len(out.BaseStats) != 3 {
		t.Errorf("released = %q, base stats = %v, want the empty stat left out", out.Released, out.BaseStats)
	}

	routes["/dex/api/v1/heroes"] = status(503)
	out = result[mlbbHeroOutput](t, run(t, newUpstream(t, routes).options(), "mlbb", `{"action":"hero","hero":"yi sun shin"}`))
	if out.Released != "" || out.BaseStats != nil || out.WinRate == "" {
		t.Errorf("hero without MLBBDex = %+v", out)
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

func TestMLBBBuild(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/arena/api/heroes":                   ok(arenaHeroes),
		"/arena/api/heroes/84":                ok(arenaLingPage),
		"/arena/api/academy/heroes/84/builds": ok(arenaLingBuilds),
		"/arena/api/academy/equipment":        ok(arenaItems),
		"/arena/api/academy/emblems":          ok(arenaEmblems),
	})

	out := result[mlbbBuildOutput](t, run(t, u.options(), "mlbb", `{"action":"build","hero":"ling","rank":"mythic"}`))

	want := mlbbBuild{Spell: "Retribution", Emblem: "Assassin", Talents: []string{"Fatal", "Seasoned Hunter"}, Items: []string{"Berserker's Fury", "Haas' Claws"}, WinRate: "58.3%", PickRate: "36.0%"}
	if len(out.Builds) != 2 || !reflect.DeepEqual(out.Builds[0], want) || out.Builds[1].Spell != "Flicker" || out.Hero != "Ling" || out.Lane != "jungle" || out.Rank != "mythic" {
		t.Errorf("build = %+v, want the hero's main lane and the unknown item left out", out)
	}
	if q := u.requests("/arena/api/academy/heroes/84/builds")[0].URL.Query(); q.Get("lane") != "jungle" || q.Get("rank") != "mythic" {
		t.Errorf("query = %v", q)
	}

	// A lane asked for is used as it is, without the hero's page.
	u = newUpstream(t, map[string]stub{
		"/arena/api/heroes":                   ok(arenaHeroes),
		"/arena/api/academy/heroes/84/builds": ok(`{"code":0,"data":{"records":[]}}`),
	})
	if env := run(t, u.options(), "mlbb", `{"action":"build","hero":"Ling","lane":"gold"}`); env.OK || !strings.Contains(env.raw, "no builds") || len(u.requests("/arena/api/heroes/84")) != 0 {
		t.Errorf("no builds: %s", env.raw)
	}
}

func TestMainLane(t *testing.T) {
	for labels, want := range map[string]string{"Jungling": "jungle", "Gold Lane": "gold", "EXP Lane": "exp", "Mid Lane": "mid", "Roaming": "roam", "": "jungle"} {
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

// The hero's profile is MLBBDex's, its rates of all ranks too; Rone Arena
// gives only the counters, partners and tips.
func TestMLBBHeroProfileFromMLBBDex(t *testing.T) {
	routes := map[string]stub{
		"/arena/api/heroes":             ok(arenaHeroes),
		"/arena/api/heroes/84/counters": ok(arenaLingCounters),
		"/arena/api/heroes/84":          ok(arenaLingPage),
		"/dex/api/v1/heroes":            ok(dexLingList),
		"/dex/api/v1/heroes/ling":       ok(dexLing),
		"/dex/api/v1/rankings":          ok(dexRankingsAll),
	}
	out := result[mlbbHeroOutput](t, run(t, newUpstream(t, routes).options(), "mlbb", `{"action":"hero","hero":"Ling"}`))

	if out.Title != "Cloudwalker" || out.DamageType != "Physical" || out.Difficulty != "9/10" || out.Ratings["offense"] != "8/10" || len(out.Ratings) != 2 {
		t.Errorf("profile = %+v, want MLBBDex's, the null rating left out", out)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "Cloud Walker" || out.Analysis == nil || out.Analysis.Summary != "Assassin des murs." || !strings.Contains(out.Note, "French") {
		t.Errorf("skills = %+v, analysis = %+v, note = %q", out.Skills, out.Analysis, out.Note)
	}
	if out.mlbbRates != (mlbbRates{Hero: "Ling", WinRate: "51.2%", PickRate: "1.1%", BanRate: "20.5%", Tier: "A"}) || out.RatesMeasured != "2026-10-19" {
		t.Errorf("rates = %+v measured %q, want MLBBDex's", out.mlbbRates, out.RatesMeasured)
	}
	if len(out.CounteredBy) != 1 || out.CounteredBy[0].Hero != "Natalia" || out.Tips.WorksWith == "" || out.Story != "A graceful Finch." {
		t.Errorf("counters = %+v, tips = %+v, want Rone Arena's", out.CounteredBy, out.Tips)
	}

	// One rank: MLBBDex does not split its rates, so they are Rone Arena's.
	u := newUpstream(t, routes)
	out = result[mlbbHeroOutput](t, run(t, u.options(), "mlbb", `{"action":"hero","hero":"Ling","rank":"mythic"}`))
	if out.WinRate != "52.8%" || out.Tier != "" || out.RatesMeasured != "" || len(u.requests("/dex/api/v1/rankings")) != 0 {
		t.Errorf("mythic rates = %+v", out.mlbbRates)
	}

	// Rone Arena down: the profile stands, and the note says what is missing.
	routes["/arena/api/heroes/84/counters"] = status(503)
	out = result[mlbbHeroOutput](t, run(t, newUpstream(t, routes).options(), "mlbb", `{"action":"hero","hero":"Ling"}`))
	if out.Title != "Cloudwalker" || out.WinRate != "51.2%" || len(out.CounteredBy) != 0 || !strings.Contains(out.Note, "Rone Arena did not answer") {
		t.Errorf("without Rone Arena = %+v", out)
	}
}

func TestMLBBMetaFromMLBBDex(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/dex/api/v1/rankings": ok(dexRankingsAll), "/arena/api/heroes/rank": ok(arenaRank)})

	out := result[mlbbMetaOutput](t, run(t, u.options(), "mlbb", `{"action":"meta","sort":"win_rate"}`))
	if len(out.Heroes) != 2 || out.Heroes[0] != (mlbbRates{Hero: "Miya", WinRate: "53.0%", PickRate: "2.0%", BanRate: "5.0%", Tier: "S"}) || out.Measured != "2026-10-19" {
		t.Errorf("meta = %+v, want Miya first and Marcel, on too few games, left out", out)
	}
	if len(u.requests("/arena/api/heroes/rank")) != 0 {
		t.Error("Rone Arena was asked for the meta of all ranks")
	}
	if out := result[mlbbMetaOutput](t, run(t, u.options(), "mlbb", `{"action":"meta","rank":"glory"}`)); out.Heroes[0].Hero != "Masha" || out.Measured != "" {
		t.Errorf("glory meta = %+v, want Rone Arena's", out)
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

// The first build goes as one PNG, even when its icons will not load; the
// test server's icons are not https, so they are left blank.
func TestMLBBBuildSendsOnePicture(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/arena/api/heroes":                   ok(arenaHeroes),
		"/arena/api/academy/heroes/84/builds": ok(arenaLingBuilds),
		"/arena/api/academy/equipment":        ok(arenaItems),
		"/arena/api/academy/emblems":          ok(arenaEmblems),
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

// With MLBBDex, the hero and its main lane come from its list and the
// academy is asked by name, without Rone Arena's hero list or page.
func TestMLBBBuildByMLBBDexName(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/dex/api/v1/heroes":                       ok(`{"data":[{"slug":"chang-e","name":"Chang'e","roles":["Mage"],"lanes":["Mid"]}]}`),
		"/arena/api/academy/heroes/Chang'e/builds": ok(arenaLingBuilds),
		"/arena/api/academy/equipment":             ok(arenaItems),
		"/arena/api/academy/emblems":               ok(arenaEmblems),
	})
	out := result[mlbbBuildOutput](t, run(t, u.options(), "mlbb", `{"action":"build","hero":"change"}`))
	if out.Hero != "Chang'e" || out.Lane != "mid" || len(out.Builds) != 2 {
		t.Errorf("build = %+v", out)
	}
	if n := len(u.requests("/arena/api/heroes")); n != 0 {
		t.Errorf("Rone Arena's hero list was asked for %d times", n)
	}
}
