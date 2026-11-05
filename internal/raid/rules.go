// Package raid is Dev jangi, the bot's boss raid game: the members of a
// chat hit one boss together from its Telegram game message, each tap a
// sword's blow, now and then a critical one, and the hit that takes its
// last HP kills it, rewards everyone who hit it, and brings the next,
// stronger one. Coins buy upgrades: a sharper sword and more critical
// blows.
//
// The page in public/game/boss draws it and sends how many times it was
// tapped; the server counts what the taps did, no more of them than a
// person taps in the time since the last, so a player can only cheat by
// tapping as fast as the limit allows.
package raid

import (
	"errors"
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
	maxSword = 60
	maxCrit  = 15
	// ScoreEvery is how often a member's total is set as their Telegram
	// score.
	ScoreEvery = 30 * time.Second
	// Kinds is how many bosses there are, taken in turn by level; every
	// chiefEvery-th is a chief, chiefTimes as strong and as rewarding.
	Kinds      = 8
	chiefEvery = 5
	chiefTimes = 3
	// Active is how long since their last hit a member counts towards how
	// strong the next boss is, and mostActive how many count at most.
	Active     = 24 * time.Hour
	mostActive = 30
)

// MaxHP is a boss's HP at level, for a chat with active members: a third
// more each level, and two fifths more for each active member past the
// first, so that a small group's boss is a small group's to kill.
func MaxHP(level, active int) int64 {
	hp := 1500 * math.Pow(1.32, float64(min(level, 120)-1)) * (0.6 + 0.4*float64(min(max(active, 1), mostActive)))
	if Chief(level) {
		hp *= chiefTimes
	}
	return int64(math.Round(hp))
}

// Chief reports whether the boss at level is a chief.
func Chief(level int) bool {
	return level%chiefEvery == 0
}

// TapDamage is a blow of the sword at level.
func TapDamage(sword int) int64 {
	return int64(math.Round(5 * math.Pow(1.4, float64(sword-1))))
}

// CritChance is the percent of blows that are critical at level.
func CritChance(crit int) int {
	return 5 + 3*crit
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
	default:
		return maxCost
	}
	return int64(math.Round(min(cost, maxCost)))
}

// maxCost is the most an upgrade costs, well within an int64.
const maxCost = 1e15

