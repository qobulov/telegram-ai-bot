package arena

import "testing"

func kitted(id int64, attack, health int, k Kit) Fighter {
	f := FighterOf(id, "k", 4, 1, attack, health)
	f.Kit = &k
	return f
}

// three foes of plenty of health, so that nobody falls
func threeFoes() Team {
	var d Team
	for i := range 3 {
		f := FighterOf(int64(200+i), "foe", 1, 1, 1, 1_000_000)
		if i < 2 {
			d.Front[i] = f
		} else {
			d.Back[0] = f
		}
	}
	return d
}

// An Ultimate on all foes hits each; one that stuns for two turns skips two.
func TestUltHitsAllAndStunsTwoTurns(t *testing.T) {
	var a Team
	a.Front[0] = kitted(1, 100, 1000, Kit{Ult: Ult{Name: "aoe", Factor: 1, Target: TargetAll, Stun: 2}})
	f := soloFight(a, threeFoes())
	u := f.teams[0][0]
	u.energy = EnergyMax
	f.act(u)
	stuns := 0
	for _, e := range f.events {
		if e.Kind == KindStun {
			stuns++
		}
	}
	if stuns != 3 || u.energy != 0 {
		t.Fatalf("events %+v, want 3 stuns", f.events)
	}
	foe := f.teams[1][0]
	f.act(foe)
	f.act(foe)
	if foe.stun != 0 || lastOf(f.events, KindStunned).To != 0 {
		t.Errorf("stun left %d", foe.stun)
	}
}

// A heal-only Ultimate with nobody hurt keeps its energy and hits plainly.
func TestHealOnlyUltWaits(t *testing.T) {
	var a Team
	a.Front[0] = kitted(1, 100, 1000, Kit{Ult: Ult{Name: "heal", Heal: WhoAll, HealShare: 0.2}})
	f := soloFight(a, threeFoes())
	u := f.teams[0][0]
	u.energy = EnergyMax
	f.act(u)
	if u.energy != EnergyMax || f.events[len(f.events)-1].Kind != KindHit {
		t.Errorf("events %+v, energy %d", f.events, u.energy)
	}
}

// Passives: a Dodge escapes hits, Thorns send damage back, Armor takes less,
// Regen heals each turn, Swift starts with energy and Aura raises the side.
func TestPassives(t *testing.T) {
	var a, d Team
	a.Front[0] = FighterOf(1, "hitter", 1, 1, 100, 100_000)
	d.Front[0] = kitted(2, 10, 100_000, Kit{Passive: Passive{Kind: Dodge, Value: 1}})
	f := soloFight(a, d)
	f.hit(f.teams[0][0], f.teams[1][0], 1, KindHit)
	if e := f.events[0]; e.Kind != KindMiss || f.teams[1][0].hp != 100_000 {
		t.Errorf("dodge: %+v", e)
	}

	d.Front[0] = kitted(2, 10, 100_000, Kit{Passive: Passive{Kind: Thorns, Value: 0.5}})
	f = soloFight(a, d)
	f.hit(f.teams[0][0], f.teams[1][0], 1, KindHit)
	taken := 100_000 - f.teams[1][0].hp
	if back := 100_000 - f.teams[0][0].hp; back != taken/2 {
		t.Errorf("thorns sent %d of %d", back, taken)
	}

	d.Front[0] = kitted(2, 10, 100_000, Kit{Passive: Passive{Kind: Armor, Value: 0.5}})
	f = soloFight(a, d)
	f.hit(f.teams[0][0], f.teams[1][0], 1, KindHit)
	if taken := 100_000 - f.teams[1][0].hp; taken > 56 {
		t.Errorf("armour let %d through of about 100", taken)
	}

	a.Front[0] = kitted(1, 100, 1000, Kit{Passive: Passive{Kind: Regen, Value: 0.1}})
	f = soloFight(a, threeFoes())
	u := f.teams[0][0]
	u.hp = 500
	f.act(u)
	if u.hp != 600 {
		t.Errorf("regen: hp %d, want 600", u.hp)
	}

	a.Front[0] = kitted(1, 100, 1000, Kit{Passive: Passive{Kind: Swift, Value: 40}})
	a.Front[1] = kitted(3, 100, 1000, Kit{Passive: Passive{Kind: Aura, Value: 0.1}})
	f = soloFight(a, threeFoes())
	if s := f.teams[0][0]; s.energy != StartEnergy(s.Class)+40 || s.Attack != 110 {
		t.Errorf("swift and aura: energy %d, attack %d", s.energy, s.Attack)
	}
}

// A piercing Ultimate goes past a shield; a lifesteal one heals its caster.
func TestPierceAndLifesteal(t *testing.T) {
	var a Team
	a.Front[0] = kitted(1, 100, 1000, Kit{Ult: Ult{Name: "pierce", Factor: 2, Pierce: true, Lifesteal: 0.5}})
	f := soloFight(a, threeFoes())
	u, foe := f.teams[0][0], f.teams[1][0]
	foe.shield = 1_000_000
	u.hp, u.energy = 500, EnergyMax
	f.act(u)
	if foe.hp == 1_000_000 || foe.shield != 1_000_000 {
		t.Errorf("pierce: hp %d, shield %d", foe.hp, foe.shield)
	}
	if u.hp <= 500 {
		t.Errorf("lifesteal: hp %d", u.hp)
	}
}

// A stun holds no boss, and none holds a unit in its turn after a stun.
func TestStunGuardAndBoss(t *testing.T) {
	u := &unit{Fighter: FighterOf(1, "u", 1, 1, 10, 100), hp: 100}
	u.stunFor(1)
	f := soloFight(Team{}, Team{})
	f.act(u)
	if u.stun != 0 || !u.stunGuard {
		t.Fatalf("after the stunned turn: stun %d, guard %v", u.stun, u.stunGuard)
	}
	u.stunFor(2)
	if u.stun != 0 {
		t.Errorf("stunned again in its guarded turn: %d", u.stun)
	}
	f.act(u)
	if u.stunGuard {
		t.Error("the guard outlived its turn")
	}
	u.stunFor(1)
	if u.stun != 1 {
		t.Errorf("not stunned after the guard: %d", u.stun)
	}
	boss := &unit{Fighter: FighterOf(2, "boss", 4, 1, 10, 100), hp: 100, boss: true}
	boss.stunFor(2)
	if boss.stun != 0 {
		t.Errorf("a boss stunned for %d", boss.stun)
	}
}
