// Package arena simulates a 5 against 5 auto-battle between two teams of
// cards. It does no I/O: the same teams and seed always give the same battle,
// and the events it returns are enough for a client to replay the fight.
package arena

import (
	"math"
	"math/rand"
	"sort"
)

// Tunable numbers of the battle.
const (
	MaxRounds = 30 // after this many rounds the team with more health left wins

	TankRatio = 4.0 // health/attack at or above this is a Tank
	MageRatio = 3.0 // health/attack at or below this is a Support

	MinSkillRarity = 3 // Legendary (3) and Mythic (4) carry a skill

	// Energy, as in Souls: a unit fills it by acting and by being hurt, and
	// one with a skill uses it when a turn starts with it full, instead of
	// its plain action, spending it all. A skipped (stunned) turn fills
	// nothing.
	EnergyMax       = 100
	EnergyPerAction = 34 // after each own action but a skill
	EnergyWhenHit   = 10 // each time it takes damage, a poison tick too
	// The energy each role starts with, so that skills do not all come on
	// the same turn; Supports, the fastest, come first.
	StartEnergyTank    = 30
	StartEnergyDealer  = 20
	StartEnergySupport = 40
	StartEnergyHealer  = 30

	// The skills' numbers below were tuned on 2026-11-19 by simulating
	// teams of five Mythics of one skill against mixed ones, until each
	// skill won 45-52%.
	StrikeFactor  = 1.8  // a Strike hits for this times the attack
	HealShare     = 0.80 // a Heal gives the weakest hurt ally this share of its health, at most what it lost
	ShieldShare   = 0.30 // a Shield gives itself and its weakest ally this share of their health (0.45 until 2026-11-25, when it came to hit as well)
	PoisonTurns   = 3    // a Poison hurts the target at the start of its next 3 turns ...
	PoisonShare   = 0.15 // ... by this share of its health, past any shield
	ProvokeRounds = 3    // a Provoke draws enemy Tanks, Dealers and Healers for 3 more rounds ...
	ProvokeGuard  = 0.7  // ... and takes this share of the damage meanwhile, so that it holds
	EdgeFactor    = 1.25 // Tank beats Dealer, Dealer Support, Support Healer, Healer Tank
	// BossPoison is a Poison's tick on a boss, as a share of the poisoner's
	// attack, the boss's own health being endless.
	BossPoison     = 0.6
	DamageSpreadLo = 0.9
	DamageSpreadHi = 1.1
)

// Class is a fighter's role, as in Souls since the owners asked on
// 2026-11-22: a Tank holds the front, a Dealer deals the damage, a Support,
// the fastest, hits the weakest in the back row, and a Healer mends. It is
// named ClassX because Fighter is the card type.
type Class int

const (
	ClassTank Class = iota
	ClassDealer
	ClassSupport
	ClassHealer
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
	// Kit is a Mythic's own Passive and Ultimate, in place of Skill; nil
	// for the others.
	Kit *Kit
}

// FighterOf makes a fighter, giving it its class by its health to attack
// ratio and, from Legendary up, a skill by its card id.
func FighterOf(id int64, name string, rarity, stars, attack, health int) Fighter {
	f := Fighter{CardID: id, Name: name, Rarity: rarity, Stars: stars, Attack: attack, Health: health, Class: ClassDealer}
	switch {
	case float64(health) >= TankRatio*float64(attack):
		f.Class = ClassTank
	case float64(health) <= MageRatio*float64(attack):
		f.Class = ClassSupport
	}
	if rarity >= MinSkillRarity {
		n := int64(len(skillPool))
		f.Skill = skillPool[((id%n)+n)%n]
	}
	return f
}

