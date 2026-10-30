package service

import (
	"fmt"
	"strings"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// doozPlayer presses data on the game message showing text, in chat.
func doozPlayer(id int, from *telegram.User, chat telegram.Chat, text, data string) telegram.Update {
	nextUpdateID++
	return telegram.Update{UpdateID: nextUpdateID, CallbackQuery: &telegram.CallbackQuery{
		ID: fmt.Sprint(id), From: from, Data: data,
		Message: &telegram.Message{MessageID: 80, Chat: chat, Text: text},
	}}
}

// doozBoard is what the board's nine cells show.
func doozBoard(menu [][]telegram.Button) string {
	var cells []string
	for _, row := range menu[:3] {
		for _, cell := range row {
			cells = append(cells, cell.Text)
		}
	}
	return strings.Join(cells, "")
}

// A member opens a game in a group, another joins it, and they play it
// out in turn on the one message: no one else may move, nor out of turn,
// and the next game changes who starts and keeps the score.
func TestDoozIsPlayedInAGroup(t *testing.T) {
	f := newFixture()
	buttons := &fakeButtons{}
	f.opts.Buttons = buttons
	group := telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup}
	bek := &telegram.User{ID: userID, FirstName: "Bek"}
	vali := &telegram.User{ID: userID + 1, FirstName: "Vali"}
	stranger := &telegram.User{ID: userID + 2, FirstName: "Sardor"}

	sent := f.run(inGroup(groupMessage("/dooz@MyBot")))
	if len(sent) != 1 || !strings.HasPrefix(sent[0].text, "❌ Bek  ⚔️  ⭕ ?") || len(sent[0].buttons) != 1 || len(sent[0].buttons[0]) != 2 {
		t.Fatalf("/dooz sent %+v, want the open game with join and bot buttons", sent)
	}
	join := sent[0].buttons[0][0].Data

	s := New(f.opts)
	defer s.stop()
	press := 0
	text := sent[0].text
	var menu [][]telegram.Button
	play := func(from *telegram.User, data string) string {
		press++
		s.Handle(doozPlayer(press, from, group, text, data))
		edits, menus := buttons.recordedMenus()
		answer := buttons.answers[len(buttons.answers)-1].text
		if answer == "" {
			text, menu = edits[len(edits)-1], menus[len(menus)-1]
		}
		return answer
	}
	cell := func(i int) string { return menu[i/3][i%3].Data }

	if refusal := play(bek, join); refusal != textDoozWaiting.uz {
		t.Fatalf("the opener joining their own game got %q", refusal)
	}
	if refusal := play(vali, join); refusal != "" || !strings.HasPrefix(text, "❌ Bek  ⚔️  ⭕ Vali\n\n") || !strings.Contains(text, "Navbat: ❌ Bek") {
		t.Fatalf("Vali joining got %q and the message %q", refusal, text)
	}
	if refusal := play(stranger, cell(0)); refusal != textDoozNotYours.uz {
		t.Errorf("a member not in the game moved, or got %q", refusal)
	}
	if refusal := play(vali, cell(0)); refusal != textDoozNotYourTurn.uz {
		t.Errorf("O moved first, or got %q", refusal)
	}
	for i, move := range []struct {
		from *telegram.User
		cell int
	}{{bek, 0}, {vali, 3}, {bek, 1}, {vali, 4}} {
		if refusal := play(move.from, cell(move.cell)); refusal != "" {
			t.Fatalf("move %d got %q", i, refusal)
		}
	}
	if refusal := play(bek, cell(3)); refusal != textDoozTaken.uz {
		t.Errorf("a taken cell was played, or got %q", refusal)
	}
	if refusal := play(bek, cell(2)); refusal != "" {
		t.Fatal(refusal)
	}
	if !strings.Contains(text, "🏆 ❌ Bek yutdi!") || !strings.Contains(text, "Hisob: 1 : 0") || doozBoard(menu) != "❎❎❎⭕⭕▫️▫️▫️▫️" {
		t.Fatalf("after the win: %q, board %s", text, doozBoard(menu))
	}
	if refusal := play(vali, cell(8)); refusal != textDoozOver.uz {
		t.Errorf("a move after the win got %q", refusal)
	}
	again := menu[3][0]
	if again.Text != textDoozAgain.uz {
		t.Fatalf("no button for the next game: %+v", menu)
	}
	if refusal := play(stranger, again.Data); refusal != textDoozNotYours.uz {
		t.Errorf("a member not in the game started the next, or got %q", refusal)
	}
	if refusal := play(vali, again.Data); refusal != "" || !strings.HasPrefix(text, "❌ Vali  ⚔️  ⭕ Bek\n\n") || !strings.Contains(text, "Navbat: ❌ Vali") || !strings.Contains(text, "Hisob: 0 : 1") {
		t.Fatalf("the next game got %q and %q", refusal, text)
	}
	if doozBoard(menu) != strings.Repeat("▫️", 9) {
		t.Errorf("the next game's board is %s", doozBoard(menu))
	}
}

