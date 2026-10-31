package mafia

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC)

// playing is a game at its first night whose players 1, 2, … have the
// roles given, in order.
func playing(roles ...string) *Game {
	g := &Game{}
	for i := range roles {
		_ = g.Join(int64(i+1), string(rune('A'+i)), t0)
	}
	_ = g.Start(1, t0, func(int) int { return 0 })
	for i, role := range roles {
		g.Players[i].Role, g.Players[i].Luck = role, role == Lucky
	}
	return g
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func alive(g *Game, id int64) bool { return g.player(id).Alive }

// Players join the lobby once each, up to the most, and may leave it; the
// game starts with enough of them, dealing the first roles of the lineup.
func TestLobbyAndDeal(t *testing.T) {
	g := &Game{}
	for id := int64(1); id <= 4; id++ {
		must(t, g.Join(id, "P", t0))
	}
	must(t, g.Join(1, "P", t0))
	if len(g.Players) != 4 || g.Phase != Lobby {
		t.Fatalf("lobby: %+v", g)
	}
	if err := g.Start(1, t0, func(int) int { return 0 }); !errors.Is(err, ErrTooFew) {
		t.Errorf("four players started: %v", err)
	}
	must(t, g.Join(5, "P", t0))
	must(t, g.Join(6, "P", t0))
	must(t, g.Leave(6))
	if err := g.Start(9, t0, func(int) int { return 0 }); !errors.Is(err, ErrNotIn) {
		t.Errorf("a stranger started: %v", err)
	}
	must(t, g.Start(2, t0, func(n int) int { return n - 1 }))
	var roles []string
	for _, p := range g.Players {
		roles = append(roles, p.Role)
	}
	slices.Sort(roles)
	want := append([]string(nil), lineup[:5]...)
	slices.Sort(want)
	if g.Phase != Night || g.Day != 1 || !slices.Equal(roles, want) || !g.Ends.Equal(t0.Add(nightFor)) {
		t.Errorf("dealt %v at %s, want %v", roles, g.Phase, want)
	}
	if err := g.Join(7, "P", t0); !errors.Is(err, ErrStarted) {
		t.Errorf("joined a started game: %v", err)
	}

	full := &Game{}
	for id := int64(1); id <= MaxPlayers; id++ {
		must(t, full.Join(id, "P", t0))
	}
	if err := full.Join(99, "P", t0); !errors.Is(err, ErrFull) {
		t.Errorf("joined a full lobby: %v", err)
	}
	if events := full.Advance(t0.Add(lobbyFor)); len(events) != 1 || full.Phase != "" || len(full.Players) != 0 {
		t.Errorf("a lobby left alone: %+v %+v", events, full)
	}
}

// Each role picks only what it may: the mafia not their own, the doctor
// anyone but whom they cured last night, no one themselves but the doctor.
func TestActs(t *testing.T) {
	g := playing(Don, Mafioso, Commissar, Doctor, Citizen, Courtesan)
	for _, c := range []struct {
		id     int64
		act    string
		target int64
		want   error
	}{
		{1, actKill, 2, ErrTarget},
		{1, actKill, 1, ErrTarget},
		{5, actKill, 3, ErrNoAct},
		{3, actCheck, 3, ErrTarget},
		{3, actCheck, 9, ErrTarget},
		{4, actHeal, 4, nil},
		{1, actKill, 3, nil},
		{1, actFind, 3, nil},
	} {
		if err := g.Act(c.id, c.act, c.target); !errors.Is(err, c.want) {
			t.Errorf("%d %s %d: %v, want %v", c.id, c.act, c.target, err, c.want)
		}
	}
	if got := g.Pending(1); len(got) != 0 {
		t.Errorf("the Don has %v left", got)
	}
	if got := g.Targets(2, actKill); !slices.Equal(got, []int64{3, 4, 5, 6}) {
		t.Errorf("the mafia may kill %v", got)
	}
	if err := g.Vote(1, 3); !errors.Is(err, ErrPhase) {
		t.Errorf("voted at night: %v", err)
	}
}

// The night plays out in its order: the courtesan keeps her host from
// acting, the doctor's cure saves, the lucky one survives once, the checks
// tell the checkers, and the mafia's victim is the Don's pick.
func TestDawn(t *testing.T) {
	t.Run("a kill", func(t *testing.T) {
		g := playing(Don, Commissar, Doctor, Citizen, Citizen)
		must(t, g.Act(1, actKill, 4))
		must(t, g.Act(1, actFind, 2))
		must(t, g.Act(2, actCheck, 1))
		must(t, g.Act(3, actHeal, 5))
		events := g.Advance(t0)
		if alive(g, 4) || g.Phase != Day || g.Known[2][1] != "mafia" || g.Known[1][2] != "commissar" {
			t.Fatalf("after the night: %+v", g)
		}
		if len(events) != 2 || !strings.Contains(events[0].Text, "D o'ldirildi") || !strings.Contains(events[0].Text, "Tinch aholi") {
			t.Errorf("told %+v", events)
		}
	})
	t.Run("the doctor's cure", func(t *testing.T) {
		g := playing(Don, Commissar, Doctor, Citizen, Citizen)
		must(t, g.Act(1, actKill, 4))
		must(t, g.Act(3, actHeal, 4))
		g.Advance(t0.Add(nightFor))
		if !alive(g, 4) || !strings.Contains(strings.Join(g.News, " "), "Shifokor") || g.Cured != 4 {
			t.Fatalf("cured: %+v", g)
		}
		g.Advance(t0.Add(nightFor + dayFor))
		g.Advance(t0.Add(nightFor + dayFor + voteFor))
		if err := g.Act(3, actHeal, 4); !errors.Is(err, ErrRepeating) {
			t.Errorf("cured the same twice: %v", err)
		}
	})
	t.Run("the courtesan's visit", func(t *testing.T) {
		g := playing(Don, Commissar, Doctor, Citizen, Courtesan, Mafioso)
		must(t, g.Act(1, actKill, 4))
		must(t, g.Act(6, actKill, 3))
		must(t, g.Act(5, actBlock, 1))
		g.Advance(t0.Add(nightFor))
		if !alive(g, 4) || alive(g, 3) {
			t.Errorf("with the Don kept home the mafioso's pick dies: 4 %v, 3 %v", alive(g, 4), alive(g, 3))
		}
	})
	t.Run("luck", func(t *testing.T) {
		g := playing(Don, Commissar, Doctor, Lucky, Citizen)
		must(t, g.Act(1, actKill, 4))
		g.Advance(t0.Add(nightFor))
		if !alive(g, 4) || g.player(4).Luck {
			t.Fatalf("the first attack: %+v", g.player(4))
		}
		g.Advance(t0.Add(nightFor + dayFor))
		g.Advance(t0.Add(nightFor + dayFor + voteFor))
		must(t, g.Act(1, actKill, 4))
		g.Advance(t0.Add(2*nightFor + dayFor + voteFor))
		if alive(g, 4) {
			t.Error("the lucky one survived twice")
		}
	})
	t.Run("two killers", func(t *testing.T) {
		g := playing(Don, Commissar, Doctor, Citizen, Citizen, Mafioso, Courtesan, Maniac)
		must(t, g.Act(1, actKill, 4))
		must(t, g.Act(8, actManiac, 1))
		g.Advance(t0.Add(nightFor))
		if alive(g, 4) || alive(g, 1) {
			t.Errorf("the mafia's and the maniac's victims: 4 %v, 1 %v", alive(g, 4), alive(g, 1))
		}
	})
}

// The night ends as soon as everyone who acts has acted; the day when its
// time is up; the vote when everyone has voted, the most votes put one out
// and a tie no one.
func TestDayAndVote(t *testing.T) {
	g := playing(Don, Commissar, Doctor, Citizen, Citizen, Mafioso)
	must(t, g.Act(1, actKill, 4))
	must(t, g.Act(1, actFind, 5))
	must(t, g.Act(6, actKill, 4))
	must(t, g.Act(2, actCheck, 6))
	if g.Advance(t0); g.Phase != Night {
		t.Fatal("the night ended before the doctor acted")
	}
	must(t, g.Act(3, actHeal, 2))
	if g.Advance(t0); g.Phase != Day || alive(g, 4) {
		t.Fatalf("the night: %s", g.Phase)
	}
	if g.Advance(t0.Add(dayFor - time.Second)); g.Phase != Day {
		t.Fatal("the day ended early")
	}
	g.Advance(t0.Add(dayFor))
	if g.Phase != Vote {
		t.Fatalf("after the day: %s", g.Phase)
	}
	for _, v := range [][2]int64{{1, 2}, {6, 2}, {2, 6}, {3, 6}} {
		must(t, g.Vote(v[0], v[1]))
	}
	if err := g.Vote(4, 1); !errors.Is(err, ErrDead) {
		t.Errorf("the dead voted: %v", err)
	}
	if err := g.Vote(5, 5); !errors.Is(err, ErrTarget) {
		t.Errorf("voted for themselves: %v", err)
	}
	must(t, g.Vote(5, 1))
	g.Advance(t0.Add(dayFor))
	if g.Phase != Night || g.Day != 2 || !alive(g, 2) || !alive(g, 6) || !strings.Contains(g.News[0], "teng") {
		t.Fatalf("a tie: %s %v", g.Phase, g.News)
	}
}

// The town wins when its last killer is voted out; the mafia when they are
// as many as the rest; the maniac when only one other is left.
func TestWinners(t *testing.T) {
	t.Run("town", func(t *testing.T) {
		g := playing(Don, Commissar, Doctor, Citizen, Citizen)
		g.Advance(t0.Add(nightFor))
		g.Advance(t0.Add(nightFor + dayFor))
		for _, id := range []int64{2, 3, 4, 5} {
			must(t, g.Vote(id, 1))
		}
		must(t, g.Vote(1, 2))
		events := g.Advance(t0.Add(nightFor + dayFor))
		last := events[len(events)-1]
		if g.Phase != Over || g.Winner != Town || last.Winner != Town || !slices.Equal(last.Winners, []int64{2, 3, 4, 5}) {
			t.Fatalf("the town: %+v %+v", g, last)
		}
		if !strings.Contains(last.Text, "A — 🤵‍♂️ Don 💀") {
			t.Errorf("the roles told: %s", last.Text)
		}
		must(t, g.Join(9, "New", t0))
		if g.Phase != Lobby || len(g.Players) != 1 {
			t.Errorf("a new game after the last: %+v", g)
		}
	})
	t.Run("mafia", func(t *testing.T) {
		g := playing(Don, Commissar, Doctor, Citizen, Citizen)
		must(t, g.Act(1, actKill, 4))
		g.Advance(t0.Add(nightFor))
		g.Advance(t0.Add(nightFor + dayFor))
		must(t, g.Vote(1, 2))
		must(t, g.Vote(5, 2))
		g.Advance(t0.Add(nightFor + dayFor + voteFor))
		must(t, g.Act(1, actKill, 3))
		if g.Advance(t0.Add(2*nightFor + dayFor + voteFor)); g.Winner != Mafia {
			t.Errorf("one mafia and one other left: %s %s", g.Phase, g.Winner)
		}
	})
	t.Run("maniac", func(t *testing.T) {
		g := playing(Don, Commissar, Doctor, Citizen, Citizen, Mafioso, Courtesan, Maniac)
		for _, id := range []int64{1, 2, 4, 5, 6, 7} {
			g.player(id).Alive = false
		}
		if end := g.end(); end == nil || g.Winner != Solo || !slices.Equal(end.Winners, []int64{8}) {
			t.Errorf("the maniac with one other: %+v", end)
		}
	})
}

// Who falls at night or by the vote may leave one last word until the next
// night or vote is played out; the living, the spared and the long dead may
// not, and the word is one line of at most MaxWords letters.
func TestLastWords(t *testing.T) {
	g := playing(Don, Commissar, Doctor, Citizen, Citizen, Mafioso)
	must(t, g.Act(1, actKill, 2))
	must(t, g.Act(1, actFind, 5))
	must(t, g.Act(6, actKill, 2))
	must(t, g.Act(2, actCheck, 1))
	must(t, g.Act(3, actHeal, 3))
	g.Advance(t0)
	if g.Phase != Day || alive(g, 2) || !g.CanSpeak(2) || g.CanSpeak(3) {
		t.Fatalf("after the night: %s, fallen %v", g.Phase, g.Fallen)
	}
	if _, err := g.Speak(3, "men tirikman"); !errors.Is(err, ErrNoWords) {
		t.Errorf("the living spoke: %v", err)
	}
	if _, err := g.Speak(2, strings.Repeat("a", MaxWords+1)); !errors.Is(err, ErrWords) {
		t.Errorf("a long word: %v", err)
	}
	if _, err := g.Speak(2, "  \n "); !errors.Is(err, ErrWords) {
		t.Errorf("an empty word: %v", err)
	}
	said, err := g.Speak(2, "A  —\nmafiya!")
	if err != nil || said.Text != "🕯 B so'nggi so'zini aytdi:\n«A — mafiya!»" || g.player(2).Words != "A — mafiya!" {
		t.Fatalf("said %q, %v", said.Text, err)
	}
	if _, err := g.Speak(2, "yana"); !errors.Is(err, ErrNoWords) {
		t.Errorf("spoke twice: %v", err)
	}

	g.Advance(t0.Add(dayFor))
	for _, voter := range []int64{3, 4, 5, 6} {
		must(t, g.Vote(voter, 1))
	}
	g.Advance(t0.Add(dayFor + voteFor))
	if !g.CanSpeak(1) || g.CanSpeak(2) {
		t.Errorf("after the vote, fallen %v", g.Fallen)
	}
}

// When the last of them fall together, as the Don and the maniac killing
// each other, no one wins and no one is paid.
func TestNoOneWinsWhenAllFall(t *testing.T) {
	g := playing(Don, Maniac, Citizen, Citizen, Citizen)
	for i := 2; i < 5; i++ {
		g.Players[i].Alive = false
	}
	must(t, g.Act(1, actKill, 2))
	must(t, g.Act(1, actFind, 2))
	must(t, g.Act(2, actManiac, 1))
	events := g.Advance(t0)
	end := events[len(events)-1]
	if g.Phase != Over || g.Winner != Nobody || len(end.Winners) != 0 || !strings.Contains(end.Text, "Hech kim yutmadi") {
		t.Errorf("all fell: %s, winner %q, paid %v, told %q", g.Phase, g.Winner, end.Winners, end.Text)
	}
}

// At night only the mafia write, and only they read it; by day everyone
// alive writes and everyone reads; the fallen are silent till the end; a
// player waits a moment between lines; a game keeps its latest lines.
func TestChat(t *testing.T) {
	g := playing(Don, Commissar, Doctor, Citizen, Citizen, Mafioso)
	must(t, g.Say(1, RoomMafia, "  Kimni   o'ldiramiz? ", t0))
	must(t, g.Say(6, RoomMafia, "B ni", t0))
	if err := g.Say(2, RoomTown, "salom", t0); !errors.Is(err, ErrChat) {
		t.Errorf("the commissar wrote at night: %v", err)
	}
	if err := g.Say(1, RoomMafia, "yana", t0.Add(lineGap/2)); !errors.Is(err, ErrTooFast) {
		t.Errorf("wrote twice at once: %v", err)
	}
	if err := g.Say(1, RoomMafia, strings.Repeat("a", MaxLine+1), t0.Add(lineGap)); !errors.Is(err, ErrLine) {
		t.Errorf("a long line: %v", err)
	}
	if heard := g.Heard(2); len(heard) != 0 {
		t.Errorf("the commissar heard the mafia: %+v", heard)
	}
	if heard := g.Heard(6); len(heard) != 2 || heard[0].Text != "Kimni o'ldiramiz?" || heard[0].Room != RoomMafia {
		t.Errorf("the mafia heard %+v", heard)
	}

	must(t, g.Act(1, actKill, 4))
	must(t, g.Act(1, actFind, 2))
	must(t, g.Act(6, actKill, 4))
	must(t, g.Act(2, actCheck, 1))
	must(t, g.Act(3, actHeal, 3))
	g.Advance(t0)
	if err := g.Say(1, RoomMafia, "endi kim?", t0.Add(time.Second)); !errors.Is(err, ErrRoom) {
		t.Errorf("a night line reached the day: %v", err)
	}
	must(t, g.Say(2, RoomTown, "A mafiya!", t0.Add(time.Second)))
	if err := g.Say(4, RoomTown, "men o'ldim", t0.Add(time.Second)); !errors.Is(err, ErrChat) {
		t.Errorf("the fallen wrote: %v", err)
	}
	if heard := g.Heard(5); len(heard) != 1 || heard[0].Name != "B" {
		t.Errorf("a citizen heard %+v", heard)
	}
	for i := range keptLines + 5 {
		must(t, g.Say(3, RoomTown, "gap", t0.Add(time.Duration(i+2)*lineGap)))
	}
	if len(g.Chat) != keptLines || g.Chat[len(g.Chat)-1].ID != g.Lines {
		t.Errorf("kept %d lines, the last %d of %d", len(g.Chat), g.Chat[len(g.Chat)-1].ID, g.Lines)
	}
}