// StartEnergy is the energy a unit of class c starts a battle with.
func StartEnergy(c Class) int {
	switch c {
	case ClassTank:
		return StartEnergyTank
	case ClassSupport:
		return StartEnergySupport
	case ClassHealer:
		return StartEnergyHealer
	}
	return StartEnergyDealer
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
// heal or a shield goes From a slot To a slot of the same side (a shield to
// its caster too); a provoke, stunned, poisoned and fall have From == To,
// the slot's own side. Amount is the damage, healing or shield (0 for
// provoke, stunned and fall), HP the health of the slot To afterwards, and
// Energy and ToEnergy the energy of the slots From and To afterwards.
type Event struct {
	Round, Side, From, To int
	Kind                  string // one of the Kind constants
	Amount, HP            int
	Energy, ToEnergy      int
	Edge                  bool
	// Ult is the Ultimate the event is of, "" for any other.
	Ult string
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
	energy     int
	stun       int // its next turns to skip
	// stunGuard is a turn after a stun ends, in which a new stun takes no
	// hold, as the owners asked on 2026-11-25: stunners in turn kept a foe,
	// a boss above all, stunned without end.
	stunGuard bool
	poisoned  int // its turns left to be hurt by poison
	provoking int // the last round it draws enemy Tanks, Dealers and Healers
	poisonDmg int // a boss's poison tick, by its poisoner's attack
	// boss is a unit that never falls: each level's health spent, it goes
	// up a level, its health and attack growing, it hits one living foe at
	// random each turn, and its attack rises by rage of itself each round
	// after the first.
	boss bool
	rage float64
}

func (u *unit) alive() bool { return u.hp > 0 }

func (u *unit) gain(energy int) { u.energy = min(u.energy+energy, EnergyMax) }

type fight struct {
	teams  [2][5]*unit
	rng    *rand.Rand
	events []Event
	round  int
	// first is the side that acts first on a tie of speed and attack in
	// round 1, the other in round 2 and so on, by the seed, so that
	// neither side is favoured.
	first int
	// bossDealt is all the damage dealt to a boss, bossLevel the level it
	// is at, bossLevelHP that level's whole health, bossBase its attack at
	// level 1 and bossLevels how it grows.
	bossDealt   int
	bossLevel   int
	bossLevelHP int
	bossBase    int
	bossLevels  BossLevels
	// rounds is the most rounds the fight lasts: MaxRounds, or a boss's.
	rounds int
	// casting is the Ultimate being cast, which its events name.
	casting string
}

// Battle plays the two teams against each other. That a team is valid (five
// distinct cards the player holds) is the caller's job.
func Battle(attacker, defender Team, seed int64) Result {
	f := newFight(attacker, defender, seed)
	f.run()
	won := false // the defender wins an exact tie, empty teams included
	switch {
	case f.living(1) == 0:
		won = f.living(0) > 0
	case f.living(0) > 0:
		won = f.health(0) > f.health(1)
	}
	return Result{AttackerWon: won, Rounds: f.round, Events: f.events}
}

// BossResult is a boss fight: all the damage the team dealt the boss, the
// level it reached, the rounds fought and the events to replay them.
type BossResult struct {
	Damage, Level, Rounds int
	Events                []Event
}

// BossLevels are how a boss grows, as the owners asked on 2026-11-24: HP is
// its first level's health and each level's is HPGrowth times the one
// before; its attack is AttackGrowth times more each level, and Rage more
// each round after the first besides.
type BossLevels struct {
	HP                     int
	HPGrowth, AttackGrowth float64
	Rage                   float64
	// Rounds is the most rounds a boss fight lasts, MaxRounds when 0: since
	// 2026-11-25, as the owners asked, more, so that a boss fight ends with
	// the team falling, not the clock, and holding out counts.
	Rounds int
}

// LevelHP is the whole health of level of a boss growing as levels say;
// the page works it out the same way, rounding each level's.
func (l BossLevels) LevelHP(level int) int {
	hp := l.HP
	for range level - 1 {
		hp = int(math.Round(float64(hp) * l.HPGrowth))
	}
	return hp
}

// KindLevel is a boss going up a level: Amount is the new level and HP the
// health of it left.
const KindLevel = "level"

// BossBattle plays team against boss, a lone foe in the defender's first
// slot that never falls: each level's health spent, it goes up a level, its
// health and attack growing as levels say, and each turn it hits one living
// card of the team at random, so the fight ends when the team falls or
// after MaxRounds. Its Health is not used.
func BossBattle(team Team, boss Fighter, levels BossLevels, seed int64) BossResult {
	f := newFight(team, Team{Front: [2]Fighter{boss}}, seed)
	b := f.teams[1][0]
	b.boss, b.rage, b.hp = true, levels.Rage, levels.HP
	f.bossLevel, f.bossLevelHP, f.bossBase, f.bossLevels = 1, levels.HP, boss.Attack, levels
	if levels.Rounds > 0 {
		f.rounds = levels.Rounds
	}
	f.run()
	return BossResult{Damage: f.bossDealt, Level: f.bossLevel, Rounds: f.round, Events: f.events}
}

// run plays rounds until a side has fallen or its rounds are over.
func (f *fight) run() {
	for f.round < f.rounds && f.living(0) > 0 && f.living(1) > 0 {
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
}

func newFight(attacker, defender Team, seed int64) *fight {
	f := &fight{rng: rand.New(rand.NewSource(seed)), first: int(seed & 1), rounds: MaxRounds}
	for side, team := range [2]Team{attacker, defender} {
		for slot, fighter := range team.slots() {
			f.teams[side][slot] = &unit{Fighter: fighter, side: side, slot: slot, hp: fighter.Health, energy: StartEnergy(fighter.Class)}
		}
	}
	f.applyKits()
	return f
}

func speed(c Class) int {
	switch c {
	case ClassSupport:
		return 3
	case ClassDealer, ClassHealer:
		return 2
	}
	return 1
}

// order lists the living units of both sides by speed, then attack, then
// the round's first side, then slot.
func (f *fight) order() []*unit {
	var all []*unit
	for _, side := range f.teams {
		for _, u := range side {
			if u.alive() {
				all = append(all, u)
			}
		}
	}
	first := (f.first + f.round + 1) % 2
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if speed(a.Class) != speed(b.Class) {
			return speed(a.Class) > speed(b.Class)
		}
		if a.Attack != b.Attack {
			return a.Attack > b.Attack
		}
		if a.side != b.side {
			return a.side == first
		}
		return a.slot < b.slot
	})
	return all
}

