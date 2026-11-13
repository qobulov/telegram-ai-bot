package service

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"

	"telegram-ai-bot/internal/cards"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// duelData starts the callback data of a duel's buttons:
// duelData+"a:<challenger>:<target>:<stake>" accepts it and "x:…" calls it
// off; target 0 is anyone.
const duelData = "kd:"

// duelCommand answers /duel [stake] in a group: a challenge, to the member
// the message replies to or to anyone, fought by each one's strongest cards
// for stake of the loser's shards.
func (s *Service) duelCommand(ctx context.Context, in incoming) outcome {
	if !in.group {
		return say(textDuelGroupOnly.to(in))
	}
	stake := 0
	if arg := strings.TrimSpace(strings.TrimPrefix(in.text, strings.Fields(in.text)[0])); arg != "" {
		n, err := strconv.Atoi(arg)
		if err != nil || n < 0 || n > cards.MostStake {
			return say(fmt.Sprintf(textDuelStake.to(in), cards.MostStake))
		}
		stake = n
	}
	var target *telegram.User
	if reply := in.msg.ReplyToMessage; reply != nil && reply.From != nil && !reply.From.IsBot && reply.From.ID != in.userID {
		target = reply.From
	}
	team, err := s.opts.Cards.DuelTeam(ctx, in.userID, cards.DuelTeam)
	if err != nil {
		s.opts.Logger.Error("could not read a duel team", "user_id", in.userID, "error", err)
		return say(textCardsUnavailable.to(in))
	}
	if len(team) == 0 {
		return say(textDuelNoCards.to(in))
	}
	if stake > 0 {
		shards, _, err := s.opts.Cards.CardWallet(ctx, in.userID)
		if err != nil {
			s.opts.Logger.Error("could not read a player's shards", "user_id", in.userID, "error", err)
			return say(textCardsUnavailable.to(in))
		}
		if shards < stake {
			return say(fmt.Sprintf(textShardsTooFew.to(in), stake, shards))
		}
	}
	var targetID int64
	text := fmt.Sprintf(textDuelOpen.to(in), doozName(in.msg.From))
	if target != nil {
		targetID = target.ID
		text = fmt.Sprintf(textDuelTo.to(in), doozName(in.msg.From), doozName(target))
	}
	stakeLine := textDuelFriendly.to(in)
	if stake > 0 {
		stakeLine = fmt.Sprintf(textDuelStakeLine.to(in), stake)
	}
	text += "\n\n" + duelTeamLine(in, duelSides[0], team) + "\n" + stakeLine + "\n\n" + textDuelRules.to(in)
	data := strconv.FormatInt(in.userID, 36) + ":" + strconv.FormatInt(targetID, 36) + ":" + strconv.Itoa(stake)
	return outcome{text: text, buttons: [][]telegram.Button{{
		{Text: textDuelAccept.to(in), Data: duelData + "a:" + data},
		{Text: textDuelCancel.to(in), Data: duelData + "x:" + data},
	}}}
}

// duelPress is a press of a duel's button, read from its data.
type duelPress struct {
	accept             bool
	challenger, target int64
	stake              int
}

func parseDuelPress(data string) (duelPress, bool) {
	parts := strings.Split(strings.TrimPrefix(data, duelData), ":")
	if len(parts) != 4 || (parts[0] != "a" && parts[0] != "x") {
		return duelPress{}, false
	}
	challenger, err1 := strconv.ParseInt(parts[1], 36, 64)
	target, err2 := strconv.ParseInt(parts[2], 36, 64)
	stake, err3 := strconv.Atoi(parts[3])
	if err1 != nil || err2 != nil || err3 != nil || stake < 0 || stake > cards.MostStake {
		return duelPress{}, false
	}
	return duelPress{accept: parts[0] == "a", challenger: challenger, target: target, stake: stake}, true
}

// pressedDuel answers a press of a duel's button: the one challenged, or
// anyone but the challenger when it is open, accepts it and the duel is
// fought; the challenger or the one challenged calls it off.
func (s *Service) pressedDuel(q *telegram.CallbackQuery) {
	in := incoming{
		msg:    &telegram.Message{MessageID: q.Message.MessageID, From: q.From, Chat: q.Message.Chat},
		scope:  storage.Scope{ChatID: q.Message.Chat.ID, UserID: q.From.ID},
		userID: q.From.ID,
		group:  q.Message.Chat.Type != telegram.ChatTypePrivate,
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	press, ok := parseDuelPress(q.Data)
	text, refusal := "", ""
	switch {
	case !ok || s.opts.Cards == nil:
		refusal = textDuelOver.to(in)
	case !press.accept && q.From.ID != press.challenger && q.From.ID != press.target:
		refusal = textDuelNotYours.to(in)
	case !press.accept:
		text, refusal = s.callOffDuel(ctx, in, q)
	case q.From.ID == press.challenger:
		refusal = textDuelOwn.to(in)
	case press.target != 0 && q.From.ID != press.target:
		refusal = textDuelNotYours.to(in)
	default:
		text, refusal = s.fightDuel(ctx, in, q, press)
	}
	if err := s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, refusal, refusal != ""); err != nil {
		s.opts.Logger.Debug("could not answer a duel press", "user_id", in.userID, "error", err)
	}
	if text == "" {
		return
	}
	if err := s.opts.Buttons.EditMessageMenu(ctx, q.Message.Chat.ID, q.Message.MessageID, text, nil); err != nil {
		s.opts.Logger.Warn("could not show a duel", "chat_id", q.Message.Chat.ID, "error", err)
	}
}

