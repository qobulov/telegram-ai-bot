package service

import (
	"context"
	"errors"
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

// cardWallet answers /kristal: the player's shards, the spare copies they may
// melt and what shards buy.
func (s *Service) cardWallet(ctx context.Context, in incoming) outcome {
	shards, spares, err := s.opts.Cards.CardWallet(ctx, in.userID)
	if err != nil {
		s.opts.Logger.Error("could not read a player's shards", "user_id", in.userID, "error", err)
		return say(textCardsUnavailable.to(in))
	}
	copies, worth := cards.SpareWorth(spares)
	lines := []string{fmt.Sprintf(textShardsTitle.to(in), doozName(in.msg.From), shards), ""}
	if copies > 0 {
		lines = append(lines, fmt.Sprintf(textShardsMelt.to(in), copies, worth))
	} else {
		lines = append(lines, textShardsNoSpares.to(in))
	}
	lines = append(lines, fmt.Sprintf(textShardsExtra.to(in), cards.ExtraCost), textShardsDuel.to(in), "", textShardsRates.to(in))
	return say(strings.Join(lines, "\n"))
}

// meltCards answers /eritish: the player's spare copies melted into shards.
func (s *Service) meltCards(ctx context.Context, in incoming) outcome {
	melted, gained, shards, err := s.opts.Cards.MeltCards(ctx, in.userID, cards.ShardRates)
	if err != nil {
		s.opts.Logger.Error("could not melt cards", "user_id", in.userID, "error", err)
		return say(textCardsUnavailable.to(in))
	}
	if melted == 0 {
		return say(textShardsNoSpares.to(in))
	}
	return say(fmt.Sprintf(textShardsMelted.to(in), melted, gained, shards))
}

// extraCard answers /qoshimcha: a card out of turn for shards.
func (s *Service) extraCard(ctx context.Context, in incoming) outcome {
	ctx, cancel := context.WithTimeout(ctx, cardsTimeout)
	defer cancel()
	if err := s.cardCatalog().Ensure(ctx); err != nil {
		s.opts.Logger.Error("could not fill the card catalog", "error", err)
		return say(textCardsUnavailable.to(in))
	}
	draw, err := s.opts.Cards.ExtraCard(ctx, in.userID, doozName(in.msg.From), cards.WithPity(cards.RollRandom), cards.ExtraCost)
	var few *storage.ShardsError
	switch {
	case errors.As(err, &few):
		return say(fmt.Sprintf(textShardsTooFew.to(in), few.Need, few.Have))
	case err != nil:
		s.opts.Logger.Error("could not draw a card for shards", "user_id", in.userID, "error", err)
		return say(textCardsUnavailable.to(in))
	}
	caption := cardCaption(in, draw) + "\n" + fmt.Sprintf(textShardsSpent.to(in), cards.ExtraCost, draw.Shards)
	return s.withPicture(ctx, say(caption), draw.Card)
}

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
	text += "\n" + fmt.Sprintf(textDuelStakeLine.to(in), stake) + "\n" + textDuelRules.to(in)
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
	rounds, winner := cards.Duel(teams, rand.Float64)
	moved, settled, err := s.opts.Cards.ResolveDuel(ctx, q.Message.Chat.ID, q.Message.MessageID, players[winner], players[1-winner], press.stake)
	switch {
	case err != nil:
		s.opts.Logger.Error("could not settle a duel", "chat_id", q.Message.Chat.ID, "error", err)
		return "", textCardsUnavailable.to(in)
	case !settled:
		return "", textDuelOver.to(in)
	}
	names := [2]string{cardPlayerName(challenger.Name), doozName(q.From)}
	return duelReport(in, names, rounds, winner, moved), ""
}

// cardPlayerName is a player's name as the table keeps it, made safe for
// a line.
func cardPlayerName(name string) string {
	return truncate(strings.TrimSpace(cardMarks.Replace(name)), cardNameLength)
}

// duelSides mark the challenger and the one who took the duel.
var duelSides = [2]string{"🔴", "🔵"}

