package maydon

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// battle is a started room of fighters 1, 2, … of the classes given, all
// at the first levels.
func battle(t *testing.T, classesOf ...string) *Room {
	t.Helper()
	r := &Room{}
	for i, c := range classesOf {
		must(t, r.Join(int64(i+1), string(rune('A'+i)), c, 1, 0, t0))
	}
	must(t, r.Start(1, t0, 7))
	return r
}

// at plays the room on to ms into the battle.
func at(r *Room, ms int) []Event {
	return r.Advance(r.Started.Add(time.Duration(ms) * time.Millisecond))
}

// A lobby holds five a side at most; joining again changes the class; the
// battle needs two.
func TestLobby(t *testing.T) {
	r := &Room{}
	if err := r.Join(1, "A", "wizard", 1, 0, t0); !errors.Is(err, ErrClass) {
		t.Errorf("an unknown class: %v", err)
	}
	must(t, r.Join(1, "A", Warrior, 1, 0, t0))
	if err := r.Start(1, t0, 1); !errors.Is(err, ErrTooFew) {
		t.Errorf("one started: %v", err)
	}
	must(t, r.Join(1, "A", Archer, 3, 1, t0))
	if len(r.Fighters) != 1 || r.Fighters[0].Class != Archer || r.Fighters[0].Sword != 3 {
		t.Errorf("joined again: %+v", r.Fighters)
	}
	for id := int64(2); id <= MaxPlayers; id++ {
		must(t, r.Join(id, "P", Mage, 1, 0, t0))
	}
	if err := r.Join(99, "P", Mage, 1, 0, t0); !errors.Is(err, ErrFull) {
		t.Errorf("an eleventh joined: %v", err)
	}
	must(t, r.Leave(10))
	must(t, r.Start(2, t0, 1))
	if err := r.Join(10, "P", Mage, 1, 0, t0); !errors.Is(err, ErrStarted) {
		t.Errorf("joined a battle: %v", err)
	}
	if events := (&Room{Phase: Lobby, Ends: t0}).Advance(t0); len(events) != 1 {
		t.Errorf("a lobby left alone: %+v", events)
	}
}

// The sides are as strong as can be, at most one fighter apart, and each
// puts its warriors in front.
func TestSidesAndPlaces(t *testing.T) {
	r := &Room{}
	levels := [][2]int{{22, 15}, {20, 15}, {14, 8}, {11, 6}, {2, 1}, {1, 0}}
	for i, l := range levels {
		class := Archer
		if i%2 == 0 {
			class = Warrior
		}
		must(t, r.Join(int64(i+1), "P", class, l[0], l[1], t0))
	}
	must(t, r.Start(1, t0, 3))
	var size, sum [2]int
	for _, f := range r.Fighters {
		size[f.Team]++
		sum[f.Team] += power(f)
		if f.Class == Warrior && f.Row != 0 {
			t.Errorf("a warrior behind: %+v", f)
		}
		if f.HP != f.Max || !f.Alive {
			t.Errorf("not ready: %+v", f)
		}
	}
	if size[0] != 3 || size[1] != 3 || abs(sum[0]-sum[1]) > power(r.Fighters[0])/2 {
		t.Errorf("sides of %v, power %v", size, sum)
	}
}

// A blow before the attack comes round slips and puts it off; right after,
// it is a precise one; a while after, an ordinary one; a fighter left idle
// strikes by itself.
func TestTiming(t *testing.T) {
	r := battle(t, Archer, Warrior)
	if err := r.Attack(1); !errors.Is(err, ErrEarly) || r.fighter(1).CD != 600+earlyMs {
		t.Errorf("early: %v, cd %d", err, r.fighter(1).CD)
	}
	at(r, 800)
	must(t, r.Attack(1))
	first := r.Hits[len(r.Hits)-1]
	if !first.Perfect || first.Kind != "arrow" || first.To != 2 {
		t.Errorf("right on time: %+v", first)
	}
	at(r, 800+900+1000)
	must(t, r.Attack(1))
	if late := r.Hits[len(r.Hits)-1]; late.Perfect {
		t.Errorf("a second late was precise: %+v", late)
	}
	before := len(r.Hits)
	at(r, 800+900+1000+900+idleMs+stepMs)
	idle := false
	for _, h := range r.Hits[before:] {
		idle = idle || h.From == 1
	}
	if !idle {
		t.Error("an idle archer did not strike")
	}
}

// A block takes 70% of a blow; a fallen fighter comes back once, with 60% of
// their life; a side that has fallen for good loses, and the group is told.
func TestBlockReviveAndEnd(t *testing.T) {
	r := battle(t, Mage, Mage)
	must(t, r.Block(2))
	if err := r.Block(2); !errors.Is(err, ErrBlock) {
		t.Errorf("blocked twice: %v", err)
	}
	at(r, 600)
	must(t, r.Attack(1))
	if h := r.Hits[len(r.Hits)-1]; !h.Blocked || h.Amount > 56*2*3/10+2 {
		t.Errorf("a blocked blow: %+v", h)
	}
	foe := r.fighter(2)
	foe.HP = 1
	at(r, 600+1300)
	must(t, r.Attack(1))
	if foe.Alive || foe.ReviveAt != r.T+reviveMs {
		t.Fatalf("after the fall: %+v", foe)
	}
	if events := at(r, r.T+reviveMs); len(events) != 0 || !foe.Alive || foe.HP != foe.Max*6/10 {
		t.Fatalf("coming back: %+v", foe)
	}
	foe.HP = 1
	r.fighter(1).CD = 0
	must(t, r.Attack(1))
	events := at(r, r.T+stepMs)
	if r.Phase != Over || r.Winner != 0 || r.Best != 1 || len(events) != 1 || len(events[0].Winners) != 1 || events[0].Winners[0] != 1 {
		t.Fatalf("the end: %s %d %+v", r.Phase, r.Winner, events)
	}
	if !strings.Contains(events[0].Text, "Qizillar yutdi") || !strings.Contains(events[0].Text, "Jang qahramoni: A") {
		t.Errorf("told %q", events[0].Text)
	}
	must(t, r.Join(5, "E", Healer, 1, 0, t0))
	if r.Phase != Lobby || len(r.Fighters) != 1 {
		t.Errorf("a new room after the battle: %+v", r)
	}
}

// When the time is up the side with more life left wins; a healer cures the
// weakest friend.
func TestTimeUpAndHealer(t *testing.T) {
	r := battle(t, Warrior, Archer, Healer, Archer)
	var healer, friend *Fighter
	for i := range r.Fighters {
		if f := &r.Fighters[i]; f.Class == Healer {
			healer = f
		}
	}
	for i := range r.Fighters {
		if f := &r.Fighters[i]; f.Team == healer.Team && f != healer {
			friend = f
		}
	}
	friend.HP = friend.Max / 2
	at(r, 600)
	must(t, r.Attack(healer.ID))
	if h := r.Hits[len(r.Hits)-1]; h.Kind != "heal" || h.To != friend.ID {
		t.Errorf("the healer did %+v", h)
	}
	if err := r.Aim(1, 1); !errors.Is(err, ErrTarget) {
		t.Errorf("aimed at themselves: %v", err)
	}
	for i := range r.Fighters {
		if f := &r.Fighters[i]; f.Team != r.fighter(1).Team {
			f.HP = 1
		}
	}
	if at(r, battleMs); r.Phase != Over || r.Winner != r.fighter(1).Team {
		t.Errorf("time up: %s, winner %d", r.Phase, r.Winner)
	}
}