// act is a unit's turn: poison hurts it first, a stun skips the rest, then
// it uses its skill with its energy full, or hits plainly.
func (f *fight) act(u *unit) {
	if f.poisonTick(u); !u.alive() {
		return
	}
	if u.stun > 0 {
		u.stun--
		u.stunGuard = u.stun == 0
		f.record(KindStunned, u, u, 0, false)
		return
	}
	defer func() { u.stunGuard = false }()
	if u.boss {
		f.bossTurn(u)
		return
	}
	f.regen(u)
	if u.Kit != nil {
		if u.energy >= EnergyMax && f.useUlt(u) {
			return
		}
	} else if u.Skill != NoSkill && u.energy >= EnergyMax && f.useSkill(u) {
		return
	}
	target := f.target(u)
	if target == nil {
		return
	}
	u.gain(EnergyPerAction)
	f.hit(u, target, 1, KindHit)
}

// bossTurn is a boss's turn: it hits one living foe picked at random, as
// the owners asked on 2026-11-24 (it hit them all before), harder each round.
func (f *fight) bossTurn(u *unit) {
	var living []*unit
	for _, foe := range f.teams[1-u.side] {
		if foe.alive() {
			living = append(living, foe)
		}
	}
	if len(living) == 0 {
		return
	}
	f.hit(u, living[f.rng.Intn(len(living))], 1+u.rage*float64(f.round-1), KindHit)
}