// Most is an upgrade's highest level.
func Most(item string) int {
	return map[string]int{"sword": maxSword, "crit": maxCrit}[item]
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

// Strike is what a hero's taps do at now since their last hit: the taps
// counted and their damage. An armed Ulti is the first blow, Kuch
// multiplies every blow while it lasts, and the taps charge Ulti and count
// in the day's quests.
func Strike(hero *storage.RaidHero, taps int, since time.Duration, now time.Time) (int, int64) {
	hero.OnDay(Day(now))
	since = min(max(since, 0), maxSince)
	counted := min(max(taps, 0), int(since*TapsPerSecond/time.Second)+tapBurst)
	blow, chance := TapDamage(hero.Sword), CritChance(hero.Crit)
	buff := now.Before(hero.BuffUntil.Add(skillGrace))
	var damage int64
	for i := range int64(counted) {
		d := blow
		if i == 0 && hero.UltArmed {
			hero.UltArmed = false
			d *= ultTimes
		}
		if Critical(hero.Seed, hero.Taps+i, chance) {
			d *= critTimes
		}
		if buff {
			d *= buffTimes
		}
		damage += d
	}
	hero.UltCharge = min(hero.UltCharge+counted, ultTaps)
	hero.QuestTaps += counted
	return counted, damage
}

// Reward is the coins one with a sword at sword who hit a killed boss with
// damage gets: a part of their sword's next level for being there, and more
// for their share of it, so that a few kills buy a level however far they
// are; never less than the boss's level alone gave, and a chief's thrice.
// The reward was the level's alone until a sword's price, growing 1.6 times
// a level, left kills worth a five-hundredth of a level.
func Reward(level int, damage, maxHP int64, sword int) int64 {
	reward := int64(20*level) + int64(100*level)*damage/max(maxHP, 1)
	// A sword at its highest has no next level to save for: its coins would
	// grow by trillions with nothing to buy.
	if sword < maxSword {
		share := float64(min(max(damage, 0), max(maxHP, 1))) / float64(max(maxHP, 1))
		next := float64(Cost("sword", max(sword, 1)))
		reward = max(reward, int64(math.Round(next*(rewardFloor+rewardShare*share)*taper(sword))))
	}
	if Chief(level) {
		reward *= chiefTimes
	}
	return reward
}

// taper is how much of a kill's part of a level a sword at sword gets: all
// of it up to taperFrom, then 3% less a level, never under taperLeast, so
// that the strongest climb their last levels slowly: at 58 a level took a
// few chiefs.
func taper(sword int) float64 {
	return max(taperLeast, 1-taperStep*float64(max(sword-taperFrom, 0)))
}

const (
	taperFrom  = 30
	taperStep  = 0.03
	taperLeast = 0.15
)

// rewardFloor is the part of their sword's next level everyone who hit a
// killed boss gets, and rewardShare the most more for their share of it: a
// boss killed alone is 6% of a level, about seventeen for one. They were 5%
// and 20% until the strongest player bought a level with nearly every
// chief.
const (
	rewardFloor = 0.01
	rewardShare = 0.05
)

// titles are the damage in all that earns each title, from Yangi jangchi
// to Alpomish.
var titles = []int64{0, 10_000, 100_000, 1_000_000, 10_000_000, 100_000_000}

// Title is the title a member's damage in all earns: 0 for the first, up
// to len(titles)-1.
func Title(total int64) int {
	title := 0
	for i, least := range titles {
		if total >= least {
			title = i
		}
	}
	return title
}

// Rules are the raid's rules as the store counts with them.
var Rules = storage.RaidRules{MaxHP: MaxHP, Strike: Strike, Reward: Reward, Day: Day, ScoreEvery: ScoreEvery, Active: Active}

// uzbekistan is the time the days of the quests and the streak are in.
var uzbekistan = time.FixedZone("UZT", 5*60*60)

// Day is the day now is in, in Uzbekistan, as midnight UTC: what a date
// column holds.
func Day(now time.Time) time.Time {
	y, m, d := now.In(uzbekistan).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// The skills: Kuch, the buff, multiplies blows for a while and is ready
// again some time after it was used; Ulti, the ultimate, is charged by
// taps and makes the next blow a hundred.
// skillGrace lets a hit sent just after a skill ended still count it, as
// the page sends its taps in batches.
const (
	buffFor    = 12 * time.Second
	buffEvery  = 50 * time.Second
	buffTimes  = 3
	ultTaps    = 300
	ultTimes   = 100
	skillGrace = time.Second
)

var (
	ErrCooldown  = errors.New("raid: the skill is not ready")
	ErrUncharged = errors.New("raid: the ultimate is not charged")
	ErrNoSkill   = errors.New("raid: no such skill")
	ErrNoQuest   = errors.New("raid: no such quest")
	ErrNotDone   = errors.New("raid: the quest is not done")
	ErrClaimed   = errors.New("raid: the quest's reward was taken")
)

// UseSkill is the change using skill at now makes to a hero; it counts in
// the day's quests.
func UseSkill(skill string, now time.Time) func(*storage.RaidHero) error {
	return func(h *storage.RaidHero) error {
		switch skill {
		case "buff":
			if now.Before(h.BuffReady) {
				return ErrCooldown
			}
			h.BuffUntil, h.BuffReady = now.Add(buffFor), now.Add(buffEvery)
		case "ult":
			if h.UltCharge < ultTaps || h.UltArmed {
				return ErrUncharged
			}
			h.UltCharge, h.UltArmed = 0, true
		default:
			return ErrNoSkill
		}
		h.OnDay(Day(now))
		h.QuestSkills++
		return nil
	}
}

// Quest is one of the day's quests: what it counts, how much of it, and
// its reward before it grows with the sword.
type Quest struct {
	Key    string
	Goal   int
	Reward int64
}

// Quests are the day's quests, the same for everyone.
var Quests = []Quest{{"taps", 300, 150}, {"kills", 1, 300}, {"skills", 3, 200}}

// QuestProgress is how far the hero is in quest i today.
func QuestProgress(h storage.RaidHero, i int, now time.Time) int {
	h.OnDay(Day(now))
	return [...]int{h.QuestTaps, h.QuestKills, h.QuestSkills}[i]
}

// QuestReward is quest i's reward for the hero: more as their sword grows,
// so that it stays worth the doing.
func QuestReward(h storage.RaidHero, i int) int64 {
	return Quests[i].Reward * int64(1+h.Sword/5)
}

// QuestClaimed reports whether the hero took quest i's reward today.
func QuestClaimed(h storage.RaidHero, i int, now time.Time) bool {
	h.OnDay(Day(now))
	return h.QuestClaimed&(1<<i) != 0
}

// Claim is the change taking quest i's reward at now makes to a hero.
func Claim(i int, now time.Time) func(*storage.RaidHero) error {
	return func(h *storage.RaidHero) error {
		if i < 0 || i >= len(Quests) {
			return ErrNoQuest
		}
		h.OnDay(Day(now))
		if QuestClaimed(*h, i, now) {
			return ErrClaimed
		}
		if QuestProgress(*h, i, now) < Quests[i].Goal {
			return ErrNotDone
		}
		h.QuestClaimed |= 1 << i
		h.Coins += QuestReward(*h, i)
		return nil
	}
}

// longestStreak is the streak whose daily gift stops growing.
const longestStreak = 7

// Daily is the change a hero's first visit of a day at now makes: their
// streak grows if they came the day before, or starts again, and they get
// a gift that grows with it, which gift says.
func Daily(now time.Time, gift *int64) func(*storage.RaidHero) error {
	return func(h *storage.RaidHero) error {
		day := Day(now)
		if h.StreakDay.Equal(day) {
			return nil
		}
		if h.StreakDay.Equal(day.AddDate(0, 0, -1)) {
			h.Streak++
		} else {
			h.Streak = 1
		}
		h.StreakDay = day
		*gift = int64(50*min(h.Streak, longestStreak)) * int64(1+h.Sword/5)
		h.Coins += *gift
		return nil
	}
}
