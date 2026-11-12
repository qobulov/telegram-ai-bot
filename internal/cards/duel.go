package cards

import (
	"math"

	"telegram-ai-bot/internal/storage"
)

// ShardRates are the 💎 shards a spare copy of a card of each rarity melts
// into, the most common first: a card drawn again gave only points before.
var ShardRates = []int{1, 3, 8, 20, 50}

// SpareWorth is how many spare copies there are, by rarity, and the shards
// they melt into.
func SpareWorth(spares map[int]int) (copies, worth int) {
	for rarity, n := range spares {
		copies += n
		if rarity >= 0 && rarity < len(ShardRates) {
			worth += n * ShardRates[rarity]
		}
	}
	return copies, worth
}

// ExtraCost is the shards a card drawn out of turn costs; it does not move
// the player's hourly card. SetCost is one drawn from a collection the player
// picks, in the Mini App's shop.
const (
	ExtraCost = 15
	SetCost   = 20
)

// IsCollection reports a collection there are cards of: the anime one and the
// trading ones.
func IsCollection(name string) bool {
	if name == "anime" {
		return true
	}
	for _, set := range tradingSets {
		if set.Collection == name {
			return true
		}
	}
	return false
}

// DuelTeam is how many of a player's strongest cards fight a duel.
const DuelTeam = 3

// MostStake bounds the shards a duel is fought for.
const MostStake = 100

// DuelResult is a fought duel: each team's power, its chance of winning in
// percent, and the winner, 0 the challenger and 1 the one who took it.
type DuelResult struct {
	Power  [2]int
	Chance [2]int
	Winner int
}

// TeamPower is what a team is worth in a duel: its cards' attack and health
// together.
func TeamPower(team []storage.Card) int {
	power := 0
	for _, c := range team {
		power += c.Attack + c.Health
	}
	return power
}

// Duel fights two players' teams: each wins with a chance as large as its
// share of the two teams' power, so that the stronger team wins more often
// and the weaker one still may. luck gives numbers in [0, 1).
func Duel(teams [2][]storage.Card, luck func() float64) DuelResult {
	var r DuelResult
	for side := range 2 {
		r.Power[side] = TeamPower(teams[side])
	}
	total := r.Power[0] + r.Power[1]
	share := 0.5
	if total > 0 {
		share = float64(r.Power[0]) / float64(total)
	}
	r.Chance[0] = int(math.Round(share * 100))
	r.Chance[1] = 100 - r.Chance[0]
	if luck() >= share {
		r.Winner = 1
	}
	return r
}
