package webapi

import (
	"fmt"
	"strings"
	"testing"
)

const patchIndex = `Patch Notes from Newest to Oldest.

<hr>[[Patch Notes 2.2.16|Patch Notes 2.2.16 (2026-09-16)]]
[[File:Patch Notes 2.2.16.png|thumb|left|270x270px]]
• Revamp Hero: {{hi|Masha}} & {{hi|Bruno}}<br>
• Hero Adjustments<br>
{{clr}}
<hr>[[Patch Notes 2.1.90|Patch Notes 2.1.90 (2026-07-08)]]
• Hero Adjustments: {{hi|Aulus}}<br>
{{clr}}`

const patchPage = `== II. Hero Adjustments ==
: The following uses {{pci|buff}} {{pci|nerf}} to indicate Buff and Nerf.

{{hi|Aulus}} {{pci|buff}}
: Aulus takes too long to ramp up.

: {{ai|The Power of Axe|Skill 2}} {{pci|buff}}
* '''New Effect:''' The first cast stuns targets hit for 0.3s.
* Cooldown: 7s → 10-8s
{{clr}}

{{hi|Edith}} {{pci|buff}}
: Stronger.
{{clr}}

== III. Battlefield Adjustments ==
{{hi|Edith}} {{pci|nerf}}
: In Mythical Honor, weaker.
{{clr}}`

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

func TestPatchListAndHeroChanges(t *testing.T) {
	patches := patchList(patchIndex)
	if len(patches) != 2 || patches[0].Page != "Patch Notes 2.2.16" || patches[0].Title != "Patch Notes 2.2.16 (2026-09-16)" || patches[0].Highlights[0] != "Revamp Hero: Masha & Bruno" {
		t.Fatalf("patches = %+v", patches)
	}
	changes := heroChanges(patchPage)
	if len(changes) != 3 || changes[0].Hero != "Aulus" || changes[2].Change != "nerf" {
		t.Fatalf("changes = %+v", changes)
	}
	for _, want := range []string{"Aulus takes too long to ramp up.", "The Power of Axe (Skill 2) [buff]", "New Effect: The first cast stuns", "Cooldown: 7s → 10-8s"} {
		if !strings.Contains(changes[0].Text, want) {
			t.Errorf("Aulus's changes lack %q: %s", want, changes[0].Text)
		}
	}
}

func TestUpcomingSectionsSkipCommentedGalleries(t *testing.T) {
	sections := upcomingSections(upcomingPage)
	got := fmt.Sprint(sections)
	if len(sections) != 2 || !strings.Contains(got, "items:[Dori (Mage) - TBA] section:New heroes") || !strings.Contains(got, "items:[Lapu-Lapu - Crystalfang Warchief] section:Special skins") || strings.Contains(got, "Hidden") {
		t.Errorf("sections = %s", got)
	}
}

// The overview names each hero once, with every kind of change it had.
func TestMLBBPatchOverview(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/mlwiki/api.php": ok(wikiAnswer(patchIndex + "\n" + patchPage))})

	env := run(t, u.options(), "mlbb_news", `{"action":"patch"}`)

	if !env.OK || !strings.Contains(env.raw, `"heroes":["Aulus: buff","Edith: buff, nerf"]`) || !strings.Contains(env.raw, `"patch":"Patch Notes 2.2.16 (2026-09-16)"`) {
		t.Errorf("patch = %s", env.raw)
	}
	env = run(t, u.options(), "mlbb_news", `{"action":"patch","hero":"edith"}`)
	if !env.OK || !strings.Contains(env.raw, `"change":"buff, nerf"`) || !strings.Contains(env.raw, "Mythical Honor") {
		t.Errorf("Edith = %s", env.raw)
	}
	env = run(t, u.options(), "mlbb_news", `{"action":"patch","hero":"Fanny"}`)
	if !env.OK || !strings.Contains(env.raw, `"changed":false`) {
		t.Errorf("Fanny = %s", env.raw)
	}
}
