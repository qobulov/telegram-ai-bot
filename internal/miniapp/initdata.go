// Package miniapp serves the group dashboard, a Telegram Mini App: the page
// in public/stats is static, and asks the handler here for a group's
// statistics. Who asks is known only from the init data Telegram signs
// with the bot's token, never from anything else the page sends.
package miniapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// InitData is what Telegram tells a Mini App about who opened it, and from
// which link.
type InitData struct {
	User User
	// StartParam is the startapp parameter of the link that opened the app,
	// "" when it was opened otherwise. Anyone can write such a link, so it
	// says which group is asked about, never who may see it.
	StartParam string
	AuthDate   time.Time
}

// User is the Telegram account that opened the Mini App.
type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
	Language  string `json:"language_code"`
}

var (
	// ErrForged means the init data was not signed with the bot's token.
	ErrForged = errors.New("miniapp: init data is not signed by Telegram")
	// ErrStale means the init data was signed too long ago to be taken: a
	// copy of it must not open the dashboard for ever.
	ErrStale = errors.New("miniapp: init data is too old")
)

// clockSkew is how far in the future auth_date may be, for clocks that
// disagree a little.
const clockSkew = time.Minute

// Validate checks init data as Telegram describes: the hash is the
// HMAC-SHA256 of its other fields, sorted and joined by line feeds, keyed
// with the HMAC-SHA256 of the bot's token keyed with "WebAppData". Data
// signed more than maxAge before now is refused.
func Validate(raw, botToken string, now time.Time, maxAge time.Duration) (InitData, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return InitData{}, ErrForged
	}
	hash, err := hex.DecodeString(values.Get("hash"))
	if err != nil || len(hash) != sha256.Size || !hmac.Equal(hash, signature(values, botToken)) {
		return InitData{}, ErrForged
	}
	seconds, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil {
		return InitData{}, ErrForged
	}
	signed := time.Unix(seconds, 0)
	if now.Sub(signed) > maxAge || signed.Sub(now) > clockSkew {
		return InitData{}, ErrStale
	}
	var user User
	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil || user.ID <= 0 {
		return InitData{}, ErrForged
	}
	return InitData{User: user, StartParam: values.Get("start_param"), AuthDate: signed}, nil
}

// signature is the hash init data with these values should carry. A field
// given twice is signed as Telegram never sends it, so it fails.
func signature(values url.Values, botToken string) []byte {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key != "hash" {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+strings.Join(values[key], "\x00"))
	}
	secret := mac([]byte("WebAppData"), []byte(botToken))
	return mac(secret, []byte(strings.Join(lines, "\n")))
}

func mac(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}
