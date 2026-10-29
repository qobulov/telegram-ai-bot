package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"telegram-ai-bot/internal/game"
	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/telegram"
)

const statsPage = "https://maria.example/stats/"

// In a private chat /stats opens the dashboard in a Mini App button.
func TestStatsInPrivateOpensTheMiniApp(t *testing.T) {
	f := newFixture()
	f.opts.StatsURL = statsPage

	sent := f.run(textFrom(userID, "/stats"))

	if len(sent) != 1 || len(sent[0].buttons) != 1 {
		t.Fatalf("sent = %+v", sent)
	}
	button := sent[0].buttons[0][0]
	if button.WebApp == nil || button.WebApp.URL != statsPage || button.URL != "" {
		t.Errorf("button = %+v, want a Mini App button to %s", button, statsPage)
	}
}

// A group cannot have a Mini App button, so there /stats links to the
// bot's main Mini App, naming the group.
func TestStatsInAGroupLinksToTheMiniApp(t *testing.T) {
	f := newFixture()
	f.opts.StatsURL = statsPage

	sent := f.run(inGroup(groupMessage("/stats@MyBot")))

	if len(sent) != 1 || len(sent[0].buttons) != 1 {
		t.Fatalf("sent = %+v", sent)
	}
	button := sent[0].buttons[0][0]
	want := "https://t.me/" + botUsername + "?startapp=" + miniapp.GroupParam(groupID)
	if button.URL != want || button.WebApp != nil {
		t.Errorf("button = %+v, want a link to %s", button, want)
	}
	if !strings.Contains(sent[0].text, "a'zolari") {
		t.Errorf("text = %q", sent[0].text)
	}
}

// ADMIN_COMMANDS can keep them for administrators, as any command.
func TestStatsAndGameCanBeAdminsOnly(t *testing.T) {
	f := newFixture()
	table := &fakeGameTable{}
	f.opts.StatsURL, f.opts.Game, f.opts.GameTable = statsPage, fakeGameLinks{}, table
	f.opts.AdminCommands = map[string]bool{"/stats": true, "/game": true}

	sent := f.run(textFrom(userID, "/stats"), textFrom(userID, "/game"))

	if len(sent) != 2 || sent[0].text != textForbidden || sent[1].text != textForbidden || len(table.games) != 0 {
		t.Errorf("sent = %+v, games %+v; want both refused", sent, table.games)
	}
}

// Without the pages' address, /stats and /game are unknown commands.
func TestStatsAndGameOffWithoutTheAddress(t *testing.T) {
	f := newFixture()

	sent := f.run(textFrom(userID, "/stats"), textFrom(userID, "/game"))

	if len(sent) != 2 || sent[0].text != textUnknown.uz || sent[1].text != textUnknown.uz || len(sent[0].buttons) != 0 {
		t.Errorf("sent = %+v, want two unknown-command replies", sent)
	}
}

type sentGame struct {
	chatID, replyTo int64
	shortName       string
}

type fakeGameTable struct {
	mu      sync.Mutex
	games   []sentGame
	answers map[string]string
	err     error
}

func (f *fakeGameTable) SendGame(_ context.Context, chatID int64, shortName string, replyTo int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.games = append(f.games, sentGame{chatID, replyTo, shortName})
	return 500, f.err
}

func (f *fakeGameTable) AnswerGameCallback(_ context.Context, id, url string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.answers == nil {
		f.answers = map[string]string{}
	}
	f.answers[id] = url
	return nil
}

type fakeGameLinks struct{}

func (fakeGameLinks) PlayURL(p game.Player) string {
	return fmt.Sprintf("https://maria.example/game/?for=%d,%d,%d,%s", p.UserID, p.ChatID, p.MessageID, p.InlineMessageID)
}

// /game posts the game, as a reply in a group, and writes nothing more.
func TestGameCommandSendsTheGame(t *testing.T) {
	f := newFixture()
	table := &fakeGameTable{}
	f.opts.Game, f.opts.GameTable = fakeGameLinks{}, table

	sent := f.run(inGroup(groupMessage("/game@MyBot")), textFrom(userID, "/game"))

	if len(sent) != 0 {
		t.Errorf("sent %+v besides the game", sent)
	}
	for _, want := range []sentGame{{groupID, 30, game.ShortName}, {userID, 0, game.ShortName}} {
		if !slices.Contains(table.games, want) {
			t.Errorf("games = %+v, want %+v among them", table.games, want)
		}
	}
}

func TestGameCommandFailureIsSaid(t *testing.T) {
	f := newFixture()
	f.opts.Game, f.opts.GameTable = fakeGameLinks{}, &fakeGameTable{err: errors.New("Bad Request: GAME_SHORTNAME_INVALID")}

	sent := f.run(textFrom(userID, "/game"))

	expectOneReply(t, sent, textError.uz)
}

// A press of Play opens the game for whoever pressed it, on that message;
// another bot's game, or a press with no one behind it, is left alone.
func TestPlayPressOpensTheGameForThePresser(t *testing.T) {
	f := newFixture()
	table := &fakeGameTable{}
	f.opts.Game, f.opts.GameTable = fakeGameLinks{}, table
	press := func(id, shortName string, from *telegram.User, msg *telegram.Message, inline string) telegram.Update {
		nextUpdateID++
		return telegram.Update{UpdateID: nextUpdateID, CallbackQuery: &telegram.CallbackQuery{
			ID: id, From: from, Message: msg, InlineMessageID: inline, GameShortName: shortName,
		}}
	}
	bek := &telegram.User{ID: userID, FirstName: "Bek"}
	gameMessage := &telegram.Message{MessageID: 500, Chat: telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup}}

	sent := f.run(
		press("1", game.ShortName, bek, gameMessage, ""),
		press("2", game.ShortName, bek, nil, "inl"),
		press("3", "other", bek, gameMessage, ""),
	)

	if len(sent) != 0 {
		t.Errorf("sent %+v", sent)
	}
	want := map[string]string{
		"1": fmt.Sprintf("https://maria.example/game/?for=%d,%d,500,", userID, groupID),
		"2": fmt.Sprintf("https://maria.example/game/?for=%d,0,0,inl", userID),
	}
	if len(table.answers) != 2 || table.answers["1"] != want["1"] || table.answers["2"] != want["2"] {
		t.Errorf("answers = %v, want %v", table.answers, want)
	}
}