func (s *Service) callOffDuel(ctx context.Context, in incoming, q *telegram.CallbackQuery) (text, refusal string) {
	_, settled, err := s.opts.Cards.ResolveDuel(ctx, q.Message.Chat.ID, q.Message.MessageID, 0, 0, 0)
	switch {
	case err != nil:
		s.opts.Logger.Error("could not call off a duel", "chat_id", q.Message.Chat.ID, "error", err)
		return "", textCardsUnavailable.to(in)
	case !settled:
		return "", textDuelOver.to(in)
	}
	return fmt.Sprintf(textDuelCalledOff.to(in), doozName(q.From)), ""
}

// fightDuel fights the duel the press accepted and settles its stake.
func (s *Service) fightDuel(ctx context.Context, in incoming, q *telegram.CallbackQuery, press duelPress) (text, refusal string) {
	players := [2]int64{press.challenger, q.From.ID}
	var teams [2][]storage.Card
	for side, id := range players {
		team, err := s.opts.Cards.DuelTeam(ctx, id, cards.DuelTeam)
		if err != nil {
			s.opts.Logger.Error("could not read a duel team", "user_id", id, "error", err)
			return "", textCardsUnavailable.to(in)
		}
		teams[side] = team
	}
	if len(teams[1]) == 0 {
		return "", textDuelNoCards.to(in)
	}
	if press.stake > 0 {
		shards, _, err := s.opts.Cards.CardWallet(ctx, q.From.ID)
		if err != nil {
			s.opts.Logger.Error("could not read a player's shards", "user_id", q.From.ID, "error", err)
			return "", textCardsUnavailable.to(in)
		}
		if shards < press.stake {
			return "", fmt.Sprintf(textShardsTooFew.to(in), press.stake, shards)
		}
	}
	challenger, _, err := s.opts.Cards.CardPlayerOf(ctx, press.challenger)
	if err != nil {
		s.opts.Logger.Error("could not read a card player", "user_id", press.challenger, "error", err)
		return "", textCardsUnavailable.to(in)
	}
	result := cards.Duel(teams, rand.Float64)
	winner := result.Winner
	moved, settled, err := s.opts.Cards.ResolveDuel(ctx, q.Message.Chat.ID, q.Message.MessageID, players[winner], players[1-winner], press.stake)
	switch {
	case err != nil:
		s.opts.Logger.Error("could not settle a duel", "chat_id", q.Message.Chat.ID, "error", err)
		return "", textCardsUnavailable.to(in)
	case !settled:
		return "", textDuelOver.to(in)
	}
	names := [2]string{cardPlayerName(challenger.Name), doozName(q.From)}
	return duelReport(in, names, teams, result, moved), ""
}

// cardPlayerName is a player's name as the table keeps it, made safe for
// a line.
func cardPlayerName(name string) string {
	return truncate(strings.TrimSpace(cardMarks.Replace(name)), cardNameLength)
}

// duelSides mark the challenger and the one who took the duel.
var duelSides = [2]string{"🔴", "🔵"}

// duelReport is a fought duel: each team with its power and chance, and
// who won what.
func duelReport(in incoming, names [2]string, teams [2][]storage.Card, result cards.DuelResult, moved int) string {
	lines := []string{fmt.Sprintf("⚔️ %s **%s**  🆚  %s **%s**", duelSides[0], names[0], duelSides[1], names[1]), ""}
	for side := range 2 {
		lines = append(lines, duelTeamLine(in, duelSides[side], teams[side]),
			fmt.Sprintf(textDuelChance.to(in), result.Chance[side]), "")
	}
	lines = append(lines, fmt.Sprintf(textDuelWinner.to(in), duelSides[result.Winner], names[result.Winner]))
	if moved > 0 {
		lines = append(lines, fmt.Sprintf(textDuelWon.to(in), moved, names[1-result.Winner]))
	}
	return strings.Join(lines, "\n")
}

