package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// fakePayments records the invoices sent, the checks answered and the
// refunds asked of Telegram.
type fakePayments struct {
	mu        sync.Mutex
	invoices  []telegram.Invoice
	answers   []checkoutAnswer
	refunds   []string
	refundErr error
}

type checkoutAnswer struct {
	id      string
	ok      bool
	refusal string
}

func (f *fakePayments) SendInvoice(_ context.Context, inv telegram.Invoice) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invoices = append(f.invoices, inv)
	return 77, nil
}

func (f *fakePayments) AnswerPreCheckoutQuery(_ context.Context, id string, ok bool, refusal string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers = append(f.answers, checkoutAnswer{id: id, ok: ok, refusal: refusal})
	return nil
}

func (f *fakePayments) RefundStarPayment(_ context.Context, userID int64, chargeID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refunds = append(f.refunds, fmt.Sprintf("%d:%s", userID, chargeID))
	return f.refundErr
}

// fakePremium keeps payments in memory, as the store does: a payment
// counts once, and a refund ends the premium.
type fakePremium struct {
	mu       sync.Mutex
	payments []storage.StarPayment
	until    map[int64]time.Time
	now      time.Time
	err      error
}

func newFakePremium(now time.Time) *fakePremium {
	return &fakePremium{until: map[int64]time.Time{}, now: now}
}

func (f *fakePremium) RecordStarPayment(_ context.Context, p storage.StarPayment) (time.Time, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return time.Time{}, false, f.err
	}
	for _, kept := range f.payments {
		if kept.ChargeID == p.ChargeID {
			return f.until[p.UserID], false, nil
		}
	}
	p.PaidAt = f.now
	f.payments = append(f.payments, p)
	if p.Days > 0 {
		from := f.now
		if f.until[p.UserID].After(from) {
			from = f.until[p.UserID]
		}
		f.until[p.UserID] = from.AddDate(0, 0, p.Days)
	}
	return f.until[p.UserID], true, nil
}

func (f *fakePremium) PremiumUntil(_ context.Context, userID int64) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.until[userID], f.err
}

func (f *fakePremium) LastStarPayment(_ context.Context, userID int64, username string) (storage.StarPayment, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.payments) - 1; i >= 0; i-- {
		p := f.payments[i]
		if p.RefundedAt.IsZero() && (p.UserID == userID || (username != "" && strings.EqualFold(p.Username, username))) {
			return p, true, nil
		}
	}
	return storage.StarPayment{}, false, nil
}

func (f *fakePremium) MarkStarRefunded(_ context.Context, chargeID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, p := range f.payments {
		if p.ChargeID == chargeID && p.RefundedAt.IsZero() {
			f.payments[i].RefundedAt = f.now
			if f.until[p.UserID].After(f.now) {
				f.until[p.UserID] = f.now
			}
			return true, nil
		}
	}
	return false, nil
}

func (f *fakePremium) StarPayments(_ context.Context, limit int) ([]storage.StarPayment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var newest []storage.StarPayment
	for i := len(f.payments) - 1; i >= 0 && len(newest) < limit; i-- {
		newest = append(newest, f.payments[i])
	}
	return newest, nil
}

// premiumNow is the clock of the premium tests: noon in Tashkent.
var premiumNow = time.Date(2026, 10, 29, 7, 0, 0, 0, time.UTC)

// withPremium sells premium, 100 Stars for 30 days of 5 answers a day,
// over a private limit of 1.
func withPremium(f *fixture) (*fakePrivateQuota, *fakePayments, *fakePremium) {
	quota := withPrivateLimit(f, 1)
	payments, book := &fakePayments{}, newFakePremium(premiumNow)
	f.opts.Now = func() time.Time { return premiumNow }
	f.opts.Payments, f.opts.Premium = payments, book
	f.opts.PremiumPrice, f.opts.PremiumDays, f.opts.PremiumDailyLimit = 100, 30, 5
	return quota, payments, book
}

