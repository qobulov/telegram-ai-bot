package storage

import (
	"context"
	"testing"
	"time"
)

func recordPayment(t *testing.T, store *Store, p StarPayment) (time.Time, bool) {
	t.Helper()
	until, recorded, err := store.RecordStarPayment(context.Background(), p)
	if err != nil {
		t.Fatalf("RecordStarPayment: %v", err)
	}
	return until, recorded
}

// within reports whether got is want, give or take a minute of the
// database's clock.
func within(got, want time.Time) bool {
	return got.Sub(want).Abs() < time.Minute
}

// A payment buys its days from now, a second one adds them to the first's
// end, and the same payment delivered again changes nothing.
func TestRecordStarPaymentStacksAndCountsOnce(t *testing.T) {
	store := newTestStore(t)
	first := StarPayment{ChargeID: "ch_1", UserID: alice, FirstName: "Alice", Username: "alice", Stars: 100, Days: 30}

	until, recorded := recordPayment(t, store, first)
	if !recorded || !within(until, time.Now().AddDate(0, 0, 30)) {
		t.Fatalf("first = %v, %v; want 30 days from now", until, recorded)
	}
	again, recorded := recordPayment(t, store, first)
	if recorded || !again.Equal(until) {
		t.Errorf("again = %v, %v; want the same end, unrecorded", again, recorded)
	}
	stacked, recorded := recordPayment(t, store, StarPayment{ChargeID: "ch_2", UserID: alice, Stars: 100, Days: 30})
	if !recorded || !within(stacked, time.Now().AddDate(0, 0, 60)) {
		t.Errorf("second = %v, %v; want 60 days from now", stacked, recorded)
	}
	if got, err := store.PremiumUntil(context.Background(), alice); err != nil || !got.Equal(stacked) {
		t.Errorf("PremiumUntil = %v, %v; want %v", got, err, stacked)
	}
	if rows := countRows(t, store, "star_payments"); rows != 2 {
		t.Errorf("star_payments rows = %d, want 2", rows)
	}
}

// A premium that ended is bought again from now, not from its old end.
func TestRecordStarPaymentAfterPremiumEnded(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	recordPayment(t, store, StarPayment{ChargeID: "ch_1", UserID: alice, Stars: 100, Days: 30})
	if _, err := store.pool.Exec(ctx, `UPDATE premium_users SET until = now() - interval '5 days'`); err != nil {
		t.Fatal(err)
	}

	until, _ := recordPayment(t, store, StarPayment{ChargeID: "ch_2", UserID: alice, Stars: 100, Days: 30})
	if !within(until, time.Now().AddDate(0, 0, 30)) {
		t.Errorf("until = %v, want 30 days from now", until)
	}
}

// A payment of no days, whose invoice could not be read, is kept for a
// refund and buys nothing.
func TestRecordStarPaymentOfNoDays(t *testing.T) {
	store := newTestStore(t)
	until, recorded := recordPayment(t, store, StarPayment{ChargeID: "ch_1", UserID: alice, Stars: 100})
	if !recorded || !until.IsZero() {
		t.Errorf("until = %v, %v; want recorded and no premium", until, recorded)
	}
}

// A refund ends the premium now, once; the latest unrefunded payment is
// found by ID or by the username it was paid under.
func TestRefundEndsPremium(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	recordPayment(t, store, StarPayment{ChargeID: "ch_1", UserID: alice, Username: "Alice", Stars: 100, Days: 30})
	recordPayment(t, store, StarPayment{ChargeID: "ch_2", UserID: alice, Username: "Alice", Stars: 100, Days: 30})
	recordPayment(t, store, StarPayment{ChargeID: "ch_bob", UserID: bob, Stars: 100, Days: 30})

	byName, found, err := store.LastStarPayment(ctx, 0, "alice")
	if err != nil || !found || byName.ChargeID != "ch_2" || byName.UserID != alice {
		t.Fatalf("LastStarPayment(@alice) = %+v, %v, %v; want ch_2", byName, found, err)
	}
	if refunded, err := store.MarkStarRefunded(ctx, "ch_2"); err != nil || !refunded {
		t.Fatalf("MarkStarRefunded = %v, %v", refunded, err)
	}
	if refunded, _ := store.MarkStarRefunded(ctx, "ch_2"); refunded {
		t.Error("a payment was refunded twice")
	}
	if until, _ := store.PremiumUntil(ctx, alice); until.After(time.Now()) {
		t.Errorf("alice's premium lasts until %v after the refund", until)
	}
	if until, _ := store.PremiumUntil(ctx, bob); !until.After(time.Now()) {
		t.Errorf("bob's premium = %v; another user's refund ended it", until)
	}
	next, found, _ := store.LastStarPayment(ctx, alice, "")
	if !found || next.ChargeID != "ch_1" {
		t.Errorf("next = %+v, %v; want the earlier payment", next, found)
	}
	if _, found, _ := store.LastStarPayment(ctx, 0, ""); found {
		t.Error("an empty username matched a payment")
	}

	payments, err := store.StarPayments(ctx, 2)
	if err != nil || len(payments) != 2 || payments[0].ChargeID != "ch_bob" || payments[1].RefundedAt.IsZero() {
		t.Errorf("StarPayments = %+v, %v; want the two newest, the refund marked", payments, err)
	}
}

// While premium lasts the premium limit applies, at once and only to that
// user; without a premium limit premium changes nothing.
func TestPremiumRaisesThePrivateLimit(t *testing.T) {
	store := newTestStore(t)
	since := time.Now().Add(-time.Hour)
	quota := DailyQuota{UserID: alice, Since: since, Limit: 1, PremiumLimit: 3}
	recordPayment(t, store, StarPayment{ChargeID: "ch_1", UserID: alice, Stars: 100, Days: 30})

	for i := range 3 {
		if _, r := reservePrivate(t, store, quota); !r.allowed {
			t.Fatalf("answer %d refused within the premium limit", i+1)
		}
	}
	if _, r := reservePrivate(t, store, quota); r.allowed || !r.notify {
		t.Errorf("past the premium limit = %+v, want the notice", r)
	}
	if _, r := reservePrivate(t, store, DailyQuota{UserID: bob, Since: since, Limit: 1, PremiumLimit: 3}); !r.allowed {
		t.Fatal("bob's first answer refused")
	}
	if _, r := reservePrivate(t, store, DailyQuota{UserID: bob, Since: since, Limit: 1, PremiumLimit: 3}); r.allowed {
		t.Error("bob, without premium, got the premium limit")
	}
	const carol = int64(1003)
	recordPayment(t, store, StarPayment{ChargeID: "ch_3", UserID: carol, Stars: 100, Days: 30})
	reservePrivate(t, store, DailyQuota{UserID: carol, Since: since, Limit: 1})
	if _, r := reservePrivate(t, store, DailyQuota{UserID: carol, Since: since, Limit: 1}); r.allowed {
		t.Error("with no premium limit, premium still raised it")
	}
}