// In a private chat /dooz offers the bot's levels, as «📋 Ko'proq» does;
// the bot answers each move on the same message at once.
func TestDoozAgainstTheBot(t *testing.T) {
	f := newFixture()
	buttons := &fakeButtons{}
	f.opts.Buttons = buttons
	withToolRunner(f)

	sent := f.run(textFrom(userID, "/dooz"), menuPress(1, "kb:item:dooz"))
	if len(sent) != 2 {
		t.Fatalf("sent %d messages, want the levels twice", len(sent))
	}
	for _, m := range sent {
		if m.text != textDoozLevels.uz || len(m.buttons) != 1 || len(m.buttons[0]) != 3 || m.buttons[0][2].Data != "dz:h" {
			t.Fatalf("sent %+v, want the three levels", m)
		}
	}

	s := New(f.opts)
	defer s.stop()
	me := &telegram.User{ID: userID, FirstName: "Bek"}
	private := telegram.Chat{ID: userID, Type: telegram.ChatTypePrivate}
	s.Handle(doozPlayer(2, me, private, textDoozLevels.uz, "dz:h"))
	edits, menus := buttons.recordedMenus()
	if len(edits) != 1 || !strings.HasPrefix(edits[0], "❌ Bek  ⚔️  ⭕ 🤖 Maria\n\n") || doozBoard(menus[0]) != strings.Repeat("▫️", 9) {
		t.Fatalf("the hard level showed %q", edits)
	}
	s.Handle(doozPlayer(3, me, private, edits[0], menus[0][1][1].Data))
	edits, menus = buttons.recordedMenus()
	board := doozBoard(menus[len(menus)-1])
	if strings.Count(board, "❌") != 1 || strings.Count(board, "⭕") != 1 || !strings.Contains(edits[len(edits)-1], "Navbat: ❌ Bek") {
		t.Fatalf("after the move: %q, board %s; want the bot's answer and Bek's turn", edits[len(edits)-1], board)
	}
}

// A button's data is read as sent, so a forged game is refused and
// nothing changes.
func TestDoozRefusesAForgedGame(t *testing.T) {
	f := newFixture()
	buttons := &fakeButtons{}
	f.opts.Buttons = buttons
	me := &telegram.User{ID: userID, FirstName: "Bek"}
	private := telegram.Chat{ID: userID, Type: telegram.ChatTypePrivate}

	s := New(f.opts)
	defer s.stop()
	s.Handle(doozPlayer(1, me, private, "", "dz:4:xxxx_____:0:0:h:0:0"))
	if edits, _ := buttons.recordedMenus(); len(edits) != 0 || buttons.answers[0].text != textDoozOver.uz {
		t.Fatalf("a forged game was played: %q, %+v", edits, buttons.answers)
	}
}
