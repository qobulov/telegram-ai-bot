package cards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

func (f *fakeStore) BuyCardTokens(_ context.Context, _ int64, _ string, tokens, price int) (storage.CardOpening, error) {
	if price > f.shards {
		return storage.CardOpening{}, &storage.CardFundsError{Tokens: f.tokens, Shards: f.shards, Need: price}
	}
	f.shards, f.tokens = f.shards-price, f.tokens+tokens
	return storage.CardOpening{Tokens: f.tokens, Shards: f.shards}, nil
}

func (f *fakeStore) OpenCardPack(_ context.Context, userID int64, _ string, n int, roll storage.CardRoll, price int, _ time.Time, quest storage.CardQuest) (storage.CardOpening, error) {
	if price > f.shards {
		return storage.CardOpening{}, &storage.CardFundsError{Tokens: f.tokens, Shards: f.shards, Need: price}
	}
	f.shards -= price
	open := storage.CardOpening{Tokens: f.tokens, Shards: f.shards}
	for i := range n {
		rarity, _ := roll(0, 0)
		card := lockedCard
		card.Rarity = rarity
		open.Draws = append(open.Draws, storage.CardDraw{Card: card, Count: 1, New: true, Serial: i + 1})
	}
	f.count(userID, quest, n)
	return open, nil
}

type fakeInvoices struct{ made []telegram.Invoice }

func (f *fakeInvoices) CreateInvoiceLink(_ context.Context, inv telegram.Invoice) (string, error) {
	f.made = append(f.made, inv)
	return "https://t.me/$invoice", nil
}

func postItem(t *testing.T, h http.Handler, initData, action, item string) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"init_data": initData, "action": action, "item": item})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cards", strings.NewReader(string(body))))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

// A pack holds at least one card of its rarity, raised on the last draw
// only when none came before.
func TestPackRoll(t *testing.T) {
	roll := PackRoll(func() Rarity { return Common }, 5, Epic)
	var got []int
	for range 5 {
		r, _ := roll(0, 0)
		got = append(got, r)
	}
	if got[4] != int(Epic) || got[0] != int(Common) {
		t.Errorf("all commons = %v, want the last raised to Epic", got)
	}
	lucky := PackRoll(func() Rarity { return Legendary }, 3, Epic)
	for range 3 {
		if r, raised := lucky(0, 0); r != int(Legendary) || raised {
			t.Errorf("a Legendary was changed: %d %v", r, raised)
		}
	}
}

// The shop sells tokens and packs for crystals, says what is missing when
// too few, and makes an invoice for crystals made out to the player.
func TestShop(t *testing.T) {
	now := time.Now()
	store, invoices := &fakeStore{shards: 200}, &fakeInvoices{}
	h := NewHandler(Options{BotToken: testToken, Store: store, Invoices: invoices, Now: func() time.Time { return now }, Roll: func() Rarity { return Common }})
	init := signedInit(7, now)
	if out := postItem(t, h, init, "shop", ""); len(out["items"].([]any)) != len(ShopItems) || out["stars"] != true {
		t.Errorf("shop = %v", out)
	}
	if out := postItem(t, h, init, "buy", "t5"); out["tokens"] != float64(5) || out["shards"] != float64(140) {
		t.Errorf("tokens = %v", out)
	}
	out := postItem(t, h, init, "buy", "epic")
	drawn, _ := out["drawn"].([]any)
	if len(drawn) != 5 || out["shards"] != float64(20) {
		t.Fatalf("epic pack = %v", out)
	}
	last := drawn[4].(map[string]any)["card"].(map[string]any)
	if last["rarity"] != float64(Epic) || drawn[4].(map[string]any)["serial"] != 5.0 {
		t.Errorf("the pack's sure card = %v", last)
	}
	if out := postItem(t, h, init, "buy", "legendary"); out["short"] != float64(330) {
		t.Errorf("too few = %v", out)
	}
	if out := postItem(t, h, init, "invoice", "c300"); out["link"] == nil || len(invoices.made) != 1 || invoices.made[0].Price != 25 || invoices.made[0].Payload != "cr:7:c300" {
		t.Errorf("invoice = %v, made %+v", out, invoices.made)
	}
	if userID, pack, ok := ParseCrystalPayload("cr:7:c300"); !ok || userID != 7 || pack.Crystals != 300 {
		t.Errorf("parse = %d %+v %v", userID, pack, ok)
	}
	if _, _, ok := ParseCrystalPayload("p:7:30:150"); ok {
		t.Error("a premium payload read as crystals")
	}
}
