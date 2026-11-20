package storage

import (
	"context"
	"testing"
	"time"
)

// The ban list holds those whose latest ban or unban was a ban, until an
// amnesty clears it.
func TestBanList(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO group_members (chat_id, user_id, first_name, username, is_bot, status, last_seen_at) VALUES
			(-100, 7, 'Bek', 'bek', false, 'member', now() - interval '40 days'),
			(-100, 8, 'Vali', '', false, 'member', now() - interval '2 days'),
			(-100, 9, 'Bot', '', true, 'member', now() - interval '90 days'),
			(-100, 10, 'Boss', '', false, 'administrator', now() - interval '90 days'),
			(-100, 1, 'Ali', '', false, 'member', now())`,
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	for i, a := range []ModerationAction{
		{ChatID: -100, Action: "ban", RequesterID: 1, TargetUserID: 7, Reason: "spam", UpdateID: 1},
		{ChatID: -100, Action: "ban", RequesterID: 1, TargetUserID: 8, UpdateID: 2},
		{ChatID: -100, Action: "unban", RequesterID: 1, TargetUserID: 8, UpdateID: 3},
	} {
		if _, err := store.RecordModeration(ctx, a); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	banned, err := store.BannedMembers(ctx, -100, 10)
	if err != nil || len(banned) != 1 || banned[0].UserID != 7 || banned[0].Reason != "spam" || banned[0].ByName != "Ali" || banned[0].Name != "Bek" {
		t.Fatalf("banned = %+v, %v", banned, err)
	}
	if _, err := store.RecordModeration(ctx, ModerationAction{ChatID: -100, Action: "amnesty", RequesterID: 1, UpdateID: 4}); err != nil {
		t.Fatal(err)
	}
	if banned, _ = store.BannedMembers(ctx, -100, 10); len(banned) != 0 {
		t.Errorf("after the amnesty = %+v", banned)
	}
}

// The members acted on lately are listed with their names even once they
// left, the latest first, and older actions and other chats are not.
func TestModerationTargets(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.pool.Exec(ctx, `INSERT INTO group_members (chat_id, user_id, first_name, username, is_bot, status, last_seen_at) VALUES
		(-100, 7, 'Spammer', 'spam_bot_7', false, 'kicked', now()),
		(-100, 8, 'Vali', '', false, 'member', now())`); err != nil {
		t.Fatal(err)
	}
	for i, a := range []ModerationAction{
		{ChatID: -100, Action: "mute", RequesterID: 1, TargetUserID: 8, UpdateID: 11},
		{ChatID: -100, Action: "ban", RequesterID: 1, TargetUserID: 7, UpdateID: 12},
		{ChatID: -200, Action: "ban", RequesterID: 1, TargetUserID: 9, UpdateID: 13},
		{ChatID: -100, Action: "amnesty", RequesterID: 1, UpdateID: 14},
	} {
		if _, err := store.RecordModeration(ctx, a); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	got, err := store.ModerationTargets(ctx, -100, time.Now().Add(-time.Hour), 10)
	if err != nil || len(got) != 2 || got[0].UserID != 7 || got[0].Username != "spam_bot_7" || got[0].Status != "kicked" || got[1].FirstName != "Vali" {
		t.Errorf("targets = %+v, %v", got, err)
	}
	if got, _ := store.ModerationTargets(ctx, -100, time.Now().Add(time.Hour), 10); len(got) != 0 {
		t.Errorf("targets since later = %+v", got)
	}
}
