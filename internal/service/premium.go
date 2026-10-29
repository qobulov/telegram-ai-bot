package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// Premium is sold in private chats for Telegram Stars: PremiumPrice Stars
// buy PremiumDays days of PremiumDailyLimit answers a day instead of
// PrivateDailyLimit. /premium sends the invoice, and so does the day's one
// notice that the limit is reached, so that the offer comes when it is
// wanted and at most once a day. Telegram asks before it takes the money,
// which is answered from the invoice's payload alone, within its 10
// seconds; then it reports the payment, which is recorded by its charge ID
// and adds its days to the buyer's premium. Owners list the payments with
// /payments and give one back with /refund, which ends that premium.

// Payments sends invoices in Stars, answers Telegram's check before a
// payment, and gives a payment back.
type Payments interface {
	SendInvoice(ctx context.Context, inv telegram.Invoice) (int64, error)
	AnswerPreCheckoutQuery(ctx context.Context, id string, ok bool, refusal string) error
	RefundStarPayment(ctx context.Context, userID int64, chargeID string) error
}

// PremiumBook keeps the payments for premium and how long each user's
// lasts. It lives in the database: a payment is money, and must outlive
// any instance.
type PremiumBook interface {
	RecordStarPayment(ctx context.Context, p storage.StarPayment) (until time.Time, recorded bool, err error)
	PremiumUntil(ctx context.Context, userID int64) (time.Time, error)
	LastStarPayment(ctx context.Context, userID int64, username string) (storage.StarPayment, bool, error)
	MarkStarRefunded(ctx context.Context, chargeID string) (bool, error)
	StarPayments(ctx context.Context, limit int) ([]storage.StarPayment, error)
}

const (
	// checkoutTimeout bounds answering Telegram's check before a payment,
	// which it waits 10 seconds for.
	checkoutTimeout = 8 * time.Second
	// paymentsListed is how many payments /payments shows.
	paymentsListed = 20
	// maxInvoiceTitle is the longest title Telegram takes on an invoice.
	maxInvoiceTitle = 32
	premiumDate     = "02.01.2006"
	paymentTime     = "02.01 15:04"
	// chargeRefunded is Telegram's word for a payment refunded already.
	chargeRefunded = "CHARGE_ALREADY_REFUNDED"
)

// premiumPlan is what an invoice sells: days of premium for stars, to one
// user. It travels as the invoice's payload, which Telegram hands back with
// the check and the payment, so that both are judged by what was offered.
type premiumPlan struct {
	userID      int64
	days, stars int
}

func (p premiumPlan) payload() string {
	return fmt.Sprintf("premium:%d:%d:%d", p.userID, p.days, p.stars)
}

// parsePlan reads a payload made by payload.
func parsePlan(payload string) (premiumPlan, bool) {
	parts := strings.Split(payload, ":")
	if len(parts) != 4 || parts[0] != "premium" {
		return premiumPlan{}, false
	}
	userID, err1 := strconv.ParseInt(parts[1], 10, 64)
	days, err2 := strconv.Atoi(parts[2])
	stars, err3 := strconv.Atoi(parts[3])
	if err := errors.Join(err1, err2, err3); err != nil || days <= 0 || stars <= 0 {
		return premiumPlan{}, false
	}
	return premiumPlan{userID: userID, days: days, stars: stars}, true
}

// plan is the premium on sale now, for userID.
func (s *Service) plan(userID int64) premiumPlan {
	return premiumPlan{userID: userID, days: s.opts.PremiumDays, stars: s.opts.PremiumPrice}
}

// sellsPremium reports whether premium is on sale: a price is set, and it
// gives more than a private chat has without it.
func (s *Service) sellsPremium() bool {
	o := s.opts
	return o.Payments != nil && o.Premium != nil && o.PremiumPrice > 0 && o.PremiumDays > 0 &&
		o.PrivateQuota != nil && o.PrivateDailyLimit > 0 && o.PremiumDailyLimit > o.PrivateDailyLimit
}

// premiumLimit is the daily limit of a user whose premium lasts, for the
// quota: bought premium holds even once it is no longer sold, and is never
// less than the limit without it. 0 when premium is not kept at all.
func (s *Service) premiumLimit() int {
	if s.opts.Premium == nil {
		return 0
	}
	return max(s.opts.PremiumDailyLimit, s.opts.PrivateDailyLimit)
}