// useSkill spends u's energy on its skill and says whether it did: a Heal
// with nobody hurt keeps it for a turn that needs it.
func (f *fight) useSkill(u *unit) bool {
	switch u.Skill {
	// A Heal or a Shield hits as well, since 2026-11-25, as the owners asked:
	// its turn went on the skill alone, while every other skill hits.
	case Heal:
		ally := f.weakestHurt(u.side)
		if ally == nil || healAmount(ally) <= 0 {
			return false
		}
		u.energy = 0
		f.heal(u, ally)
		f.skillAndHit(u)
	case Shield:
		u.energy = 0
		f.shield(u, u)
		if ally := f.weakestAlly(u); ally != nil {
			f.shield(u, ally)
		}
		f.skillAndHit(u)
	case Provoke:
		u.energy = 0
		f.provoke(u)
		if target := f.target(u); target != nil {
			f.hit(u, target, 1, KindHit)
		}
	default:
		f.skillHit(u)
	}
	return true
}

// skillHit is a Strike, Stun or Poison on u's target.
// skillAndHit is the plain hit a Heal or a Shield adds to its skill.
func (f *fight) skillAndHit(u *unit) {
	if target := f.target(u); target != nil {
		f.hit(u, target, 1, KindHit)
	}
}

func (f *fight) skillHit(u *unit) {
	target := f.target(u)
	if target == nil {
		return
	}
	u.energy = 0
	switch u.Skill {
	case Strike:
		f.hit(u, target, StrikeFactor, KindStrike)
	case Stun:
		if f.hit(u, target, 1, KindStun); target.alive() {
			target.stunFor(1)
		}
	case Poison:
		if f.hit(u, target, 1, KindPoison); target.alive() {
			target.poisoned = PoisonTurns
			target.poisonDmg = max(int(BossPoison*float64(u.Attack)), 1)
		}
	}
}

// poisonTick hurts a poisoned unit at the start of its turn, past its shield.
func (f *fight) poisonTick(u *unit) {
	if u.poisoned == 0 {
		return
	}
	u.poisoned--
	dmg := max(int(PoisonShare*float64(u.Health)), 1)
	if u.boss {
		dmg = u.poisonDmg
	}
	f.hurt(u, dmg)
	u.gain(EnergyWhenHit)
	f.record(KindPoisoned, u, u, dmg, false)
	if !u.alive() {
		f.record(KindFall, u, u, 0, false)
	}
}

// provoke draws enemy Tanks, Dealers and Healers to u for the rest of this round
// and the next ProvokeRounds.
func (f *fight) provoke(u *unit) {
	u.provoking = f.round + ProvokeRounds
	f.record(KindProvoke, u, u, 0, false)
}

