package webapi

import (
	"strings"
	"testing"
)

func TestMLBBEmblemsSpellsAndNewSkins(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/dex/api/v1/emblems": ok(`{"data":[{"slug":"tank","name":"Tank Emblem","role":"Tank","description":"HP and resistances","bestFor":"Engage tanks."}]}`),
		"/dex/api/v1/spells":  ok(`{"data":[{"slug":"aegis","name":"Aegis","description":"Grants a shield.","cooldown":75,"roles":["Support","Tank"],"bestFor":"Supports."},{"name":"Petrify","description":"Stuns.","cooldown":0}]}`),
		"/dex/api/v1/skins":   ok(`{"data":[{"hero":"ling","name":"Night Shade","release":"2026-11","rarity":"Supreme","series":"Legend"},{"hero":"newcomer","name":"Debut","release":"2026-10-01","rarity":"Elite"}]}`),
		"/dex/api/v1/heroes":  ok(dexLingList),
	})

	env := run(t, u.options(), "mlbb", `{"action":"emblems"}`)
	if !env.OK || !strings.Contains(env.raw, `"name":"Tank Emblem","role":"Tank","gives":"HP and resistances","best_for":"Engage tanks."`) {
		t.Errorf("emblems = %s", env.raw)
	}
	env = run(t, u.options(), "mlbb", `{"action":"spells"}`)
	if !env.OK || !strings.Contains(env.raw, `"cooldown":"75s"`) || !strings.Contains(env.raw, `"roles":["Support","Tank"]`) || strings.Contains(env.raw, `"cooldown":"0s"`) {
		t.Errorf("spells = %s", env.raw)
	}
	env = run(t, u.options(), "mlbb", `{"action":"new_skins"}`)
	if !env.OK || !strings.Contains(env.raw, `"hero":"Ling","skin":"Night Shade","released":"2026-11","tier":"Legend (Supreme)"`) || !strings.Contains(env.raw, `"hero":"newcomer"`) {
		t.Errorf("new skins = %s, want the hero named and an unknown slug kept", env.raw)
	}
	if q := u.requests("/dex/api/v1/skins")[0].URL.Query(); q.Get("limit") != "20" {
		t.Errorf("skins query = %v", q)
	}
}
