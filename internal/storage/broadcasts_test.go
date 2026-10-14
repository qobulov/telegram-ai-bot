package storage

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

const owner = int64(9001)

func TestBroadcastIsConfirmedOnceInALaterMessage(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	draft := Broadcast{OwnerID: owner, UpdateID: 10, Text: "Master qaytdi", ChatIDs: []int64{group, otherGroup}}
	if err := store.SaveBroadcast(ctx, draft); err != nil {
		t.Fatalf("SaveBroadcast: %v", err)
	}

	if _, err := store.ConfirmBroadcast(ctx, owner, 10, time.Minute); !errors.Is(err, ErrNoBroadcast) {
		t.Errorf("confirmed in the message that drafted it: err = %v", err)
	}
	if _, err := store.ConfirmBroadcast(ctx, owner+1, 11, time.Minute); !errors.Is(err, ErrNoBroadcast) {
		t.Errorf("another owner confirmed it: err = %v", err)
	}
	got, err := store.ConfirmBroadcast(ctx, owner, 11, time.Minute)
	if err != nil || got.Text != draft.Text || !reflect.DeepEqual(got.ChatIDs, draft.ChatIDs) {
		t.Fatalf("confirmed = %+v, err = %v", got, err)
	}
	if _, err := store.ConfirmBroadcast(ctx, owner, 12, time.Minute); !errors.Is(err, ErrNoBroadcast) {
		t.Errorf("confirmed twice: err = %v", err)
	}
}

func TestOnlyTheNewestFreshDraftCanBeConfirmed(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for i, text := range []string{"first", "second"} {
		if err := store.SaveBroadcast(ctx, Broadcast{OwnerID: owner, UpdateID: int64(i + 1), Text: text, ChatIDs: []int64{group}}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.ConfirmBroadcast(ctx, owner, 5, time.Minute)
	if err != nil || got.Text != "second" {
		t.Errorf("confirmed = %+v, err = %v", got, err)
	}
	if _, err := store.ConfirmBroadcast(ctx, owner, 6, time.Minute); !errors.Is(err, ErrNoBroadcast) {
		t.Errorf("an older draft was left to confirm: err = %v", err)
	}

	if err := store.SaveBroadcast(ctx, Broadcast{OwnerID: owner, UpdateID: 7, Text: "stale", ChatIDs: []int64{group}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE broadcasts SET created_at = now() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConfirmBroadcast(ctx, owner, 8, 10*time.Minute); !errors.Is(err, ErrNoBroadcast) {
		t.Errorf("a stale draft was confirmed: err = %v", err)
	}
}

func TestCancelBroadcastDropsTheDraft(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SaveBroadcast(ctx, Broadcast{OwnerID: owner, UpdateID: 1, Text: "x", ChatIDs: []int64{group}}); err != nil {
		t.Fatal(err)
	}
	if cancelled, err := store.CancelBroadcast(ctx, owner); err != nil || !cancelled {
		t.Fatalf("cancelled = %v, err = %v", cancelled, err)
	}
	if _, err := store.ConfirmBroadcast(ctx, owner, 2, time.Minute); !errors.Is(err, ErrNoBroadcast) {
		t.Errorf("a cancelled draft was confirmed: err = %v", err)
	}
}
