package cards

import (
	"math"

	"telegram-ai-bot/internal/storage"
)

// FuseCost is how many free copies of a card raise it one star, by the
// stars it has: ten copies take a card from ★1 to ★5.
var FuseCost = [5]int{0, 1, 2, 3, 4}

// StarMultiplier is how much stronger a card of so many stars is.
var StarMultiplier = [6]float64{0, 1.00, 1.20, 1.45, 1.70, 2.00}

// MaxStars is the most stars a card reaches.
const MaxStars = len(StarMultiplier) - 1

// Boosted is the card's attack and health at so many stars, rounded to
// tens; stars out of 1 to MaxStars count as the nearest.
func Boosted(c storage.Card, stars int) (attack, health int) {
	m := StarMultiplier[min(max(stars, 1), MaxStars)]
	return rounded(int(math.Round(float64(c.Attack) * m))), rounded(int(math.Round(float64(c.Health) * m)))
}
