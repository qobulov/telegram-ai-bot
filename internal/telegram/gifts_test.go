package telegram

import (
	"context"
	"reflect"
	"testing"
)

func TestSendGiftSendsExactParams(t *testing.T) {
	cases := map[string]struct {
		text string
		want map[string]any
	}{
		"with text": {"Tabriklayman!", map[string]any{"user_id": float64(42), "gift_id": "g1", "text": "Tabriklayman!"}},
		"alone":     {"", map[string]any{"user_id": float64(42), "gift_id": "g1"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			client, api := newTestClient(t, okTrue)

			if err := client.SendGift(context.Background(), 42, "g1", c.text); err != nil {
				t.Fatal(err)
			}
			sent := api.recorded()[0]
			if sent.method != "sendGift" || !reflect.DeepEqual(sent.params, c.want) {
				t.Errorf("call = %+v, want sendGift %v", sent, c.want)
			}
		})
	}
}

func TestGetAvailableGiftsAndStarBalance(t *testing.T) {
	client, api := newTestClient(t,
		`{"ok":true,"result":{"gifts":[{"id":"g1","sticker":{"file_id":"s","emoji":"🧸"},"star_count":15},{"id":"g2","sticker":{"file_id":"t","emoji":"💎"},"star_count":100,"total_count":1000,"remaining_count":7}]}}`,
		`{"ok":true,"result":{"amount":250,"nanostar_amount":5}}`)

	gifts, err := client.GetAvailableGifts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Gift{
		{ID: "g1", Sticker: Sticker{FileID: "s", Emoji: "🧸"}, StarCount: 15},
		{ID: "g2", Sticker: Sticker{FileID: "t", Emoji: "💎"}, StarCount: 100, TotalCount: 1000, RemainingCount: 7},
	}
	if !reflect.DeepEqual(gifts, want) {
		t.Errorf("gifts = %+v, want %+v", gifts, want)
	}
	balance, err := client.GetMyStarBalance(context.Background())
	if err != nil || balance != 250 {
		t.Errorf("balance = %d, %v; want 250", balance, err)
	}
	if calls := api.recorded(); calls[0].method != "getAvailableGifts" || calls[1].method != "getMyStarBalance" {
		t.Errorf("calls = %+v", calls)
	}
}
