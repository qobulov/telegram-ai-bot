package arena

import (
	"math/rand"
	"reflect"
	"testing"
)

func commonTeam(attack, health int) Team {
	var t Team
	for i := 0; i < 5; i++ {
		f := FighterOf(int64(i*3+1000), "c", 1, 1, attack, health)
		if i < 2 {
			t.Front[i] = f
		} else {
			t.Back[i-2] = f
		}
	}
	return t
}

func TestFighterOfIsStable(t *testing.T) {
	cases := []struct {
		attack, health int
		class          Class
	}{
		{100, 400, ClassTank},
		{100, 399, ClassDealer},
		{100, 301, ClassDealer},
		{100, 300, ClassSupport},
		{100, 200, ClassSupport},
	}
	for _, c := range cases {
		if got := FighterOf(1, "x", 1, 1, c.attack, c.health).Class; got != c.class {
			t.Errorf("%d/%d: class %v, want %v", c.attack, c.health, got, c.class)
		}
	}
	for id, want := range map[int64]Skill{6: Strike, 7: Heal, 8: Shield, 9: Stun, 10: Poison, 11: Provoke, -1: Provoke} {
		if got := FighterOf(id, "x", 3, 1, 100, 350).Skill; got != want {
			t.Errorf("id %d: skill %v, want %v", id, got, want)
		}
		if got := FighterOf(id, "x", 4, 1, 100, 350).Skill; got != want {
			t.Errorf("mythic id %d: skill %v, want %v", id, got, want)
		}
		if got := FighterOf(id, "x", 2, 1, 100, 350).Skill; got != NoSkill {
			t.Errorf("rare id %d has skill %v", id, got)
		}
	}
	a := FighterOf(7, "x", 4, 2, 120, 480)
	if a != FighterOf(7, "x", 4, 2, 120, 480) {
		t.Error("FighterOf is not stable")
	}
}

func TestPower(t *testing.T) {
	if got := commonTeam(100, 400).Power(); got != 5*(100+100) {
		t.Errorf("power %d", got)
	}
}

func TestBattleIsDeterministic(t *testing.T) {
	// Every skill on both sides, of all three classes.
	skilled := func(from int64) Team {
		var t Team
		stats := [][2]int{{100, 450}, {100, 350}, {100, 250}, {90, 400}, {110, 300}}
		for i, s := range stats {
			f := FighterOf(from+int64(i), "s", 4, 1, s[0], s[1])
			if i < 2 {
				t.Front[i] = f
			} else {
				t.Back[i-2] = f
			}
		}
		return t
	}
	for _, teams := range [][2]Team{{commonTeam(100, 350), commonTeam(90, 380)}, {skilled(0), skilled(3)}} {
		a, d := teams[0], teams[1]
		r1, r2 := Battle(a, d, 42), Battle(a, d, 42)
		if !reflect.DeepEqual(r1, r2) {
			t.Fatal("same seed gave different battles")
		}
		if reflect.DeepEqual(r1.Events, Battle(a, d, 43).Events) {
			t.Error("a different seed gave the same events")
		}
	}
}

func TestStrongerTeamWins(t *testing.T) {
	var strong, weak Team
	for i := 0; i < 5; i++ {
		s := FighterOf(int64(i+1), "m", 4, 5, 400, 1200)
		w := FighterOf(int64(i+100), "c", 1, 1, 100, 350)
		if i < 2 {
			strong.Front[i], weak.Front[i] = s, w
		} else {
			strong.Back[i-2], weak.Back[i-2] = s, w
		}
	}
	for seed := int64(0); seed < 20; seed++ {
		r := Battle(strong, weak, seed)
		if !r.AttackerWon || r.Rounds >= MaxRounds {
			t.Fatalf("seed %d: attacker won %v in %d rounds", seed, r.AttackerWon, r.Rounds)
		}
		r = Battle(weak, strong, seed)
		if r.AttackerWon {
			t.Fatalf("seed %d: weak attacker won", seed)
		}
	}
}

