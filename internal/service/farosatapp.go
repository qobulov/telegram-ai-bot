package service

import (
	"context"
	"fmt"
	"strings"

	"telegram-ai-bot/internal/farosat"
	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// The Farosat Mini App, as the owners asked on 2026-11-27: a member's
// galaxies and the shop where grams are bought with Stars, a group at a
// time. In a group its button sends "/start farosat_g<chat>" to the private
// chat, where the app opens for that group.

// farosatStart starts the /start payload a group's button sends.
const farosatStart = "farosat_"

const (
	textFarosatAppButton = "🌌 Farosat ilovasi"
	textFarosatApp       = "🌌 Farosat ilovasi: galaktikalaringiz va farosat do'koni."
	textFarosatBought    = "🧠 +%d gram farosat qo'shildi!\n🧠 Hozir: %d gram"
	textFarosatNotSaved  = "To'lov qabul qilindi, lekin grammni yozib bo'lmadi: egalariga yozing, qo'shib berishadi yoki pulni qaytarishadi."
	textFarosatOff       = "Farosat do'koni hozir yopiq."
	textFarosatChanged   = "Narx o'zgargan: ilovani qayta ochib, yangidan sotib oling."
	textGalaxyBought     = "🌌 %s galaktikasi sizga ochildi! Farosat ilovasida ko'rishingiz mumkin."
	textGalaxyNotSaved   = "To'lov qabul qilindi, lekin galaktikani ochib bo'lmadi: egalariga yozing, ochib berishadi yoki pulni qaytarishadi."
)

// farosatAppButton opens the app for the group chatID: a link to the
// private chat.
func (s *Service) farosatAppButton(chatID int64) []telegram.Button {
	if s.opts.FarosatURL == "" || s.opts.Bot.Username == "" || chatID >= 0 {
		return nil
	}
	start := farosatStart + miniapp.GroupParam(chatID)
	return []telegram.Button{{Text: textFarosatAppButton, URL: "https://t.me/" + s.opts.Bot.Username + "?start=" + start}}
}

// withFarosatApp is an outcome with the app's button under it.
func (s *Service) withFarosatApp(in incoming, out outcome) outcome {
	if !in.group {
		return out
	}
	if row := s.farosatAppButton(in.scope.ChatID); row != nil {
		out.buttons = append(out.buttons, row)
	}
	return out
}

// startFarosat answers "/start farosat_g<chat>" with the app's button for
// that group; ok is false for any other /start.
func (s *Service) startFarosat(in incoming) (outcome, bool) {
	payload := strings.TrimSpace(strings.TrimPrefix(in.text, commandName(in.text)))
	param, found := strings.CutPrefix(payload, farosatStart)
	if !found || in.group || s.opts.FarosatURL == "" {
		return outcome{}, false
	}
	if _, ok := miniapp.ParseGroupParam(param); !ok {
		return outcome{}, false
	}
	page := s.opts.FarosatURL + "?g=" + param
	return outcome{text: textFarosatApp, buttons: [][]telegram.Button{{{Text: textFarosatAppButton, WebApp: &telegram.WebAppInfo{URL: page}}}}}, true
}

// checkoutFarosat answers Telegram's check before a farosat payment: yes
// for a pack on sale, paid by whom it was made out to, in Stars.
func (s *Service) checkoutFarosat(q *telegram.PreCheckoutQuery) {
	userID, _, pack, _ := farosat.ParsePayload(q.InvoicePayload)
	refusal := ""
	switch {
	case s.opts.Farosat == nil:
		refusal = textFarosatOff
	case q.From == nil || q.From.ID != userID:
		refusal = textCheckoutNotYours.inLanguage("uz")
	case q.Currency != telegram.Stars || q.TotalAmount != pack.Stars:
		refusal = textFarosatChanged
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkoutTimeout)
	defer cancel()
	if err := s.opts.Payments.AnswerPreCheckoutQuery(ctx, q.ID, refusal == "", refusal); err != nil {
		s.opts.Logger.Error("could not answer a farosat payment's check", "error", err)
	}
}

// recordFarosat adds the grams a payment bought to the buyer's farosat in
// its group, once, and says so; a payment not matching its pack is kept
// without grams, for a refund.
func (s *Service) recordFarosat(msg *telegram.Message) {
	paid := msg.SuccessfulPayment
	in := incoming{msg: msg, scope: storage.Scope{ChatID: msg.Chat.ID, UserID: msg.From.ID}, userID: msg.From.ID}
	if s.opts.Farosat == nil {
		s.opts.Logger.Error("a farosat payment arrived with nowhere to record it; refund it by hand", "charge_id", paid.TelegramPaymentChargeID)
		return
	}
	userID, chatID, pack, _ := farosat.ParsePayload(paid.InvoicePayload)
	grams := pack.Grams()
	if userID != msg.From.ID || paid.Currency != telegram.Stars || paid.TotalAmount != pack.Stars {
		s.opts.Logger.Error("a farosat payment does not match its pack; kept without grams, for a refund",
			"user_id", msg.From.ID, "charge_id", paid.TelegramPaymentChargeID, "payload", paid.InvoicePayload, "stars", paid.TotalAmount)
		grams = 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	total, recorded, err := s.opts.Farosat.RecordFarosatPurchase(ctx, storage.FarosatPurchase{
		ChargeID: paid.TelegramPaymentChargeID, ChatID: chatID, UserID: msg.From.ID,
		Name: cardMarks.Replace(displayName(msg.From)), Stars: paid.TotalAmount, Grams: grams,
	})
	switch {
	case err != nil:
		s.opts.Logger.Error("could not record a farosat payment; add the grams by hand or refund it", "user_id", msg.From.ID, "charge_id", paid.TelegramPaymentChargeID, "error", err)
		s.reply(in, textFarosatNotSaved)
		return
	case !recorded:
		return
	case grams == 0:
		s.reply(in, textFarosatNotSaved)
		return
	}
	s.opts.Logger.Info("farosat bought", "user_id", msg.From.ID, "chat_id", chatID, "stars", paid.TotalAmount, "grams", grams)
	s.reply(in, fmt.Sprintf(textFarosatBought, grams, total))
}

// refundFarosat takes a refunded payment's grams back.
func (s *Service) refundFarosat(msg *telegram.Message) {
	if s.opts.Farosat == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	chargeID := msg.RefundedPayment.TelegramPaymentChargeID
	found, err := s.opts.Farosat.RefundFarosatPurchase(ctx, chargeID)
	if err != nil {
		s.opts.Logger.Error("could not take back a refunded farosat payment's grams", "charge_id", chargeID, "error", err)
		return
	}
	if found {
		s.opts.Logger.Info("farosat payment refunded; grams taken back", "charge_id", chargeID)
	}
}

// checkoutGalaxy answers Telegram's check before a galaxy's payment: yes
// for a galaxy on sale at its price, paid by whom it was made out to.
func (s *Service) checkoutGalaxy(q *telegram.PreCheckoutQuery) {
	userID, _, galaxy, _ := farosat.ParseGalaxyPayload(q.InvoicePayload)
	refusal := ""
	switch {
	case s.opts.Farosat == nil:
		refusal = textFarosatOff
	case q.From == nil || q.From.ID != userID:
		refusal = textCheckoutNotYours.inLanguage("uz")
	case q.Currency != telegram.Stars || q.TotalAmount != galaxy.Stars():
		refusal = textFarosatChanged
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkoutTimeout)
	defer cancel()
	if err := s.opts.Payments.AnswerPreCheckoutQuery(ctx, q.ID, refusal == "", refusal); err != nil {
		s.opts.Logger.Error("could not answer a galaxy payment's check", "error", err)
	}
}

// recordGalaxy opens a bought galaxy for the buyer in its group, once, and
// says so; a payment not matching the galaxy's price is not kept, for a
// refund.
func (s *Service) recordGalaxy(msg *telegram.Message) {
	paid := msg.SuccessfulPayment
	in := incoming{msg: msg, scope: storage.Scope{ChatID: msg.Chat.ID, UserID: msg.From.ID}, userID: msg.From.ID}
	userID, chatID, galaxy, _ := farosat.ParseGalaxyPayload(paid.InvoicePayload)
	if s.opts.Farosat == nil || userID != msg.From.ID || paid.Currency != telegram.Stars || paid.TotalAmount != galaxy.Stars() {
		s.opts.Logger.Error("a galaxy payment could not be kept; refund it", "user_id", msg.From.ID, "charge_id", paid.TelegramPaymentChargeID, "payload", paid.InvoicePayload, "stars", paid.TotalAmount)
		s.reply(in, textGalaxyNotSaved)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	recorded, err := s.opts.Farosat.RecordFarosatGalaxy(ctx, storage.FarosatGalaxyPurchase{
		ChargeID: paid.TelegramPaymentChargeID, ChatID: chatID, UserID: msg.From.ID, Galaxy: galaxy.ID, Stars: paid.TotalAmount,
	})
	switch {
	case err != nil:
		s.opts.Logger.Error("could not record a galaxy payment; open it by hand or refund it", "user_id", msg.From.ID, "charge_id", paid.TelegramPaymentChargeID, "error", err)
		s.reply(in, textGalaxyNotSaved)
		return
	case !recorded:
		return
	}
	s.opts.Logger.Info("galaxy bought", "user_id", msg.From.ID, "chat_id", chatID, "galaxy", galaxy.ID, "stars", paid.TotalAmount)
	s.reply(in, fmt.Sprintf(textGalaxyBought, galaxy.Name))
}

// refundGalaxy closes a refunded galaxy again.
func (s *Service) refundGalaxy(msg *telegram.Message) {
	if s.opts.Farosat == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	chargeID := msg.RefundedPayment.TelegramPaymentChargeID
	if found, err := s.opts.Farosat.RefundFarosatGalaxy(ctx, chargeID); err != nil {
		s.opts.Logger.Error("could not close a refunded galaxy", "charge_id", chargeID, "error", err)
	} else if found {
		s.opts.Logger.Info("galaxy payment refunded; galaxy closed", "charge_id", chargeID)
	}
}
