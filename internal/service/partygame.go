package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// /ship's button, as the owners asked on 2026-11-26: whoever presses it is
// asked as if they had written its command, so that the model matches them
// with the ship tool, under every rule and limit a message has, once a
// message. (/tod, Truth or Dare, which shared it, was taken out the same
// day.)

// partyData starts the data of the game's buttons: "tod:" and the command;
// the name stays, as buttons sent with it are still in the groups.
const partyData = "tod:"

// partyCommands are the commands behind the game's buttons, by the request
// each stands for; typed, they work as well.
var partyCommands = map[string]string{
	"/shipme": "ship qil: menga guruhda yaqinda yozganlardan tasodifiy juft top (random)",
}

var (
	textShip = localText{
		uz: "💘 **Moslik!** Tugmani bosing: bosgan odamga guruhdan tasodifiy juft topaman. Aniq odam bilan: uning xabariga reply qilib yoki teg qilib /ship yozing.",
		ru: "💘 **Совместимость!** Нажмите кнопку: найду нажавшему случайную пару из группы. С конкретным человеком: ответьте на его сообщение или отметьте его с /ship.",
		en: "💘 **Match!** Press the button: I find whoever presses it a random match from the group. With someone in particular: reply to them or tag them with /ship.",
	}
	textShipMe   = localText{uz: "💘 Menga juft top", ru: "💘 Найди мне пару", en: "💘 Find me a match"}
	textChosen   = localText{uz: "%s\n\n👉 %s: %s", ru: "%s\n\n👉 %s: %s", en: "%s\n\n👉 %s: %s"}
	textTakenYet = localText{uz: "Bu tugmani allaqachon boshqasi bosdi: yangi /ship yozing", ru: "Кнопку уже нажали: напишите /ship заново", en: "Someone already pressed it: write /ship again"}
)

// partyPressed are the /ship messages already pressed, by chat
// and message, so that a second press at the same moment does nothing.
var partyPressed sync.Map

// partyLanguage is the language of a game's message: a group's games speak
// Uzbek, or Russian to one who writes in it, whatever the app is set to.
func partyLanguage(in incoming) string {
	if languageOf(in) == "ru" {
		return "ru"
	}
	return "uz"
}

// partyLabels name the buttons' commands, to say what was chosen.
var partyLabels = map[string]localText{"/shipme": textShipMe}

// shipGame answers /ship: with a member replied to or tagged, the model
// matches them with the sender; bare, it gives the button for a random
// match.
func (s *Service) shipGame(ctx context.Context, in incoming, rules []storage.GroupRule) outcome {
	replied := in.msg.ReplyToMessage != nil && in.msg.ReplyToMessage.From != nil && !in.msg.ReplyToMessage.From.IsBot
	if replied || len(s.mentions(in)) > 0 {
		in.text = "ship qil"
		in.described = in.text
		return s.ask(ctx, in, rules)
	}
	lang := partyLanguage(in)
	return outcome{text: textShip.inLanguage(lang), buttons: [][]telegram.Button{{{Text: textShipMe.inLanguage(lang), Data: partyData + "/shipme"}}}}
}

// pressedParty answers the first press of a /ship button as its
// command written by whoever pressed it, in reply to the message, whose
// buttons it takes away, saying who chose what: one game a message.
func (s *Service) pressedParty(q *telegram.CallbackQuery) {
	command := strings.TrimPrefix(q.Data, partyData)
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	in := incoming{msg: &telegram.Message{From: q.From}}
	key := fmt.Sprint(q.Message.Chat.ID, ":", q.Message.MessageID)
	if _, pressed := partyPressed.LoadOrStore(key, true); pressed || partyCommands[command] == "" {
		_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, textTakenYet.inLanguage(partyLanguage(in)), false)
		return
	}
	_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, "", false)
	lang := partyLanguage(in)
	chosen := fmt.Sprintf(textChosen.inLanguage(lang), q.Message.Text, cardMarks.Replace(displayName(q.From)), partyLabels[command].inLanguage(lang))
	if err := s.opts.Buttons.EditMessageText(ctx, q.Message.Chat.ID, q.Message.MessageID, chosen); err != nil {
		s.opts.Logger.Debug("could not take a game's buttons away", "chat_id", q.Message.Chat.ID, "error", err)
	}
	msg := &telegram.Message{
		MessageID: q.Message.MessageID,
		From:      q.From,
		Chat:      q.Message.Chat,
		Date:      s.opts.Now().Unix(),
		Text:      command,
	}
	// A press has no update of its own: one from its ID stands in, below
	// those Telegram gives, so it is never taken for one already handled.
	update := telegram.Update{UpdateID: -int64(hashOf(q.ID)>>1) - int64(time.Hour), Message: msg}
	if in, ok := s.accept(update); ok {
		s.process(in)
	}
}