// The day's one notice past the limit offers premium, with its invoice made
// out to the sender; later messages that day get nothing more.
func TestLimitNoticeOffersPremiumOnce(t *testing.T) {
	f := newFixture()
	quota, payments, _ := withPremium(f)

	sent := f.run(textFrom(userID, "hi"), textFrom(userID, "again"), textFrom(userID, "and again"))

	if len(sent) != 2 || !strings.HasPrefix(sent[1].text, fmt.Sprintf(textDailyLimit.uz, 1)) || !strings.Contains(sent[1].text, "100 Stars") {
		t.Fatalf("sent = %+v, want the answer, then the notice with the offer", sent)
	}
	if len(payments.invoices) != 1 {
		t.Fatalf("invoices = %+v, want one", payments.invoices)
	}
	inv := payments.invoices[0]
	if inv.ChatID != userID || inv.Payload != "premium:2:30:100" || inv.Price != 100 || inv.Title != "Mira Premium" || !strings.Contains(inv.Description, "30 kun") {
		t.Errorf("invoice = %+v", inv)
	}
	if asked, _ := quota.recorded(); asked[0].PremiumLimit != 5 {
		t.Errorf("quota asked = %+v, want the premium limit 5 with it", asked[0])
	}
}

// After the day's notice, a sender past the limit is pointed to /premium
// at most once an hour: one who missed the notice saw only a reaction.
func TestPastTheLimitPremiumIsRemindedHourly(t *testing.T) {
	f := newFixture()
	withPremium(f)
	now := premiumNow
	f.opts.Now = func() time.Time { return now }
	s := serverless(f, &fakeMuteStore{})
	defer s.Close()
	say := func(text string) {
		s.HandleNow(textFrom(userID, text))
	}

	say("hi")
	say("again")
	say("still")
	now = now.Add(premiumNudgeEvery + time.Minute)
	say("later")
	say("and later")

	sent := f.sender.messages()
	if len(sent) != 3 || sent[2].text != textPremiumNudge.uz {
		t.Errorf("sent = %+v; want the answer, the notice, then one reminder an hour on", sent)
	}
}

// Without premium on sale there is nothing to point to.
func TestNoReminderWithoutPremium(t *testing.T) {
	f := newFixture()
	withPremium(f)
	f.opts.PremiumPrice = 0
	now := premiumNow
	f.opts.Now = func() time.Time { return now }
	s := serverless(f, &fakeMuteStore{})
	defer s.Close()

	s.HandleNow(textFrom(userID, "hi"))
	s.HandleNow(textFrom(userID, "again"))
	now = now.Add(2 * premiumNudgeEvery)
	s.HandleNow(textFrom(userID, "later"))

	if sent := f.sender.messages(); len(sent) != 2 {
		t.Errorf("sent = %+v; want the answer and the plain notice only", sent)
	}
}

// Without a price nothing is sold: the notice is the plain one, and
// /premium says so.
func TestNoPremiumWithoutAPrice(t *testing.T) {
	f := newFixture()
	_, payments, _ := withPremium(f)
	f.opts.PremiumPrice = 0

	sent := f.run(textFrom(userID, "hi"), textFrom(userID, "again"), textFrom(userID, "/premium"))

	if len(sent) != 3 || sent[1].text != fmt.Sprintf(textDailyLimit.uz, 1) || sent[2].text != textNoPremium.uz || len(payments.invoices) != 0 {
		t.Errorf("sent = %+v, invoices %+v; want the plain notice, no premium and no invoice", sent, payments.invoices)
	}
}

// Someone with premium has its limit, and past it the notice names it and
// offers nothing.
func TestPremiumUserGetsThePremiumLimit(t *testing.T) {
	f := newFixture()
	quota, payments, book := withPremium(f)
	quota.premium = true
	book.until[userID] = premiumNow.Add(48 * time.Hour)
	var updates []telegram.Update
	for range 7 {
		updates = append(updates, textFrom(userID, "hi"))
	}

	sent := f.run(updates...)

	if calls := len(f.replier.recorded()); calls != 5 {
		t.Errorf("model calls = %d, want the premium limit of 5", calls)
	}
	if len(sent) != 6 || sent[5].text != fmt.Sprintf(textDailyLimit.uz, 5) || len(payments.invoices) != 0 {
		t.Errorf("sent = %+v, invoices %d; want five answers and the premium limit's notice alone", sent, len(payments.invoices))
	}
}

