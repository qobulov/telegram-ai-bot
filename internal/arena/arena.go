// Package arena simulates a 5 against 5 auto-battle between two teams of
// cards. It does no I/O: the same teams and seed always give the same battle,
// and the events it returns are enough for a client to replay the fight.
package arena

import (
	"math/rand"
	"sort"
)

// Tunable numbers of the battle.
const (
	MaxRounds = 30 // after this many rounds the team with more health left wins

	TankRatio = 4.0 // health/attack at or above this is a Tank
	MageRatio = 3.0 // health/attack at or below this is a Mage

	MinSkillRarity = 3 // Legendary (3) and Mythic (4) carry a skill

	StrikeEvery    = 3   // a Strike on every 3rd own turn
	StrikeFactor   = 2.5 // ... for this times the attack
	HealEvery      = 4   // a Heal on every 4th own turn
	HealShare      = 0.25
	ShieldShare    = 0.30
	DamageSpreadLo = 0.9
	DamageSpreadHi = 1.1
)

// Class is named ClassX because Fighter is the card type.
type Class int

const (
	ClassTank Class = iota
	ClassFighter
	ClassMage
)

type Skill int

const (
	NoSkill Skill = iota
	Strike
	Heal
	Shield
)

// Fighter is one card in the fight; Attack and Health are already boosted by
// its stars. A zero Fighter (no health) is an empty slot.
type Fighter struct {
	CardID         int64
	Name           string
	Rarity, Stars  int
	Attack, Health int
	Class          Class
	Skill          Skill
}

// FighterOf makes a fighter, giving it its class by its health to attack
// ratio and, from Legendary up, a skill by its card id.
func FighterOf(id int64, name string, rarity, stars, attack, health int) Fighter {
	f := Fighter{CardID: id, Name: name, Rarity: rarity, Stars: stars, Attack: attack, Health: health, Class: ClassFighter}
	switch {
	case float64(health) >= TankRatio*float64(attack):
		f.Class = ClassTank
	case float64(health) <= MageRatio*float64(attack):
		f.Class = ClassMage
	}
	if rarity >= MinSkillRarity {
		f.Skill = [3]Skill{Strike, Heal, Shield}[((id%3)+3)%3]
	}
	return f
}

// Team is two fighters in front and three behind: slots 0-1, then 2-4.
type Team struct {
	Front [2]Fighter
	Back  [3]Fighter
}

// Power is the sum of attack and a quarter of health over the team.
func (t Team) Power() int {
	sum := 0
	for _, f := range t.slots() {
		sum += f.Attack + f.Health/4
	}
	return sum
}

func (t Team) slots() [5]Fighter {
	return [5]Fighter{t.Front[0], t.Front[1], t.Back[0], t.Back[1], t.Back[2]}
}

// Kinds of Event.
const (
	KindHit    = "hit"
	KindStrike = "strike"
	KindHeal   = "heal"
	KindShield = "shield"
	KindFall   = "fall"
)

// Event is one thing that happened. Side is 0 for the attacker's team and 1
// for the defender's. A hit or strike goes From a slot of Side To a slot of
// the other side; a heal goes From a slot To a slot of the same side; a
// shield (at Round 0) and a fall have From == To. Amount is the damage,
// healing or shield, and HP the health of the slot To afterwards.
type Event struct {
	Round, Side, From, To int
	Kind                  string // one of the Kind constants
	Amount, HP            int
}

type Result struct {
	AttackerWon bool
	Rounds      int
	Events      []Event
}

type unit struct {
	Fighter
	side, slot int
	hp, shield int
	turns      int
}

func (u *unit) alive() bool { return u.hp > 0 }

type fight struct {
	teams  [2][5]*unit
	rng    *rand.Rand
	events []Event
	round  int
}

// Battle plays the two teams against each other. That a team is valid (five
// distinct cards the player holds) is the caller's job.
func Battle(attacker, defender Team, seed int64) Result {
	f := &fight{rng: rand.New(rand.NewSource(seed))}
	for side, team := range [2]Team{attacker, defender} {
		for slot, fighter := range team.slots() {
			f.teams[side][slot] = &unit{Fighter: fighter, side: side, slot: slot, hp: fighter.Health}
		}
	}
	f.startShields()
	for f.round < MaxRounds && f.living(0) > 0 && f.living(1) > 0 {
		f.round++
		for _, u := range f.order() {
			if f.living(0) == 0 || f.living(1) == 0 {
				break
			}
			if u.alive() {
				f.act(u)
			}
		}
	}
	won := false // the defender wins an exact tie, empty teams included
	switch {
	case f.living(1) == 0:
		won = f.living(0) > 0
	case f.living(0) > 0:
		won = f.health(0) > f.health(1)
	}
	return Result{AttackerWon: won, Rounds: f.round, Events: f.events}
}