func TestTimeoutGoesByHealthLeft(t *testing.T) {
	// Tanks with tiny attack never finish each other in 30 rounds.
	a, d := commonTeam(10, 40000), commonTeam(10, 40000)
	r := Battle(a, d, 1)
	if r.Rounds != MaxRounds {
		t.Fatalf("rounds %d, want %d", r.Rounds, MaxRounds)
	}
	// A clearly healthier attacker wins the timeout.
	a = commonTeam(10, 50000)
	if r = Battle(a, d, 1); !r.AttackerWon || r.Rounds != MaxRounds {
		t.Errorf("healthier attacker lost: %v in %d", r.AttackerWon, r.Rounds)
	}
	// A clearly healthier defender wins.
	if r = Battle(d, a, 1); r.AttackerWon {
		t.Error("healthier defender lost")
	}
	// Damage floors at 1, so identical zero-attack teams chip each other by 1
	// a hit; the defender, acting second, is not behind at the end.
	z := commonTeam(0, 400)
	if r = Battle(z, z, 1); r.AttackerWon {
		t.Error("exact tie went to the attacker")
	}
}

func TestMageHitsTheBackRow(t *testing.T) {
	var a, d Team
	a.Front[0] = FighterOf(1, "mage", 1, 1, 100, 200) // Mage
	d.Front[0] = FighterOf(2, "f0", 1, 1, 100, 5000)
	d.Front[1] = FighterOf(3, "f1", 1, 1, 100, 5000)
	d.Back[0] = FighterOf(4, "b0", 1, 1, 100, 900)
	d.Back[1] = FighterOf(5, "b1", 1, 1, 100, 500) // least health
	d.Back[2] = FighterOf(6, "b2", 1, 1, 100, 700)
	r := Battle(a, d, 5)
	for _, e := range r.Events {
		if e.Round == 1 && e.Side == 0 && e.From == 0 && e.Kind == KindHit {
			if e.To != 3 {
				t.Fatalf("mage hit slot %d, want 3", e.To)
			}
			return
		}
	}
	t.Fatal("mage never hit in round 1")
}

func TestFallIsEmitted(t *testing.T) {
	var a, d Team
	a.Front[0] = FighterOf(1, "a", 1, 1, 500, 500)
	d.Front[0] = FighterOf(2, "d", 1, 1, 10, 100)
	r := Battle(a, d, 3)
	last := r.Events[len(r.Events)-1]
	if !r.AttackerWon || last.Kind != KindFall || last.Side != 1 || last.HP != 0 {
		t.Errorf("result %v, last event %+v", r.AttackerWon, last)
	}
}

func TestHealGoesToTheHurtAllyNotTheLowestHP(t *testing.T) {
	f := &fight{}
	for slot := range f.teams[0] {
		f.teams[0][slot] = &unit{} // empty slots
	}
	// slot 1: full health but the lowest hp; slot 2: hurt, with more hp.
	f.teams[0][0] = &unit{Fighter: Fighter{Health: 1000}, side: 0, slot: 0, hp: 1000}
	f.teams[0][1] = &unit{Fighter: Fighter{Health: 200}, side: 0, slot: 1, hp: 200}
	f.teams[0][2] = &unit{Fighter: Fighter{Health: 1000}, side: 0, slot: 2, hp: 500}
	if got := f.weakestHurt(0); got == nil || got.slot != 2 {
		t.Fatalf("weakestHurt = %+v, want slot 2", got)
	}
	f.teams[0][2].hp = 1000
	if got := f.weakestHurt(0); got != nil {
		t.Errorf("nobody is hurt but got slot %d", got.slot)
	}
}

func TestEmptyTeamsDefenderWins(t *testing.T) {
	if r := Battle(Team{}, Team{}, 1); r.AttackerWon {
		t.Error("two empty teams: attacker won")
	}
}

// A hit from the class that beats the target's does EdgeFactor times the
// damage, a skill's too, and says so.
func TestEdgeTriangle(t *testing.T) {
	cases := []struct {
		from, to Class
		edge     bool
	}{
		{ClassTank, ClassDealer, true},
		{ClassDealer, ClassSupport, true},
		{ClassSupport, ClassHealer, true},
		{ClassHealer, ClassTank, true},
		{ClassDealer, ClassTank, false},
		{ClassSupport, ClassDealer, false},
		{ClassTank, ClassSupport, false},
		{ClassHealer, ClassSupport, false},
		{ClassTank, ClassTank, false},
	}
	for _, c := range cases {
		for _, factor := range []float64{1, StrikeFactor} {
			f := &fight{rng: rand.New(rand.NewSource(7))}
			from := &unit{Fighter: Fighter{Attack: 1000, Class: c.from}}
			to := &unit{Fighter: Fighter{Health: 100000, Class: c.to}, hp: 100000}
			f.hit(from, to, factor, KindHit)
			spread := DamageSpreadLo + (DamageSpreadHi-DamageSpreadLo)*rand.New(rand.NewSource(7)).Float64()
			want := factor
			if c.edge {
				want *= EdgeFactor
			}
			e := f.events[0]
			if e.Amount != int(1000*want*spread+0.5) || e.Edge != c.edge {
				t.Errorf("%v on %v ×%v: %+v, want %d edge %v", c.from, c.to, factor, e, int(1000*want*spread+0.5), c.edge)
			}
		}
	}
}

