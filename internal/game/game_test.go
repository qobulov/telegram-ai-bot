package game

import (
	"errors"
	"net/url"
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

// A session is for the game its link opens: O'tinchi's, Dev jangi's and
// Mafia's pages are under the games' address, and no other game has one.
func TestPlayURLOpensEachGamesPage(t *testing.T) {
	g, _ := newTestGame()
	for name, path := range map[string]string{OtinchiShortName: OtinchiPath, BossShortName: BossPath, MafiaShortName: MafiaPath} {
		link := playURL(t, g, name, player)
		if !strings.HasPrefix(link, "https://example.com/game/"+path+"?s=") {
			t.Errorf("%s's page is %s", name, link)
		}
		got, game, err := g.Session(sessionOf(t, link))
		if err != nil || got != player || game != name {
			t.Errorf("%s's session: %+v %q %v", name, got, game, err)
		}
	}
	for _, gone := range []string{"tarozi", "ilon", ""} {
		if link, ok := g.PlayURL(gone, player); ok {
			t.Errorf("%q has a page: %s", gone, link)
		}
	}
}

func TestTokensAreThisServersAndCurrent(t *testing.T) {
	g, c := newTestGame()
	session := sessionOf(t, playURL(t, g, OtinchiShortName, player))
	run, _, _ := g.StartOtinchi(session)
	other := New("999:other", "https://example.com/game/", c.Now)

	if _, _, err := other.StartOtinchi(session); !errors.Is(err, ErrToken) {
		t.Errorf("another bot's session: %v", err)
	}
	if _, err := g.FinishOtinchi(session, "L", []int{0}); !errors.Is(err, ErrToken) {
		t.Errorf("a session as a run: %v", err)
	}
	body, tag, _ := strings.Cut(run, ".")
	if _, err := g.FinishOtinchi(body+"x."+tag, "L", []int{0}); !errors.Is(err, ErrToken) {
		t.Errorf("a changed run: %v", err)
	}
	if _, _, err := g.StartOtinchi(sessionOf(t, playURL(t, g, BossShortName, player))); !errors.Is(err, ErrToken) {
		t.Errorf("O'tinchi started from Dev jangi's session: %v", err)
	}
	c.now = c.now.Add(otinchiRunAge + time.Second)
	if _, err := g.FinishOtinchi(run, "L", []int{0}); !errors.Is(err, ErrToken) {
		t.Errorf("an old run: %v", err)
	}
	if _, _, err := g.StartOtinchi(session); !errors.Is(err, ErrToken) {
		t.Errorf("an old session: %v", err)
	}
}
