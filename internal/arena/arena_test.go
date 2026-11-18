package arena

import (
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
	for id, want := range map[int64]Skill{3: Strike, 4: Heal, 5: Shield} {
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
	a, d := commonTeam(100, 350), commonTeam(90, 380)
	r1, r2 := Battle(a, d, 42), Battle(a, d, 42)
	if !reflect.DeepEqual(r1, r2) {
		t.Fatal("same seed gave different battles")
	}
	if reflect.DeepEqual(r1.Events, Battle(a, d, 43).Events) {
		t.Error("a different seed gave the same events")
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
	// Zero damage on both sides is an exact tie: the defender wins.
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
		if e.Round == 1 && e.Side == 0 && e.From == 0 && e.Kind == "hit" {
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
	a.Front[0] = FighterOf(5, "shield", 4, 1, 50, 1000)   // Shield
	a.Front[1] = FighterOf(4, "healer", 4, 1, 50, 1000)   // Heal
	a.Back[0] = FighterOf(3, "striker", 4, 1, 50, 1000)   // Strike
	d.Front[0] = FighterOf(100, "wall", 1, 1, 150, 20000) // hurts the attackers
	d.Front[1] = FighterOf(101, "wall2", 1, 1, 150, 20000)
	r := Battle(a, d, 9)
	count := map[string]int{}
	for _, e := range r.Events {
		count[e.Kind]++
		if e.Kind == "shield" && (e.Round != 0 || e.Amount != 300 || e.Side != 0) {
			t.Errorf("bad shield event %+v", e)
		}
		if e.Kind == "heal" && (e.Side != 0 || e.Amount <= 0) {
			t.Errorf("bad heal event %+v", e)
		}
	}
	for _, k := range []string{"shield", "heal", "strike", "hit"} {
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
	if !r.AttackerWon || last.Kind != "fall" || last.Side != 1 || last.HP != 0 {
		t.Errorf("result %v, last event %+v", r.AttackerWon, last)
	}
}