// soloFight is a fight of a against d in round 1, built as Battle builds
// it, for a turn to be played by hand.
func soloFight(a, d Team) *fight {
	f := newFight(a, d, 2)
	f.round = 1
	return f
}

// lastOf is the last event of kind, or a zero one.
func lastOf(events []Event, kind string) Event {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == kind {
			return events[i]
		}
	}
	return Event{}
}

func TestStartEnergyByClass(t *testing.T) {
	var a Team
	a.Front[0] = FighterOf(1, "tank", 1, 1, 100, 500)
	a.Front[1] = FighterOf(2, "fighter", 1, 1, 100, 350)
	a.Back[0] = FighterOf(3, "mage", 1, 1, 100, 200)
	f := newFight(a, Team{}, 1)
	for slot, want := range []int{StartEnergyTank, StartEnergyDealer, StartEnergySupport} {
		if got := f.teams[0][slot].energy; got != want {
			t.Errorf("slot %d starts with %d energy, want %d", slot, got, want)
		}
	}
}

// A plain hit fills the hitter by EnergyPerAction and the one hit by
// EnergyWhenHit, both capped at EnergyMax, and the event says so.
func TestEnergyGain(t *testing.T) {
	var a Team
	a.Front[0] = FighterOf(1, "a", 1, 1, 100, 350) // Fighter, no skill
	f := soloFight(a, stunWall())
	u, wall := f.teams[0][0], f.teams[1][0]
	f.act(u)
	e := lastOf(f.events, KindHit)
	if e.Energy != StartEnergyDealer+EnergyPerAction || u.energy != e.Energy {
		t.Errorf("hitter's energy %d (event %d), want %d", u.energy, e.Energy, StartEnergyDealer+EnergyPerAction)
	}
	if e.ToEnergy != StartEnergyTank+EnergyWhenHit || wall.energy != e.ToEnergy {
		t.Errorf("wall's energy %d (event %d), want %d", wall.energy, e.ToEnergy, StartEnergyTank+EnergyWhenHit)
	}
	for range 5 {
		f.act(u)
	}
	if u.energy != EnergyMax {
		t.Errorf("a unit without a skill holds %d energy, want it capped at %d", u.energy, EnergyMax)
	}
	if lastOf(f.events, KindStrike).Kind != "" {
		t.Error("a unit without a skill used one")
	}
}

// A skill comes on the turn that starts with full energy, spends it all
// and gains nothing for that turn.
func TestSkillAtFullEnergy(t *testing.T) {
	var a Team
	a.Front[0] = FighterOf(6, "striker", 4, 1, 100, 350) // Strike, Fighter
	f := soloFight(a, stunWall())
	u := f.teams[0][0]
	u.energy = EnergyMax - 1
	f.act(u)
	if e := f.events[len(f.events)-1]; e.Kind != KindHit || e.Energy != EnergyMax {
		t.Fatalf("at %d energy: %+v, want a plain hit filling it", EnergyMax-1, e)
	}
	f.act(u)
	e := f.events[len(f.events)-1]
	if e.Kind != KindStrike || e.Energy != 0 || u.energy != 0 {
		t.Errorf("at full energy: %+v, energy %d, want a strike spending it", e, u.energy)
	}
	if e.Amount < int(100*StrikeFactor*DamageSpreadLo) {
		t.Errorf("strike for %d", e.Amount)
	}
}

// A Heal with nobody hurt hits plainly and keeps its energy; with an ally
// hurt it heals the weakest one and spends it.
func TestHealWithNobodyHurtKeepsEnergy(t *testing.T) {
	var a Team
	a.Front[0] = FighterOf(7, "healer", 4, 1, 100, 350) // Heal
	a.Front[1] = FighterOf(1, "ally", 1, 1, 100, 1000)
	f := soloFight(a, stunWall())
	u, ally := f.teams[0][0], f.teams[0][1]
	u.energy = EnergyMax
	f.act(u)
	if e := f.events[len(f.events)-1]; e.Kind != KindHit || u.energy != EnergyMax {
		t.Fatalf("nobody hurt: %+v, energy %d", e, u.energy)
	}
	ally.hp = 200
	f.act(u)
	e := f.events[len(f.events)-1]
	if e.Kind != KindHeal || e.To != 1 || e.Amount != int(HealShare*1000) || u.energy != 0 {
		t.Errorf("an ally hurt: %+v, energy %d", e, u.energy)
	}
}

