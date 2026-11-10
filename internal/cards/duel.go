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
// the player's hourly card.
const ExtraCost = 15

// DuelTeam is how many of a player's strongest cards fight a duel.
const DuelTeam = 3

// MostStake bounds the shards a duel is fought for.
const MostStake = 100

// DuelRound is one card against the other: how many hits each needed to
// beat the other, and which won, 0 the first player's card and 1 the
// second's. A side with no card left loses the round, its card zero.
type DuelRound struct {
	Cards [2]storage.Card
	Hits  [2]int
	Won   int
}

// Duel fights two players' teams, the strongest card of each against the
// other's and so on: a card needs as many hits to beat the other as the
// other's health takes of its attack, each hit a little stronger or weaker
// by luck, and the fewer wins the round. The player who won more rounds wins,
// 0 or 1; a draw goes by luck. luck gives numbers in [0, 1).
func Duel(teams [2][]storage.Card, luck func() float64) (rounds []DuelRound, winner int) {
	won := [2]int{}
	for i := range max(len(teams[0]), len(teams[1])) {
		r := DuelRound{}
		for side := range 2 {
			if i < len(teams[side]) {
				r.Cards[side] = teams[side][i]
			}
		}
		r.Won = roundWinner(&r, luck)
		won[r.Won]++
		rounds = append(rounds, r)
	}
	switch {
	case won[0] > won[1]:
		return rounds, 0
	case won[1] > won[0]:
		return rounds, 1
	case luck() < 0.5:
		return rounds, 0
	}
	return rounds, 1
}

// roundWinner counts each card's hits and says which won the round.
func roundWinner(r *DuelRound, luck func() float64) int {
	for side := range 2 {
		if r.Cards[side].ID == 0 {
			return 1 - side
		}
	}
	for side := range 2 {
		hit := float64(r.Cards[side].Attack) * (0.85 + 0.3*luck())
		r.Hits[side] = int(math.Ceil(float64(r.Cards[1-side].Health) / max(hit, 1)))
	}
	switch {
	case r.Hits[0] < r.Hits[1]:
		return 0
	case r.Hits[1] < r.Hits[0]:
		return 1
	case luck() < 0.5:
		return 0
	}
	return 1
}