// duelReport is a fought duel: each round's cards and hits, the winner of
// each marked, the score, and who won what.
func duelReport(in incoming, names [2]string, rounds []cards.DuelRound, winner, moved int) string {
	lines := []string{fmt.Sprintf("⚔️ %s **%s**  🆚  %s **%s**", duelSides[0], names[0], duelSides[1], names[1])}
	won := [2]int{}
	for i, r := range rounds {
		won[r.Won]++
		lines = append(lines, "", fmt.Sprintf(textDuelRound.to(in), i+1))
		for side := range 2 {
			lines = append(lines, duelSides[side]+" "+duelCard(in, r, side))
		}
	}
	score := fmt.Sprintf(textDuelScore.to(in), duelSides[0], won[0], won[1], duelSides[1])
	if won[0] == won[1] {
		score += " 🎲"
	}
	lines = append(lines, "", "━━━━━━━━━━━━", score, fmt.Sprintf(textDuelWinner.to(in), names[winner]))
	if moved > 0 {
		lines = append(lines, fmt.Sprintf(textDuelWon.to(in), moved, names[1-winner]))
	}
	return strings.Join(lines, "\n")
}

// duelCard is one side's card in a round: its rarity's emoji, its name, the
// hits it needed and a tick if it won, a die if luck settled it.
func duelCard(in incoming, r cards.DuelRound, side int) string {
	c := r.Cards[side]
	if c.ID == 0 {
		return textDuelNoCard.to(in)
	}
	mark := strings.Fields(rarityNames[c.Rarity].to(in))[0]
	text := mark + " " + cardPlayerName(c.Name)
	if r.Hits[side] > 0 {
		text += " — " + fmt.Sprintf(textDuelHits.to(in), r.Hits[side])
	}
	if r.Won == side {
		text += " ✅"
		if r.Hits[0] == r.Hits[1] && r.Hits[0] > 0 {
			text += "🎲"
		}
	}
	return text
}

