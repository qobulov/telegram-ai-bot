package game

import (
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testToken = "123456:TEST-token"

var player = Player{UserID: 42, ChatID: -100123, MessageID: 77}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func newTestGame() (*Game, *clock) {
	c := &clock{now: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
	return New(testToken, "https://example.com/game/", c.Now), c
}

// sessionOf is the session token in a play link.
func sessionOf(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil || !strings.HasPrefix(link, "https://example.com/game/") || u.Query().Get("s") == "" {
		t.Fatalf("link %q", link)
	}
	return u.Query().Get("s")
}

// playURL is the page a press of shortName's Play opens for player.
func playURL(t *testing.T, g *Game, shortName string, player Player) string {
	t.Helper()
	link, ok := g.PlayURL(shortName, player)
	if !ok {
		t.Fatalf("no page for %q", shortName)
	}
	return link
}

// rightAnswers are the answers to rounds, and times a person could give
// them at, a second apart.
func rightAnswers(dealt []Round, n int) (string, []int) {
	var answers []byte
	var times []int
	for i := range n {
		answers = append(answers, dealt[i].answer())
		times = append(times, (i+1)*1000)
	}
	return string(answers), times
}

func TestAPlayIsDealtScoredAndForThePlayer(t *testing.T) {
	g, c := newTestGame()
	run, dealt, err := g.Start(sessionOf(t, playURL(t, g, ShortName, player)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(dealt) != rounds {
		t.Fatalf("%d rounds dealt", len(dealt))
	}
	answers, times := rightAnswers(dealt, 12)
	c.now = c.now.Add(13 * time.Second)

	result, err := g.Finish(run, answers, times)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	// 10, 12, 14, 16, 18, then 20 for each of the other seven.
	if result.Player != player || result.Score != 10+12+14+16+18+7*20 {
		t.Errorf("result = %+v", result)
	}
}

// Each play is dealt afresh; one play's rounds are the same each time they
// are dealt, which is how the server scores what it dealt.
func TestEachPlayIsDealtAfresh(t *testing.T) {
	g, _ := newTestGame()
	session := sessionOf(t, playURL(t, g, ShortName, player))
	_, first, _ := g.Start(session)
	_, second, _ := g.Start(session)
	if reflect.DeepEqual(first, second) {
		t.Error("two plays were dealt the same rounds")
	}
	if !reflect.DeepEqual(deal(7), deal(7)) {
		t.Error("one seed dealt different rounds")
	}
}

// The board changes every few rounds, the pans fill as the play goes on,
// and some rounds balance.
func TestRoundsGrowHarder(t *testing.T) {
	dealt := deal(99)
	balanced := 0
	for i, r := range dealt {
		if len(r.Fruits) != boardFruits || len(r.Weights) != boardFruits {
			t.Fatalf("round %d board = %+v", i, r)
		}
		most := min(1+i/roundsPerStep, maxFruits)
		if len(r.Left) > most || len(r.Right) > most || len(r.Left) < most-1 || len(r.Left) == 0 || len(r.Right) == 0 {
			t.Errorf("round %d pans %v %v, want %d or one fewer", i, r.Left, r.Right, most)
		}
		if i%roundsPerDay != 0 && !reflect.DeepEqual(r.Weights, dealt[i-1].Weights) {
			t.Errorf("round %d changed the board mid-day", i)
		}
		if r.answer() == 'E' {
			balanced++
		}
	}
	if balanced < rounds/10 {
		t.Errorf("%d of %d rounds balance", balanced, rounds)
	}
}

func TestScore(t *testing.T) {
	dealt := deal(5)
	right, times := rightAnswers(dealt, 4)
	wrong := []byte(right)
	for i := range wrong {
		wrong[i] = map[byte]byte{'L': 'R', 'R': 'E', 'E': 'L'}[wrong[i]]
	}
	cases := map[string]struct {
		answers string
		times   []int
		want    int
		err     error
	}{
		"right":                 {right, times, 10 + 12 + 14 + 16, nil},
		"a wrong one in a row":  {right[:2] + string(wrong[2]) + right[3:], times, 10 + 12 - 5 + 10, nil},
		"never below zero":      {string(wrong), times, 0, nil},
		"none":                  {"", nil, 0, nil},
		"quicker than a person": {right[:2], []int{1000, 1200}, 0, ErrImplausible},
		"first one at once":     {right[:1], []int{100}, 0, ErrImplausible},
		"after the time":        {right[:1], []int{Seconds*1000 + 1500}, 0, ErrImplausible},
		"times out of order":    {right[:2], []int{2000, 1000}, 0, ErrImplausible},
		"not a pan":             {"X", []int{1000}, 0, ErrImplausible},
		"times missing":         {right, times[:2], 0, ErrImplausible},
		"more than dealt":       {strings.Repeat("L", rounds+1), make([]int, rounds+1), 0, ErrImplausible},
	}
	for name, c := range cases {
		got, err := Score(dealt, c.answers, c.times)
		if got != c.want || !errors.Is(err, c.err) {
			t.Errorf("%s: %d, %v; want %d, %v", name, got, err, c.want, c.err)
		}
	}
}

// A play the page says took longer than the server saw is refused: the
// page's times cannot be stretched to answer slowly at leisure.
func TestFinishRefusesTimeThatDidNotPass(t *testing.T) {
	g, c := newTestGame()
	run, dealt, _ := g.Start(sessionOf(t, playURL(t, g, ShortName, player)))
	answers, times := rightAnswers(dealt, 30)
	c.now = c.now.Add(10 * time.Second)

	if _, err := g.Finish(run, answers, times); !errors.Is(err, ErrImplausible) {
		t.Errorf("30 s of answers after 10 s: err = %v, want ErrImplausible", err)
	}
}

func TestTokensAreThisServersAndCurrent(t *testing.T) {
	g, c := newTestGame()
	session := sessionOf(t, playURL(t, g, ShortName, player))
	run, _, _ := g.Start(session)
	other := New("999:other", "https://example.com/game/", c.Now)

	if _, _, err := other.Start(session); !errors.Is(err, ErrToken) {
		t.Errorf("another bot's session: %v", err)
	}
	if _, err := g.Finish(session, "", nil); !errors.Is(err, ErrToken) {
		t.Errorf("a session as a run: %v", err)
	}
	body, tag, _ := strings.Cut(run, ".")
	if _, err := g.Finish(body+"x."+tag, "", nil); !errors.Is(err, ErrToken) {
		t.Errorf("a changed run: %v", err)
	}
	c.now = c.now.Add(runAge + time.Second)
	if _, err := g.Finish(run, "", nil); !errors.Is(err, ErrToken) {
		t.Errorf("an old run: %v", err)
	}
	c.now = c.now.Add(sessionAge)
	if _, _, err := g.Start(session); !errors.Is(err, ErrToken) {
		t.Errorf("an old session: %v", err)
	}
}
