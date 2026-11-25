package webapi

import (
	"strings"
	"testing"
)

const (
	dexFreyaList   = `{"data":[{"slug":"freya","name":"Freya","roles":["Fighter"],"lanes":["Exp"]},{"slug":"minsitthar","name":"Minsitthar"},{"slug":"masha","name":"Masha"}]}`
	dexFreyaBuilds = `{"data":{"Exp":{"all":[{"items":["Golden Staff","War Axe"],"emblem":"Fighter","talents":["Thrill"],"spell":"Vengeance","winRate":60.1,"pickRate":6.5}]}}}`
)

// With no builds from Rone Arena, MLBBDex's are the answer.
func TestBuildFallsBackToMLBBDex(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/dex/api/v1/heroes": ok(dexFreyaList), "/dex/api/v1/heroes/freya/builds": ok(dexFreyaBuilds)})

	env := run(t, u.options(), "mlbb", `{"action":"build","hero":"Freya","lane":"exp"}`)

	if !env.OK || !strings.Contains(env.raw, `"core_items":["Golden Staff","War Axe"]`) || !strings.Contains(env.raw, `"win_rate":"60.1%"`) || !strings.Contains(env.raw, "MLBBDex") {
		t.Errorf("build = %s", env.raw)
	}
}

func TestDexMatchups(t *testing.T) {
	got := dexMatchups([]dexCounter{{Slug: "minsitthar", Advantage: -4.4}, {Slug: "unknown", Advantage: 1}}, map[string]string{"minsitthar": "Minsitthar"})
	if len(got) != 1 || got[0].Hero != "Minsitthar" || got[0].WinRateChange != "-4.4%" {
		t.Errorf("matchups = %+v", got)
	}
}
