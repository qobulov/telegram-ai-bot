package telegram

import "context"

// Stars is the currency of payments in Telegram Stars, the only one the bot
// takes: digital goods sold in a bot must be paid in Stars, which needs no
// payment provider.
const Stars = "XTR"

// PreCheckoutQuery is a user about to pay one of the bot's invoices.
// Telegram takes the payment only when the bot answers yes within 10
// seconds.
type PreCheckoutQuery struct {
	ID   string `json:"id"`
	From *User  `json:"from"`
	// Currency is Stars for the bot's invoices, and TotalAmount the price
	// in them.
	Currency    string `json:"currency"`
	TotalAmount int    `json:"total_amount"`
	// InvoicePayload is the payload the bot put on the invoice.
	InvoicePayload string `json:"invoice_payload"`
}

// SuccessfulPayment is a payment Telegram took for one of the bot's
// invoices. TelegramPaymentChargeID names it, for a refund too.
type SuccessfulPayment struct {
	Currency                string `json:"currency"`
	TotalAmount             int    `json:"total_amount"`
	InvoicePayload          string `json:"invoice_payload"`
	TelegramPaymentChargeID string `json:"telegram_payment_charge_id"`
}

// RefundedPayment is a payment given back to the user who made it, by the
// bot or by Telegram.
type RefundedPayment struct {
	Currency                string `json:"currency"`
	TotalAmount             int    `json:"total_amount"`
	InvoicePayload          string `json:"invoice_payload"`
	TelegramPaymentChargeID string `json:"telegram_payment_charge_id"`
}

// Invoice is a bill for one thing, priced in Stars, sent as a message with
// a button to pay it.
type Invoice struct {
	ChatID int64
	// Title is at most 32 characters, Description at most 255.
	Title       string
	Description string
	// Payload comes back with the pre-checkout query and the payment, at
	// most 128 bytes; users never see it.
	Payload string
	// Label names the price; Price is it, in Stars.
	Label string
	Price int
}

type labeledPrice struct {
	Label  string `json:"label"`
	Amount int    `json:"amount"`
}

type sendInvoiceParams struct {
	ChatID      int64  `json:"chat_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Payload     string `json:"payload"`
	// ProviderToken is empty: Stars need no payment provider.
	ProviderToken string         `json:"provider_token"`
	Currency      string         `json:"currency"`
	Prices        []labeledPrice `json:"prices"`
}

// SendInvoice sends an invoice in Stars and returns its message's ID.
func (c *Client) SendInvoice(ctx context.Context, inv Invoice) (int64, error) {
	params := sendInvoiceParams{
		ChatID:      inv.ChatID,
		Title:       inv.Title,
		Description: inv.Description,
		Payload:     inv.Payload,
		Currency:    Stars,
		Prices:      []labeledPrice{{Label: inv.Label, Amount: inv.Price}},
	}
	var sent sentMessage
	if err := c.call(ctx, "sendInvoice", params, &sent); err != nil {
		return 0, err
	}
	return sent.MessageID, nil
}

type answerPreCheckoutParams struct {
	PreCheckoutQueryID string `json:"pre_checkout_query_id"`
	OK                 bool   `json:"ok"`
	ErrorMessage       string `json:"error_message,omitempty"`
}

// AnswerPreCheckoutQuery lets a payment go ahead, or with ok false stops it
// and shows the user refusal.
func (c *Client) AnswerPreCheckoutQuery(ctx context.Context, id string, ok bool, refusal string) error {
	params := answerPreCheckoutParams{PreCheckoutQueryID: id, OK: ok}
	if !ok {
		params.ErrorMessage = refusal
	}
	return c.call(ctx, "answerPreCheckoutQuery", params, nil)
}

type refundParams struct {
	UserID   int64  `json:"user_id"`
	ChargeID string `json:"telegram_payment_charge_id"`
}

// RefundStarPayment gives a payment in Stars back to the user who made it.
func (c *Client) RefundStarPayment(ctx context.Context, userID int64, chargeID string) error {
	return c.call(ctx, "refundStarPayment", refundParams{UserID: userID, ChargeID: chargeID}, nil)
}
