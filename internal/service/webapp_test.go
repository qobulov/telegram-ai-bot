package service

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	"telegram-ai-bot/internal/game"
	"telegram-ai-bot/internal/telegram"
)

const statsPage = "https://maria.example/stats/"

// In an owner's private chat /stats opens the dashboard in a Mini App
// button.
func TestStatsInPrivateOpensTheMiniApp(t *testing.T) {
	f := newFixture()
	f.opts.StatsURL, f.opts.OwnerIDs = statsPage, map[int64]bool{userID: true}

	sent := f.run(textFrom(userID, "/stats"))

	if len(sent) != 1 || len(sent[0].buttons) != 1 {
		t.Fatalf("sent = %+v", sent)
	}
	button := sent[0].buttons[0][0]
	if button.WebApp == nil || button.WebApp.URL != statsPage || button.URL != "" {
		t.Errorf("button = %+v, want a Mini App button to %s", button, statsPage)
	}
}

// The dashboard names who swears most, so /stats is the owners' alone: to
// anyone else it is an unknown command, and in a group it gets no answer.
func TestStatsIsTheOwnersAlone(t *testing.T) {
	f := newFixture()
	f.opts.StatsURL, f.opts.OwnerIDs = statsPage, map[int64]bool{userID + 1: true}

	sent := f.run(textFrom(userID, "/stats"), inGroup(asAdmin(groupMessage("/stats@MyBot"))))

	if len(sent) != 1 || len(sent[0].buttons) != 0 || !strings.Contains(sent[0].text, "bilmayman") {
		t.Errorf("sent = %+v; want unknown in private and nothing in the group", sent)
	}
}

// ADMIN_COMMANDS can keep /woodcutter for administrators, as any command.
func TestGameCanBeAdminsOnly(t *testing.T) {
	f := newFixture()
	table := &fakeGameTable{}
	f.opts.Game, f.opts.GameTable = fakeGameLinks{}, table
	f.opts.AdminCommands = map[string]bool{"/woodcutter": true}

	sent := f.run(textFrom(userID, "/woodcutter"))

	if len(sent) != 1 || sent[0].text != textForbidden || len(table.games) != 0 {
		t.Errorf("sent = %+v, games %+v; want it refused", sent, table.games)
	}
}

// Without the pages' address, /stats and /woodcutter are unknown commands.
func TestStatsAndGameOffWithoutTheAddress(t *testing.T) {
	f := newFixture()

	sent := f.run(textFrom(userID, "/stats"), textFrom(userID, "/woodcutter"))

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

func (fakeGameLinks) PlayURL(shortName string, p game.Player) (string, bool) {
	page := map[string]string{game.OtinchiShortName: "game/otinchi/", game.BossShortName: "game/boss/", game.MafiaShortName: "game/mafiya/"}[shortName]
	return fmt.Sprintf("https://maria.example/%s?for=%d,%d,%d,%s", page, p.UserID, p.ChatID, p.MessageID, p.InlineMessageID), page != ""
}

// /woodcutter posts O'tinchi, /boss Dev jangi and /mafia Mafia, as a reply
// in a group, and nothing more is written; the removed games' /game and
// /snake are unknown commands, and in a group get no answer.
func TestGameCommandSendsTheGame(t *testing.T) {
	f := newFixture()
	table := &fakeGameTable{}
	f.opts.Game, f.opts.GameTable = fakeGameLinks{}, table

	sent := f.run(inGroup(groupMessage("/woodcutter@MyBot")), textFrom(userID, "/woodcutter"), textFrom(userID, "/boss"), textFrom(userID, "/mafia"),
		inGroup(groupMessage("/game")), inGroup(groupMessage("/snake")), textFrom(userID, "/game"), textFrom(userID, "/snake"))

	if len(sent) != 2 || sent[0].text != textUnknown.uz || sent[1].text != textUnknown.uz {
		t.Errorf("sent %+v besides the games; want two unknown-command replies", sent)
	}
	if len(table.games) != 4 {
		t.Errorf("games = %+v, want four", table.games)
	}
	for _, want := range []sentGame{{groupID, 30, game.OtinchiShortName}, {userID, 0, game.OtinchiShortName}, {userID, 0, game.BossShortName}, {userID, 0, game.MafiaShortName}} {
		if !slices.Contains(table.games, want) {
			t.Errorf("games = %+v, want %+v among them", table.games, want)
		}
	}
}

func TestGameCommandFailureIsSaid(t *testing.T) {
	f := newFixture()
	f.opts.Game, f.opts.GameTable = fakeGameLinks{}, &fakeGameTable{err: errors.New("Bad Request: GAME_SHORTNAME_INVALID")}

	sent := f.run(textFrom(userID, "/woodcutter"))

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
		press("1", game.OtinchiShortName, bek, gameMessage, ""),
		press("2", game.OtinchiShortName, bek, nil, "inl"),
		press("3", "tarozi", bek, gameMessage, ""),
		press("4", game.BossShortName, bek, gameMessage, ""),
	)

	if len(sent) != 0 {
		t.Errorf("sent %+v", sent)
	}
	want := map[string]string{
		"1": fmt.Sprintf("https://maria.example/game/otinchi/?for=%d,%d,500,", userID, groupID),
		"2": fmt.Sprintf("https://maria.example/game/otinchi/?for=%d,0,0,inl", userID),
		"4": fmt.Sprintf("https://maria.example/game/boss/?for=%d,%d,500,", userID, groupID),
	}
	if !maps.Equal(table.answers, want) {
		t.Errorf("answers = %v, want %v", table.answers, want)
	}
}
