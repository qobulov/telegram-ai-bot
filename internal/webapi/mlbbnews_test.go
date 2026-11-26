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
	dexPatches = `{"data":[{"version":"2.2.23","date":"2026-11-11"},{"version":"2.2.16a","date":"2026-10-14"},{"version":"2.2.16","date":"2026-09-16"}]}`
	dexPatch23 = `{"data":{"version":"2.2.23","date":"2026-11-11",
		"sections":[{"anchor":null,"title":null,"html":"<p>Highlights of patch 2.2.23:</p><ul><li>Revamped Hero Popol and Kupa</li></ul>"}],
		"newHeroes":[{"name":"Popol and Kupa","epithet":"Icefield Companions","feature":"A marksman who fights alongside Kupa."}],
		"adjustments":[{"name":"Aldous","type":"buff","intro":"Less downtime.","sections":[{"name":"Contract: Explosion","category":"Skill 2","type":"buff",
			"changes":[{"label":"Cooldown","before":"12-9.5s","after":"8s at all levels"},{"text":"New Effect: more damage."}]}]}]}}`
	dexPatch16 = `{"data":{"version":"2.2.16","date":"2026-09-16","newHeroes":[],
		"sections":[{"anchor":null,"title":null,"html":"<p>September update.</p>"},
			{"title":"I. Revamped Hero","html":"<h3 id=\"a\"><span>1. 10th Anniversary Revamped Hero: Masha, the Wild-oats Fist</span></h3><p>A Berserker.</p><h3 id=\"b\"><span>2. 10th Anniversary Revamped Hero: Bruno, The Protector</span></h3><p><b>Skill 2 - Slide BUFF</b></p><ul><li>Mana Cost: 60 → 90</li></ul>"},
			{"title":"II. Hero Adjustments","html":"","role":"adjustments"},
			{"title":"V. Game Modes/Mode Adjustments","html":"<h3 id=\"a-brawl\"><span>A. Brawl</span></h3><p>Dyrroth NERF</p><ul><li>DMG +14% → +8%</li></ul>"},
			{"title":"References","html":""}],
		"adjustments":[{"name":"Aulus","type":"buff","intro":"","sections":[]},
			{"name":"Aulus","type":"buff","intro":"Ramps up faster.","sections":[{"name":"Cleaving Axe","category":"Ultimate","type":"nerf","changes":[{"label":"Base Damage","before":"150-270","after":"110-270"}]}]}]}}`
	dexPatch16a = `{"data":{"version":"2.2.16a","date":"2026-10-14","adjustments":[{"name":"Fanny","type":"nerf","intro":"","sections":[]}]}}`
	dexEvents   = `{"data":[{"name":"StarLight skin: Tideborn Sailor (Kalea)","kind":"starlight","start":"2026-11-01T00:00:00.000Z","end":"2026-11-30T23:59:59.999Z"}]}`
)

