package storage

import (
	"context"
	"testing"
)

// A rank is given, changed and taken away, every change logged with who did
// it; a command's rank is set per chat.
func TestRanksAreGivenAndLogged(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.pool.Exec(ctx, `INSERT INTO group_members (chat_id, user_id, first_name, username) VALUES (-100, 7, 'Bek', 'bek'), (-100, 1, 'Ali', '')`); err != nil {
		t.Fatal(err)
	}
	if r, err := store.RankOf(ctx, -100, 7); err != nil || r != 0 {
		t.Fatalf("no rank yet = %d, %v", r, err)
	}
	for _, r := range []int{3, 2} {
		if err := store.SetRank(ctx, -100, 7, r, 1); err != nil {
			t.Fatal(err)
		}
	}
	if r, _ := store.RankOf(ctx, -100, 7); r != 2 {
		t.Errorf("rank = %d, want 2", r)
	}
	ranked, err := store.Ranks(ctx, -100)
	if err != nil || len(ranked) != 1 || ranked[0].Name != "Bek" || ranked[0].Rank != 2 || ranked[0].GivenBy != 1 {
		t.Fatalf("ranks = %+v, %v", ranked, err)
	}
	if err := store.SetRank(ctx, -100, 7, 0, 1); err != nil {
		t.Fatal(err)
	}
	if r, _ := store.RankOf(ctx, -100, 7); r != 0 {
		t.Errorf("after taking it away = %d", r)
	}
	log, err := store.RankLog(ctx, -100, 7, 10)
	if err != nil || len(log) != 3 || log[0].Rank != 0 || log[2].Rank != 3 || log[0].ByName != "Ali" || log[0].Name != "Bek" {
		t.Fatalf("log = %+v, %v", log, err)
	}
	if err := store.SetCommandRank(ctx, -100, "ban", 3); err != nil {
		t.Fatal(err)
	}
	if set, err := store.CommandRanks(ctx, -100); err != nil || set["ban"] != 3 || len(set) != 1 {
		t.Errorf("command ranks = %v, %v", set, err)
	}
	if set, _ := store.CommandRanks(ctx, -200); len(set) != 0 {
		t.Errorf("another chat's = %v", set)
	}
}
