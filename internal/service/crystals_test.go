package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

type fakeCrystals struct {
	mu       sync.Mutex
	shards   int
	charges  map[string]int
	refunded []string
}

func (f *fakeCrystals) RecordCrystalPurchase(_ context.Context, p storage.CrystalPurchase) (int, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, seen := f.charges[p.ChargeID]; seen {
		return f.shards, false, nil
	}
	f.charges[p.ChargeID] = p.Crystals
	f.shards += p.Crystals
	return f.shards, true, nil
}

func (f *fakeCrystals) MarkCrystalRefunded(_ context.Context, chargeID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refunded = append(f.refunded, chargeID)
	return true, nil
}

// A crystal payment is checked against the pack on sale and its buyer,
// added once and thanked; a refund takes the crystals back. Premium's
// payments are left to premium.
func TestCrystalPayments(t *testing.T) {
	f := newFixture()
	_, payments, _ := withPremium(f)
	crystals := &fakeCrystals{charges: map[string]int{}}
	f.opts.Crystals = crystals
	s := New(f.opts)
	payload := fmt.Sprintf("cr:%d:c300", userID)
	s.HandleNow(checkoutUpdate(userID, payload, 25))
	s.HandleNow(checkoutUpdate(9, payload, 25))
	s.HandleNow(checkoutUpdate(userID, payload, 50))
	if len(payments.answers) != 3 || !payments.answers[0].ok || payments.answers[1].ok || payments.answers[2].refusal != textCrystalsChanged.uz {
		t.Fatalf("answers = %+v", payments.answers)
	}
	payment := paymentUpdate(userID, "ch_c", payload, 25)
	sent := f.run(payment, payment)
	if crystals.shards != 300 || len(sent) != 1 || !strings.Contains(sent[0].text, "+300") {
		t.Fatalf("shards = %d, sent %+v", crystals.shards, sent)
	}
	nextUpdateID++
	refund := telegram.Update{UpdateID: nextUpdateID, Message: &telegram.Message{
		MessageID: 61, From: &telegram.User{ID: userID}, Chat: telegram.Chat{ID: userID, Type: telegram.ChatTypePrivate}, Service: "refunded_payment",
		RefundedPayment: &telegram.RefundedPayment{Currency: telegram.Stars, TotalAmount: 25, InvoicePayload: payload, TelegramPaymentChargeID: "ch_c"},
	}}
	f.run(refund)
	if len(crystals.refunded) != 1 || crystals.refunded[0] != "ch_c" {
		t.Errorf("refunded = %v", crystals.refunded)
	}
}