func TestMLBBPatch(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/dex/api/v1/patches": ok(dexPatches), "/dex/api/v1/patches/2.2.23": ok(dexPatch23), "/dex/api/v1/patches/2.2.16a": ok(dexPatch16a)})

	env := run(t, u.options(), "mlbb_news", `{"action":"patch"}`)
	for _, want := range []string{`"patch":"2.2.23"`, `"date":"2026-11-11"`, `"heroes":["Aldous: buff"]`, `"new_or_revamped_heroes":["Popol and Kupa, Icefield Companions"]`, "Revamped Hero Popol and Kupa", `"previous":"2.2.16a (2026-10-14)"`, `**🌟 Aldous ↑**\n_Less downtime._\n\n**[Skill 2 · Contract: Explosion] (↑)**\n• Cooldown: 12-9.5s → 8s at all levels\n• New Effect: more damage.`, "**🌟 Popol and Kupa, Icefield Companions (revamp)**", "Never answer from a web search", "never a plain list"} {
		if !env.OK || !strings.Contains(env.raw, want) {
			t.Errorf("patch lacks %s: %s", want, env.raw)
		}
	}
	env = run(t, u.options(), "mlbb_news", `{"action":"patch","hero":"aldous"}`)
	if !env.OK || !strings.Contains(env.raw, `"notes":"**🌟 Aldous ↑**\n_Less downtime._\n\n**[Skill 2 · Contract: Explosion] (↑)**\n• Cooldown: 12-9.5s → 8s at all levels\n• New Effect: more damage."`) {
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

func TestMLBBPatchNotesParts(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/dex/api/v1/patches": ok(dexPatches), "/dex/api/v1/patches/2.2.16": ok(dexPatch16)})

	env := run(t, u.options(), "mlbb_news", `{"action":"patch","version":"2.2.16"}`)
	for _, want := range []string{`"heroes":["Aulus: buff"]`, `"new_or_revamped_heroes":["Masha, the Wild-oats Fist","Bruno, The Protector"]`, `"other_sections":["V. Game Modes/Mode Adjustments"]`, `**🌟 Aulus ↑**\n_Ramps up faster._\n\n**[Ultimate · Cleaving Axe] (↓)**\n• Base Damage: 150-270 → 110-270`, "**🌟 Bruno, The Protector (revamp)**"} {
		if !env.OK || !strings.Contains(env.raw, want) {
			t.Errorf("2.2.16 lacks %s: %s", want, env.raw)
		}
	}
	if env = run(t, u.options(), "mlbb_news", `{"action":"patch","version":"2.2.16","hero":"aulus"}`); !strings.Contains(env.raw, `**[Ultimate · Cleaving Axe] (↓)**\n• Base Damage: 150-270 → 110-270`) {
		t.Errorf("Aulus, listed twice = %s", env.raw)
	}
	if env = run(t, u.options(), "mlbb_news", `{"action":"patch","version":"2.2.16","hero":"Bruno"}`); !strings.Contains(env.raw, `"change":"revamp"`) || !strings.Contains(env.raw, "Mana Cost: 60 → 90") || strings.Contains(env.raw, "Berserker") {
		t.Errorf("Bruno, revamped = %s", env.raw)
	}
	if env = run(t, u.options(), "mlbb_news", `{"action":"patch","version":"2.2.16","section":"brawl"}`); !strings.Contains(env.raw, `"section":"A. Brawl"`) || !strings.Contains(env.raw, "DMG +14% → +8%") {
		t.Errorf("Brawl = %s", env.raw)
	}
	if env = run(t, u.options(), "mlbb_news", `{"action":"patch","version":"2.2.16","section":"ranked"}`); !strings.Contains(env.raw, `"found":false`) {
		t.Errorf("a part not there = %s", env.raw)
	}
}

func TestMLBBPatchVersion(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/dex/api/v1/patches": ok(dexPatches), "/dex/api/v1/patches/2.2.23": ok(dexPatch23), "/dex/api/v1/patches/2.2.16a": ok(dexPatch16a)})

	env := run(t, u.options(), "mlbb_news", `{"action":"patch","version":"Patch 2.2.16A"}`)
	for _, want := range []string{`"patch":"2.2.16a"`, `"heroes":["Fanny: nerf"]`, `"previous":"2.2.16 (2026-09-16)"`} {
		if !env.OK || !strings.Contains(env.raw, want) {
			t.Errorf("2.2.16a lacks %s: %s", want, env.raw)
		}
	}
	if env = run(t, u.options(), "mlbb_news", `{"action":"patch","version":"v2.2.16a","hero":"Aldous"}`); !strings.Contains(env.raw, `"changed":false`) || !strings.Contains(env.raw, `"patches_searched":["2.2.16a (2026-10-14)"]`) {
		t.Errorf("Aldous in 2.2.16a alone = %s", env.raw)
	}
	if env = run(t, u.options(), "mlbb_news", `{"action":"patch","version":"2.2.16"}`); !env.OK || !strings.Contains(env.raw, `"notes":false`) {
		t.Errorf("2.2.16, its notes missing = %s", env.raw)
	}
	if env = run(t, u.options(), "mlbb_news", `{"action":"patch","version":"9.9.9"}`); !env.OK || !strings.Contains(env.raw, `"known":false`) || !strings.Contains(env.raw, `"latest":"2.2.23"`) {
		t.Errorf("9.9.9 = %s", env.raw)
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

// Asked for the notes themselves, the tool sends them and ends the turn:
// left to the model, they came back as a list of names.
func TestMLBBPatchNotesSent(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/dex/api/v1/patches": ok(dexPatches), "/dex/api/v1/patches/2.2.23": ok(dexPatch23), "/dex/api/v1/patches/2.2.16a": ok(dexPatch16a)})

	env, sink := runTool(t, u, "mlbb_news", `{"action":"patch","send_notes":true}`)
	text, done := sink.Finished()
	if !env.OK || !done || !strings.HasPrefix(text, "📋 **Patch 2.2.23** · 2026-11-11\n\n") || !strings.Contains(text, "**🌟 Aldous ↑**\n_Less downtime._\n\n**[Skill 2 · Contract: Explosion] (↑)**\n• Cooldown: 12-9.5s → 8s at all levels") {
		t.Fatalf("sent %v %q, result %s", done, text, env.raw)
	}
	if _, sink = runTool(t, u, "mlbb_news", `{"action":"patch"}`); func() bool { _, done := sink.Finished(); return done }() {
		t.Errorf("notes sent unasked")
	}
}
