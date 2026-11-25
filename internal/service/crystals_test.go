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
	payments []storage.CrystalPayment
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

func (f *fakeCrystals) StarPayers(context.Context, int) ([]storage.StarPayer, int, error) {
	if len(f.payments) == 0 {
		return nil, 0, nil
	}
	return []storage.StarPayer{{UserID: 42, Name: "Shahruh", Stars: 150}}, 400, nil
}

func (f *fakeCrystals) CrystalPayments(context.Context, int) ([]storage.CrystalPayment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.payments, nil
}

// An owner's /payments lists the crystal purchases too, even with no
// premium payment at all.
func TestPaymentsListCrystalPurchases(t *testing.T) {
	f := newFixture()
	withPremium(f)
	f.opts.OwnerIDs = map[int64]bool{premiumOwner: true}
	f.opts.Crystals = &fakeCrystals{payments: []storage.CrystalPayment{
		{ChargeID: "ch_9", UserID: 42, Name: "Shahruh", Stars: 99, Crystals: 1500, PaidAt: premiumNow},
	}}

	sent := f.run(ownerWrites("/payments"))

	if len(sent) != 1 || !strings.Contains(sent[0].text, "So'nggi kristall to'lovlari:") || !strings.Contains(sent[0].text, "Shahruh (42): 99 ⭐, 1500 💎") || !strings.Contains(sent[0].text, "Jami to'langan (qaytarilganlarsiz): 400 ⭐") || !strings.Contains(sent[0].text, "• Shahruh (42): 150 ⭐") || strings.Contains(sent[0].text, "premium") {
		t.Fatalf("sent = %+v", sent)
	}
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

func TestDiamondBoxPayment(t *testing.T) {
	f := newFixture()
	_, payments, _ := withPremium(f)
	crystals := &fakeCrystals{charges: map[string]int{}}
	f.opts.Crystals = crystals
	f.opts.BoxRoll = func(n int) int { return n - 1 } // the last percent: the rarest prize
	s := New(f.opts)
	payload := fmt.Sprintf("cr:%d:box", userID)
	s.HandleNow(checkoutUpdate(userID, payload, 129))
	s.HandleNow(checkoutUpdate(userID, payload, 99))
	if len(payments.answers) != 2 || !payments.answers[0].ok || payments.answers[1].refusal != textCrystalsChanged.uz {
		t.Fatalf("answers = %+v", payments.answers)
	}
	sent := f.run(paymentUpdate(userID, "ch_box", payload, 129))
	if crystals.shards != 5000 || len(sent) != 1 || !strings.Contains(sent[0].text, "Almaz box") || !strings.Contains(sent[0].text, "+5000") {
		t.Fatalf("shards = %d, sent %+v", crystals.shards, sent)
	}
}
