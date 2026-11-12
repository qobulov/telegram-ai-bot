package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/cards"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

const (
	// tradeData starts the callback data of a trade's buttons:
	// tradeData+"a:<trade>:<target>" accepts it and "x:…" calls it off.
	tradeData = "kt:"
	// cardGiveLimit is how many cards a player may give a day, trades and
	// the Mini App's gifts together, as the Mini App counts them.
	cardGiveLimit = cards.GiveLimit
	// cardTradeDay is the day that limit counts and an offer stays open.
	cardTradeDay = cards.GiveDay
	// cardsMatched is the most cards a name finds before it asks which.
	cardsMatched = 6
)

// tradeRules are the limits of passing cards, as of now.
func tradeRules() storage.CardTradeRules {
	return storage.CardTradeRules{Limit: cardGiveLimit, Day: cardTradeDay, Now: time.Now()}
}

// tradeCardCommand answers /almash <my card> = <their card> in a group,
// replying to the member it is offered to: an offer they accept or either
// calls off with its buttons.
func (s *Service) tradeCardCommand(ctx context.Context, in incoming) outcome {
	target, refusal := tradeTarget(in)
	if refusal != "" {
		return say(refusal)
	}
	mine, theirs, ok := strings.Cut(commandArgument(in.text), "=")
	if !ok || strings.TrimSpace(mine) == "" || strings.TrimSpace(theirs) == "" {
		return say(textTradeUsage.to(in))
	}
	give, refusal := s.heldCard(ctx, in, in.userID, mine, textCardNotMine)
	if refusal != "" {
		return say(refusal)
	}
	take, refusal := s.heldCard(ctx, in, target.ID, theirs, textCardNotTheirs)
	if refusal != "" {
		return say(refusal)
	}
	id, err := s.opts.Cards.OfferCardTrade(ctx, in.msg.Chat.ID, in.userID, target.ID, give.ID, take.ID, time.Now())
	if err != nil {
		s.opts.Logger.Error("could not offer a card trade", "user_id", in.userID, "error", err)
		return say(textCardsUnavailable.to(in))
	}
	data := strconv.FormatInt(id, 36) + ":" + strconv.FormatInt(target.ID, 36)
	text := fmt.Sprintf(textTradeOffer.to(in), tradeName(in.msg.From), tradeName(target), cardLabel(in, give), cardLabel(in, take))
	return outcome{text: text, buttons: [][]telegram.Button{{
		{Text: textTradeAccept.to(in), Data: tradeData + "a:" + data},
		{Text: textTradeCancel.to(in), Data: tradeData + "x:" + data},
	}}}
}

// tradeTarget is the member a gift or trade is for: the one the command
// replies to, in a group.
func tradeTarget(in incoming) (*telegram.User, string) {
	if !in.group {
		return nil, textTradeGroupOnly.to(in)
	}
	reply := in.msg.ReplyToMessage
	if reply == nil || reply.From == nil || reply.From.IsBot || reply.From.ID == in.userID {
		return nil, textTradeReply.to(in)
	}
	return reply.From, ""
}

// commandArgument is what follows the command's name.
func commandArgument(text string) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), words[0]))
}

// heldCard finds the card of player's named name: the one so named, or the
// only one whose name holds it; else the refusal says which there are.
func (s *Service) heldCard(ctx context.Context, in incoming, player int64, name string, none localText) (storage.Card, string) {
	name = strings.TrimSpace(name)
	found, err := s.opts.Cards.HeldCardsNamed(ctx, player, name, cardsMatched)
	if err != nil {
		s.opts.Logger.Error("could not find a player's card", "user_id", player, "error", err)
		return storage.Card{}, textCardsUnavailable.to(in)
	}
	switch {
	case len(found) == 0:
		return storage.Card{}, fmt.Sprintf(none.to(in), cardMarks.Replace(name))
	case len(found) == 1 || strings.EqualFold(found[0].Name, name):
		return found[0], ""
	}
	lines := []string{textCardWhich.to(in)}
	for _, c := range found {
		lines = append(lines, "• "+cardLabel(in, c))
	}
	return storage.Card{}, strings.Join(lines, "\n")
}