// /premium offers it with the invoice, or says until when the sender has
// it, with the invoice to extend it; in a group it points to the private
// chat.
func TestPremiumCommand(t *testing.T) {
	t.Run("offer", func(t *testing.T) {
		f := newFixture()
		_, payments, _ := withPremium(f)
		sent := f.run(textFrom(userID, "/premium"))
		if len(sent) != 1 || !strings.Contains(sent[0].text, "100 Stars") || len(payments.invoices) != 1 {
			t.Errorf("sent = %+v, invoices %d", sent, len(payments.invoices))
		}
		if len(f.replier.recorded()) != 0 {
			t.Error("/premium reached the model")
		}
	})
	t.Run("extend", func(t *testing.T) {
		f := newFixture()
		_, payments, book := withPremium(f)
		book.until[userID] = time.Date(2026, 11, 20, 12, 0, 0, 0, tashkent)
		sent := f.run(textFrom(userID, "/premium"))
		if len(sent) != 1 || !strings.Contains(sent[0].text, "20.11.2026") || len(payments.invoices) != 1 {
			t.Errorf("sent = %+v, invoices %d", sent, len(payments.invoices))
		}
	})
	t.Run("group", func(t *testing.T) {
		f := newFixture()
		_, payments, _ := withPremium(f)
		sent := f.run(inGroup(groupMessage("/premium@MyBot", "@MyBot")))
		if len(sent) != 1 || sent[0].text != textPremiumPrivateOnly.uz || len(payments.invoices) != 0 {
			t.Errorf("sent = %+v, invoices %d", sent, len(payments.invoices))
		}
	})
}

func checkoutUpdate(from int64, payload string, stars int) telegram.Update {
	nextUpdateID++
	return telegram.Update{UpdateID: nextUpdateID, PreCheckoutQuery: &telegram.PreCheckoutQuery{
		ID: "q1", From: &telegram.User{ID: from}, Currency: telegram.Stars, TotalAmount: stars, InvoicePayload: payload,
	}}
}

// Telegram's check is yes only for the plan on sale, paid by the user it
// was made out to; anything else is refused with the reason.
func TestCheckoutValidatesThePayload(t *testing.T) {
	cases := map[string]struct {
		update  telegram.Update
		price   int
		refusal string
	}{
		"the plan on sale":      {checkoutUpdate(userID, "premium:2:30:100", 100), 100, ""},
		"someone else's":        {checkoutUpdate(9, "premium:2:30:100", 100), 100, textCheckoutNotYours.uz},
		"not ours":              {checkoutUpdate(userID, "donate", 100), 100, textCheckoutNotYours.uz},
		"from before the price": {checkoutUpdate(userID, "premium:2:30:80", 80), 100, textCheckoutChanged.uz},
		"no longer sold":        {checkoutUpdate(userID, "premium:2:30:100", 100), 0, textNoPremium.uz},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			_, payments, _ := withPremium(f)
			f.opts.PremiumPrice = c.price

			// The webhook's path, which must answer before it returns.
			New(f.opts).HandleNow(c.update)

			if len(payments.answers) != 1 {
				t.Fatalf("answers = %+v, want one", payments.answers)
			}
			got := payments.answers[0]
			if got.id != "q1" || got.ok != (c.refusal == "") || got.refusal != c.refusal {
				t.Errorf("answer = %+v, want refusal %q", got, c.refusal)
			}
		})
	}
}

func paymentUpdate(from int64, chargeID, payload string, stars int) telegram.Update {
	nextUpdateID++
	return telegram.Update{UpdateID: nextUpdateID, Message: &telegram.Message{
		MessageID: 60,
		From:      &telegram.User{ID: from, FirstName: "Ann", Username: "ann"},
		Chat:      telegram.Chat{ID: from, Type: telegram.ChatTypePrivate},
		Service:   "successful_payment",
		SuccessfulPayment: &telegram.SuccessfulPayment{
			Currency: telegram.Stars, TotalAmount: stars, InvoicePayload: payload, TelegramPaymentChargeID: chargeID,
		},
	}}
}

