package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"unicode/utf8"

	"telegram-ai-bot/internal/dooz"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// doozData starts the data of a dooz game's buttons: "dz:4:<game>" plays
// cell 4, "dz:r:<game>" starts the next game, "dz:j:<id>" joins the game a
// member opened and "dz:b:<id>" has the bot take the other side, "dz:e",
// "dz:m" and "dz:h" start a game against the bot at that level, and "dz:t"
// and "dz:x" are a taken cell and a game that is over.
const doozData = "dz:"

// doozVs divides the players on a game's first line, "❌ Ali  ⚔️  ⭕ Vali",
// which is in no language: the next press, by someone of another, reads the
// players' names back from it, as the buttons' data has no room for them.
const doozVs = "  ⚔️  "

// doozBotName is the bot's side on the first line.
const doozBotName = "🤖 Maria"

// maxDoozName bounds a player's name on the first line.
const maxDoozName = 24

// doozCommand answers /dooz: in a group, a game for whoever sent it, open
// to the first member who joins it or to the bot; in a private chat, the
// bot's levels to choose from.
func (s *Service) doozCommand(in incoming) outcome {
	if s.opts.Buttons == nil {
		return say(textUnknown.to(in))
	}
	if !in.group {
		return doozLevels(in)
	}
	id := strconv.FormatInt(in.userID, 36)
	return outcome{
		text: doozHeader(doozName(in.msg.From), "?") + "\n\n" + textDoozOpen.to(in),
		buttons: [][]telegram.Button{{
			{Text: textDoozJoin.to(in), Data: doozData + "j:" + id},
			{Text: textDoozWithBot.to(in), Data: doozData + "b:" + id},
		}},
	}
}

func init() {
	codeAnswers["dooz"] = func(_ context.Context, s *Service, in incoming, _ string) outcome {
		return s.doozCommand(in)
	}
}

// doozLevels offers the bot's levels, in a private chat.
func doozLevels(in incoming) outcome {
	return outcome{
		text: textDoozLevels.to(in),
		buttons: [][]telegram.Button{{
			{Text: textDoozEasy.to(in), Data: doozData + string(dooz.Easy)},
			{Text: textDoozMedium.to(in), Data: doozData + string(dooz.Medium)},
			{Text: textDoozHard.to(in), Data: doozData + string(dooz.Hard)},
		}},
	}
}

// doozGame is a game with the names of its players, as its message shows.
type doozGame struct {
	dooz.Game
	nameX, nameO string
}

// pressedDooz answers a press of a dooz game's button: a move, the next
// game, a join or a level. The game's message is changed in place; a press
// that cannot be played, out of turn or by someone not in the game, gets a
// box saying why and changes nothing.
func (s *Service) pressedDooz(q *telegram.CallbackQuery) {
	in := incoming{
		msg:    &telegram.Message{MessageID: q.Message.MessageID, From: q.From, Chat: q.Message.Chat},
		scope:  storage.Scope{ChatID: q.Message.Chat.ID, UserID: q.From.ID},
		userID: q.From.ID,
		group:  q.Message.Chat.Type != telegram.ChatTypePrivate,
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	game, refusal := s.doozPress(in, q.Message.Text, strings.TrimPrefix(q.Data, doozData))
	if err := s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, refusal, false); err != nil {
		s.opts.Logger.Debug("could not answer a dooz press", "user_id", in.userID, "error", err)
	}
	if refusal != "" {
		return
	}
	if err := s.opts.Buttons.EditMessageMenu(ctx, q.Message.Chat.ID, q.Message.MessageID, doozText(in, game), doozButtons(in, game)); err != nil {
		s.opts.Logger.Warn("could not show a dooz move", "chat_id", q.Message.Chat.ID, "error", err)
	}
}