// tradeRefusal is what a player is told of a gift or trade that could not
// be done, "" when it was.
func (s *Service) tradeRefusal(in incoming, err error, target *telegram.User) string {
	var limit *storage.CardGiveLimitError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &limit) && limit.UserID == in.userID:
		return fmt.Sprintf(textGiveLimit.to(in), cardGiveLimit)
	case errors.As(err, &limit):
		return fmt.Sprintf(textGiveLimitOther.to(in), cardGiveLimit)
	case errors.Is(err, storage.ErrCardNoPlayer):
		return fmt.Sprintf(textTradeNoPlayer.to(in), tradeName(target))
	case errors.Is(err, storage.ErrCardNotHeld):
		return textTradeGone.to(in)
	case errors.Is(err, storage.ErrCardTradeOver):
		return textTradeOver.to(in)
	}
	s.opts.Logger.Error("could not pass a card", "user_id", in.userID, "error", err)
	return textCardsUnavailable.to(in)
}

// pressedTrade answers a press of a trade's button: the member it was
// offered to accepts it, and either of the two calls it off.
func (s *Service) pressedTrade(q *telegram.CallbackQuery) {
	in := incoming{
		msg:    &telegram.Message{MessageID: q.Message.MessageID, From: q.From, Chat: q.Message.Chat},
		scope:  storage.Scope{ChatID: q.Message.Chat.ID, UserID: q.From.ID},
		userID: q.From.ID,
		group:  q.Message.Chat.Type != telegram.ChatTypePrivate,
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	accept, id, target, ok := parseTradePress(q.Data)
	text, refusal := "", ""
	switch {
	case !ok || s.opts.Cards == nil:
		refusal = textTradeOver.to(in)
	case accept && q.From.ID != target:
		refusal = textTradeNotYours.to(in)
	case accept:
		trade, err := s.opts.Cards.AcceptCardTrade(ctx, id, q.From.ID, tradeRules())
		if refusal = s.tradeRefusal(in, err, q.From); refusal == "" {
			text = fmt.Sprintf(textTradeDone.to(in), tradeName(q.From), cardLabel(in, trade.Give), cardLabel(in, trade.Take))
		}
	default:
		switch done, err := s.opts.Cards.CallOffCardTrade(ctx, id, q.From.ID); {
		case err != nil:
			s.opts.Logger.Error("could not call off a card trade", "trade", id, "error", err)
			refusal = textCardsUnavailable.to(in)
		case !done:
			refusal = textTradeNotYours.to(in)
		default:
			text = fmt.Sprintf(textTradeCalledOff.to(in), tradeName(q.From))
		}
	}
	if err := s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, refusal, refusal != ""); err != nil {
		s.opts.Logger.Debug("could not answer a trade press", "user_id", in.userID, "error", err)
	}
	if text == "" {
		return
	}
	if err := s.opts.Buttons.EditMessageMenu(ctx, q.Message.Chat.ID, q.Message.MessageID, text, nil); err != nil {
		s.opts.Logger.Warn("could not show a trade", "chat_id", q.Message.Chat.ID, "error", err)
	}
}

func parseTradePress(data string) (accept bool, id, target int64, ok bool) {
	parts := strings.Split(strings.TrimPrefix(data, tradeData), ":")
	if len(parts) != 3 || (parts[0] != "a" && parts[0] != "x") {
		return false, 0, 0, false
	}
	id, err1 := strconv.ParseInt(parts[1], 36, 64)
	target, err2 := strconv.ParseInt(parts[2], 36, 64)
	return parts[0] == "a", id, target, err1 == nil && err2 == nil
}

// cardLabel is a card as a gift or trade names it: its rarity's emoji and
// its name in bold.
func cardLabel(in incoming, c storage.Card) string {
	emoji, _, _ := strings.Cut(rarityNames[min(max(c.Rarity, 0), len(rarityNames)-1)].to(in), " ")
	return emoji + " **" + cardMarks.Replace(c.Name) + "**"
}

// tradeName is a player as a gift or trade names them.
func tradeName(user *telegram.User) string { return cardMarks.Replace(doozName(user)) }

