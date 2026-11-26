package service

import (
	"context"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// /tod is Truth or Dare, as the owners asked on 2026-11-26: one message with
// buttons, Truth, Dare and Would You Rather, under it. Whoever presses one
// is asked as if they had written its command, so that the model gives them
// a question in the chat's language with the party_game tool, under every
// rule and limit a message has.

// partyData starts the data of /tod's buttons: "tod:" and the command.
const partyData = "tod:"

// partyCommands are the commands behind /tod's buttons, by the request each
// stands for; typed, they work as well.
var partyCommands = map[string]string{
	"/haqiqat": "haqiqat ber",
	"/jurat":   "jur'at ber",
	"/tanla":   "nimani tanlarding, would you rather savoli ber",
	"/shipme":  "ship qil: menga guruhda yaqinda yozganlardan tasodifiy juft top (random)",
}

var (
	textParty = localText{
		uz: "🎲 **Truth or Dare!** Tugmani bosing: savol bosgan odamga keladi.",
		ru: "🎲 **Правда или действие!** Нажмите кнопку: вопрос получит тот, кто нажал.",
		en: "🎲 **Truth or Dare!** Press a button: the question goes to whoever presses it.",
	}
	textPartyTruth = localText{uz: "😇 Haqiqat", ru: "😇 Правда", en: "😇 Truth"}
	textPartyDare  = localText{uz: "😈 Jur'at", ru: "😈 Действие", en: "😈 Dare"}
	textPartyWYR   = localText{uz: "🤔 Nimani tanlarding?", ru: "🤔 Что выберешь?", en: "🤔 Would you rather?"}
)

var (
	textShip = localText{
		uz: "💘 **Moslik!** Tugmani bosing: bosgan odamga guruhdan tasodifiy juft topaman. Aniq odam bilan: uning xabariga reply qilib yoki teg qilib /ship yozing.",
		ru: "💘 **Совместимость!** Нажмите кнопку: найду нажавшему случайную пару из группы. С конкретным человеком: ответьте на его сообщение или отметьте его с /ship.",
		en: "💘 **Match!** Press the button: I find whoever presses it a random match from the group. With someone in particular: reply to them or tag them with /ship.",
	}
	textShipMe = localText{uz: "💘 Menga juft top", ru: "💘 Найди мне пару", en: "💘 Find me a match"}
)

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
	return outcome{text: textShip.to(in), buttons: [][]telegram.Button{{{Text: textShipMe.to(in), Data: partyData + "/shipme"}}}}
}

// partyGame answers /tod with its buttons.
func partyGame(in incoming) outcome {
	return outcome{text: textParty.to(in), buttons: [][]telegram.Button{
		{{Text: textPartyTruth.to(in), Data: partyData + "/haqiqat"}, {Text: textPartyDare.to(in), Data: partyData + "/jurat"}},
		{{Text: textPartyWYR.to(in), Data: partyData + "/tanla"}},
	}}
}

// pressedParty answers a press of a /tod button as its command written by
// whoever pressed it, in reply to the /tod message.
func (s *Service) pressedParty(q *telegram.CallbackQuery) {
	command := strings.TrimPrefix(q.Data, partyData)
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, "", false)
	cancel()
	if partyCommands[command] == "" {
		return
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