// premiumUntil is when the user's premium ends, zero for none. When it
// cannot be read the user is taken to have none: that only offers them
// premium.
func (s *Service) premiumUntil(ctx context.Context, userID int64) time.Time {
	if s.opts.Premium == nil {
		return time.Time{}
	}
	ctx, cancel := context.WithTimeout(ctx, logTimeout)
	defer cancel()
	until, err := s.opts.Premium.PremiumUntil(ctx, userID)
	if err != nil {
		s.opts.Logger.Warn("could not read a user's premium", "user_id", userID, "error", err)
		return time.Time{}
	}
	return until
}

// hasPremium reports a premium that has not ended.
func (s *Service) hasPremium(ctx context.Context, userID int64) (until time.Time, ok bool) {
	until = s.premiumUntil(ctx, userID)
	return until, until.After(s.opts.Now())
}

// limitReached is the answer to a private message past the day's limit:
// the day's one notice, offering premium with an invoice to a sender who
// has none while it is sold, and nothing after it.
func (s *Service) limitReached(ctx context.Context, in incoming, notify bool) outcome {
	out := outcome{skipped: "private_limit"}
	if !notify {
		return out
	}
	if _, ok := s.hasPremium(ctx, in.userID); ok {
		out.text = fmt.Sprintf(textDailyLimit.to(in), s.premiumLimit())
		return out
	}
	out.text = fmt.Sprintf(textDailyLimit.to(in), s.opts.PrivateDailyLimit)
	if s.sellsPremium() {
		out.text += "\n\n" + s.premiumOffer(in)
		out.invoice = true
	}
	return out
}

// premiumOffer says what premium gives and costs.
func (s *Service) premiumOffer(in incoming) string {
	return fmt.Sprintf(textPremiumOffer.to(in), s.opts.PremiumDays, s.opts.PremiumDailyLimit, s.opts.PremiumPrice)
}

// premiumCommand answers /premium: in a private chat, what premium gives,
// or until when the sender has it, with the invoice to buy or extend it.
func (s *Service) premiumCommand(ctx context.Context, in incoming) outcome {
	switch {
	case in.group:
		return say(textPremiumPrivateOnly.to(in))
	case !s.sellsPremium():
		return say(textNoPremium.to(in))
	}
	if until, ok := s.hasPremium(ctx, in.userID); ok {
		date := until.In(s.opts.Location).Format(premiumDate)
		return outcome{text: fmt.Sprintf(textPremiumActive.to(in), date, s.opts.PremiumDailyLimit), invoice: true}
	}
	return outcome{text: s.premiumOffer(in), invoice: true}
}

// sendInvoice sends the sender the invoice for premium as on sale now.
// Like reply, it has its own deadline.
func (s *Service) sendInvoice(in incoming) {
	plan := s.plan(in.userID)
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	_, err := s.opts.Payments.SendInvoice(ctx, telegram.Invoice{
		ChatID:      in.msg.Chat.ID,
		Title:       truncate(s.opts.DisplayName+" Premium", maxInvoiceTitle),
		Description: fmt.Sprintf(textPremiumDescription.to(in), plan.days, s.opts.PremiumDailyLimit, s.opts.PrivateDailyLimit),
		Payload:     plan.payload(),
		Label:       fmt.Sprintf(textPremiumLabel.to(in), plan.days),
		Price:       plan.stars,
	})
	if err != nil {
		s.opts.Logger.Error("could not send the premium invoice", "user_id", in.userID, "error", err)
	}
}

// paymentUpdate handles what Telegram says about payments: its check
// before one, and the service messages that one was made or given back.
// They belong to no conversation, and are handled even where private chats
// are closed: the money moved either way.
func (s *Service) paymentUpdate(update telegram.Update) bool {
	switch msg := update.Message; {
	case update.PreCheckoutQuery != nil:
		s.checkout(update.PreCheckoutQuery)
	case msg != nil && msg.SuccessfulPayment != nil:
		s.recordPayment(msg)
	case msg != nil && msg.RefundedPayment != nil:
		s.noteRefund(msg)
	default:
		return false
	}
	return true
}

