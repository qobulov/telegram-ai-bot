package cards

import "testing"

// Every Mythic has a tier and a role, so that its strength is its
// character's; each tier's power is the same whatever the class.
func TestMythicsBalanced(t *testing.T) {
	roles = handPicked
	t.Cleanup(func() { roles = nil })
	for id := range mythicIDs {
		attack, health, ok := mythicStats(id)
		if !ok {
			t.Errorf("Mythic %d has no tier or role", id)
			continue
		}
		power := attack + health/4
		want := tierPower[mythicTiers[id]]
		if power < want-60 || power > want+60 {
			t.Errorf("Mythic %d's power = %d, want about %d", id, power, want)
		}
		if c := CardOf(id, "", "", "", 0, Mythic); c.Attack != attack || c.Health != health {
			t.Errorf("CardOf(%d) = %d/%d, want the tier's %d/%d", id, c.Attack, c.Health, attack, health)
		}
	}
	if len(mythicTiers) != len(mythicIDs) {
		t.Errorf("%d tiers for %d Mythics", len(mythicTiers), len(mythicIDs))
	}
}
