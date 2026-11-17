package storage

import (
	"context"
	"testing"
	"time"
)

// A report is open until its first settling; a ban settles it and lists the
// member as banned; a second press settles nothing.
func TestReportsSettleOnce(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	id, err := store.AddReport(ctx, Report{ChatID: -100, ReporterID: 1, ReporterName: "Ali", TargetID: 7, TargetName: "Spam", MessageID: 5, Quote: "reklama"})
	if err != nil {
		t.Fatal(err)
	}
	open, err := store.OpenReports(ctx, -100, time.Now().Add(-time.Hour), 10)
	if err != nil || len(open) != 1 || open[0].Quote != "reklama" {
		t.Fatalf("open = %+v, %v", open, err)
	}
	r, settled, err := store.SettleReport(ctx, id, "banned", 9)
	if err != nil || !settled || r.TargetID != 7 {
		t.Fatalf("settle = %+v %v %v", r, settled, err)
	}
	if _, settled, _ := store.SettleReport(ctx, id, "rejected", 9); settled {
		t.Error("settled twice")
	}
	if open, _ := store.OpenReports(ctx, -100, time.Now().Add(-time.Hour), 10); len(open) != 0 {
		t.Errorf("open after settling = %+v", open)
	}
	banned, err := store.BannedMembers(ctx, -100, 10)
	if err != nil || len(banned) != 1 || banned[0].UserID != 7 {
		t.Errorf("banned = %+v, %v", banned, err)
	}
}

// A warning a month old has lapsed: the next starts the count again.
func TestWarnsLapse(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, _ = store.AddWarn(ctx, -100, 8, "a")
	_, _ = store.AddWarn(ctx, -100, 8, "b")
	if _, err := store.pool.Exec(ctx, `UPDATE member_warns SET updated_at = now() - interval '31 days' WHERE user_id = 8`); err != nil {
		t.Fatal(err)
	}
	if n, err := store.AddWarn(ctx, -100, 8, "c"); err != nil || n != 1 {
		t.Errorf("after a month = %d, %v", n, err)
	}
	warns, err := store.Warns(ctx, -100, 8, 5)
	if err != nil || len(warns) != 1 || warns[0].Reason != "c" {
		t.Errorf("warns = %+v, %v", warns, err)
	}
}