// checkout answers Telegram's check before it takes a payment: yes for the
// plan on sale now, paid by the user it was made out to, in Stars, and no,
// with the reason, for anything else, such as an invoice from before the
// price changed or one forwarded from someone else.
func (s *Service) checkout(q *telegram.PreCheckoutQuery) {
	if s.opts.Payments == nil {
		return
	}
	in := incoming{msg: &telegram.Message{From: q.From}} // for the language
	refusal := s.checkoutRefusal(q)
	ctx, cancel := context.WithTimeout(context.Background(), checkoutTimeout)
	defer cancel()
	if err := s.opts.Payments.AnswerPreCheckoutQuery(ctx, q.ID, refusal == (localText{}), refusal.to(in)); err != nil {
		s.opts.Logger.Error("could not answer a payment's check", "error", err)
		return
	}
	if refusal != (localText{}) {
		s.opts.Logger.Info("payment refused before it was made", "payload", q.InvoicePayload, "reason", refusal.en)
	}
}

// checkoutRefusal is why a payment may not go ahead, or the zero text when
// it may.
func (s *Service) checkoutRefusal(q *telegram.PreCheckoutQuery) localText {
	plan, ok := parsePlan(q.InvoicePayload)
	switch {
	case !s.sellsPremium():
		return textNoPremium
	case !ok || q.From == nil || plan.userID != q.From.ID:
		return textCheckoutNotYours
	case q.Currency != telegram.Stars || q.TotalAmount != plan.stars || plan != s.plan(q.From.ID):
		return textCheckoutChanged
	}
	return localText{}
}