// A payment is recorded once, with the payer and the plan's days, and
// thanked once; it is handled even where private chats are closed.
func TestPaymentIsRecordedOnceAndThanked(t *testing.T) {
	f := newFixture()
	_, _, book := withPremium(f)
	f.opts.PrivateChats = "off"
	payment := paymentUpdate(userID, "ch_1", "premium:2:30:100", 100)

	sent := f.run(payment, payment)

	if len(book.payments) != 1 {
		t.Fatalf("payments = %+v, want one", book.payments)
	}
	p := book.payments[0]
	if p.ChargeID != "ch_1" || p.UserID != userID || p.Username != "ann" || p.Stars != 100 || p.Days != 30 {
		t.Errorf("payment = %+v", p)
	}
	until := premiumNow.AddDate(0, 0, 30).In(tashkent).Format(premiumDate)
	if len(sent) != 1 || sent[0].text != fmt.Sprintf(textPremiumThanks.uz, until, 5) {
		t.Errorf("sent = %+v, want one thanks", sent)
	}
	if len(f.replier.recorded()) != 0 {
		t.Error("a payment reached the model")
	}
}

// A payment that does not match its invoice is kept with no days, for a
// refund; one that cannot be stored is logged and the payer told.
func TestPaymentsThatGoWrong(t *testing.T) {
	t.Run("mismatch", func(t *testing.T) {
		f := newFixture()
		_, _, book := withPremium(f)
		sent := f.run(paymentUpdate(userID, "ch_1", "premium:9:30:100", 100))
		if len(book.payments) != 1 || book.payments[0].Days != 0 || !book.until[userID].IsZero() {
			t.Errorf("payments = %+v, until %v; want kept without premium", book.payments, book.until[userID])
		}
		if len(sent) != 1 || sent[0].text != textPaymentNotSaved.uz {
			t.Errorf("sent = %+v", sent)
		}
	})
	t.Run("store down", func(t *testing.T) {
		f := newFixture()
		_, _, book := withPremium(f)
		book.err = errors.New("database down")
		sent := f.run(paymentUpdate(userID, "ch_1", "premium:2:30:100", 100))
		if len(sent) != 1 || sent[0].text != textPaymentNotSaved.uz {
			t.Errorf("sent = %+v", sent)
		}
	})
}

// A refund Telegram reports, by an owner's /refund or its own, ends the
// premium.
func TestRefundedPaymentEndsPremium(t *testing.T) {
	f := newFixture()
	_, _, book := withPremium(f)
	refund := paymentUpdate(userID, "ch_1", "premium:2:30:100", 100)
	refund.UpdateID++
	refund.Message = &telegram.Message{
		From: refund.Message.From, Chat: refund.Message.Chat, Service: "refunded_payment",
		RefundedPayment: &telegram.RefundedPayment{Currency: telegram.Stars, TotalAmount: 100, TelegramPaymentChargeID: "ch_1"},
	}

	sent := f.run(paymentUpdate(userID, "ch_1", "premium:2:30:100", 100), refund)

	if book.payments[0].RefundedAt.IsZero() || book.until[userID].After(premiumNow) {
		t.Errorf("payment = %+v, until %v; want refunded and ended", book.payments[0], book.until[userID])
	}
	if len(sent) != 1 {
		t.Errorf("sent = %+v, want only the thanks: Telegram tells of the refund", sent)
	}
}

const premiumOwner = int64(9001)

func ownerWrites(text string) telegram.Update {
	update := textFrom(premiumOwner, text)
	update.Message.From.Username = ownerUsername
	return update
}

