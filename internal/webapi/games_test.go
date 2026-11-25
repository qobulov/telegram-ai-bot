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

// Deals are Steam's, at its prices for Uzbekistan.
func TestGameDealsOnSteam(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/steam/api/featuredcategories/": ok(`{"specials":{"items":[{"id":1091500,"name":"Cyberpunk 2077","currency":"USD","original_price":2999,"final_price":899,"discount_percent":70}]}}`)})

	env := run(t, u.options(), "free_games", `{"deals":true}`)

	if !env.OK || !strings.Contains(env.raw, `"discount":"70%"`) || !strings.Contains(env.raw, `"price":"8.99 USD"`) || !strings.Contains(env.raw, `/app/1091500`) {
		t.Errorf("deals = %s", env.raw)
	}
	if q := u.requests("/steam/api/featuredcategories/")[0].URL.Query(); q.Get("cc") != "uz" {
		t.Errorf("query = %v, want Uzbekistan's prices", q)
	}
}

func TestSteamGameGivesItsPriceForUzbekistan(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/steam/api/storesearch/": ok(`{"total":1,"items":[{"id":1245620,"name":"ELDEN RING"}]}`),
		"/steam/api/appdetails":   ok(`{"1245620":{"success":true,"data":{"name":"ELDEN RING","is_free":false,"short_description":"Rise, Tarnished.","pc_requirements":{"minimum":"<strong>Minimum:</strong><br><ul><li>GTX 1060</li></ul>"},"price_overview":{"currency":"USD","initial":3999,"final":1999,"discount_percent":50},"genres":[{"description":"Action"},{"description":"RPG"}],"release_date":{"coming_soon":false,"date":"24 Feb, 2022"},"metacritic":{"score":94}}}}`),
	})

	env := run(t, u.options(), "games", `{"action":"steam_game","name":"elden ring"}`)

	// Without the player count the page still stands.
	if !env.OK || strings.Contains(env.raw, "playing_now") || !strings.Contains(env.raw, `"price":"19.99 USD"`) || !strings.Contains(env.raw, `"discount":"50%"`) || !strings.Contains(env.raw, `"metacritic":94`) ||
		!strings.Contains(env.raw, `GTX 1060`) || strings.Contains(env.raw, `<li>`) {
		t.Errorf("game = %s", env.raw)
	}
	if q := u.requests("/steam/api/appdetails")[0].URL.Query(); q.Get("appids") != "1245620" || q.Get("cc") != "uz" {
		t.Errorf("query = %v", q)
	}
	playing := newUpstream(t, map[string]stub{
		"/steam/api/storesearch/":                                 ok(`{"total":1,"items":[{"id":1245620,"name":"ELDEN RING"}]}`),
		"/steam/api/appdetails":                                   ok(`{"1245620":{"success":true,"data":{"name":"ELDEN RING","is_free":false}}}`),
		"/steamapi/ISteamUserStats/GetNumberOfCurrentPlayers/v1/": ok(`{"response":{"player_count":51230,"result":1}}`),
	})
	if env := run(t, playing.options(), "games", `{"action":"steam_game","name":"elden ring"}`); !strings.Contains(env.raw, `"playing_now_on_steam":51230`) {
		t.Errorf("game with players = %s", env.raw)
	}
	u2 := newUpstream(t, map[string]stub{"/steam/api/storesearch/": ok(`{"total":0,"items":[]}`)})
	wantError(t, run(t, u2.options(), "games", `{"action":"steam_game","name":"Nothing"}`), "invalid_arguments", "Steam has no game")
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

const (
	dotaSvenStats     = `[{"name":"npc_dota_hero_sven","localized_name":"Sven","primary_attr":"str","attack_type":"Melee","roles":["Carry"],"pub_pick":100,"pub_win":51,"pro_pick":1,"pro_win":1,"pro_ban":0}]`
	dotaHeroAbilities = `{"npc_dota_hero_sven":{"abilities":["sven_storm_bolt","generic_hidden","sven_great_cleave"]}}`
	dotaAbilities     = `{
		"sven_storm_bolt":{"dname":"Storm Hammer","desc":"Sven unleashes his magical gauntlet.","behavior":["Unit Target","AOE"],"dmg_type":"Magical","dmg":["80","160"],"cd":["18","16"],"mc":"110","attrib":[{"header":"STUN DURATION:","value":["1","1.25"]},{"header":"X:","value":false},{"header":"HEAL:","value":"0"}]},
		"sven_great_cleave":{"dname":"Great Cleave","desc":"Cleaves.","behavior":"Passive","cd":null,"mc":false}}`
	dotaItems = `{
		"black_king_bar":{"dname":"Black King Bar","cost":4050,"cd":95,"mc":50,"notes":"No reset.","attrib":[{"display":"+ {value} Strength","value":"10"},{"key":"max_level","value":"3"}],"abilities":[{"type":"active","title":"Avatar","description":"Magic resistance."}],"components":["mithril_hammer","ogre_axe"]},
		"recipe_black_king_bar":{"dname":"Black King Bar Recipe","cost":1450},
		"mithril_hammer":{"dname":"Mithril Hammer","cost":1600,"components":null},
		"ogre_axe":{"dname":"Ogre Axe","cost":1000,"components":null}}`
)

// A hero's skills come from the game's texts, the hidden ones left out.
func TestDotaHeroGivesItsSkills(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/opendota/api/heroStats": ok(dotaSvenStats),
		"/opendota/api/constants/hero_abilities": ok(dotaHeroAbilities), "/opendota/api/constants/abilities": ok(dotaAbilities)})

	env := run(t, u.options(), "games", `{"action":"dota_hero","name":"sven"}`)

	for _, want := range []string{`"name":"Storm Hammer"`, `"cooldown":"18 / 16"`, `"damage_type":"Magical"`, `"mana":"110"`, `"STUN DURATION: 1 / 1.25"`, `"name":"Great Cleave"`, `"kind":"Passive"`} {
		if !env.OK || !strings.Contains(env.raw, want) {
			t.Errorf("hero lacks %s: %s", want, env.raw)
		}
	}
	if strings.Contains(env.raw, "generic") || strings.Contains(env.raw, "X:") || strings.Contains(env.raw, "HEAL") {
		t.Errorf("hero has a hidden skill or an empty value: %s", env.raw)
	}
}

// A hero is still answered when its skills cannot be read.
func TestDotaHeroWithoutSkills(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/opendota/api/heroStats": ok(dotaSvenStats)})

	env := run(t, u.options(), "games", `{"action":"dota_hero","name":"Sven"}`)

	if !env.OK || strings.Contains(env.raw, `"skills":`) {
		t.Errorf("hero = %s", env.raw)
	}
}

func TestDotaItem(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/opendota/api/constants/items": ok(dotaItems)})

	wantError(t, run(t, u.options(), "games", `{"action":"dota_item","name":"Saber"}`), "invalid_arguments", "no Dota 2 item")
	if env := run(t, u.options(), "games", `{"action":"dota_item","name":"BKB"}`); !strings.Contains(env.raw, `"item":"Black King Bar"`) {
		t.Errorf("BKB = %s", env.raw)
	}

	env := run(t, u.options(), "games", `{"action":"dota_item","name":"black king"}`)

	for _, want := range []string{`"item":"Black King Bar"`, `"cost":4050`, `"built_from":["Mithril Hammer","Ogre Axe","Recipe"]`, `"+ 10 Strength"`, `"active Avatar: Magic resistance."`, `"cooldown":"95"`} {
		if !env.OK || !strings.Contains(env.raw, want) {
			t.Errorf("item lacks %s: %s", want, env.raw)
		}
	}
}
