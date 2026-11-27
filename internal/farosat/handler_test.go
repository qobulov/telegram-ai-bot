package farosat

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/telegram"
)

const testToken = "123:test-token"

func signedInit(user int64, at time.Time) string {
	values := url.Values{"user": {fmt.Sprintf(`{"id":%d,"first_name":"Bek"}`, user)}, "auth_date": {fmt.Sprint(at.Unix())}}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + values.Get(k)
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(testToken))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	values.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return values.Encode()
}

type fakeStore struct{ grams map[int64]int }

func (f fakeStore) FarosatOf(_ context.Context, chatID, userID int64) (int, int, error) {
	if chatID != -100123 {
		return 0, 1, nil
	}
	return f.grams[userID], 2, nil
}

type fakeInvoices struct{ got telegram.Invoice }

func (f *fakeInvoices) CreateInvoiceLink(_ context.Context, inv telegram.Invoice) (string, error) {
	f.got = inv
	return "https://t.me/$abc", nil
}

func call(t *testing.T, h http.Handler, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/farosat", strings.NewReader(string(raw))))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// The page gets the member's farosat in the group and the galaxies it has
// opened; a pack's invoice is made out to the member for that group.
func TestFarosatApp(t *testing.T) {
	now := time.Now()
	invoices := &fakeInvoices{}
	h := NewHandler(Options{BotToken: testToken, Store: fakeStore{grams: map[int64]int{7: 60}}, Invoices: invoices, Now: func() time.Time { return now }})
	init := signedInit(7, now)

	code, out := call(t, h, map[string]any{"init_data": init, "group": "g100123", "action": "state"})
	if code != http.StatusOK || out["grams"] != float64(60) || out["place"] != float64(2) || out["open"] != float64(4) {
		t.Fatalf("state = %d %v", code, out)
	}
	galaxies := out["galaxies"].([]any)
	if len(galaxies) != len(Galaxies) || galaxies[3].(map[string]any)["open"] != true || galaxies[4].(map[string]any)["open"] != false {
		t.Errorf("galaxies = %v", galaxies[:5])
	}
	code, out = call(t, h, map[string]any{"init_data": init, "group": "g100123", "action": "invoice", "pack": "g100"})
	if code != http.StatusOK || out["link"] != "https://t.me/$abc" || invoices.got.Price != 100 || invoices.got.Payload != "frs:7:-100123:g100" {
		t.Fatalf("invoice = %d %v %+v", code, out, invoices.got)
	}
	if code, _ = call(t, h, map[string]any{"init_data": init, "action": "state"}); code != http.StatusBadRequest {
		t.Errorf("without a group = %d", code)
	}
	if code, _ = call(t, h, map[string]any{"init_data": init + "x", "group": "g100123", "action": "state"}); code != http.StatusUnauthorized {
		t.Errorf("forged = %d", code)
	}
}

// A payload names who pays, for which group and pack; anything else is not
// a farosat payment.
func TestFarosatPayload(t *testing.T) {
	pack, _ := PackByID("g250")
	user, chat, got, ok := ParsePayload(Payload(7, -100123, pack))
	if !ok || user != 7 || chat != -100123 || got.Stars != 250 || got.Grams() != 250 {
		t.Fatalf("parsed = %d %d %+v %v", user, chat, got, ok)
	}
	for _, bad := range []string{"crystal:7:p1", "frs:7:100123:g250", "frs:7:-100123:g7", "frs:x:-1:g50", "frs:7:-1"} {
		if _, _, _, ok := ParsePayload(bad); ok {
			t.Errorf("%q read as a farosat payment", bad)
		}
	}
	for i := 1; i < len(Galaxies); i++ {
		if Galaxies[i].Grams <= Galaxies[i-1].Grams || Galaxies[i].ID != i+1 {
			t.Errorf("galaxy %d out of order", i+1)
		}
	}
}
