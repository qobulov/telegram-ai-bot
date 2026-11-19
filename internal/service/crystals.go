package service

import (
	"context"
	"fmt"

	"telegram-ai-bot/internal/cards"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// CrystalBook keeps crystals bought for Stars in the cards app.
type CrystalBook interface {
	RecordCrystalPurchase(ctx context.Context, p storage.CrystalPurchase) (shards int, recorded bool, err error)
	MarkCrystalRefunded(ctx context.Context, chargeID string) (bool, error)
	CrystalPayments(ctx context.Context, limit int) ([]storage.CrystalPayment, error)
}

var (
	textCrystalsOff      = localText{uz: "Kristall sotuvi hozir yopiq.", ru: "Продажа кристаллов сейчас закрыта.", en: "Crystals are not on sale now."}
	textCrystalsChanged  = localText{uz: "Narx o'zgargan: ilovani qayta ochib, yana sotib oling.", ru: "Цена изменилась: откройте приложение заново.", en: "The price changed: open the app again."}
	textCrystalsThanks   = localText{uz: "💎 +%d kristall qo'shildi! Endi sizda %d 💎. Do'kon: /card", ru: "💎 +%d кристаллов! Теперь у вас %d 💎. Магазин: /card", en: "💎 +%d crystals! You now have %d 💎. Shop: /card"}
	textCrystalsNotSaved = localText{uz: "To'lov qabul qilindi, lekin kristallni yozib bo'lmadi. Egalari tez orada qo'shadi yoki pulni qaytaradi.", ru: "Оплата принята, но кристаллы не записались. Владельцы скоро добавят их или вернут деньги.", en: "The payment went through but the crystals could not be added; the owners will add them or refund it."}
)

// checkoutCrystals answers Telegram's check before a crystal payment: yes
// for a pack on sale at the price paid, by the player it was made out to.
func (s *Service) checkoutCrystals(q *telegram.PreCheckoutQuery) {
	in := incoming{msg: &telegram.Message{From: q.From}}
	userID, pack, _ := cards.ParseCrystalPayload(q.InvoicePayload)
	refusal := localText{}
	switch {
	case s.opts.Crystals == nil:
		refusal = textCrystalsOff
	case q.From == nil || q.From.ID != userID:
		refusal = textCheckoutNotYours
	case q.Currency != telegram.Stars || q.TotalAmount != pack.Stars:
		refusal = textCrystalsChanged
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkoutTimeout)
	defer cancel()
	if err := s.opts.Payments.AnswerPreCheckoutQuery(ctx, q.ID, refusal == (localText{}), refusal.to(in)); err != nil {
		s.opts.Logger.Error("could not answer a crystal payment's check", "error", err)
	}
}

// recordCrystals adds the crystals a payment bought, once, and thanks the
// buyer; a payment not matching its pack is kept without crystals, for a
// refund.
func (s *Service) recordCrystals(msg *telegram.Message) {
	paid := msg.SuccessfulPayment
	in := incoming{msg: msg, scope: storage.Scope{ChatID: msg.Chat.ID, UserID: msg.From.ID}, userID: msg.From.ID}
	if s.opts.Crystals == nil {
		s.opts.Logger.Error("a crystal payment arrived with nowhere to record it; refund it by hand", "charge_id", paid.TelegramPaymentChargeID)
		return
	}
	userID, pack, _ := cards.ParseCrystalPayload(paid.InvoicePayload)
	crystals := pack.Crystals
	if userID != msg.From.ID || paid.Currency != telegram.Stars || paid.TotalAmount != pack.Stars {
		s.opts.Logger.Error("a crystal payment does not match its pack; kept without crystals, for a refund",
			"user_id", msg.From.ID, "charge_id", paid.TelegramPaymentChargeID, "payload", paid.InvoicePayload, "stars", paid.TotalAmount)
		crystals = 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	shards, recorded, err := s.opts.Crystals.RecordCrystalPurchase(ctx, storage.CrystalPurchase{
		ChargeID: paid.TelegramPaymentChargeID, UserID: msg.From.ID, Name: msg.From.FirstName, Stars: paid.TotalAmount, Crystals: crystals,
	})
	switch {
	case err != nil:
		s.opts.Logger.Error("could not record a crystal payment; add them by hand or refund it", "user_id", msg.From.ID, "charge_id", paid.TelegramPaymentChargeID, "error", err)
		s.reply(in, textCrystalsNotSaved.to(in))
		return
	case !recorded:
		return
	case crystals == 0:
		s.reply(in, textCrystalsNotSaved.to(in))
		return
	}
	s.opts.Logger.Info("crystals bought", "user_id", msg.From.ID, "stars", paid.TotalAmount, "crystals", crystals)
	s.reply(in, fmt.Sprintf(textCrystalsThanks.to(in), crystals, shards))
}

// refundCrystals takes back a refunded payment's crystals.
func (s *Service) refundCrystals(msg *telegram.Message) {
	if s.opts.Crystals == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	chargeID := msg.RefundedPayment.TelegramPaymentChargeID
	if refunded, err := s.opts.Crystals.MarkCrystalRefunded(ctx, chargeID); err != nil {
		s.opts.Logger.Error("could not record a crystal refund", "charge_id", chargeID, "error", err)
	} else if refunded {
		s.opts.Logger.Info("crystal payment refunded; crystals taken back", "charge_id", chargeID)
	}
}
