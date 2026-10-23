package storage

import (
	"context"
	"testing"
	"time"
)

// A round is open until it ends or someone wins it; only the first right
// guess wins, and points add up. A new round tells the hero nobody guessed.
func TestHeroGameRoundsAndScores(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const group = int64(-100900)
	now := time.Now()

	if prev, err := store.StartHeroGame(ctx, group, "Ling", now.Add(3*time.Minute)); err != nil || prev != "" {
		t.Fatalf("start = %q, %v", prev, err)
	}
	if g, open, err := store.OpenHeroGame(ctx, group, now); err != nil || !open || g.Hero != "Ling" {
		t.Fatalf("open = %+v, %v, %v", g, open, err)
	}
	if _, open, _ := store.OpenHeroGame(ctx, group, now.Add(4*time.Minute)); open {
		t.Error("an ended round is still open")
	}
	if points, won, err := store.WinHeroGame(ctx, group, 1, "Bek", now); err != nil || !won || points != 1 {
		t.Fatalf("win = %d, %v, %v", points, won, err)
	}
	if _, won, _ := store.WinHeroGame(ctx, group, 2, "Ann", now); won {
		t.Error("a second guess won the same round")
	}
	if prev, _ := store.StartHeroGame(ctx, group, "Hanzo", now.Add(3*time.Minute)); prev != "" {
		t.Errorf("a won round was told as unguessed: %q", prev)
	}
	if points, won, _ := store.WinHeroGame(ctx, group, 1, "Bek", now); !won || points != 2 {
		t.Errorf("second win = %d, %v; want 2 points", points, won)
	}
	if prev, _ := store.StartHeroGame(ctx, group, "Odette", now.Add(3*time.Minute)); prev != "" {
		t.Errorf("prev = %q", prev)
	}
	if prev, _ := store.StartHeroGame(ctx, group, "Natalia", now.Add(3*time.Minute)); prev != "Odette" {
		t.Errorf("unguessed round told as %q, want Odette", prev)
	}
	scores, err := store.HeroScores(ctx, group, 10)
	if err != nil || len(scores) != 1 || scores[0].Name != "Bek" || scores[0].Points != 2 {
		t.Errorf("scores = %+v, %v", scores, err)
	}
}

// The report counts a day's group messages, answers, private chats and
// moderation, and names the groups first seen that day.
func TestReportCountsTheDay(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO groups (chat_id, title, first_seen_at) VALUES (-1, 'Old', now() - interval '5 days'), (-2, 'New', now() - interval '1 hour');
		INSERT INTO group_messages (chat_id, author_name, text, created_at) VALUES (-1, 'a', 'x', now() - interval '1 hour'), (-1, 'a', 'y', now() - interval '3 days');
		INSERT INTO group_replies (chat_id, user_id, created_at) VALUES (-1, 5, now() - interval '1 hour');
		INSERT INTO private_messages (user_id, denied, created_at) VALUES (7, false, now() - interval '1 hour'), (7, true, now() - interval '1 hour'), (8, false, now() - interval '1 hour');
		INSERT INTO moderation_actions (chat_id, action, requester_id, update_id, created_at) VALUES (-1, 'mute', 1, 99, now() - interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	r, err := store.Report(ctx, time.Now().Add(-24*time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Groups) != 1 || r.Groups[0] != (GroupDay{Title: "Old", Messages: 1, Replies: 1}) {
		t.Errorf("groups = %+v", r.Groups)
	}
	if len(r.NewGroups) != 1 || r.NewGroups[0] != "New" || r.PrivateUsers != 2 || r.PrivateAnswers != 2 || r.PrivateRefused != 1 || r.Moderation["mute"] != 1 {
		t.Errorf("report = %+v", r)
	}
}