var (
	textShardsTitle    = localText{uz: "💎 %s, kristallaringiz: **%d**", ru: "💎 %s, ваши кристаллы: **%d**", en: "💎 %s, your crystals: **%d**"}
	textShardsMelt     = localText{uz: "♻️ %d ta takror karta → +%d 💎: /eritish", ru: "♻️ %d повторных карт → +%d 💎: /eritish", en: "♻️ %d spare cards → +%d 💎: /eritish"}
	textShardsNoSpares = localText{uz: "♻️ Kristalga aylantirish uchun takror karta yo'q.", ru: "♻️ Повторных карт для кристаллов нет.", en: "♻️ No spare cards to melt."}
	textShardsExtra    = localText{uz: "🎴 Navbatdan tashqari karta: %d 💎 — /qoshimcha", ru: "🎴 Карта вне очереди: %d 💎 — /qoshimcha", en: "🎴 A card out of turn: %d 💎 — /qoshimcha"}
	textShardsDuel     = localText{
		uz: "⚔️ Duel: /duel 10 — xabarga javob qilib yoki hammaga",
		ru: "⚔️ Дуэль: /duel 10 — ответом на сообщение или всем",
		en: "⚔️ Duel: /duel 10 — as a reply, or open to all",
	}
	textShardsRates = localText{
		uz: "Takror karta: ⚪1 · 🔷3 · 💜8 · 🔥20 · 👑50 💎. Ochko va reyting kamaymaydi.",
		ru: "Повтор: ⚪1 · 🔷3 · 💜8 · 🔥20 · 👑50 💎. Очки и рейтинг не уменьшаются.",
		en: "A spare: ⚪1 · 🔷3 · 💜8 · 🔥20 · 👑50 💎. Points and ranking stay.",
	}
	textShardsMelted = localText{
		uz: "♻️ %d ta takror karta kristalga aylandi: +%d 💎 (jami %d). Ochkolaringiz o'zgarmadi.",
		ru: "♻️ %d повторных карт стали кристаллами: +%d 💎 (всего %d). Очки не изменились.",
		en: "♻️ %d spare cards melted: +%d 💎 (%d in all). Your points stay.",
	}
	textShardsTooFew = localText{uz: "💎 Kerak: %d, sizda: %d. Kristal: /kristal", ru: "💎 Нужно: %d, у вас: %d. Кристаллы: /kristal", en: "💎 It takes %d, you have %d. Crystals: /kristal"}
	textShardsSpent  = localText{uz: "💎 -%d (qoldi %d)", ru: "💎 -%d (осталось %d)", en: "💎 -%d (%d left)"}

	textDuelGroupOnly = localText{uz: "⚔️ Duel guruhda o'ynaladi.", ru: "⚔️ Дуэль играют в группе.", en: "⚔️ Duels are fought in a group."}
	textDuelStake     = localText{uz: "Tikish 0 dan %d gacha son bo'lsin: /duel 10", ru: "Ставка — число от 0 до %d: /duel 10", en: "The stake is a number from 0 to %d: /duel 10"}
	textDuelNoCards   = localText{uz: "Duel uchun avval karta oling: /karta", ru: "Для дуэли сначала получите карту: /karta", en: "Draw a card for a duel first: /karta"}
	textDuelOpen      = localText{uz: "⚔️ **%s** duelga chaqiradi! Kim qabul qiladi?", ru: "⚔️ **%s** вызывает на дуэль! Кто примет?", en: "⚔️ **%s** calls for a duel! Who takes it?"}
	textDuelTo        = localText{uz: "⚔️ **%s** → **%s**: duelga chaqiraman!", ru: "⚔️ **%s** → **%s**: вызываю на дуэль!", en: "⚔️ **%s** → **%s**: I call you to a duel!"}
	textDuelStakeLine = localText{uz: "💎 Tikish: %d", ru: "💎 Ставка: %d", en: "💎 Stake: %d"}
	textDuelRules     = localText{
		uz: "🃏 Har kimning eng kuchli 3 kartasi jang qiladi: kam zarbada yenggan karta raundni oladi.",
		ru: "🃏 Сражаются 3 сильнейшие карты каждого: раунд берёт карта, победившая за меньше ударов.",
		en: "🃏 Each one's 3 strongest cards fight: the card that wins in fewer hits takes the round.",
	}
	textDuelAccept    = localText{uz: "⚔️ Qabul qilish", ru: "⚔️ Принять", en: "⚔️ Accept"}
	textDuelCancel    = localText{uz: "❌ Bekor", ru: "❌ Отмена", en: "❌ Call off"}
	textDuelOver      = localText{uz: "Bu duel tugagan.", ru: "Эта дуэль окончена.", en: "This duel is over."}
	textDuelNotYours  = localText{uz: "Bu duel sizga emas.", ru: "Эта дуэль не для вас.", en: "This duel is not yours."}
	textDuelOwn       = localText{uz: "O'z duelingizni qabul qila olmaysiz.", ru: "Свою дуэль принять нельзя.", en: "You cannot take your own duel."}
	textDuelCalledOff = localText{uz: "❌ Duel bekor qilindi (%s).", ru: "❌ Дуэль отменена (%s).", en: "❌ The duel was called off (%s)."}
	textDuelRound     = localText{uz: "**%d-raund**", ru: "**Раунд %d**", en: "**Round %d**"}
	textDuelHits      = localText{uz: "%d zarba", ru: "ударов: %d", en: "%d hits"}
	textDuelNoCard    = localText{uz: "— karta yo'q", ru: "— нет карты", en: "— no card"}
	textDuelScore     = localText{uz: "📊 Hisob: %s %d : %d %s", ru: "📊 Счёт: %s %d : %d %s", en: "📊 Score: %s %d : %d %s"}
	textDuelWinner    = localText{uz: "🏆 G'olib: **%s**!", ru: "🏆 Победитель: **%s**!", en: "🏆 The winner: **%s**!"}
	textDuelWon       = localText{uz: "💎 +%d (%s dan)", ru: "💎 +%d (от %s)", en: "💎 +%d (from %s)"}
)
