package cards

import (
	"testing"

	"telegram-ai-bot/internal/storage"
)

// Each team wins with its share of the two teams' power: the stronger more
// often, the weaker still sometimes.
func TestDuel(t *testing.T) {
	strong := storage.Card{ID: 1, Attack: 5000, Health: 15000}
	weak := storage.Card{ID: 2, Attack: 1000, Health: 4000}
	teams := [2][]storage.Card{{strong}, {weak, weak}}
	r := Duel(teams, func() float64 { return 0.5 })
	if r.Power != [2]int{20000, 10000} || r.Chance != [2]int{67, 33} || r.Winner != 0 {
		t.Fatalf("at luck 0.5 = %+v", r)
	}
	if r = Duel(teams, func() float64 { return 0.7 }); r.Winner != 1 {
		t.Errorf("the weaker team at luck 0.7 lost: %+v", r)
	}
	if r = Duel([2][]storage.Card{nil, nil}, func() float64 { return 0.2 }); r.Chance != [2]int{50, 50} || r.Winner != 0 {
		t.Errorf("two empty teams = %+v", r)
	}
}
