package cards

import (
	"testing"

	"telegram-ai-bot/internal/storage"
)

// Fusion from ★1 to ★5 uses up ten copies, and a card raised to ★5 is
// twice as strong.
func TestFusionCosts(t *testing.T) {
	sum := 0
	for stars := 1; stars < len(FuseCost); stars++ {
		sum += FuseCost[stars]
	}
	if sum != 10 {
		t.Errorf("copies from ★1 to ★5 = %d, want 10", sum)
	}
	gojo := storage.Card{Attack: 5000, Health: 18000}
	for _, c := range []struct{ stars, attack, health int }{{1, 5000, 18000}, {3, 7250, 26100}, {5, 10000, 36000}} {
		if attack, health := Boosted(gojo, c.stars); attack != c.attack || health != c.health {
			t.Errorf("★%d = %d/%d, want %d/%d", c.stars, attack, health, c.attack, c.health)
		}
	}
}