// A Shield shields its caster and its ally with the least health, each by
// ShieldShare of its own health, and takes the turn.
func TestShieldOnSelfAndWeakest(t *testing.T) {
	var a Team
	a.Front[0] = FighterOf(8, "shield", 4, 1, 100, 1000) // Shield, Tank
	a.Front[1] = FighterOf(1, "strong", 1, 1, 100, 2000)
	a.Back[0] = FighterOf(2, "weak", 1, 1, 100, 800)
	f := soloFight(a, stunWall())
	f.teams[0][2].hp = 300
	u := f.teams[0][0]
	u.energy = EnergyMax
	f.act(u)
	if len(f.events) != 2 {
		t.Fatalf("events %+v, want two shields", f.events)
	}
	for i, want := range [][2]int{{0, int(ShieldShare * 1000)}, {2, int(ShieldShare * 800)}} {
		e := f.events[i]
		if e.Kind != KindShield || e.Side != 0 || e.From != 0 || e.To != want[0] || e.Amount != want[1] || e.Energy != 0 {
			t.Errorf("shield %d = %+v, want to slot %d for %d", i, e, want[0], want[1])
		}
	}
	if u.shield != int(ShieldShare*1000) || f.teams[0][2].shield != int(ShieldShare*800) || f.teams[0][1].shield != 0 {
		t.Errorf("shields %d, %d, %d", u.shield, f.teams[0][1].shield, f.teams[0][2].shield)
	}
}

// A stunned unit's turn is skipped and fills no energy.
func TestStunnedTurnGainsNothing(t *testing.T) {
	var a Team
	a.Front[0] = FighterOf(9, "stunner", 4, 1, 100, 350) // Stun
	f := soloFight(a, stunWall())
	u, wall := f.teams[0][0], f.teams[1][0]
	u.energy = EnergyMax
	f.act(u)
	if e := lastOf(f.events, KindStun); e.Kind == "" || !wall.stunned {
		t.Fatalf("no stun: %+v", f.events)
	}
	before := wall.energy
	f.act(wall)
	e := f.events[len(f.events)-1]
	if e.Kind != KindStunned || wall.energy != before || e.Energy != before || wall.stunned {
		t.Errorf("stunned turn %+v, energy %d → %d", e, before, wall.energy)
	}
	f.act(wall)
	if e := f.events[len(f.events)-1]; e.Kind != KindHit {
		t.Errorf("the turn after a stun: %+v", e)
	}
}

// A Poison hurts its target at the start of its next PoisonTurns turns by
// PoisonShare of its health, past any shield, each tick filling energy.
func TestPoisonTicks(t *testing.T) {
	var a Team
	a.Front[0] = FighterOf(10, "poisoner", 4, 1, 100, 350) // Poison
	f := soloFight(a, stunWall())
	u, wall := f.teams[0][0], f.teams[1][0]
	u.energy = EnergyMax
	f.act(u)
	if wall.poisoned != PoisonTurns {
		t.Fatalf("poisoned %d", wall.poisoned)
	}
	wall.shield = 1_000_000
	for i := range PoisonTurns + 1 {
		hp, energy := wall.hp, wall.energy
		f.act(wall)
		tick := lastOf(f.events, KindPoisoned)
		if i == PoisonTurns {
			if wall.hp != hp {
				t.Errorf("poison lasted past %d turns", PoisonTurns)
			}
			break
		}
		want := int(PoisonShare * float64(wall.Health))
		if tick.Amount != want || wall.hp != hp-want || wall.energy < min(energy+EnergyWhenHit, EnergyMax) {
			t.Errorf("tick %d = %+v, hp %d → %d, energy %d → %d", i, tick, hp, wall.hp, energy, wall.energy)
		}
	}
}

// Poison can fell its unit.
func TestPoisonFells(t *testing.T) {
	f := &fight{}
	u := &unit{Fighter: Fighter{Health: 10}, hp: 1, poisoned: 1} // the share of 10 floors at 1
	f.poisonTick(u)
	if len(f.events) != 2 || f.events[0].Amount != 1 || f.events[0].HP != 0 || f.events[1].Kind != KindFall {
		t.Errorf("a poison fall = %+v", f.events)
	}
}

