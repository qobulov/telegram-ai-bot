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
	StunEvery      = 3    // a Stun on every 3rd own turn skips the target's next turn
	PoisonEvery    = 3    // a Poison on every 3rd own turn ...
	PoisonTurns    = 3    // ... hurts the target at the start of its next 3 turns
	PoisonShare    = 0.08 // ... by this share of its health, past any shield
	ProvokeEvery   = 4    // a Provoke at the start and on every 4th own turn ...
	ProvokeRounds  = 2    // ... draws enemy Tanks and Fighters for 2 rounds
	EdgeFactor     = 1.25 // Tank beats Fighter, Fighter beats Mage, Mage beats Tank
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
	Stun
	Poison
	Provoke
)

// skillPool is the skills by card id, id%6 picking one, so that a card's
// skill never changes. The page works it out the same way.
var skillPool = [6]Skill{Strike, Heal, Shield, Stun, Poison, Provoke}

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
		n := int64(len(skillPool))
		f.Skill = skillPool[((id%n)+n)%n]
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
	KindHit      = "hit"
	KindStrike   = "strike"
	KindStun     = "stun"   // a hit that stuns its target
	KindPoison   = "poison" // a hit that poisons its target
	KindHeal     = "heal"
	KindShield   = "shield"
	KindProvoke  = "provoke"
	KindStunned  = "stunned"  // a stunned unit's turn, skipped
	KindPoisoned = "poisoned" // poison hurting a unit at the start of its turn
	KindFall     = "fall"
)

// Event is one thing that happened. Side is 0 for the attacker's team and 1
// for the defender's. A hit, strike, stun or poison goes From a slot of Side
// To a slot of the other side, Edge when its class beats the target's; a
// heal goes From a slot To a slot of the same side; a shield, provoke,
// stunned, poisoned and fall have From == To, the slot's own side. Amount is
// the damage, healing or shield (0 for provoke, stunned and fall), and HP
// the health of the slot To afterwards.
type Event struct {
	Round, Side, From, To int
	Kind                  string // one of the Kind constants
	Amount, HP            int
	Edge                  bool
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
	stunned    bool // skips its next turn
	poisoned   int  // its turns left to be hurt by poison
	provoking  int  // the last round it draws enemy Tanks and Fighters
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
	f.start()
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

// start raises the shields and the first provokes, at round 0.
func (f *fight) start() {
	for _, side := range f.teams {
		for _, u := range side {
			if !u.alive() {
				continue
			}
			switch u.Skill {
			case Shield:
				u.shield = int(ShieldShare * float64(u.Health))
				f.emit(Event{Side: u.side, From: u.slot, To: u.slot, Kind: KindShield, Amount: u.shield, HP: u.hp})
			case Provoke:
				f.provoke(u)
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

// act is a unit's turn: poison hurts it first, a stun skips the rest, then
// it heals, or hits with its skill or plainly.
func (f *fight) act(u *unit) {
	if f.poisonTick(u); !u.alive() {
		return
	}
	if u.stunned {
		u.stunned = false
		f.emit(Event{Side: u.side, From: u.slot, To: u.slot, Kind: KindStunned, HP: u.hp})
		return
	}
	u.turns++
	if u.Skill == Heal && u.turns%HealEvery == 0 {
		if ally := f.weakestHurt(u.side); ally != nil && healAmount(ally) > 0 {
			f.heal(u, ally)
			return
		}
	}
	// A provoke costs no turn, as a shield costs none at the start.
	if u.Skill == Provoke && u.turns%ProvokeEvery == 0 {
		f.provoke(u)
	}
	target := f.target(u)
	if target == nil {
		return
	}
	switch {
	case u.Skill == Strike && u.turns%StrikeEvery == 0:
		f.hit(u, target, StrikeFactor, KindStrike)
	case u.Skill == Stun && u.turns%StunEvery == 0 && !target.stunned:
		if f.hit(u, target, 1, KindStun); target.alive() {
			target.stunned = true
		}
	case u.Skill == Poison && u.turns%PoisonEvery == 0:
		if f.hit(u, target, 1, KindPoison); target.alive() {
			target.poisoned = PoisonTurns
		}
	default:
		f.hit(u, target, 1, KindHit)
	}
}

// poisonTick hurts a poisoned unit at the start of its turn, past its shield.
func (f *fight) poisonTick(u *unit) {
	if u.poisoned == 0 {
		return
	}
	u.poisoned--
	dmg := max(int(PoisonShare*float64(u.Health)), 1)
	u.hp = max(u.hp-dmg, 0)
	f.emit(Event{Side: u.side, From: u.slot, To: u.slot, Kind: KindPoisoned, Amount: dmg, HP: u.hp})
	if !u.alive() {
		f.emit(Event{Side: u.side, From: u.slot, To: u.slot, Kind: KindFall})
	}
}

// provoke draws enemy Tanks and Fighters to u for the rest of this round
// and the next ProvokeRounds; at round 0 that is rounds 1 and 2.
func (f *fight) provoke(u *unit) {
	u.provoking = f.round + ProvokeRounds
	f.emit(Event{Side: u.side, From: u.slot, To: u.slot, Kind: KindProvoke, HP: u.hp})
}

// target is a living provoker for Tanks and Fighters, else the first living
// front unit, or the first back one; a Mage ignores provokers and picks the
// back unit with the least health, else the front.
func (f *fight) target(u *unit) *unit {
	foes := f.teams[1-u.side]
	if u.Class == ClassMage {
		return mageTarget(foes)
	}
	if c := f.provoker(foes); c != nil {
		return c
	}
	if c := firstLiving(foes[:2]); c != nil {
		return c
	}
	return firstLiving(foes[2:])
}

func mageTarget(foes [5]*unit) *unit {
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

// provoker is the first living foe provoking this round, if any.
func (f *fight) provoker(foes [5]*unit) *unit {
	for _, c := range foes {
		if c.alive() && c.provoking >= f.round {
			return c
		}
	}
	return nil
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

// edge is whether a's class beats b's: Tank beats Fighter, Fighter beats
// Mage, Mage beats Tank.
func edge(a, b Class) bool {
	return a == ClassTank && b == ClassFighter || a == ClassFighter && b == ClassMage || a == ClassMage && b == ClassTank
}

func (f *fight) hit(from, to *unit, factor float64, kind string) {
	edged := edge(from.Class, to.Class)
	if edged {
		factor *= EdgeFactor
	}
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
	f.emit(Event{Side: from.side, From: from.slot, To: to.slot, Kind: kind, Amount: dmg, HP: to.hp, Edge: edged})
	if !to.alive() {
		f.emit(Event{Side: to.side, From: to.slot, To: to.slot, Kind: KindFall})
	}
}

func (f *fight) heal(from, to *unit) {
	amount := healAmount(to)
	to.hp += amount
	f.emit(Event{Side: from.side, From: from.slot, To: to.slot, Kind: KindHeal, Amount: amount, HP: to.hp})
}

// emit records e in the current round.
func (f *fight) emit(e Event) {
	e.Round = f.round
	f.events = append(f.events, e)
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