// doozPress is the game after the press of data on the message showing
// shown, or why it cannot be pressed.
func (s *Service) doozPress(in incoming, shown, data string) (doozGame, string) {
	action, state, _ := strings.Cut(data, ":")
	me := doozName(in.msg.From)
	switch action {
	case string(dooz.Easy), string(dooz.Medium), string(dooz.Hard):
		if in.group {
			return doozGame{}, textDoozOver.to(in)
		}
		return s.doozBotTurn(doozGame{dooz.New(in.userID, 0, dooz.Level(action[0])), me, doozBotName}), ""
	case "j", "b":
		opener, err := strconv.ParseInt(state, 36, 64)
		if err != nil || opener <= 0 {
			return doozGame{}, textDoozOver.to(in)
		}
		openerName, _ := doozNames(shown)
		switch {
		case action == "j" && opener == in.userID:
			return doozGame{}, textDoozWaiting.to(in)
		case action == "j":
			return doozGame{dooz.New(opener, in.userID, dooz.Human), openerName, me}, ""
		case opener != in.userID:
			return doozGame{}, textDoozNotOpener.to(in)
		}
		return doozGame{dooz.New(opener, 0, dooz.Medium), openerName, doozBotName}, ""
	case "t":
		return doozGame{}, textDoozTaken.to(in)
	case "x":
		return doozGame{}, textDoozOver.to(in)
	}
	g, err := dooz.Decode(state)
	if err != nil {
		return doozGame{}, textDoozOver.to(in)
	}
	nameX, nameO := doozNames(shown)
	game := doozGame{g, nameX, nameO}
	if in.userID != g.X && in.userID != g.O {
		return doozGame{}, textDoozNotYours.to(in)
	}
	if action == "r" {
		if !g.Over() {
			return doozGame{}, textDoozNotOver.to(in)
		}
		return s.doozBotTurn(doozGame{g.Rematch(), nameO, nameX}), ""
	}
	cell, err := strconv.Atoi(action)
	if err != nil {
		return doozGame{}, textDoozOver.to(in)
	}
	if g.Player(g.Turn()) != in.userID {
		return doozGame{}, textDoozNotYourTurn.to(in)
	}
	switch err := game.Play(cell); {
	case errors.Is(err, dooz.ErrTaken):
		return doozGame{}, textDoozTaken.to(in)
	case err != nil:
		return doozGame{}, textDoozOver.to(in)
	}
	return s.doozBotTurn(game), ""
}

// doozBotTurn has the bot move when the turn is its own.
func (s *Service) doozBotTurn(game doozGame) doozGame {
	if !game.Over() && game.Bots(game.Turn()) {
		_ = game.Play(game.Move(rand.IntN))
	}
	return game
}

// doozHeader is a game's first line.
func doozHeader(nameX, nameO string) string {
	return "❌ " + nameX + doozVs + "⭕ " + nameO
}

// doozNames reads the players' names back from a game's first line.
func doozNames(shown string) (nameX, nameO string) {
	first, _, _ := strings.Cut(shown, "\n")
	x, o, _ := strings.Cut(first, doozVs)
	return strings.TrimPrefix(x, "❌ "), strings.TrimPrefix(o, "⭕ ")
}

// doozName is how a player is named on the first line: their first name,
// or username, short, and without what would break the line.
func doozName(user *telegram.User) string {
	name := strings.TrimSpace(user.FirstName)
	if name == "" {
		name = user.Username
	}
	name = strings.Join(strings.Fields(strings.ReplaceAll(name, "⚔️", "")), " ")
	if utf8.RuneCountInString(name) > maxDoozName {
		name = string([]rune(name)[:maxDoozName-1]) + "…"
	}
	if name == "" {
		return "?"
	}
	return name
}

// doozText is the game's message: the players, then whose turn it is or
// how the game ended, and the series' score once a game has been won.
func doozText(in incoming, game doozGame) string {
	nameOf := func(mark byte) string {
		if mark == dooz.X {
			return "❌ " + game.nameX
		}
		return "⭕ " + game.nameO
	}
	var status string
	switch winner, _ := game.Winner(); {
	case winner != dooz.Empty && game.Bots(winner):
		status = fmt.Sprintf(textDoozBotWon.to(in), nameOf(winner))
	case winner != dooz.Empty:
		status = fmt.Sprintf(textDoozWon.to(in), nameOf(winner))
	case game.Over():
		status = textDoozDraw.to(in)
	default:
		status = fmt.Sprintf(textDoozTurn.to(in), nameOf(game.Turn()))
	}
	text := doozHeader(game.nameX, game.nameO) + "\n\n" + status
	if game.ScoreX+game.ScoreO > 0 {
		text += "\n" + fmt.Sprintf(textDoozScore.to(in), game.ScoreX, game.ScoreO)
	}
	return text
}

