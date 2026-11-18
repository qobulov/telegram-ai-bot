package cards

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

// CardCost is the crystals a card costs once the player has no token; a
// player opens one card or Batch at once.
const (
	CardCost = 15
	Batch    = 5
)
