package cards

import (
	"os"
	"testing"

	"telegram-ai-bot/internal/arena"
)

// handPicked keeps the real roles while the other tests run without them:
// their made-up cards have small ids that real cards also have.
var handPicked map[int64]role

func TestMain(m *testing.M) {
	handPicked, roles = roles, nil
	os.Exit(m.Run())
}

// A card with a hand-picked role fights in it, whatever its numbers, and
// the page is sent it; a card without one keeps what its numbers give.
func TestRoles(t *testing.T) {
	roles = handPicked
	t.Cleanup(func() { roles = nil })

	// Saitama's numbers alone would make him a Mage.
	if f := fighterOf(73935, "Saitama", int(Mythic), 1, 5800, 15610); f.Class != arena.ClassFighter || f.Skill != arena.Strike {
		t.Errorf("Saitama = class %v skill %v, want a Fighter with Strike", f.Class, f.Skill)
	}
	// Gojo's numbers alone would make him a Tank.
	if f := fighterOf(127691, "Satoru Gojou", int(Mythic), 1, 4500, 21890); f.Class != arena.ClassMage || f.Skill != arena.Shield {
		t.Errorf("Gojo = class %v skill %v, want a Mage with Shield", f.Class, f.Skill)
	}
	if class, skill := roleNames(127691, int(Mythic)); class != "mage" || skill != "shield" {
		t.Errorf("Gojo's role for the page = %q %q", class, skill)
	}
	if f, want := fighterOf(999999999, "nobody", int(Common), 1, 100, 500), arena.FighterOf(999999999, "nobody", int(Common), 1, 100, 500); f != want {
		t.Errorf("a card without a role = %+v, want %+v", f, want)
	}
	if class, skill := roleNames(999999999, int(Common)); class != "" || skill != "" {
		t.Errorf("a card without a role for the page = %q %q", class, skill)
	}
	for id, r := range roles {
		if r.skill == arena.NoSkill || classNames[r.class] == "" {
			t.Errorf("card %d has an incomplete role %+v", id, r)
		}
	}
}
