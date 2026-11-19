package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// A letter's gifts are claimed once, even by claims made at the same time:
// boxes into the inventory, tokens and crystals into the wallet; a box is
// opened once per box held.
func TestCardMailAndBoxes(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedArena(t, store)
	setWallet(t, store, 1, 0, 5)

	id, err := store.SendCardMail(ctx, CardMail{UserID: 1, Note: "Tabriklaymiz!", Box: "box", Boxes: 2, Tokens: 3, Crystals: 100, SentBy: 9})
	if err != nil {
		t.Fatal(err)
	}
	if boxes, unclaimed, _ := store.CardInbox(ctx, 1); boxes != 0 || unclaimed != 1 {
		t.Fatalf("inbox = %d boxes, %d unclaimed", boxes, unclaimed)
	}
	if _, _, claimed, _ := store.ClaimCardMail(ctx, 2, "Bob", id); claimed {
		t.Fatal("another player claimed the letter")
	}

	var wg sync.WaitGroup
	results := make(chan bool, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, claimed, err := store.ClaimCardMail(ctx, 1, "Ali", id)
			if err != nil {
				t.Error(err)
			}
			results <- claimed
		}()
	}
	wg.Wait()
	close(results)
	n := 0
	for claimed := range results {
		if claimed {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the letter was claimed %d times", n)
	}
	mail, err := store.CardMailbox(ctx, 1, 10)
	if err != nil || len(mail) != 1 || mail[0].ClaimedAt == nil || mail[0].Note != "Tabriklaymiz!" {
		t.Fatalf("mailbox = %+v, %v", mail, err)
	}
	if boxes, _ := store.CardBoxes(ctx, 1); boxes["box"] != 2 {
		t.Fatalf("boxes = %v", boxes)
	}

	shards, left, err := store.OpenCardBox(ctx, 1, "Ali", "box", 2500)
	if err != nil || shards != 5+100+2500 || left != 1 {
		t.Fatalf("open = %d shards, %d left, %v", shards, left, err)
	}
	if _, left, err := store.OpenCardBox(ctx, 1, "Ali", "box", 1500); err != nil || left != 0 {
		t.Fatalf("second open = %d left, %v", left, err)
	}
	if _, _, err := store.OpenCardBox(ctx, 1, "Ali", "box", 1500); !errors.Is(err, ErrNoCardBox) {
		t.Fatalf("a third open = %v, want ErrNoCardBox", err)
	}
	if boxes, unclaimed, _ := store.CardInbox(ctx, 1); boxes != 0 || unclaimed != 0 {
		t.Fatalf("inbox after = %d boxes, %d unclaimed", boxes, unclaimed)
	}
}
