package webapi

import (
	"strings"
	"testing"
)

const dotaHeroStats = `[
	{"localized_name":"Pudge","primary_attr":"str","attack_type":"Melee","roles":["Disabler","Initiator"],"pub_pick":1000,"pub_win":520,"pro_pick":40,"pro_win":22,"pro_ban":60},
	{"localized_name":"Anti-Mage","primary_attr":"agi","attack_type":"Melee","roles":["Carry"],"pub_pick":800,"pub_win":360,"pro_pick":5,"pro_win":2,"pro_ban":3},
	{"localized_name":"Io","primary_attr":"all","attack_type":"Ranged","roles":["Support"],"pub_pick":10,"pub_win":9,"pro_pick":1,"pro_win":1,"pro_ban":0}
]`

func TestDotaHeroIsFoundByAnySpellingOfItsName(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/opendota/api/heroStats": ok(dotaHeroStats)})

	env := run(t, u.options(), "games", `{"action":"dota_hero","name":"anti mage"}`)

	if !env.OK || !strings.Contains(env.raw, `"hero":"Anti-Mage"`) || !strings.Contains(env.raw, `"attribute":"agility"`) || !strings.Contains(env.raw, `"win_rate":"45.0%"`) {
		t.Errorf("hero = %s", env.raw)
	}
	wantError(t, run(t, u.options(), "games", `{"action":"dota_hero","name":"Saber"}`), "invalid_arguments", "no Dota 2 hero")
}

// A hero hardly anyone plays is not ranked by its win rate.
func TestDotaMetaRanksOnlyHeroesPeoplePlay(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/opendota/api/heroStats": ok(dotaHeroStats)})

	env := run(t, u.options(), "games", `{"action":"dota_meta"}`)

	winning := env.raw[strings.Index(env.raw, `"winning_public"`):]
	if !env.OK || strings.Contains(winning[:strings.Index(winning, "]")], "Io") || !strings.Contains(env.raw, `"most_contested_pro":[{"bans":60,"hero":"Pudge"`) {
		t.Errorf("meta = %s", env.raw)
	}
}

func TestGameDealsOnSteam(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/cheapshark/api/1.0/deals": ok(`[{"title":"Hades","dealID":"a b","salePrice":"6.24","normalPrice":"24.99","savings":"75.030012","metacriticScore":"93"}]`)})

	env := run(t, u.options(), "free_games", `{"deals":true}`)

	if !env.OK || !strings.Contains(env.raw, `"discount":"75%"`) || !strings.Contains(env.raw, `"price_usd":"6.24"`) || !strings.Contains(env.raw, `dealID=a+b`) {
		t.Errorf("deals = %s", env.raw)
	}
	if q := u.requests("/cheapshark/api/1.0/deals")[0].URL.Query(); q.Get("storeID") != "1" || q.Get("onSale") != "1" {
		t.Errorf("query = %v", q)
	}
}

// Launches already gone by are left out; the list keeps the next ones.
func TestLaunchesAreTheNextOnes(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/ll2/2.3.0/launches/upcoming/": ok(`{"results":[
		{"name":"Gone","net":"2026-10-15T00:00:00Z","status":{"name":"Launch Successful"}},
		{"name":"Falcon 9 | Starlink","net":"2026-10-20T04:45:00Z","status":{"name":"Go for Launch"},"launch_service_provider":{"name":"SpaceX"},"pad":{"location":{"name":"Vandenberg SFB, CA, USA"}},"mission":{"description":"27 satellites."}}]}`)})

	env := run(t, u.options(), "more_info", `{"action":"launches"}`)

	if !env.OK || strings.Contains(env.raw, "Gone") || !strings.Contains(env.raw, `"time_utc":"2026-10-20 04:45"`) || !strings.Contains(env.raw, `"provider":"SpaceX"`) {
		t.Errorf("launches = %s", env.raw)
	}
}
