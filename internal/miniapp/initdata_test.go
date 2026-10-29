package miniapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testToken = "123456:TEST-token"

var testNow = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

// signed is init data as Telegram would sign it with token, for fields.
func signed(token string, fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	values := url.Values{}
	for _, k := range keys {
		lines = append(lines, k+"="+fields[k])
		values.Set(k, fields[k])
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	h := hmac.New(sha256.New, secret.Sum(nil))
	h.Write([]byte(strings.Join(lines, "\n")))
	values.Set("hash", hex.EncodeToString(h.Sum(nil)))
	return values.Encode()
}

func openedBy(userID int64, at time.Time, startParam string) string {
	fields := map[string]string{
		"auth_date":     strconv.FormatInt(at.Unix(), 10),
		"user":          `{"id":` + strconv.FormatInt(userID, 10) + `,"first_name":"Ali","username":"ali","language_code":"uz"}`,
		"chat_instance": "-42",
		"chat_type":     "supergroup",
		"signature":     "abc",
	}
	if startParam != "" {
		fields["start_param"] = startParam
	}
	return signed(testToken, fields)
}

func TestValidateAcceptsTelegramsSignature(t *testing.T) {
	got, err := Validate(openedBy(42, testNow.Add(-time.Minute), "g1001234"), testToken, testNow, time.Hour)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got.User.ID != 42 || got.User.FirstName != "Ali" || got.StartParam != "g1001234" || !got.AuthDate.Equal(testNow.Add(-time.Minute)) {
		t.Errorf("got %+v", got)
	}
}

func TestValidateRefusesWhatTelegramDidNotSign(t *testing.T) {
	good := openedBy(42, testNow, "")
	tampered := strings.Replace(good, "%22id%22%3A42", "%22id%22%3A7", 1)
	if tampered == good {
		t.Fatal("the user ID was not found to tamper with")
	}
	values, _ := url.ParseQuery(good)
	values.Add("user", `{"id":7}`)
	doubled := values.Encode()
	cases := map[string]struct{ raw, token string }{
		"another bot's token": {good, "999:other"},
		"user changed":        {tampered, testToken},
		"field given twice":   {doubled, testToken},
		"no hash":             {"auth_date=1&user=%7B%22id%22%3A42%7D", testToken},
		"not a query":         {"%zz", testToken},
		"no user":             {signed(testToken, map[string]string{"auth_date": strconv.FormatInt(testNow.Unix(), 10)}), testToken},
	}
	for name, c := range cases {
		if _, err := Validate(c.raw, c.token, testNow, time.Hour); !errors.Is(err, ErrForged) {
			t.Errorf("%s: err = %v, want ErrForged", name, err)
		}
	}
}

func TestValidateRefusesOldOrFutureData(t *testing.T) {
	for name, at := range map[string]time.Time{
		"two hours ago": testNow.Add(-2 * time.Hour),
		"in an hour":    testNow.Add(time.Hour),
	} {
		if _, err := Validate(openedBy(42, at, ""), testToken, testNow, time.Hour); !errors.Is(err, ErrStale) {
			t.Errorf("%s: err = %v, want ErrStale", name, err)
		}
	}
}

func TestGroupParamRoundTrips(t *testing.T) {
	for _, chatID := range []int64{-1001234567890, -4242} {
		param := GroupParam(chatID)
		if got, ok := ParseGroupParam(param); !ok || got != chatID {
			t.Errorf("%d -> %q -> %d, %v", chatID, param, got, ok)
		}
	}
	for _, bad := range []string{"", "g", "1001", "g-1001", "g12a", "g0", "g99999999999999999999"} {
		if _, ok := ParseGroupParam(bad); ok {
			t.Errorf("%q was taken", bad)
		}
	}
}
