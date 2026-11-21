package cards

// CardCost is the crystals a card costs once the player has no token; a
// player opens one card, Batch or BigBatch at once. BigBatch opens an
// event's pulls too, as the owners asked on 2026-11-21 for those with many
// tokens.
const (
	CardCost = 15
	Batch    = 5
	BigBatch = 50
)
