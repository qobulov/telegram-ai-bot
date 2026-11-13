package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// memoryLobbies keeps lobbies as the store does, one a chat.
type memoryLobbies struct {
	mu     sync.Mutex
	states map[int64][]byte
}

func (m *memoryLobbies) ChangeTeamLobby(_ context.Context, chatID int64, change func([]byte) ([]byte, error)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.states == nil {
		m.states = map[int64][]byte{}
	}
	state := m.states[chatID]
	if state == nil {
		state = []byte("{}")
	}
	changed, err := change(state)
	if err != nil {
		return err
	}
	m.states[chatID] = changed
	return nil
}

func (m *memoryLobbies) lobby(chatID int64) teamLobby {
	var l teamLobby
	_ = json.Unmarshal(m.states[chatID], &l)
	return l
}

// /teams opens a lobby; members join and leave; ten are split at random
// into two teams of five and the rest benched, by the opener alone; fewer
// than ten cannot be split, and an older lobby's buttons are closed.
func TestTeamsLobby(t *testing.T) {
	f := newFixture()
	lobbies := &memoryLobbies{}
	buttons := &fakeButtons{}
	f.opts.Teams, f.opts.Buttons = lobbies, buttons
	chat := telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup}
	sent := f.run(inGroup(groupMessage("/teams")))
	if len(sent) != 1 || !strings.Contains(sent[0].text, "MLBB") || len(sent[0].buttons) != 1 {
		t.Fatalf("lobby = %+v", sent)
	}
	token := lobbies.lobby(groupID).Token
	press := func(id int64, action string) telegram.Update {
		return doozPlayer(int(id), &telegram.User{ID: id, FirstName: "P" + string(rune('A'+id%26))}, chat, "", teamsData+action+":"+token)
	}
	var updates []telegram.Update
	for id := int64(2); id <= 12; id++ {
		updates = append(updates, press(id, "j"))
	}
	updates = append(updates, press(12, "l"), press(3, "s"))
	f.run(updates...)
	l := lobbies.lobby(groupID)
	if len(l.Players) != 10 || l.Split {
		t.Fatalf("after joins = %d players, split %v", len(l.Players), l.Split)
	}
	if last := buttons.answers[len(buttons.answers)-1]; !last.alert || !strings.Contains(last.text, "ochgan") {
		t.Errorf("a split by someone else = %+v", last)
	}
	f.run(press(userID, "s"), press(12, "j"), press(userID, "s"))
	l = lobbies.lobby(groupID)
	seen := map[int64]bool{}
	for _, p := range append(append(append([]teamPlayer{}, l.TeamA...), l.TeamB...), l.Bench...) {
		seen[p.ID] = true
	}
	if !l.Split || len(l.TeamA) != 5 || len(l.TeamB) != 5 || len(l.Bench) != 1 || len(seen) != 11 {
		t.Fatalf("split = %+v", l)
	}
	if menu := buttons.edits[len(buttons.edits)-1]; !strings.Contains(menu, "1-jamoa") || !strings.Contains(menu, "Zaxira") {
		t.Errorf("shown = %q", menu)
	}
	again := groupMessage("/teams")
	again.MessageID = 99
	f.run(inGroup(again))
	f.run(press(5, "j"))
	if last := buttons.answers[len(buttons.answers)-1]; !strings.Contains(last.text, "yopilgan") {
		t.Errorf("an old lobby's press = %+v", last)
	}
}
