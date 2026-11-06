package telegram

import (
	"context"
	"reflect"
	"testing"
)

func TestPaymentMethodsSendExactParams(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		method string
		call   func(*Client) error
		want   map[string]any
	}{
		{
			// Stars: the currency XTR, one price, and an empty provider token.
			name:   "invoice",
			method: "sendInvoice",
			call: func(c *Client) error {
				_, err := c.SendInvoice(ctx, Invoice{ChatID: 42, Title: "Maria Premium", Description: "30 kun", Payload: "premium:42:30:100", Label: "30 kun", Price: 100})
				return err
			},
			want: map[string]any{
				"chat_id": float64(42), "title": "Maria Premium", "description": "30 kun", "payload": "premium:42:30:100",
				"provider_token": "", "currency": "XTR",
				"prices": []any{map[string]any{"label": "30 kun", "amount": float64(100)}},
			},
		},
		{
			name:   "pre-checkout yes",
			method: "answerPreCheckoutQuery",
			call:   func(c *Client) error { return c.AnswerPreCheckoutQuery(ctx, "q1", true, "ignored") },
			want:   map[string]any{"pre_checkout_query_id": "q1", "ok": true},
		},
		{
			name:   "pre-checkout no",
			method: "answerPreCheckoutQuery",
			call:   func(c *Client) error { return c.AnswerPreCheckoutQuery(ctx, "q1", false, "Narx o'zgardi") },
			want:   map[string]any{"pre_checkout_query_id": "q1", "ok": false, "error_message": "Narx o'zgardi"},
		},
		{
			name:   "refund",
			method: "refundStarPayment",
			call:   func(c *Client) error { return c.RefundStarPayment(ctx, 42, "ch_1") },
			want:   map[string]any{"user_id": float64(42), "telegram_payment_charge_id": "ch_1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, api := newTestClient(t, okSent)

			if err := tt.call(client); err != nil {
				t.Fatalf("%s: %v", tt.method, err)
			}
			sent := api.recorded()[0]
			if sent.method != tt.method || !reflect.DeepEqual(sent.params, tt.want) {
				t.Errorf("call = %+v, want %s %v", sent, tt.method, tt.want)
			}
		})
	}
}

// A payment's check and its service messages arrive as their own fields;
// a paid or refunded message is still a service message.
func TestGetUpdatesParsesPayments(t *testing.T) {
	client, _ := newTestClient(t, `{"ok":true,"result":[
		{"update_id":1,"pre_checkout_query":{"id":"q1","from":{"id":42,"first_name":"Ann"},"currency":"XTR","total_amount":100,"invoice_payload":"premium:42:30:100"}},
		{"update_id":2,"message":{"message_id":5,"from":{"id":42},"chat":{"id":42,"type":"private"},
			"successful_payment":{"currency":"XTR","total_amount":100,"invoice_payload":"premium:42:30:100","telegram_payment_charge_id":"ch_1","provider_payment_charge_id":""}}},
		{"update_id":3,"message":{"message_id":6,"from":{"id":42},"chat":{"id":42,"type":"private"},
			"refunded_payment":{"currency":"XTR","total_amount":100,"invoice_payload":"premium:42:30:100","telegram_payment_charge_id":"ch_1"}}}]}`)

	updates, err := client.GetUpdates(context.Background(), 0, 0)
	if err != nil || len(updates) != 3 {
		t.Fatalf("updates = %+v, %v", updates, err)
	}
	q := updates[0].PreCheckoutQuery
	if q == nil || q.ID != "q1" || q.From.ID != 42 || q.Currency != Stars || q.TotalAmount != 100 || q.InvoicePayload != "premium:42:30:100" {
		t.Errorf("pre-checkout = %+v", q)
	}
	paid := updates[1].Message
	if p := paid.SuccessfulPayment; p == nil || p.TelegramPaymentChargeID != "ch_1" || p.TotalAmount != 100 || !paid.IsService() {
		t.Errorf("payment = %+v, service %v", p, paid.IsService())
	}
	refunded := updates[2].Message
	if r := refunded.RefundedPayment; r == nil || r.TelegramPaymentChargeID != "ch_1" || !refunded.IsService() {
		t.Errorf("refund = %+v, service %v", r, refunded.IsService())
	}
}
