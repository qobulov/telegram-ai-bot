package arena

// A Kit is a Mythic's own two skills, as heroes have in Mobile Legends, as
// the owners asked on 2026-11-25: a Passive always at work, and an Ultimate
// used when its energy is full, in place of the six shared skills.
type Kit struct {
	Ult     Ult
	Passive Passive
}

// UltTarget is whom an Ultimate's hit falls on.
type UltTarget int

const (
	TargetNormal      UltTarget = iota // the unit's own target, as a plain hit
	TargetAll                          // every living foe
	TargetBackWeakest                  // the back foe with the least health, else the front
	TargetFront                        // the living foes in front, else the first behind
	TargetTwo                          // two living foes at random
	TargetStrongest                    // the living foe with the most attack
)

// Who is whom an Ultimate's heal or shield goes to.
type Who int

const (
	WhoNone        Who = iota
	WhoSelf            // the unit itself
	WhoWeakest         // the ally with the least health (the hurt one, for a heal)
	WhoSelfWeakest     // the unit and its weakest other ally
	WhoAll             // every living ally, the unit too
)

// Ult is an Ultimate: what it does, in this order: a Provoke, a Shield and a
// Heal on its side, then a hit of Factor times the attack on its targets
// (none for 0), each of which it may stun for Stun turns or poison; Pierce
// hits past shields, Execute raises the hit on a target below ExecuteBelow
// of its health by that share, and Lifesteal heals the unit that share of
// the damage it dealt.
type Ult struct {
	Name        string
	Factor      float64
	Target      UltTarget
	Stun        int
	Poison      bool
	Pierce      bool
	Execute     float64
	Lifesteal   float64
	Heal        Who
	HealShare   float64
	Shield      Who
	ShieldShare float64
	Provoke     bool
}

// PassiveKind is a Passive's kind.
type PassiveKind int

const (
	PassiveNone PassiveKind = iota
	Berserk                 // attack raised by Value below half health
	Armor                   // damage taken lowered by Value
	Vampire                 // heals Value of the damage it deals
	Thorns                  // sends Value of the damage it takes back
	Dodge                   // escapes a hit at a chance of Value
	Crit                    // hits for CritFactor at a chance of Value
	Regen                   // heals Value of its health at each of its turns
	Aura                    // its side's attack raised by Value
	Executioner             // hits a foe below ExecuteBelow of its health Value harder
	Swift                   // starts with Value more energy
)

// Passive is a Kit's always-on skill.
type Passive struct {
	Kind  PassiveKind
	Value float64
}

const (
	CritFactor   = 1.6  // a Crit's hit, times the attack
	ExecuteBelow = 0.3  // an Execute or Executioner bites below this share of health
	AuraMost     = 0.25 // a side's Auras raise its attack at most this much
	BossStunMost = 1    // a boss skips at most one turn to a stun
)

// KindMiss is a hit dodged: Amount 0.
const KindMiss = "miss"

func (u *unit) passive(k PassiveKind) float64 {
	if u.Kit == nil || u.Kit.Passive.Kind != k {
		return 0
	}
	return u.Kit.Passive.Value
}

// applyKits sets up the passives a fight starts with: each side's Auras on
// its attack and the Swift ones' energy.
func (f *fight) applyKits() {
	for _, side := range f.teams {
		aura := 0.0
		for _, u := range side {
			if u.alive() {
				aura += u.passive(Aura)
			}
		}
		aura = min(aura, AuraMost)
		for _, u := range side {
			if aura > 0 {
				u.Attack = int(float64(u.Attack)*(1+aura) + 0.5)
			}
			u.energy = min(u.energy+int(u.passive(Swift)), EnergyMax)
		}
	}
}

// regen heals a Regen unit at the start of its turn.
func (f *fight) regen(u *unit) {
	v := u.passive(Regen)
	if v == 0 || u.hp >= u.Health {
		return
	}
	f.healAmountBy(u, u, int(v*float64(u.Health)))
}

// healOnly reports an Ultimate that does nothing but heal.
func (ult Ult) healOnly() bool {
	return ult.Heal != WhoNone && ult.Factor == 0 && ult.Shield == WhoNone && !ult.Provoke && ult.Stun == 0
}

