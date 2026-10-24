package webapi

import (
	"context"
	"encoding/json"
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
		`{"action":"build"}`:                     "action",
		`{"action":"meta","rank":"grandmaster"}`: "rank",
		`{"action":"meta","sort":"kda"}`:         "sort",
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
