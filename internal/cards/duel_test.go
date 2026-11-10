package cards

import (
	"testing"

	"telegram-ai-bot/internal/storage"
)

// The card that beats the other in fewer hits takes the round, a side with
// no card left loses it, and the player of more rounds wins.
func TestDuel(t *testing.T) {
	strong := storage.Card{ID: 1, Attack: 5000, Health: 20000}
	weak := storage.Card{ID: 2, Attack: 400, Health: 1500}
	half := func() float64 { return 0.5 }
	rounds, winner := Duel([2][]storage.Card{{weak, weak}, {strong}}, half)
	if len(rounds) != 2 || winner != 1 || rounds[0].Won != 1 || rounds[1].Won != 0 {
		t.Fatalf("rounds %+v, winner %d", rounds, winner)
	}
	if rounds[0].Hits != [2]int{50, 1} {
		t.Errorf("hits = %v, want 50 for the weak card and 1 for the strong", rounds[0].Hits)
	}
	// One round each is a draw, which luck settles.
	if _, winner = Duel([2][]storage.Card{{strong, weak}, {weak, strong}}, func() float64 { return 0.9 }); winner != 1 {
		t.Errorf("a drawn duel by luck 0.9 went to %d", winner)
	}
}