// recordPayment keeps a payment Telegram took and adds the days its invoice
// was for to the payer's premium, then thanks them. A payment that does not
// match its invoice, which Telegram's check should never let through, is
// kept without days, for the owners to refund. Telegram may report a
// payment twice; it is recorded and thanked once.
func (s *Service) recordPayment(msg *telegram.Message) {
	paid := msg.SuccessfulPayment
	if s.opts.Premium == nil || msg.From == nil {
		s.opts.Logger.Error("a payment arrived with nowhere to record it; refund it by hand", "charge_id", paid.TelegramPaymentChargeID)
		return
	}
	in := incoming{msg: msg, scope: storage.Scope{ChatID: msg.Chat.ID, UserID: msg.From.ID}, userID: msg.From.ID}
	plan, ok := parsePlan(paid.InvoicePayload)
	days := plan.days
	if !ok || plan.userID != msg.From.ID || paid.Currency != telegram.Stars || paid.TotalAmount != plan.stars {
		s.opts.Logger.Error("a payment does not match its invoice; kept without premium, for a refund",
			"user_id", in.userID, "charge_id", paid.TelegramPaymentChargeID, "payload", paid.InvoicePayload, "stars", paid.TotalAmount)
		days = 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	until, recorded, err := s.opts.Premium.RecordStarPayment(ctx, storage.StarPayment{
		ChargeID:  paid.TelegramPaymentChargeID,
		UserID:    in.userID,
		FirstName: msg.From.FirstName,
		Username:  msg.From.Username,
		Stars:     paid.TotalAmount,
		Days:      days,
	})
	switch {
	case err != nil:
		s.opts.Logger.Error("could not record a payment; give the premium by hand or refund it",
			"user_id", in.userID, "charge_id", paid.TelegramPaymentChargeID, "stars", paid.TotalAmount, "days", days, "error", err)
		s.reply(in, textPaymentNotSaved.to(in))
		return
	case !recorded:
		return
	}
	s.opts.Logger.Info("premium bought", "user_id", in.userID, "stars", paid.TotalAmount, "days", days)
	if days == 0 {
		s.reply(in, textPaymentNotSaved.to(in))
		return
	}
	s.reply(in, fmt.Sprintf(textPremiumThanks.to(in), until.In(s.opts.Location).Format(premiumDate), s.premiumLimit()))
}

// noteRefund records a payment given back, by an owner's /refund or by
// Telegram, and so ends its premium; the user is told by Telegram.
func (s *Service) noteRefund(msg *telegram.Message) {
	if s.opts.Premium == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	chargeID := msg.RefundedPayment.TelegramPaymentChargeID
	refunded, err := s.opts.Premium.MarkStarRefunded(ctx, chargeID)
	if err != nil {
		s.opts.Logger.Error("could not record a refund", "charge_id", chargeID, "error", err)
		return
	}
	if refunded {
		s.opts.Logger.Info("payment refunded; premium ended", "charge_id", chargeID)
	}
}

// refusePaymentsCommand is the answer to /payments or /refund from someone
// who may not use them, or "" for an owner in a private chat: payments are
// shown nowhere else.
func (s *Service) refusePaymentsCommand(in incoming) string {
	switch {
	case !s.isOwner(in.msg.From) || s.opts.Premium == nil || s.opts.Payments == nil:
		return textUnknown.to(in)
	case in.group:
		return textPaymentsPrivateOnly
	}
	return ""
}

// showPayments answers an owner's /payments: the latest payments, newest
// first.
func (s *Service) showPayments(ctx context.Context, in incoming) string {
	if refusal := s.refusePaymentsCommand(in); refusal != "" {
		return refusal
	}
	payments, err := s.opts.Premium.StarPayments(ctx, paymentsListed)
	if err != nil {
		s.opts.Logger.Error("could not list payments", "error", err)
		return textError.to(in)
	}
	if len(payments) == 0 {
		return textNoPayments
	}
	lines := []string{"So'nggi to'lovlar:"}
	for _, p := range payments {
		line := fmt.Sprintf("• %s — %s (%d): %d ⭐, %d kun", p.PaidAt.In(s.opts.Location).Format(paymentTime), payerName(p), p.UserID, p.Stars, p.Days)
		if !p.RefundedAt.IsZero() {
			line += ", qaytarilgan"
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", "Qaytarish: /refund ID yoki /refund @username — oxirgi to'lovi qaytadi, premiumi tugaydi.")
	return strings.Join(lines, "\n")
}

// payerName is who made a payment, as Telegram named them then.
func payerName(p storage.StarPayment) string {
	return plainLabel(&telegram.User{FirstName: p.FirstName, Username: p.Username})
}

// refund answers an owner's /refund ID or /refund @username: the user's
// latest payment not refunded goes back to them, by Telegram, and their
// premium ends. A payment Telegram already gave back is only recorded.
func (s *Service) refund(ctx context.Context, in incoming) string {
	if refusal := s.refusePaymentsCommand(in); refusal != "" {
		return refusal
	}
	fields := strings.Fields(in.text)
	if len(fields) != 2 {
		return textRefundUsage
	}
	userID, err := strconv.ParseInt(fields[1], 10, 64)
	username := ""
	if err != nil {
		userID, username = 0, strings.TrimPrefix(fields[1], "@")
	}
	p, found, err := s.opts.Premium.LastStarPayment(ctx, userID, username)
	switch {
	case err != nil:
		s.opts.Logger.Error("could not find a payment to refund", "error", err)
		return textError.to(in)
	case !found:
		return fmt.Sprintf(textNothingToRefund, fields[1])
	}
	err = s.opts.Payments.RefundStarPayment(ctx, p.UserID, p.ChargeID)
	var refused *telegram.APIError
	if err != nil && !(errors.As(err, &refused) && strings.Contains(refused.Description, chargeRefunded)) {
		s.opts.Logger.Error("refund failed", "user_id", p.UserID, "charge_id", p.ChargeID, "error", err)
		if refused != nil {
			return fmt.Sprintf(textRefundRefused, refused.Description)
		}
		return textError.to(in)
	}
	if _, err := s.opts.Premium.MarkStarRefunded(ctx, p.ChargeID); err != nil {
		s.opts.Logger.Error("refunded, but could not record it", "user_id", p.UserID, "charge_id", p.ChargeID, "error", err)
		return fmt.Sprintf(textRefundUnrecorded, payerName(p))
	}
	s.opts.Logger.Info("payment refunded by an owner", "owner_id", in.userID, "user_id", p.UserID, "stars", p.Stars)
	return fmt.Sprintf(textRefunded, payerName(p), p.Stars)
}

// paySupport answers /paysupport, which Telegram asks every bot taking
// Stars to have: whom to write to about a payment, the owners.
func (s *Service) paySupport(in incoming) string {
	if len(s.opts.OwnerUsernames) == 0 {
		return textPaySupport.to(in)
	}
	owners := make([]string, len(s.opts.OwnerUsernames))
	for i, owner := range s.opts.OwnerUsernames {
		owners[i] = "@" + owner
	}
	return fmt.Sprintf(textPaySupportOwners.to(in), strings.Join(owners, ", "))
}

// The owners' texts about payments, in Uzbek like /settings.
const (
	textPaymentsPrivateOnly = "To'lovlar faqat shaxsiy chatda ko'rsatiladi."
	textNoPayments          = "Hali to'lov yo'q."
	textRefundUsage         = "Yozilishi: /refund ID yoki /refund @username. To'lovlar: /payments."
	textNothingToRefund     = "%s ning qaytariladigan to'lovi yo'q. To'lovlar: /payments."
	textRefundRefused       = "Telegram qaytarmadi: %s"
	textRefundUnrecorded    = "%s ga Stars qaytarildi, lekin bazaga yozolmadim: premiumi hali tugamagan bo'lishi mumkin."
	textRefunded            = "✅ %s ga %d ⭐ qaytarildi, premiumi tugadi."
)

// The members' texts about premium.
var (
	// textPremiumDescription takes the days, the premium limit and the
	// limit without it; Telegram shows at most 255 characters.
	textPremiumDescription = localText{
		uz: "%d kun davomida shaxsiy chatda kuniga %d ta xabar (odatda %d ta).",
		ru: "%d дней: %d сообщений в день в личном чате (обычно %d).",
		en: "%d days of %d messages a day in private chat (normally %d).",
	}
	// textPremiumLabel takes the days.
	textPremiumLabel = localText{uz: "Premium, %d kun", ru: "Premium, %d дней", en: "Premium, %d days"}
	// textPremiumOffer takes the days, the premium limit and the price.
	textPremiumOffer = localText{
		uz: "⭐ Premium: %d kun davomida kuniga %d ta xabar, %d Stars. To'lash uchun pastdagi tugmani bosing.",
		ru: "⭐ Premium: %d дней по %d сообщений в день за %d Stars. Нажмите кнопку ниже, чтобы оплатить.",
		en: "⭐ Premium: %d days of %d messages a day for %d Stars. Tap the button below to pay.",
	}
	// textPremiumActive takes the end date and the premium limit.
	textPremiumActive = localText{
		uz: "⭐ Premiumingiz %s gacha: kuniga %d ta xabar. Uzaytirsangiz, kunlari qo'shiladi:",
		ru: "⭐ Ваш Premium действует до %s: %d сообщений в день. Если продлить, дни добавятся:",
		en: "⭐ Your Premium lasts until %s: %d messages a day. Extend it and the days add up:",
	}
	// textPremiumThanks takes the end date and the premium limit.
	textPremiumThanks = localText{
		uz: "✅ Rahmat! Premium %s gacha: kuniga %d ta xabar.",
		ru: "✅ Спасибо! Premium до %s: %d сообщений в день.",
		en: "✅ Thank you! Premium until %s: %d messages a day.",
	}
	textNoPremium = localText{
		uz: "Premium hozircha sotilmayapti.",
		ru: "Premium сейчас не продаётся.",
		en: "Premium isn't on sale right now.",
	}
	textPremiumPrivateOnly = localText{
		uz: "Premium shaxsiy chatda olinadi: menga /premium deb yozing.",
		ru: "Premium оформляется в личном чате: напишите мне /premium.",
		en: "Premium is bought in a private chat: send me /premium there.",
	}
	textCheckoutNotYours = localText{
		uz: "Bu to'lov boshqa foydalanuvchi uchun. O'zingiz menga /premium deb yozing.",
		ru: "Этот счёт выписан другому пользователю. Напишите мне /premium сами.",
		en: "This invoice is someone else's. Send me /premium yourself.",
	}
	textCheckoutChanged = localText{
		uz: "Narx yoki muddat o'zgardi. /premium ni qayta yuboring.",
		ru: "Цена или срок изменились. Отправьте /premium ещё раз.",
		en: "The price or the period has changed. Send /premium again.",
	}
	textPaySupport = localText{
		uz: "To'lov bo'yicha savol yoki qaytarish so'rovingizni shu yerga yozing, egalarim ko'rib chiqishadi.",
		ru: "Вопрос об оплате или просьбу о возврате напишите сюда, мои владельцы рассмотрят.",
		en: "Write your question about a payment, or a refund request, here and my owners will look into it.",
	}
	// textPaySupportOwners takes the owners' @usernames.
	textPaySupportOwners = localText{
		uz: "To'lov bo'yicha savol yoki qaytarish uchun egalarimga yozing: %s.",
		ru: "По вопросам оплаты и возврата напишите моим владельцам: %s.",
		en: "For a question about a payment, or a refund, write to my owners: %s.",
	}
	textPaymentNotSaved = localText{
		uz: "To'lovingiz keldi, lekin uni hozir yoza olmadim. Egalarim tekshirib, hal qilib berishadi.",
		ru: "Оплата пришла, но я не смогла её записать. Мои владельцы проверят и всё исправят.",
		en: "Your payment arrived, but I couldn't record it just now. My owners will check and sort it out.",
	}
)