// target is a living provoker for Tanks, Dealers and Healers, else the first
// living front unit, or the first back one; a Support ignores provokers and picks the
// back unit with the least health, else the front.
func (f *fight) target(u *unit) *unit {
	foes := f.teams[1-u.side]
	if u.Class == ClassSupport {
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

// weakestAlly is u's living ally, not u, with the least health, if any.
func (f *fight) weakestAlly(u *unit) *unit {
	var weakest *unit
	for _, c := range f.teams[u.side] {
		if c != u && c.alive() && (weakest == nil || c.hp < weakest.hp) {
			weakest = c
		}
	}
	return weakest
}

func healAmount(to *unit) int {
	return min(int(HealShare*float64(to.Health)), to.Health-to.hp)
}

// edge is whether a's role beats b's, round the circle of four: Tank beats
// Dealer, Dealer Support, Support Healer, Healer Tank.
func edge(a, b Class) bool {
	return b == (a+1)%4
}

func (f *fight) hit(from, to *unit, factor float64, kind string) {
	f.strike(from, to, factor, kind, false)
}

// strike is a hit of factor times from's attack on to, past to's shield
// with pierce, with both units' passives at work; it gives the damage dealt,
// or -1 for a hit dodged.
func (f *fight) strike(from, to *unit, factor float64, kind string, pierce bool) int {
	if v := to.passive(Dodge); v > 0 && f.rng.Float64() < v {
		f.record(KindMiss, from, to, 0, false)
		return -1
	}
	edged := edge(from.Class, to.Class)
	if edged {
		factor *= EdgeFactor
	}
	if v := from.passive(Berserk); v > 0 && 2*from.hp < from.Health {
		factor *= 1 + v
	}
	if v := from.passive(Executioner); v > 0 && to.hp < int(ExecuteBelow*float64(to.Health)) {
		factor *= 1 + v
	}
	if v := from.passive(Crit); v > 0 && f.rng.Float64() < v {
		factor *= CritFactor
		if kind == KindHit {
			kind = KindStrike
		}
	}
	spread := DamageSpreadLo + (DamageSpreadHi-DamageSpreadLo)*f.rng.Float64()
	dmg := int(float64(from.Attack)*factor*spread + 0.5)
	if dmg < 1 {
		dmg = 1
	}
	if to.provoking > 0 && to.provoking >= f.round {
		dmg = max(int(float64(dmg)*ProvokeGuard+0.5), 1)
	}
	if v := to.passive(Armor); v > 0 {
		dmg = max(int(float64(dmg)*(1-v)+0.5), 1)
	}
	rest := dmg
	if to.shield > 0 && !pierce {
		absorbed := min(rest, to.shield)
		to.shield -= absorbed
		rest -= absorbed
	}
	f.hurt(to, rest)
	to.gain(EnergyWhenHit)
	f.record(kind, from, to, dmg, edged)
	if !to.alive() {
		f.record(KindFall, to, to, 0, false)
	}
	if v := to.passive(Thorns); v > 0 && from.alive() && rest > 0 {
		back := max(int(v*float64(rest)), 1)
		f.hurt(from, back)
		f.record(KindHit, to, from, back, false)
		if !from.alive() {
			f.record(KindFall, from, from, 0, false)
		}
	}
	if v := from.passive(Vampire); v > 0 && from.alive() {
		f.healAmountBy(from, from, int(v*float64(dmg)))
	}
	return dmg
}

// hurt takes dmg off u's health; a boss's spent takes it up a level, as
// many as dmg spends, the rest going on into the next.
func (f *fight) hurt(u *unit, dmg int) {
	if !u.boss {
		u.hp = max(u.hp-dmg, 0)
		return
	}
	f.bossDealt += dmg
	u.hp -= dmg
	for u.hp <= 0 {
		f.bossLevel++
		f.bossLevelHP = f.bossLevels.LevelHP(f.bossLevel)
		u.hp += f.bossLevelHP
		u.Attack = int(math.Round(float64(f.bossBase) * math.Pow(f.bossLevels.AttackGrowth, float64(f.bossLevel-1))))
		f.events = append(f.events, Event{Round: f.round, Side: u.side, From: u.slot, To: u.slot, Kind: KindLevel,
			Amount: f.bossLevel, HP: max(u.hp, 0), Energy: u.energy, ToEnergy: u.energy})
	}
}

func (f *fight) heal(from, to *unit) {
	amount := healAmount(to)
	to.hp += amount
	f.record(KindHeal, from, to, amount, false)
}

func (f *fight) shield(from, to *unit) {
	amount := int(ShieldShare * float64(to.Health))
	to.shield += amount
	f.record(KindShield, from, to, amount, false)
}

// record keeps what from did to to in the current round, with both units'
// health and energy as they are now.
func (f *fight) record(kind string, from, to *unit, amount int, edged bool) {
	f.events = append(f.events, Event{
		Round: f.round, Side: from.side, From: from.slot, To: to.slot, Kind: kind,
		Amount: amount, HP: to.hp, Energy: from.energy, ToEnergy: to.energy, Edge: edged, Ult: f.casting,
	})
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