// Owners list the payments and refund one by ID or @username, which ends
// that premium; nobody else may, and not in a group.
func TestOwnersListAndRefundPayments(t *testing.T) {
	f := newFixture()
	_, payments, book := withPremium(f)
	f.opts.OwnerIDs = map[int64]bool{premiumOwner: true}
	paid := paymentUpdate(userID, "ch_1", "premium:2:30:100", 100)
	group := groupMessage("/payments@MyBot", "@MyBot")
	group.From = &telegram.User{ID: premiumOwner, Username: ownerUsername}

	// Each chat's updates are in order; the group's are run on their own.
	f.run(paid, textFrom(userID, "/payments"))
	f.run(ownerWrites("/payments"), ownerWrites("/refund @Ann"), ownerWrites("/refund @ann"), ownerWrites("/refund"))
	sent := f.run(inGroup(group))

	if len(sent) != 7 {
		t.Fatalf("sent = %+v", sent)
	}
	if sent[1].text != textUnknown.uz {
		t.Errorf("a member's /payments = %q, want the unknown command", sent[1].text)
	}
	if list := sent[2].text; !strings.Contains(list, "Ann @ann (2): 100 ⭐, 30 kun") || strings.Contains(list, "qaytarilgan") {
		t.Errorf("list = %q", list)
	}
	if sent[3].text != fmt.Sprintf(textRefunded, "Ann @ann", 100) || len(payments.refunds) != 1 || payments.refunds[0] != "2:ch_1" {
		t.Errorf("refund = %q, asked %v", sent[3].text, payments.refunds)
	}
	if book.until[userID].After(premiumNow) {
		t.Errorf("premium lasts until %v after the refund", book.until[userID])
	}
	if sent[4].text != fmt.Sprintf(textNothingToRefund, "@ann") || sent[5].text != textRefundUsage {
		t.Errorf("again = %q, bare = %q", sent[4].text, sent[5].text)
	}
	if sent[6].text != textPaymentsPrivateOnly {
		t.Errorf("in a group = %q", sent[6].text)
	}
}

// A payment Telegram gave back already is only recorded; one Telegram will
// not give back is left as it was, with Telegram's reason.
func TestRefundTelegramRefuses(t *testing.T) {
	cases := map[string]struct {
		err      error
		refunded bool
	}{
		"already refunded": {&telegram.APIError{Code: 400, Description: "Bad Request: CHARGE_ALREADY_REFUNDED"}, true},
		"refused":          {&telegram.APIError{Code: 400, Description: "Bad Request: CHARGE_NOT_FOUND"}, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			_, payments, book := withPremium(f)
			f.opts.OwnerIDs = map[int64]bool{premiumOwner: true}
			payments.refundErr = c.err

			sent := f.run(paymentUpdate(userID, "ch_1", "premium:2:30:100", 100), ownerWrites("/refund 2"))

			if refunded := !book.payments[0].RefundedAt.IsZero(); refunded != c.refunded {
				t.Errorf("refunded = %v, want %v; sent %+v", refunded, c.refunded, sent)
			}
			if !c.refunded && !strings.Contains(sent[1].text, "CHARGE_NOT_FOUND") {
				t.Errorf("reply = %q, want Telegram's reason", sent[1].text)
			}
		})
	}
}

func TestParsePlan(t *testing.T) {
	plan := premiumPlan{userID: 42, days: 30, stars: 100}
	if got, ok := parsePlan(plan.payload()); !ok || got != plan {
		t.Errorf("round trip = %+v, %v", got, ok)
	}
	for _, bad := range []string{"", "premium:42:30", "premium:x:30:100", "premium:42:0:100", "premium:42:30:-5", "gift:42:30:100"} {
		if _, ok := parsePlan(bad); ok {
			t.Errorf("parsePlan(%q) accepted", bad)
		}
	}
}

// /paysupport, which Telegram asks of bots that take Stars, names the
// owners.
func TestPaySupportNamesTheOwners(t *testing.T) {
	f := newFixture()
	f.opts.OwnerUsernames = []string{"pronium", "tursunoffm"}
	sent := f.run(textFrom(userID, "/paysupport"))
	expectOneReply(t, sent, "@pronium, @tursunoffm")
}
