// Package raid is Dev jangi, the bot's boss raid game: the members of a
// chat hit one boss together from its Telegram game message, each tap a
// sword's blow, now and then a critical one, and the hit that takes its
// last HP kills it, rewards everyone who hit it, and brings the next,
// stronger one. Coins buy upgrades: a sharper sword, more critical blows,
// and Barchinoy, who shoots arrows at the boss while the player hits it.
//
// The page in public/game/boss draws it and sends how many times it was
// tapped; the server counts what the taps did, no more of them than a
// person taps in the time since the last, so a player can only cheat by
// tapping as fast as the limit allows.
package raid

import (
	"math"
	"time"

	"telegram-ai-bot/internal/storage"
)

const (
	// TapsPerSecond and tapBurst bound the taps counted: as many as a
	// quick person taps in the time since the last hit, at most maxSince
	// of it, and a few more.
	TapsPerSecond = 12
	tapBurst      = 4
	maxSince      = 2 * time.Second
	// A critical blow does critTimes a blow's damage.
	critTimes = 3
	// The upgrades' highest levels.
	maxSword  = 60
	maxCrit   = 15
	maxHelper = 40
	// ScoreEvery is how often a member's total is set as their Telegram
	// score.
	ScoreEvery = 30 * time.Second
	// Kinds is how many bosses there are, taken in turn by level.
	Kinds = 5
)

// MaxHP is a boss's HP at level.
func MaxHP(level int) int64 {
	return int64(math.Round(2000 * math.Pow(1.55, float64(min(level, 80)-1))))
}

// TapDamage is a blow of the sword at level.
func TapDamage(sword int) int64 {
	return int64(math.Round(5 * math.Pow(1.4, float64(sword-1))))
}

// CritChance is the percent of blows that are critical at level.
func CritChance(crit int) int {
	return 5 + 3*crit
}

// HelperDPS is what Barchinoy's arrows do a second at level, 0 before she
// is hired.
func HelperDPS(helper int) int64 {
	if helper == 0 {
		return 0
	}
	return int64(math.Round(3 * math.Pow(1.45, float64(helper-1))))
}

// Cost is the coins the next level of an upgrade at level costs, at most
// maxCost.
func Cost(item string, level int) int64 {
	var cost float64
	switch item {
	case "sword":
		cost = 20 * math.Pow(1.6, float64(level-1))
	case "crit":
		cost = 50 * math.Pow(2, float64(level))
	case "helper":
		cost = 80 * math.Pow(1.7, float64(level))
	default:
		return maxCost
	}
	return int64(math.Round(min(cost, maxCost)))
}

// maxCost is the most an upgrade costs, well within an int64.
const maxCost = 1e15

// Most is an upgrade's highest level.
func Most(item string) int {
	return map[string]int{"sword": maxSword, "crit": maxCrit, "helper": maxHelper}[item]
}

// Critical reports whether a hero's tap is a critical blow: a hash of
// their seed and the tap's number, which the page counts the same, so
// that it shows each blow as the server counts it.
func Critical(seed uint32, tap int64, chance int) bool {
	x := seed ^ uint32(tap)*0x9e3779b9 ^ uint32(tap>>32)
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return int(x%100) < chance
}

// Strike is what a hero's taps do since their last hit: the taps counted,
// and their damage with Barchinoy's arrows for the time.
func Strike(hero storage.RaidHero, taps int, since time.Duration) (int, int64) {
	since = min(max(since, 0), maxSince)
	counted := min(max(taps, 0), int(since*TapsPerSecond/time.Second)+tapBurst)
	blow, chance := TapDamage(hero.Sword), CritChance(hero.Crit)
	var damage int64
	for i := range int64(counted) {
		if Critical(hero.Seed, hero.Taps+i, chance) {
			damage += blow * critTimes
		} else {
			damage += blow
		}
	}
	damage += HelperDPS(hero.Helper) * int64(since) / int64(time.Second)
	return counted, damage
}

// Reward is the coins one who hit a killed boss with damage gets: some for
// being there, and more for their share of it.
func Reward(level int, damage, maxHP int64) int64 {
	return int64(20*level) + int64(100*level)*damage/max(maxHP, 1)
}

// Rules are the raid's rules as the store counts with them.
var Rules = storage.RaidRules{MaxHP: MaxHP, Strike: Strike, Reward: Reward, ScoreEvery: ScoreEvery}
