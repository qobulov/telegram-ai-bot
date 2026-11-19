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
		{100, 399, ClassFighter},
		{100, 301, ClassFighter},
		{100, 300, ClassMage},
		{100, 200, ClassMage},
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

func TestHealAndShieldFire(t *testing.T) {
	var a, d Team
	a.Front[0] = FighterOf(8, KindShield, 4, 1, 50, 1000) // Shield
	a.Front[1] = FighterOf(7, "healer", 4, 1, 50, 1000)   // Heal
	a.Back[0] = FighterOf(6, "striker", 4, 1, 50, 1000)   // Strike
	d.Front[0] = FighterOf(100, "wall", 1, 1, 150, 20000) // hurts the attackers
	d.Front[1] = FighterOf(101, "wall2", 1, 1, 150, 20000)
	r := Battle(a, d, 9)
	count := map[string]int{}
	for _, e := range r.Events {
		count[e.Kind]++
		if e.Kind == KindShield && (e.Round != 0 || e.Amount != 300 || e.Side != 0) {
			t.Errorf("bad shield event %+v", e)
		}
		if e.Kind == KindHeal && (e.Side != 0 || e.Amount <= 0) {
			t.Errorf("bad heal event %+v", e)
		}
	}
	for _, k := range []string{KindShield, KindHeal, KindStrike, KindHit} {
		if count[k] == 0 {
			t.Errorf("no %s event", k)
		}
	}
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
		{ClassTank, ClassFighter, true},
		{ClassFighter, ClassMage, true},
		{ClassMage, ClassTank, true},
		{ClassFighter, ClassTank, false},
		{ClassMage, ClassFighter, false},
		{ClassTank, ClassMage, false},
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

// stunWall is a defender of one Tank too tough to fall, with no shield.
func stunWall() Team {
	var d Team
	d.Front[0] = FighterOf(100, "wall", 1, 1, 1, 1_000_000)
	return d
}

// A Stun on its 3rd turn skips the target's next turn; a second stunner
// the same round only hits, as the target is stunned already.
func TestStunSkipsATurn(t *testing.T) {
	var a Team
	a.Front[0] = FighterOf(9, "stun1", 3, 1, 100, 350) // Stun, Fighter
	a.Front[1] = FighterOf(15, "stun2", 3, 1, 90, 350) // Stun, Fighter, acts after
	r := Battle(a, stunWall(), 1)
	stuns, wallActs := map[int]int{}, map[int][]string{}
	for _, e := range r.Events {
		if e.Kind == KindStun {
			stuns[e.Round]++
			if e.Side != 0 || e.To != 0 {
				t.Errorf("bad stun %+v", e)
			}
		}
		if e.Side == 1 && e.From == 0 {
			wallActs[e.Round] = append(wallActs[e.Round], e.Kind)
		}
	}
	if stuns[3] != 1 || stuns[1] != 0 || stuns[2] != 0 {
		t.Errorf("stuns by round = %v, want one in round 3", stuns)
	}
	if !reflect.DeepEqual(wallActs[3], []string{KindStunned}) {
		t.Errorf("the wall's round 3 = %v, want only stunned", wallActs[3])
	}
	if !reflect.DeepEqual(wallActs[4], []string{KindHit}) {
		t.Errorf("the wall's round 4 = %v, want a hit", wallActs[4])
	}
}

// A Poison on its 3rd turn hurts the target by PoisonShare of its health at
// the start of its next PoisonTurns turns, past its shield.
func TestPoisonTicks(t *testing.T) {
	var a, d Team
	a.Front[0] = FighterOf(10, "poisoner", 3, 1, 50, 175) // Poison, Fighter
	d.Front[0] = FighterOf(8, "wall", 3, 1, 1, 1000)      // Shield, Tank
	r := Battle(a, d, 1)
	var ticks []Event
	for _, e := range r.Events {
		if e.Kind == KindPoisoned {
			ticks = append(ticks, e)
		}
		if e.Kind == KindPoison && e.Round%PoisonEvery != 0 {
			t.Errorf("poison in round %d", e.Round)
		}
	}
	if len(ticks) < 3 {
		t.Fatalf("ticks = %+v", ticks)
	}
	for i, e := range ticks[:3] {
		if e.Round != 3+i || e.Side != 1 || e.From != 0 || e.To != 0 || e.Amount != 80 {
			t.Errorf("tick %d = %+v", i, e)
		}
	}
	// The shield of 300 still holds after three hits of about 50: the poison goes past it.
	if ticks[0].HP != 920 {
		t.Errorf("hp after the first tick = %d, want 920", ticks[0].HP)
	}
}

// Poison lasts PoisonTurns turns and can fell its unit.
func TestPoisonRunsOutAndFells(t *testing.T) {
	f := &fight{}
	u := &unit{Fighter: Fighter{Health: 1000}, hp: 1000, poisoned: PoisonTurns}
	for range PoisonTurns + 2 {
		f.poisonTick(u)
	}
	if len(f.events) != PoisonTurns || u.hp != 1000-PoisonTurns*80 || u.poisoned != 0 {
		t.Errorf("events %+v, hp %d", f.events, u.hp)
	}
	f = &fight{}
	u = &unit{Fighter: Fighter{Health: 10}, hp: 1, poisoned: 1} // 8% of 10 floors at 1
	f.poisonTick(u)
	if len(f.events) != 2 || f.events[0].Amount != 1 || f.events[0].HP != 0 || f.events[1].Kind != KindFall {
		t.Errorf("a poison fall = %+v", f.events)
	}
}

// A Provoke at the start draws enemy Tanks and Fighters for rounds 1 and 2,
// again after its 4th turn; a Mage keeps to the back row's weakest.
func TestProvokeRedirects(t *testing.T) {
	var a, d Team
	a.Front[0] = FighterOf(1, "fighter", 1, 1, 100, 350) // Fighter
	a.Back[0] = FighterOf(2, "mage", 1, 1, 100, 200)     // Mage
	d.Front[0] = FighterOf(100, "f0", 1, 1, 1, 1_000_000)
	d.Front[1] = FighterOf(101, "f1", 1, 1, 1, 1_000_000)
	d.Back[0] = FighterOf(11, "provoker", 3, 1, 1, 1_000_000) // Provoke, Tank, acts last
	d.Back[1] = FighterOf(102, "b1", 1, 1, 1, 500_000)        // the weakest behind
	d.Back[2] = FighterOf(103, "b2", 1, 1, 1, 600_000)
	r := Battle(a, d, 1)
	provokes := []int{}
	fighterHit := map[int]int{}
	for _, e := range r.Events {
		switch {
		case e.Kind == KindProvoke:
			provokes = append(provokes, e.Round)
		case e.Side == 0 && e.From == 0 && e.Kind == KindHit:
			fighterHit[e.Round] = e.To
		case e.Side == 0 && e.From == 2 && e.To != 3:
			t.Fatalf("the mage hit slot %d in round %d", e.To, e.Round)
		}
	}
	if len(provokes) < 2 || provokes[0] != 0 || provokes[1] != 4 {
		t.Errorf("provokes in rounds %v", provokes)
	}
	want := map[int]int{1: 2, 2: 2, 3: 0, 4: 0, 5: 2, 6: 2, 7: 0}
	for round, slot := range want {
		if fighterHit[round] != slot {
			t.Errorf("round %d: the fighter hit slot %d, want %d", round, fighterHit[round], slot)
		}
	}
}
