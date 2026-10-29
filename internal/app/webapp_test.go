package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/game"
	"telegram-ai-bot/internal/miniapp"
)

type fakeMenuSetter struct {
	text, url string
	calls     int
}

func (f *fakeMenuSetter) SetMenuWebApp(_ context.Context, text, url string) error {
	f.calls++
	f.text, f.url = text, url
	return nil
}

// The menu button opens the dashboard while the pages are served, and is
// left alone otherwise.
func TestSetMenuApp(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	setter := &fakeMenuSetter{}
	SetMenuApp(context.Background(), setter, config.Config{}, logger)
	if setter.calls != 0 {
		t.Errorf("set without WEBAPP_URL: %+v", setter)
	}
	SetMenuApp(context.Background(), setter, config.Config{WebAppURL: "https://maria.example"}, logger)
	if setter.calls != 1 || setter.url != "https://maria.example/stats/" || setter.text == "" {
		t.Errorf("menu = %+v", setter)
	}
}

// Without WEBAPP_URL the pages' functions refuse to start, naming it.
func TestPagesNeedTheirAddress(t *testing.T) {
	if _, err := NewGame(config.Config{TelegramToken: "1:test"}, slog.Default()); !errors.Is(err, errNoPages) {
		t.Errorf("NewGame: %v", err)
	}
	if _, err := NewStats(context.Background(), config.Config{TelegramToken: "1:test"}, slog.Default()); !errors.Is(err, errNoPages) || !strings.Contains(err.Error(), "WEBAPP_URL") {
		t.Errorf("NewStats: %v", err)
	}
}

// The game's function deals a play for the link a Play press opened, which
// the webhook's service made with the same token.
func TestGameEndpointDealsAPlay(t *testing.T) {
	cfg := config.Config{TelegramToken: "1:test", TelegramAPIURL: "http://127.0.0.1:1", WebAppURL: "https://maria.example"}
	endpoint, err := NewGame(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	link, err := url.Parse(newGame(cfg).PlayURL(game.Player{UserID: hookUser, ChatID: hookGroup, MessageID: 5}))
	if err != nil || link.Path != "/game/" {
		t.Fatalf("link %v, %v", link, err)
	}
	body := `{"action":"start","session":"` + link.Query().Get("s") + `"}`
	resp := httptest.NewRecorder()
	endpoint.Handler.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/api/game", strings.NewReader(body)))
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"rounds":[`) {
		t.Errorf("start: %d %s", resp.Code, resp.Body.String())
	}
}

// signedInitData is init data as Telegram signs it for a Mini App opened by
// userID from a link naming startParam.
func signedInitData(token string, userID int64, startParam string) string {
	fields := url.Values{
		"auth_date":   {strconv.FormatInt(time.Now().Unix(), 10)},
		"user":        {`{"id":` + strconv.FormatInt(userID, 10) + `,"first_name":"Bek"}`},
		"start_param": {startParam},
	}
	check := "auth_date=" + fields.Get("auth_date") + "\nstart_param=" + startParam + "\nuser=" + fields.Get("user")
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	h := hmac.New(sha256.New, secret.Sum(nil))
	h.Write([]byte(check))
	fields.Set("hash", hex.EncodeToString(h.Sum(nil)))
	return fields.Encode()
}

// The dashboard's function, against the database and a Telegram that says
// the user is a member, counts the group named in the link.
func TestStatsEndpointShowsAMembersGroup(t *testing.T) {
	env := startWebhook(t)
	env.baseEnv["WEBAPP_URL"] = "https://maria.example"
	env.deliver(groupUpdate("920001", "salom hammaga"), hookSecret)

	endpoint, err := NewStats(context.Background(), env.config(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewStats: %v", err)
	}
	defer endpoint.Close()
	body := `{"init_data":"` + signedInitData("1:test", hookUser, miniapp.GroupParam(hookGroup)) + `"}`
	resp := httptest.NewRecorder()
	endpoint.Handler.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/api/stats", strings.NewReader(body)))
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"messages":`) || !strings.Contains(resp.Body.String(), `"hours":[`) {
		t.Errorf("stats: %d %s", resp.Code, resp.Body.String())
	}

	forged := `{"init_data":"` + signedInitData("2:other", hookUser, miniapp.GroupParam(hookGroup)) + `"}`
	resp = httptest.NewRecorder()
	endpoint.Handler.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/api/stats", strings.NewReader(forged)))
	if resp.Code != http.StatusUnauthorized {
		t.Errorf("signed with another token: %d %s", resp.Code, resp.Body.String())
	}
}