// doozButtons are the board, a row of three cells each, and once the game
// is over the button for the next one. A cell of the winning line is
// marked as such.
func doozButtons(in incoming, game doozGame) [][]telegram.Button {
	state := game.Encode()
	winner, line := game.Winner()
	won := map[int]bool{}
	if winner != dooz.Empty {
		won = map[int]bool{line[0]: true, line[1]: true, line[2]: true}
	}
	over := game.Over()
	rows := make([][]telegram.Button, 0, 4)
	for row := range 3 {
		cells := make([]telegram.Button, 0, 3)
		for col := range 3 {
			cell := row*3 + col
			button := telegram.Button{Text: doozMark(game.Board[cell], won[cell]), Data: doozData + strconv.Itoa(cell) + ":" + state}
			switch {
			case over:
				button.Data = doozData + "x"
			case game.Board[cell] != dooz.Empty:
				button.Data = doozData + "t"
			}
			cells = append(cells, button)
		}
		rows = append(rows, cells)
	}
	if over {
		rows = append(rows, []telegram.Button{{Text: textDoozAgain.to(in), Data: doozData + "r:" + state}})
	}
	return rows
}

// doozMark is what a cell shows.
func doozMark(mark byte, winning bool) string {
	switch {
	case mark == dooz.X && winning:
		return "❎"
	case mark == dooz.O && winning:
		return "🟢"
	case mark == dooz.X:
		return "❌"
	case mark == dooz.O:
		return "⭕"
	}
	return "▫️"
}

var (
	textDoozOpen        = lt("Kim qarshi o'ynaydi? «⚔️ Qo'shilish»ni bosing yoki Maria bilan o'ynang.", "Кто сыграет против? Жмите «⚔️ Играть» или сыграйте с Марией.", "Who plays against? Press «⚔️ Join», or play Maria.")
	textDoozJoin        = lt("⚔️ Qo'shilish", "⚔️ Играть", "⚔️ Join")
	textDoozWithBot     = lt("🤖 Maria bilan", "🤖 С Марией", "🤖 Play Maria")
	textDoozLevels      = lt("❌⭕ Dooz! Qaysi darajada o'ynaymiz?", "❌⭕ Крестики-нолики! На каком уровне играем?", "❌⭕ Tic-tac-toe! Which level?")
	textDoozEasy        = lt("😊 Oson", "😊 Легко", "😊 Easy")
	textDoozMedium      = lt("🙂 O'rta", "🙂 Средне", "🙂 Medium")
	textDoozHard        = lt("😈 Qiyin", "😈 Сложно", "😈 Hard")
	textDoozTurn        = lt("Navbat: %s", "Ходит: %s", "Turn: %s")
	textDoozWon         = lt("🏆 %s yutdi!", "🏆 %s победил(а)!", "🏆 %s wins!")
	textDoozBotWon      = lt("😏 %s yutdi! Yana urinib ko'ring.", "😏 %s победила! Попробуйте ещё.", "😏 %s wins! Try again.")
	textDoozDraw        = lt("🤝 Durang!", "🤝 Ничья!", "🤝 A draw!")
	textDoozScore       = lt("Hisob: %d : %d", "Счёт: %d : %d", "Score: %d : %d")
	textDoozAgain       = lt("🔁 Yana o'ynash", "🔁 Ещё раз", "🔁 Play again")
	textDoozWaiting     = lt("Raqib kutilmoqda: bu o'yinni siz ochgansiz.", "Ждём соперника: эту игру открыли вы.", "Waiting for someone: you opened this game.")
	textDoozNotOpener   = lt("Maria bilan o'ynashni o'yinni ochgan kishi tanlaydi.", "Играть с Марией решает тот, кто открыл игру.", "Only whoever opened the game can play Maria.")
	textDoozNotYours    = lt("Bu o'yin sizniki emas. O'zingiznikini /dooz bilan oching.", "Это не ваша игра. Откройте свою: /dooz.", "This game isn't yours. Open your own with /dooz.")
	textDoozNotYourTurn = lt("Hozir navbat sizda emas.", "Сейчас не ваш ход.", "It's not your turn.")
	textDoozTaken       = lt("Bu katak band.", "Эта клетка занята.", "That cell is taken.")
	textDoozOver        = lt("Bu o'yin tugagan.", "Эта игра окончена.", "This game is over.")
	textDoozNotOver     = lt("O'yin hali tugamagan.", "Игра ещё не окончена.", "The game isn't over yet.")
)
