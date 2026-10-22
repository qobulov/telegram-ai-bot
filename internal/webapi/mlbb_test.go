package webapi

import (
	"strings"
	"testing"
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
	arenaLingTeammates = `{"code":0,"data":{"records":[{"data":{
		"main_hero":{"data":{"name":"Ling"}},"main_hero_win_rate":0.527652,
		"sub_hero":[{"heroid":46,"increase_win_rate":0.014886}],
		"sub_hero_last":[{"heroid":69,"increase_win_rate":-0.178989}]}}]}}`
	arenaRank = `{"code":0,"data":{"records":[
		{"data":{"main_hero":{"data":{"name":"Masha"}},"main_hero_win_rate":0.60985,"main_hero_appearance_rate":0.005404,"main_hero_ban_rate":0.35482}},
		{"data":{"main_hero":{"data":{"name":"Marcel"}},"main_hero_win_rate":0.585272,"main_hero_appearance_rate":0.002,"main_hero_ban_rate":0.01}}]}}`
)

func TestMLBBHeroGivesRatesCountersAndTeammates(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/arena/api/heroes":                  ok(arenaHeroes),
		"/arena/api/heroes/84/counters":      ok(arenaLingCounters),
		"/arena/api/heroes/84/compatibility": ok(arenaLingTeammates),
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
	if len(out.BestTeammates) != 1 || out.BestTeammates[0].Hero != "Odette" || len(out.WorstTeammates) != 1 || out.WorstTeammates[0] != (mlbbMatchup{Hero: "Hanzo", WinRateChange: "-17.9%"}) {
		t.Errorf("teammates = %+v / %+v", out.BestTeammates, out.WorstTeammates)
	}
	if q := u.requests("/arena/api/heroes/84/counters")[0].URL.Query(); q.Get("rank") != "mythic" || q.Get("days") != "7" {
		t.Errorf("query = %v", q)
	}
}

// A hero's name is found however it is written, and the hero list is asked
// for once.
func TestMLBBHeroNames(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/arena/api/heroes":                  ok(arenaHeroes),
		"/arena/api/heroes/30/counters":      ok(arenaLingCounters),
		"/arena/api/heroes/30/compatibility": ok(arenaLingTeammates),
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
		`{"action":"player"}`:                    "action",
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
		"/arena/api/heroes":                  ok(arenaHeroes),
		"/arena/api/heroes/84/counters":      ok(`{"code":0,"data":{"records":[]}}`),
		"/arena/api/heroes/84/compatibility": ok(arenaLingTeammates),
		"/arena/api/heroes/rank":             status(503),
	})
	if env := run(t, u.options(), "mlbb", `{"action":"hero","hero":"Ling"}`); env.OK || !strings.Contains(env.raw, "no statistics") {
		t.Errorf("an empty answer was taken for statistics: %s", env.raw)
	}
	if env := run(t, u.options(), "mlbb", `{"action":"meta"}`); env.OK {
		t.Errorf("a 503 was taken for an answer: %s", env.raw)
	}
}