// A Provoke draws enemy Tanks and Fighters for the rest of the round and
// ProvokeRounds more, hits besides, and takes ProvokeGuard of the damage
// meanwhile; a Mage keeps to the back row's weakest.
func TestProvokeRedirects(t *testing.T) {
	var a, d Team
	a.Front[0] = FighterOf(1, "fighter", 1, 1, 100, 350) // Fighter
	a.Back[0] = FighterOf(2, "mage", 1, 1, 100, 200)     // Mage
	d.Front[0] = FighterOf(100, "f0", 1, 1, 1, 1_000_000)
	d.Front[1] = FighterOf(101, "f1", 1, 1, 1, 1_000_000)
	d.Back[0] = FighterOf(11, "provoker", 4, 1, 1, 1_000_000) // Provoke, Tank
	d.Back[1] = FighterOf(102, "b1", 1, 1, 1, 500_000)        // the weakest behind
	f := soloFight(a, d)
	p := f.teams[1][2]
	p.energy = EnergyMax
	f.act(p)
	kinds := []string{f.events[0].Kind, f.events[1].Kind}
	if !reflect.DeepEqual(kinds, []string{KindProvoke, KindHit}) || p.energy != 0 || p.provoking != 1+ProvokeRounds {
		t.Fatalf("provoke: %+v, energy %d", f.events, p.energy)
	}
	if got := f.target(f.teams[0][0]); got != p {
		t.Errorf("the fighter goes for slot %d", got.slot)
	}
	if got := f.target(f.teams[0][2]); got.slot != 3 {
		t.Errorf("the mage goes for slot %d, want 3", got.slot)
	}
	f.hit(f.teams[0][0], p, 1, KindHit)
	if e := f.events[len(f.events)-1]; e.Amount > int(100*DamageSpreadHi*ProvokeGuard+1) {
		t.Errorf("the provoker took %d", e.Amount)
	}
	f.round = 1 + ProvokeRounds + 1
	if got := f.target(f.teams[0][0]); got.slot != 0 {
		t.Errorf("after the provoke the fighter goes for slot %d", got.slot)
	}
}

// A tie of speed and attack goes to the seed's side in odd rounds and the
// other in even ones, so that a team against itself wins half the time.
func TestTieBreakIsFair(t *testing.T) {
	team := commonTeam(100, 350)
	for _, seed := range []int64{4, 5} {
		f := newFight(team, team, seed)
		for round := 1; round <= 2; round++ {
			f.round = round
			want := (int(seed&1) + round + 1) % 2
			if got := f.order()[0].side; got != want {
				t.Errorf("seed %d round %d: side %d acts first, want %d", seed, round, got, want)
			}
		}
	}
	var mixed Team
	for i := range 5 {
		f := FighterOf(int64(i), "m", 4, 1, 100+10*i, 250+60*i)
		if i < 2 {
			mixed.Front[i] = f
		} else {
			mixed.Back[i-2] = f
		}
	}
	wins := 0
	const n = 2000
	for seed := range int64(n) {
		if Battle(mixed, mixed, seed).AttackerWon {
			wins++
		}
	}
	if wins < n*40/100 || wins > n*60/100 {
		t.Errorf("a team against itself won %d of %d attacking", wins, n)
	}
}

// stunWall is a defender of one Tank too tough to fall, with no skill.
func stunWall() Team {
	var d Team
	d.Front[0] = FighterOf(100, "wall", 1, 1, 1, 1_000_000)
	return d
}

