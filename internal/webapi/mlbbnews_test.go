package webapi

import (
	"fmt"
	"strings"
	"testing"
)

const upcomingPage = `==Hero==
===New heroes===
<gallery>
File:Hero000-portrait.png|'''[[Dori]] (Mage) - TBA'''
</gallery>
==Skins==
====Elite skins {{Skin tag|type=Elite}}====
<!--<gallery>
File:Hero1.png|'''[[Hidden]] - Commented'''
</gallery>-->
====Special skins {{Skin tag|type=Special}}====
<gallery>
File:Hero2.png|'''[[Lapu-Lapu]] - Crystalfang Warchief'''
</gallery>`

func wikiAnswer(text string) string {
	return fmt.Sprintf(`{"parse":{"wikitext":{"*":%q}}}`, text)
}

func TestUpcomingSectionsSkipCommentedGalleries(t *testing.T) {
	sections := upcomingSections(upcomingPage)
	got := fmt.Sprint(sections)
	if len(sections) != 2 || !strings.Contains(got, "items:[Dori (Mage) - TBA] section:New heroes") || !strings.Contains(got, "items:[Lapu-Lapu - Crystalfang Warchief] section:Special skins") || strings.Contains(got, "Hidden") {
		t.Errorf("sections = %s", got)
	}
}

const (
	dexPatches = `{"data":[{"version":"2.2.23","date":"2026-11-11"},{"version":"2.2.16a","date":"2026-10-14"}]}`
	dexPatch23 = `{"data":{"version":"2.2.23","date":"2026-11-11",
		"sections":[{"anchor":null,"title":null,"html":"<p>Highlights of patch 2.2.23:</p><ul><li>Revamped Hero Popol and Kupa</li></ul>"}],
		"newHeroes":[{"name":"Popol and Kupa","epithet":"Icefield Companions","feature":"A marksman who fights alongside Kupa."}],
		"adjustments":[{"name":"Aldous","type":"buff","intro":"Less downtime.","sections":[{"name":"Contract: Explosion","category":"Skill 2","type":"buff",
			"changes":[{"label":"Cooldown","before":"12-9.5s","after":"8s at all levels"},{"text":"New Effect: more damage."}]}]}]}}`
	dexPatch16a = `{"data":{"version":"2.2.16a","date":"2026-10-14","adjustments":[{"name":"Fanny","type":"nerf","intro":"","sections":[]}]}}`
	dexEvents   = `{"data":[{"name":"StarLight skin: Tideborn Sailor (Kalea)","kind":"starlight","start":"2026-11-01T00:00:00.000Z","end":"2026-11-30T23:59:59.999Z"}]}`
)

func TestMLBBPatch(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/dex/api/v1/patches": ok(dexPatches), "/dex/api/v1/patches/2.2.23": ok(dexPatch23), "/dex/api/v1/patches/2.2.16a": ok(dexPatch16a)})

	env := run(t, u.options(), "mlbb_news", `{"action":"patch"}`)
	for _, want := range []string{`"patch":"2.2.23"`, `"date":"2026-11-11"`, `"heroes":["Aldous: buff"]`, `"new_or_revamped_heroes":["Popol and Kupa, Icefield Companions"]`, "Revamped Hero Popol and Kupa", `"previous":"2.2.16a (2026-10-14)"`} {
		if !env.OK || !strings.Contains(env.raw, want) {
			t.Errorf("patch lacks %s: %s", want, env.raw)
		}
	}
	env = run(t, u.options(), "mlbb_news", `{"action":"patch","hero":"aldous"}`)
	if !env.OK || !strings.Contains(env.raw, `"Skill 2, Contract: Explosion [buff]: Cooldown: 12-9.5s → 8s at all levels; New Effect: more damage."`) {
		t.Errorf("Aldous = %s", env.raw)
	}
	if env = run(t, u.options(), "mlbb_news", `{"action":"patch","hero":"Fanny"}`); !strings.Contains(env.raw, `"patch":"2.2.16a"`) || !strings.Contains(env.raw, `"change":"nerf"`) || !strings.Contains(env.raw, "never soften a nerf") {
		t.Errorf("Fanny, in the patch before = %s", env.raw)
	}
	if env = run(t, u.options(), "mlbb_news", `{"action":"patch","hero":"popol"}`); !strings.Contains(env.raw, `"change":"revamp"`) {
		t.Errorf("Popol = %s", env.raw)
	}
	if env = run(t, u.options(), "mlbb_news", `{"action":"patch","hero":"Miya"}`); !strings.Contains(env.raw, `"changed":false`) {
		t.Errorf("Miya = %s", env.raw)
	}
}

func TestMLBBEvents(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/dex/api/v1/events": ok(dexEvents)})

	env := run(t, u.options(), "mlbb_news", `{"action":"events"}`)

	if !env.OK || !strings.Contains(env.raw, `"from":"2026-11-01"`) || !strings.Contains(env.raw, `"until":"2026-11-30"`) || !strings.Contains(env.raw, "Tideborn Sailor") {
		t.Errorf("events = %s", env.raw)
	}
}

func TestMLBBUpcoming(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/mlwiki/api.php": ok(wikiAnswer(upcomingPage))})

	env := run(t, u.options(), "mlbb_news", `{"action":"upcoming"}`)

	if !env.OK || !strings.Contains(env.raw, "Dori (Mage) - TBA") || !strings.Contains(env.raw, "unconfirmed") && !strings.Contains(env.raw, "none of it is confirmed") {
		t.Errorf("upcoming = %s", env.raw)
	}
}