var (
	textTradeGroupOnly = localText{
		uz: "🔄 Almashish guruhda bo'ladi: kim bilan almashmoqchi bo'lsangiz, o'sha odamning xabariga javob qilib yozing. Sovg'a qilish: /kartalar ilovasida kartani bosing.",
		ru: "🔄 Обмен — в группе: ответьте на сообщение того, с кем хотите обменяться. Подарить карту: нажмите на неё в приложении /kartalar.",
		en: "🔄 Trades are made in a group: reply to a message of the member to trade with. To give a card, tap it in the /kartalar app.",
	}
	textTradeReply = localText{
		uz: "↩️ Kim bilan almashmoqchi bo'lsangiz, o'sha odamning xabariga javob qilib yozing.",
		ru: "↩️ Ответьте на сообщение того, с кем хотите обменяться.",
		en: "↩️ Reply to a message of the member to trade with.",
	}
	textTradeUsage = localText{
		uz: "🔄 Almashish: odamning xabariga javob qilib yozing\n/almash Mening kartam = Uning kartasi\nMasalan: /almash Levi = Gojo",
		ru: "🔄 Обмен: ответьте на сообщение человека\n/almash Моя карта = Его карта\nНапример: /almash Levi = Gojo",
		en: "🔄 A trade: reply to the member's message with\n/almash My card = Their card\nFor example: /almash Levi = Gojo",
	}
	textCardNotMine = localText{
		uz: "Sizda «%s» degan karta yo'q. Kartalaringiz: /kartalarim",
		ru: "У вас нет карты «%s». Ваши карты: /kartalarim",
		en: "You have no card called “%s”. Your cards: /kartalarim",
	}
	textCardNotTheirs = localText{
		uz: "Unda «%s» degan karta yo'q.",
		ru: "У него нет карты «%s».",
		en: "They have no card called “%s”.",
	}
	textCardWhich = localText{
		uz: "Qaysi biri? To'liq nomini yozing:",
		ru: "Какая именно? Напишите полное имя:",
		en: "Which one? Write its full name:",
	}
	textGiveLimit = localText{
		uz: "⏳ Kuniga %d ta karta berish mumkin. Ertaga yana urinib ko'ring.",
		ru: "⏳ В день можно отдать %d карт. Попробуйте завтра.",
		en: "⏳ You may give %d cards a day. Try again tomorrow.",
	}
	textGiveLimitOther = localText{
		uz: "⏳ Ikkinchi o'yinchi bugun %d ta karta berib bo'lgan. Ertaga yana urinib ko'ring.",
		ru: "⏳ Другой игрок уже отдал сегодня %d карт. Попробуйте завтра.",
		en: "⏳ The other player has given %d cards today. Try again tomorrow.",
	}
	textTradeNoPlayer = localText{
		uz: "%s hali birorta ham karta ochmagan. Avval /karta yozsin.",
		ru: "%s ещё не открыл ни одной карты. Пусть сначала напишет /karta.",
		en: "%s has not opened a card yet. They should send /karta first.",
	}
	textTradeGone = localText{
		uz: "Bu karta endi yo'q.",
		ru: "Этой карты уже нет.",
		en: "That card is not there any more.",
	}
	textTradeOffer = localText{
		uz: "🔄 %s → %s: almashamizmi?\nBeradi: %s\nOladi: %s",
		ru: "🔄 %s → %s: меняемся?\nОтдаёт: %s\nПолучает: %s",
		en: "🔄 %s → %s: a trade?\nGives: %s\nGets: %s",
	}
	textTradeAccept    = localText{uz: "✅ Qabul", ru: "✅ Принять", en: "✅ Accept"}
	textTradeCancel    = localText{uz: "❌ Bekor", ru: "❌ Отмена", en: "❌ Cancel"}
	textTradeNotYours  = localText{uz: "Bu taklif sizga emas.", ru: "Это предложение не для вас.", en: "This offer is not yours."}
	textTradeOver      = localText{uz: "Bu taklif tugagan.", ru: "Это предложение закрыто.", en: "This offer is over."}
	textTradeCalledOff = localText{uz: "❌ Almashish bekor qilindi (%s).", ru: "❌ Обмен отменён (%s).", en: "❌ The trade was called off (%s)."}
	textTradeDone      = localText{
		uz: "✅ Almashildi! %s qabul qildi.\n%s ↔ %s",
		ru: "✅ Обмен состоялся! %s принял.\n%s ↔ %s",
		en: "✅ Traded! %s accepted.\n%s ↔ %s",
	}
)