// duelTeamLine is a side's team: its cards by their rarity's emoji and name,
// and its power.
func duelTeamLine(in incoming, side string, team []storage.Card) string {
	var cardNames []string
	for _, c := range team {
		cardNames = append(cardNames, strings.Fields(rarityNames[c.Rarity].to(in))[0]+" "+cardPlayerName(c.Name))
	}
	return side + " " + strings.Join(cardNames, " · ") + "\n" + fmt.Sprintf(textDuelPower.to(in), spaced(int64(cards.TeamPower(team))))
}

var (
	textShardsTooFew = localText{
		uz: "💎 Kerak: %d, sizda: %d. Kristallar: /cards ilovasida takror kartalarni aylantiring.",
		ru: "💎 Нужно: %d, у вас: %d. Кристаллы: расплавьте повторы в приложении /cards.",
		en: "💎 It takes %d, you have %d. Crystals: melt your spares in the /cards app.",
	}

	textDuelGroupOnly = localText{uz: "⚔️ Duel guruhda o'ynaladi.", ru: "⚔️ Дуэль играют в группе.", en: "⚔️ Duels are fought in a group."}
	textDuelStake     = localText{uz: "Tikish 0 dan %d gacha son bo'lsin: /duel 10", ru: "Ставка — число от 0 до %d: /duel 10", en: "The stake is a number from 0 to %d: /duel 10"}
	textDuelNoCards   = localText{uz: "Duel uchun avval karta oling: /card", ru: "Для дуэли сначала получите карту: /card", en: "Draw a card for a duel first: /card"}
	textDuelOpen      = localText{uz: "⚔️ **%s** duelga chaqiradi! Kim qabul qiladi?", ru: "⚔️ **%s** вызывает на дуэль! Кто примет?", en: "⚔️ **%s** calls for a duel! Who takes it?"}
	textDuelTo        = localText{uz: "⚔️ **%s** → **%s**: duelga chaqiraman!", ru: "⚔️ **%s** → **%s**: вызываю на дуэль!", en: "⚔️ **%s** → **%s**: I call you to a duel!"}
	textDuelStakeLine = localText{uz: "💎 Tikish: %d", ru: "💎 Ставка: %d", en: "💎 Stake: %d"}
	textDuelFriendly  = localText{uz: "🤝 Tikishsiz, do'stona duel", ru: "🤝 Без ставки, дружеская дуэль", en: "🤝 No stake, a friendly duel"}
	textDuelRules     = localText{
		uz: "📜 Qoida: har kimning eng kuchli 3 kartasi jamoa bo'ladi. Kuch = hujum + jon. Kuchi ko'p jamoaning yutish imkoni ko'proq, lekin kuchsizi ham yutishi mumkin.",
		ru: "📜 Правило: 3 сильнейшие карты каждого — его команда. Сила = атака + жизнь. У сильной команды шанс выше, но слабая тоже может победить.",
		en: "📜 Rule: each one's 3 strongest cards are their team. Power = attack + life. The stronger team has the better chance, but the weaker can still win.",
	}
	textDuelAccept    = localText{uz: "⚔️ Qabul qilish", ru: "⚔️ Принять", en: "⚔️ Accept"}
	textDuelCancel    = localText{uz: "❌ Bekor", ru: "❌ Отмена", en: "❌ Call off"}
	textDuelOver      = localText{uz: "Bu duel tugagan.", ru: "Эта дуэль окончена.", en: "This duel is over."}
	textDuelNotYours  = localText{uz: "Bu duel sizga emas.", ru: "Эта дуэль не для вас.", en: "This duel is not yours."}
	textDuelOwn       = localText{uz: "O'z duelingizni qabul qila olmaysiz.", ru: "Свою дуэль принять нельзя.", en: "You cannot take your own duel."}
	textDuelCalledOff = localText{uz: "❌ Duel bekor qilindi (%s).", ru: "❌ Дуэль отменена (%s).", en: "❌ The duel was called off (%s)."}
	textDuelPower     = localText{uz: "💪 Kuch: %s", ru: "💪 Сила: %s", en: "💪 Power: %s"}
	textDuelChance    = localText{uz: "🎯 Yutish imkoni: %d%%", ru: "🎯 Шанс победы: %d%%", en: "🎯 Chance to win: %d%%"}
	textDuelWinner    = localText{uz: "🏆 G'olib: %s **%s**!", ru: "🏆 Победитель: %s **%s**!", en: "🏆 The winner: %s **%s**!"}
	textDuelWon       = localText{uz: "💎 +%d (%s dan)", ru: "💎 +%d (от %s)", en: "💎 +%d (from %s)"}
)
