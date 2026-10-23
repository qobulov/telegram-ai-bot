package webapi

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/tools"
)

type fakeHeroGames struct {
	mu      sync.Mutex
	started []string
	ends    time.Time
	scores  []storage.HeroScore
}

func (f *fakeHeroGames) StartHeroGame(_ context.Context, _ int64, hero string, endsAt time.Time) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	previous := ""
	if len(f.started) > 0 {
		previous = f.started[len(f.started)-1]
	}
	f.started, f.ends = append(f.started, hero), endsAt
	return previous, nil
}

func (f *fakeHeroGames) HeroScores(context.Context, int64, int) ([]storage.HeroScore, error) {
	return f.scores, nil
}

func heroQuizRun(t *testing.T, opts Options, args string) (envelope, *tools.Sink) {
	t.Helper()
	sink := &tools.Sink{}
	ctx := tools.WithTurn(context.Background(), tools.Caller{UserID: 1, ChatID: -100, Human: true}, sink)
	var env envelope
	output := newRegistry(t, opts).Execute(ctx, 1, "hero_quiz", args)
	if err := json.Unmarshal([]byte(output), &env); err != nil {
		t.Fatalf("output %q", output)
	}
	env.raw = output
	return env, sink
}

// A round sends a hero's picture and starts the game with that hero as the
// answer, which the model is never told; a second round tells the one
// nobody guessed.
func TestHeroQuizStartsARoundWithoutTellingTheHero(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/arena/api/heroes": ok(arenaHeroes),
		"/img/hero.jpg":     ok("JPEG"),
	})
	page := strings.Replace(arenaLingPage, `"story":`, `"squareheadbig":"`+u.server.URL+`/img/hero.jpg","story":`, 1)
	for _, id := range []string{"84", "30", "69", "24", "46"} {
		u.routes["/arena/api/heroes/"+id] = ok(page)
	}
	games := &fakeHeroGames{}
	opts := u.options()
	opts.HeroGames = games

	env, sink := heroQuizRun(t, opts, `{"action":"start"}`)

	if !env.OK || len(games.started) != 1 || len(sink.Take()) != 1 {
		t.Fatalf("env = %s, started %v", env.raw, games.started)
	}
	hero := games.started[0]
	if strings.Contains(env.raw, hero) {
		t.Errorf("the model was told the hero %q: %s", hero, env.raw)
	}
	if want := testNow.Add(heroRound); !games.ends.Equal(want) {
		t.Errorf("ends %v, want %v", games.ends, want)
	}

	env, _ = heroQuizRun(t, opts, `{"action":"start"}`)
	if !strings.Contains(env.raw, `"previous_answer":"`+hero+`"`) {
		t.Errorf("second round = %s, want the last hero told", env.raw)
	}
}

func TestHeroQuizScoreboard(t *testing.T) {
	u := newUpstream(t, map[string]stub{})
	games := &fakeHeroGames{scores: []storage.HeroScore{{UserID: 5, Name: "Bek", Points: 3}}}
	opts := u.options()
	opts.HeroGames = games

	env, _ := heroQuizRun(t, opts, `{"action":"top"}`)

	if !env.OK || !strings.Contains(env.raw, `"name":"Bek"`) || !strings.Contains(env.raw, `"points":3`) || strings.Contains(env.raw, "5") {
		t.Errorf("top = %s", env.raw)
	}
}
