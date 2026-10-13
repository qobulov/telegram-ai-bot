package webhook

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

const secret = "test-secret_123"

const updateJSON = `{"update_id":77,"message":{"message_id":5,"from":{"id":42,"first_name":"Bek"},
	"chat":{"id":42,"type":"private"},"text":"salom"}}`

type recorder struct {
	updates []telegram.Update
}

func (r *recorder) handle(update telegram.Update) {
	r.updates = append(r.updates, update)
}

func newHandler(t *testing.T) (http.Handler, *recorder) {
	t.Helper()
	rec := &recorder{}
	handler, err := NewHandler(Options{Secret: secret, Handle: rec.handle})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return handler, rec
}

func post(handler http.Handler, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/webhook", strings.NewReader(body))
	if token != "" {
		req.Header.Set(SecretHeader, token)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func TestUpdateWithTheSecretIsHandled(t *testing.T) {
	handler, rec := newHandler(t)

	resp := post(handler, updateJSON, secret)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}
	if len(rec.updates) != 1 {
		t.Fatalf("handled updates = %d, want 1", len(rec.updates))
	}
	got := rec.updates[0]
	if got.UpdateID != 77 || got.Message == nil || got.Message.Text != "salom" || got.Message.From.ID != 42 {
		t.Errorf("update = %+v", got)
	}
}

func TestRequestsWithoutTheSecretAreRejected(t *testing.T) {
	for name, token := range map[string]string{
		"no header":       "",
		"wrong secret":    "not-the-secret",
		"prefix only":     secret[:5],
		"secret and more": secret + "x",
	} {
		t.Run(name, func(t *testing.T) {
			handler, rec := newHandler(t)

			resp := post(handler, updateJSON, token)

			if resp.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", resp.Code)
			}
			if len(rec.updates) != 0 {
				t.Error("an update without the right secret was handled; anyone could forge messages from any user")
			}
			if strings.Contains(resp.Body.String(), secret) {
				t.Error("the response reveals the secret")
			}
		})
	}
}

func TestMalformedBodyIsRejectedWithoutCallingTheBot(t *testing.T) {
	for name, body := range map[string]string{"not JSON": "<html>", "empty": "", "wrong type": `{"update_id":"x"}`} {
		t.Run(name, func(t *testing.T) {
			handler, rec := newHandler(t)

			resp := post(handler, body, secret)

			if resp.Code != http.StatusBadRequest || len(rec.updates) != 0 {
				t.Errorf("status = %d, handled = %d; want 400 and nothing handled", resp.Code, len(rec.updates))
			}
		})
	}
}

func TestOversizedBodyIsRejected(t *testing.T) {
	handler, rec := newHandler(t)
	huge := `{"update_id":1,"message":{"text":"` + strings.Repeat("a", maxBodyBytes) + `"}}`

	resp := post(handler, huge, secret)

	if resp.Code != http.StatusBadRequest || len(rec.updates) != 0 {
		t.Errorf("status = %d, handled = %d; want 400 and nothing handled", resp.Code, len(rec.updates))
	}
}

func TestGetIsAHealthCheck(t *testing.T) {
	handler, rec := newHandler(t)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/webhook", nil))

	if resp.Code != http.StatusOK || len(rec.updates) != 0 {
		t.Errorf("status = %d, handled = %d", resp.Code, len(rec.updates))
	}
}

func TestOtherMethodsAreRefused(t *testing.T) {
	handler, _ := newHandler(t)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, httptest.NewRequest(http.MethodPut, "/api/webhook", strings.NewReader(updateJSON)))

	if resp.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.Code)
	}
}

func TestHandlerNeedsASecretAndAHandler(t *testing.T) {
	if _, err := NewHandler(Options{Handle: func(telegram.Update) {}}); err == nil {
		t.Error("a handler without a secret was created; it would accept forged updates")
	}
	if _, err := NewHandler(Options{Secret: secret}); err == nil {
		t.Error("a handler without a Handle function was created")
	}
}

func TestPanicInTheBotBecomesA200(t *testing.T) {
	handler, err := NewHandler(Options{Secret: secret, Handle: func(telegram.Update) { panic("boom") }})
	if err != nil {
		t.Fatal(err)
	}

	resp := post(handler, updateJSON, secret)

	// Anything but 2xx makes Telegram send the same update again and again.
	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.Code)
	}
}
