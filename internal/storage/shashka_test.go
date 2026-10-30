package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

const shashkaStart = "bbbbbbbbbbbb........wwwwwwwwwwww:w:0"

// An invite is joined once, by someone else; a move is saved only at the
// version it was read at, and the rated game's end moves both ratings by
// Elo and counts in both records.
func TestShashkaGameIsJoinedPlayedAndRated(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.SaveShashkaPlayer(ctx, alice, "Alice", "uz"); err != nil {
		t.Fatal(err)
	}
	invite, err := store.CreateShashkaGame(ctx, ShashkaGame{Kind: "friend", WhiteID: alice, Position: shashkaStart, Status: ShashkaWaiting, InviteCode: "abc", Rated: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.JoinShashkaGame(ctx, "abc", alice, time.Time{}); !errors.Is(err, ErrShashkaOwnInvite) {
		t.Fatalf("joining one's own invite = %v", err)
	}
	if _, err := store.JoinShashkaGame(ctx, "nope", bob, time.Time{}); !errors.Is(err, ErrNoShashkaGame) {
		t.Fatalf("joining no invite = %v", err)
	}
	deadline := time.Now().Add(time.Minute).Truncate(time.Second)
	game, err := store.JoinShashkaGame(ctx, "abc", bob, deadline)
	if err != nil || game.ID != invite.ID || game.BlackID != bob || game.Status != ShashkaPlaying || !game.Deadline.Equal(deadline) || game.Version != 1 {
		t.Fatalf("JoinShashkaGame = %+v, %v", game, err)
	}
	if _, err := store.JoinShashkaGame(ctx, "abc", bob+1, time.Time{}); !errors.Is(err, ErrShashkaTaken) {
		t.Fatalf("joining a taken invite = %v", err)
	}
	open, ok, err := store.OpenShashkaGame(ctx, bob)
	if err != nil || !ok || open.ID != game.ID {
		t.Fatalf("OpenShashkaGame(bob) = %+v, %v, %v", open, ok, err)
	}

	moved := game
	moved.Position, moved.LastPath, moved.Captured = "bbbbbbbbbbbb.......w.wwwwwwwwwww:b:0", []int{21, 17}, []int{}
	saved, err := store.UpdateShashkaGame(ctx, moved)
	if err != nil || saved.Version != 2 || len(saved.LastPath) != 2 || saved.LastPath[1] != 17 {
		t.Fatalf("UpdateShashkaGame = %+v, %v", saved, err)
	}
	if _, err := store.UpdateShashkaGame(ctx, moved); !errors.Is(err, ErrShashkaStale) {
		t.Fatalf("a second move from the same version = %v, want ErrShashkaStale", err)
	}

	over := saved
	over.Status, over.Winner, over.Reason, over.Deadline = ShashkaOver, 1, "resign", time.Time{}
	ended, err := store.UpdateShashkaGame(ctx, over)
	if err != nil || ended.WhiteChange != 16 || ended.BlackChange != -16 || !ended.Deadline.IsZero() {
		t.Fatalf("the game's end = %+v, %v; want ±16 between equal ratings", ended, err)
	}
	a, _ := store.ShashkaPlayer(ctx, alice)
	b, _ := store.ShashkaPlayer(ctx, bob)
	if a.Rating != 1216 || a.Wins != 1 || a.Name != "Alice" || a.Language != "uz" || b.Rating != 1184 || b.Losses != 1 {
		t.Fatalf("after the game: %+v, %+v", a, b)
	}
	top, err := store.ShashkaTop(ctx, 10)
	if err != nil || len(top) != 2 || top[0].UserID != alice {
		t.Fatalf("ShashkaTop = %+v, %v", top, err)
	}
	if _, ok, _ := store.OpenShashkaGame(ctx, alice); ok {
		t.Error("a game over is still open")
	}
}

// Two players asking for a random opponent at once make one game between
// them, never two.
func TestShashkaQueuePairsOnce(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	newGame := func(first, second int64) ShashkaGame {
		return ShashkaGame{Kind: "random", WhiteID: first, BlackID: second, Position: shashkaStart, Status: ShashkaPlaying, Rated: true}
	}
	if _, paired, err := store.QueueShashka(ctx, alice, 1200, time.Minute, newGame); err != nil || paired {
		t.Fatalf("the first in the queue was paired, or %v", err)
	}
	if waiting, _ := store.InShashkaQueue(ctx, alice, time.Minute); !waiting {
		t.Fatal("alice is not waiting")
	}
	var wg sync.WaitGroup
	games := make(chan ShashkaGame, 2)
	for _, user := range []int64{alice, bob} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if g, paired, err := store.QueueShashka(ctx, user, 1200, time.Minute, newGame); err != nil {
				t.Error(err)
			} else if paired {
				games <- g
			}
		}()
	}
	wg.Wait()
	close(games)
	var made []ShashkaGame
	for g := range games {
		made = append(made, g)
	}
	// Both may be told of it, but it is the one game.
	if len(made) == 0 || (len(made) == 2 && made[0].ID != made[1].ID) {
		t.Fatalf("games made: %+v, want one", made)
	}
	if g := made[0]; (g.WhiteID != alice || g.BlackID != bob) && (g.WhiteID != bob || g.BlackID != alice) {
		t.Fatalf("the game is %+v", g)
	}
	for _, user := range []int64{alice, bob} {
		if waiting, _ := store.InShashkaQueue(ctx, user, time.Minute); waiting {
			t.Errorf("%d still waits after being paired", user)
		}
	}
}

// A wait not renewed in time is given up and pairs no one.
func TestShashkaQueueForgetsStaleWaits(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.pool.Exec(ctx, `INSERT INTO shashka_queue (user_id, rating, joined_at) VALUES ($1, 1200, now() - interval '1 minute')`, alice); err != nil {
		t.Fatal(err)
	}
	newGame := func(first, second int64) ShashkaGame {
		t.Fatalf("paired with a stale wait: %d, %d", first, second)
		return ShashkaGame{}
	}
	if _, paired, err := store.QueueShashka(ctx, bob, 1200, 10*time.Second, newGame); err != nil || paired {
		t.Fatalf("QueueShashka = %v, %v", paired, err)
	}
}

func TestEloChange(t *testing.T) {
	for _, c := range []struct {
		rating, opponent int
		score            float64
		want             int
	}{
		{1200, 1200, 1, 16},
		{1200, 1200, 0.5, 0},
		{1200, 1600, 1, 29},
		{1600, 1200, 1, 3},
		{1600, 1200, 0, -29},
	} {
		if got := eloChange(c.rating, c.opponent, c.score); got != c.want {
			t.Errorf("eloChange(%d, %d, %v) = %d, want %d", c.rating, c.opponent, c.score, got, c.want)
		}
	}
}