// useUlt spends u's energy on its Ultimate and says whether it did: one
// that only heals, with nobody hurt, keeps it for a turn that needs it.
func (f *fight) useUlt(u *unit) bool {
	ult := u.Kit.Ult
	if ult.healOnly() && f.weakestHurt(u.side) == nil {
		return false
	}
	u.energy = 0
	f.casting = ult.Name
	defer func() { f.casting = "" }()
	if ult.Provoke {
		f.provoke(u)
	}
	for _, to := range f.allies(u, ult.Shield) {
		f.shieldBy(u, to, ult.ShieldShare)
	}
	for _, to := range f.allies(u, ult.Heal) {
		f.healBy(u, to, ult.HealShare)
	}
	if ult.Factor == 0 && ult.Stun == 0 {
		return true
	}
	kind := KindStrike
	switch {
	case ult.Stun > 0:
		kind = KindStun
	case ult.Poison:
		kind = KindPoison
	case ult.Factor < 1.5:
		kind = KindHit
	}
	for _, to := range f.ultTargets(u, ult.Target) {
		x := ult.Factor
		if ult.Execute > 0 && to.hp < int(ExecuteBelow*float64(to.Health)) {
			x *= 1 + ult.Execute
		}
		dealt := f.strike(u, to, x, kind, ult.Pierce)
		if ult.Lifesteal > 0 && dealt > 0 && u.alive() {
			f.healAmountBy(u, u, int(ult.Lifesteal*float64(dealt)))
		}
		if !to.alive() || dealt < 0 {
			continue
		}
		if ult.Stun > 0 {
			turns := ult.Stun
			if to.boss {
				turns = BossStunMost
			}
			to.stun = max(to.stun, turns)
		}
		if ult.Poison {
			to.poisoned = PoisonTurns
			to.poisonDmg = max(int(BossPoison*float64(u.Attack)), 1)
		}
	}
	return true
}

// allies are whom who names on u's side.
func (f *fight) allies(u *unit, who Who) []*unit {
	switch who {
	case WhoSelf:
		return []*unit{u}
	case WhoWeakest:
		if w := f.weakestHurt(u.side); w != nil {
			return []*unit{w}
		}
		if w := f.weakestAlly(u); w != nil {
			return []*unit{w}
		}
		return []*unit{u}
	case WhoSelfWeakest:
		if w := f.weakestAlly(u); w != nil {
			return []*unit{u, w}
		}
		return []*unit{u}
	case WhoAll:
		var all []*unit
		for _, c := range f.teams[u.side] {
			if c.alive() {
				all = append(all, c)
			}
		}
		return all
	}
	return nil
}

// ultTargets are the living foes an Ultimate's hit falls on.
func (f *fight) ultTargets(u *unit, t UltTarget) []*unit {
	foes := f.teams[1-u.side]
	var living []*unit
	for _, c := range foes {
		if c.alive() {
			living = append(living, c)
		}
	}
	if len(living) == 0 {
		return nil
	}
	switch t {
	case TargetAll:
		return living
	case TargetBackWeakest:
		return []*unit{mageTarget(foes)}
	case TargetFront:
		var front []*unit
		for _, c := range foes[:2] {
			if c.alive() {
				front = append(front, c)
			}
		}
		if len(front) > 0 {
			return front
		}
		return []*unit{firstLiving(foes[2:])}
	case TargetTwo:
		if len(living) <= 2 {
			return living
		}
		i := f.rng.Intn(len(living))
		j := f.rng.Intn(len(living) - 1)
		if j >= i {
			j++
		}
		return []*unit{living[i], living[j]}
	case TargetStrongest:
		best := living[0]
		for _, c := range living[1:] {
			if c.Attack > best.Attack {
				best = c
			}
		}
		return []*unit{best}
	}
	if c := f.target(u); c != nil {
		return []*unit{c}
	}
	return nil
}

func (f *fight) healBy(from, to *unit, share float64) {
	f.healAmountBy(from, to, int(share*float64(to.Health)))
}

func (f *fight) healAmountBy(from, to *unit, amount int) {
	if !to.alive() {
		return
	}
	amount = min(amount, to.Health-to.hp)
	if amount <= 0 {
		return
	}
	to.hp += amount
	f.record(KindHeal, from, to, amount, false)
}

func (f *fight) shieldBy(from, to *unit, share float64) {
	amount := int(share * float64(to.Health))
	if amount <= 0 {
		return
	}
	to.shield += amount
	f.record(KindShield, from, to, amount, false)
}