// A boss never falls, hits one living card at random each turn, harder each
// round, and the damage dealt it is the sum of the team's hits on it.
func TestBossBattle(t *testing.T) {
	boss := FighterOf(99, "boss", 4, 5, 100, 400)
	got := BossBattle(commonTeam(50, 1000), boss, BossLevels{HP: 1 << 30, HPGrowth: 1, AttackGrowth: 1, Rage: 0.5}, 7)
	if got.Rounds == 0 || got.Rounds >= MaxRounds {
		t.Fatalf("rounds %d, want the team to fall before %d", got.Rounds, MaxRounds)
	}
	dealt, hitsByRound, targets := 0, map[int]int{}, map[int]bool{}
	for _, e := range got.Events {
		switch {
		case e.Side == 0 && e.To == 0 && (e.Kind == KindHit || e.Kind == KindStrike):
			dealt += e.Amount
		case e.Side == 1 && e.Kind == KindFall:
			t.Fatalf("the boss fell: %+v", e)
		case e.Side == 1 && e.Kind == KindHit:
			hitsByRound[e.Round]++
			targets[e.To] = true
			if e.HP < 0 {
				t.Fatalf("negative health %+v", e)
			}
		}
	}
	if dealt != got.Damage || dealt == 0 {
		t.Errorf("damage %d, the events say %d", got.Damage, dealt)
	}
	for round, hits := range hitsByRound {
		if hits != 1 {
			t.Errorf("round %d: the boss hit %d cards, want 1", round, hits)
		}
	}
	if len(targets) < 3 {
		t.Errorf("the boss hit only cards %v, want them picked at random", targets)
	}
	again := BossBattle(commonTeam(50, 1000), boss, BossLevels{HP: 1 << 30, HPGrowth: 1, AttackGrowth: 1, Rage: 0.5}, 7)
	if !reflect.DeepEqual(got, again) {
		t.Error("the same seed gave another boss fight")
	}
}

// A boss's rage makes a team that would hold forever fall in the end, and
// with no rage a strong team lasts all MaxRounds.
func TestBossRage(t *testing.T) {
	boss := FighterOf(99, "boss", 4, 5, 10, 400)
	if r := BossBattle(commonTeam(50, 500), boss, BossLevels{HP: 1 << 30, HPGrowth: 1, AttackGrowth: 1}, 1); r.Rounds != MaxRounds {
		t.Errorf("no rage: %d rounds, want %d", r.Rounds, MaxRounds)
	}
	if r := BossBattle(commonTeam(50, 500), boss, BossLevels{HP: 1 << 30, HPGrowth: 1, AttackGrowth: 1, Rage: 1}, 1); r.Rounds >= MaxRounds {
		t.Errorf("rage 1: %d rounds, want fewer than %d", r.Rounds, MaxRounds)
	}
}

// A poisoned boss is hurt by a share of its poisoner's attack, counted as
// dealt and taken off its level's health.
func TestBossPoison(t *testing.T) {
	f := &fight{bossLevel: 1, bossLevels: BossLevels{HP: 1000, HPGrowth: 2, AttackGrowth: 1}}
	u := &unit{Fighter: Fighter{Health: 10}, hp: 1000, boss: true, poisoned: 1, poisonDmg: 30}
	f.poisonTick(u)
	if f.bossDealt != 30 || u.hp != 970 || len(f.events) != 1 || f.events[0].Amount != 30 {
		t.Errorf("dealt %d, hp %d, events %+v", f.bossDealt, u.hp, f.events)
	}
}

// A boss whose level's health is spent goes up a level, as many as a hit
// spends, the rest of the hit going into the next; each level has
// HPGrowth times the health and AttackGrowth times the attack.
func TestBossLevels(t *testing.T) {
	levels := BossLevels{HP: 100, HPGrowth: 2, AttackGrowth: 1.5}
	if levels.LevelHP(1) != 100 || levels.LevelHP(3) != 400 {
		t.Fatalf("level health %d, %d", levels.LevelHP(1), levels.LevelHP(3))
	}
	f := &fight{bossLevel: 1, bossLevelHP: 100, bossBase: 10, bossLevels: levels}
	u := &unit{Fighter: Fighter{Attack: 10}, hp: 100, boss: true}
	f.hurt(u, 350) // 100 for level 1, 200 for level 2, 50 into level 3
	if f.bossLevel != 3 || u.hp != 350 || u.Attack != 23 || f.bossDealt != 350 {
		t.Fatalf("level %d, hp %d, attack %d, dealt %d", f.bossLevel, u.hp, u.Attack, f.bossDealt)
	}
	if len(f.events) != 2 || f.events[0].Kind != KindLevel || f.events[0].Amount != 2 || f.events[1].Amount != 3 || f.events[1].HP != 350 {
		t.Errorf("events %+v", f.events)
	}
	// a strong team against a weak boss climbs levels in a fight
	boss := FighterOf(99, "boss", 4, 5, 10, 400)
	r := BossBattle(commonTeam(500, 5000), boss, BossLevels{HP: 2000, HPGrowth: 1.2, AttackGrowth: 1.1}, 3)
	if r.Level < 5 {
		t.Errorf("reached level %d, want 5 or more", r.Level)
	}
}