func (f *fight) startShields() {
	for _, side := range f.teams {
		for _, u := range side {
			if u.alive() && u.Skill == Shield {
				u.shield = int(ShieldShare * float64(u.Health))
				f.emit(u.side, u.slot, u.slot, KindShield, u.shield, u.hp)
			}
		}
	}
}

func speed(c Class) int {
	switch c {
	case ClassMage:
		return 3
	case ClassFighter:
		return 2
	}
	return 1
}

// order lists the living units of both sides by speed, then attack, then the
// attacker's side first, then slot.
func (f *fight) order() []*unit {
	var all []*unit
	for _, side := range f.teams {
		for _, u := range side {
			if u.alive() {
				all = append(all, u)
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if speed(a.Class) != speed(b.Class) {
			return speed(a.Class) > speed(b.Class)
		}
		if a.Attack != b.Attack {
			return a.Attack > b.Attack
		}
		if a.side != b.side {
			return a.side < b.side
		}
		return a.slot < b.slot
	})
	return all
}

func (f *fight) act(u *unit) {
	u.turns++
	if u.Skill == Heal && u.turns%HealEvery == 0 {
		if ally := f.weakestHurt(u.side); ally != nil && healAmount(ally) > 0 {
			f.heal(u, ally)
			return
		}
	}
	target := f.target(u)
	if target == nil {
		return
	}
	if u.Skill == Strike && u.turns%StrikeEvery == 0 {
		f.hit(u, target, StrikeFactor, KindStrike)
		return
	}
	f.hit(u, target, 1, KindHit)
}

// target is the first living front unit, or the first back one; a Mage picks
// the back unit with the least health, else the front.
func (f *fight) target(u *unit) *unit {
	foes := f.teams[1-u.side]
	if u.Class == ClassMage {
		var best *unit
		for _, c := range foes[2:] {
			if c.alive() && (best == nil || c.hp < best.hp) {
				best = c
			}
		}
		if best != nil {
			return best
		}
		return firstLiving(foes[:2])
	}
	if c := firstLiving(foes[:2]); c != nil {
		return c
	}
	return firstLiving(foes[2:])
}

func firstLiving(us []*unit) *unit {
	for _, c := range us {
		if c.alive() {
			return c
		}
	}
	return nil
}

// weakestHurt is the hurt living ally with the least health, if any.
func (f *fight) weakestHurt(side int) *unit {
	var weakest *unit
	for _, c := range f.teams[side] {
		if c.alive() && c.hp < c.Health && (weakest == nil || c.hp < weakest.hp) {
			weakest = c
		}
	}
	return weakest
}

func healAmount(to *unit) int {
	return min(int(HealShare*float64(to.Health)), to.Health-to.hp)
}

func (f *fight) hit(from, to *unit, factor float64, kind string) {
	spread := DamageSpreadLo + (DamageSpreadHi-DamageSpreadLo)*f.rng.Float64()
	dmg := int(float64(from.Attack)*factor*spread + 0.5)
	if dmg < 1 {
		dmg = 1
	}
	rest := dmg
	if to.shield > 0 {
		absorbed := min(rest, to.shield)
		to.shield -= absorbed
		rest -= absorbed
	}
	to.hp = max(to.hp-rest, 0)
	f.emit(from.side, from.slot, to.slot, kind, dmg, to.hp)
	if !to.alive() {
		f.emit(to.side, to.slot, to.slot, KindFall, 0, 0)
	}
}

func (f *fight) heal(from, to *unit) {
	amount := healAmount(to)
	to.hp += amount
	f.emit(from.side, from.slot, to.slot, KindHeal, amount, to.hp)
}

func (f *fight) emit(side, from, to int, kind string, amount, hp int) {
	f.events = append(f.events, Event{Round: f.round, Side: side, From: from, To: to, Kind: kind, Amount: amount, HP: hp})
}

func (f *fight) living(side int) int {
	n := 0
	for _, u := range f.teams[side] {
		if u.alive() {
			n++
		}
	}
	return n
}

func (f *fight) health(side int) int {
	sum := 0
	for _, u := range f.teams[side] {
		sum += u.hp
	}
	return sum
}
