package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/cards"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
	"telegram-ai-bot/internal/tools"
)

// CardBook keeps Kartalar: the catalog of cards, the players' draws and
// their collections.
type CardBook interface {
	cards.CatalogStore
	DrawCard(ctx context.Context, userID int64, name string, roll storage.CardRoll, every time.Duration, now time.Time) (storage.CardDraw, error)
	CardCounts(ctx context.Context, userID int64) (total, owned map[int]int, points int64, err error)
	OwnedCards(ctx context.Context, userID int64, rarity int) ([]storage.OwnedCard, error)
	CardTop(ctx context.Context, limit int) ([]storage.CardPlayer, error)
	CardPlayerOf(ctx context.Context, userID int64) (storage.CardPlayer, time.Time, error)
	CardWallet(ctx context.Context, userID int64) (shards int, spares map[int]int, err error)
	MeltCards(ctx context.Context, userID int64, rates []int) (melted, gained, shards int, err error)
	ExtraCard(ctx context.Context, userID int64, name string, roll storage.CardRoll, cost int) (storage.CardDraw, error)
	DuelTeam(ctx context.Context, userID int64, n int) ([]storage.Card, error)
	ResolveDuel(ctx context.Context, chatID, messageID, winner, loser int64, stake int) (moved int, settled bool, err error)
	ToggleCardAuto(ctx context.Context, userID int64, name, language string) (bool, error)
}

const (
	// cardData starts the callback data of a collection's buttons:
	// cardData+"<player>:<rarity>" lists a rarity's cards, "a" all of
	// them and "b" goes back; only the player whose collection it is may
	// press them.
	cardData = "kc:"
	// cardsTimeout covers filling the catalog the first time, the draw and
	// fetching the card's picture.
	cardsTimeout = 25 * time.Second
	// cardsShown is the most cards one list shows.
	cardsShown = 40
	// cardTopSize is the players the table shows.
	cardTopSize = 10
	// cardRankSize is the players read to find the asker's place below the
	// table; cardNameLength bounds a name, as decorated ones broke the rows.
	cardRankSize   = 1000
	cardNameLength = 22
)

// cardsHTTP fetches the catalog from AniList and the cards' pictures.
var cardsHTTP = &http.Client{Timeout: 10 * time.Second}

// cardCatalog is the catalog of cards, made once: it remembers when it last
// asked PokéWallet.
func (s *Service) cardCatalog() *cards.Catalog {
	s.cardsOnce.Do(func() {
		s.cardsCatalog = &cards.Catalog{Store: s.opts.Cards, Client: cardsHTTP, AniList: s.opts.CardsAPI, PokeWallet: s.opts.CardsPokeWallet, Key: s.opts.CardsKey, Logger: s.opts.Logger}
	})
	return s.cardsCatalog
}

// rarityNames are the rarities' emoji and names, the most common first.
var rarityNames = []localText{
	{uz: "⚪ Oddiy", ru: "⚪ Обычная", en: "⚪ Common"},
	{uz: "🔷 Noyob", ru: "🔷 Редкая", en: "🔷 Rare"},
	{uz: "💜 Epik", ru: "💜 Эпическая", en: "💜 Epic"},
	{uz: "🔥 Afsonaviy", ru: "🔥 Легендарная", en: "🔥 Legendary"},
	{uz: "👑 Mifik", ru: "👑 Мифическая", en: "👑 Mythic"},
}

// cardMarks are what Telegram's formatting would read in a character's
// name, which is outside data, as a mark of its own.
var cardMarks = strings.NewReplacer("*", "", "`", "", "[", "(", "]", ")")

// isCardDraw reports a message asking for a card: /karta anywhere, or the
// word alone in a private chat.
func isCardDraw(in incoming) bool {
	if commandName(in.text) == "/karta" {
		return true
	}
	word := strings.ToLower(strings.TrimSpace(in.text))
	return !in.group && (word == "karta" || word == "карта" || word == "получить карту")
}

// drawCard answers a draw: the card's picture with its name, numbers and
// rarity, or how long until the next one.
func (s *Service) drawCard(ctx context.Context, in incoming) outcome {
	ctx, cancel := context.WithTimeout(ctx, cardsTimeout)
	defer cancel()
	catalog := s.cardCatalog()
	if err := catalog.Ensure(ctx); err != nil {
		s.opts.Logger.Error("could not fill the card catalog", "error", err)
		return say(textCardsUnavailable.to(in))
	}
	draw, err := s.opts.Cards.DrawCard(ctx, in.userID, doozName(in.msg.From), cards.WithPity(cards.RollRandom), cards.Every, time.Now())
	var wait *storage.CardWaitError
	switch {
	case errors.As(err, &wait):
		return say(fmt.Sprintf(textCardWait.to(in), max(1, int(wait.Wait.Round(time.Minute).Minutes()))))
	case err != nil:
		s.opts.Logger.Error("could not draw a card", "user_id", in.userID, "error", err)
		return say(textCardsUnavailable.to(in))
	}
	return s.withPicture(ctx, say(cardCaption(languageOf(in), draw)), draw.Card)
}

// withPicture adds the card's picture to out, which goes without it when
// the picture cannot be had.
func (s *Service) withPicture(ctx context.Context, out outcome, card storage.Card) outcome {
	if picture, _, err := s.cardCatalog().FetchPicture(ctx, card.ImageURL); err == nil {
		out.drawings = []tools.Drawing{{Data: picture, MimeType: http.DetectContentType(picture)}}
	} else {
		s.opts.Logger.Warn("could not fetch a card's picture", "card", card.ID, "error", err)
	}
	return out
}

// cardCaption is a drawn card: its name and anime, numbers, rarity, the
// points it gave, and in how many draws a Legendary and a Mythic are sure.
func cardCaption(language string, draw storage.CardDraw) string {
	c := draw.Card
	lines := []string{rarityNames[c.Rarity].inLanguage(language) + " **" + cardMarks.Replace(c.Name) + "**"}
	if c.Anime != "" {
		lines = append(lines, "🎬 "+cardMarks.Replace(c.Anime))
	}
	lines = append(lines, "",
		fmt.Sprintf(textCardAttack.inLanguage(language), spaced(int64(c.Attack))),
		fmt.Sprintf(textCardHealth.inLanguage(language), spaced(int64(c.Health))),
		"",
		fmt.Sprintf(textCardPoints.inLanguage(language), spaced(int64(c.Value)), spaced(draw.Points)))
	if draw.New {
		lines = append(lines, textCardNew.inLanguage(language))
	} else {
		lines = append(lines, fmt.Sprintf(textCardAgain.inLanguage(language), draw.Count))
	}
	if draw.Guaranteed {
		lines = append(lines, textCardGuaranteed.inLanguage(language))
	}
	legendary, mythic := cards.PityIn(draw)
	lines = append(lines, fmt.Sprintf(textCardPity.inLanguage(language), legendary, mythic))
	return strings.Join(lines, "\n")
}

// spaced writes n with its thousands apart, "12 300", as the cards do.
func spaced(n int64) string {
	digits := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(d)
	}
	return b.String()
}

// cardCollection answers /kartalarim: the player's points, and for each
// rarity how many of its cards they hold, as buttons that list them.
func (s *Service) cardCollection(ctx context.Context, in incoming) outcome {
	text, buttons, err := s.collectionMenu(ctx, in, in.userID)
	if err != nil {
		s.opts.Logger.Error("could not read a card collection", "user_id", in.userID, "error", err)
		return say(textCardsUnavailable.to(in))
	}
	return outcome{text: text, buttons: buttons}
}

func (s *Service) collectionMenu(ctx context.Context, in incoming, player int64) (string, [][]telegram.Button, error) {
	total, owned, points, err := s.opts.Cards.CardCounts(ctx, player)
	if err != nil {
		return "", nil, err
	}
	id := strconv.FormatInt(player, 36)
	var buttons [][]telegram.Button
	all, held := 0, 0
	for _, r := range cards.Rarities {
		all, held = all+total[int(r)], held+owned[int(r)]
		label := fmt.Sprintf("%s - %d/%d", rarityNames[r].to(in), owned[int(r)], total[int(r)])
		buttons = append(buttons, []telegram.Button{{Text: label, Data: cardData + id + ":" + strconv.Itoa(int(r))}})
	}
	buttons = append(buttons, []telegram.Button{{Text: fmt.Sprintf(textCardAll.to(in), held, all), Data: cardData + id + ":a"}})
	buttons = append(buttons, s.cardsAppButton(in)...)
	return fmt.Sprintf(textCardCollection.to(in), doozName(in.msg.From), spaced(points)), buttons, nil
}

// pressedCards answers a press of a collection's button: a rarity's cards,
// all of them, or the collection again. Only its player may press them.
func (s *Service) pressedCards(q *telegram.CallbackQuery) {
	in := incoming{
		msg:    &telegram.Message{MessageID: q.Message.MessageID, From: q.From, Chat: q.Message.Chat},
		scope:  storage.Scope{ChatID: q.Message.Chat.ID, UserID: q.From.ID},
		userID: q.From.ID,
		group:  q.Message.Chat.Type != telegram.ChatTypePrivate,
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	who, choice, _ := strings.Cut(strings.TrimPrefix(q.Data, cardData), ":")
	player, err := strconv.ParseInt(who, 36, 64)
	refusal := ""
	if err != nil || player != q.From.ID || s.opts.Cards == nil {
		refusal = textCardNotYours.to(in)
	}
	if err := s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, refusal, refusal != ""); err != nil {
		s.opts.Logger.Debug("could not answer a card press", "user_id", in.userID, "error", err)
	}
	if refusal != "" {
		return
	}
	text, buttons, err := s.cardPage(ctx, in, player, choice)
	if err != nil {
		s.opts.Logger.Error("could not show a card collection", "user_id", in.userID, "error", err)
		return
	}
	if err := s.opts.Buttons.EditMessageMenu(ctx, q.Message.Chat.ID, q.Message.MessageID, text, buttons); err != nil {
		s.opts.Logger.Warn("could not show a card collection", "chat_id", q.Message.Chat.ID, "error", err)
	}
}

// cardPage is what a press of choice shows: "b" the collection, "a" every
// card held, or a rarity's.
func (s *Service) cardPage(ctx context.Context, in incoming, player int64, choice string) (string, [][]telegram.Button, error) {
	if choice == "b" {
		return s.collectionMenu(ctx, in, player)
	}
	rarity, title := -1, textCardAllTitle.to(in)
	if choice != "a" {
		r, err := strconv.Atoi(choice)
		if err != nil || r < 0 || r >= len(rarityNames) {
			return s.collectionMenu(ctx, in, player)
		}
		rarity, title = r, rarityNames[r].to(in)
	}
	owned, err := s.opts.Cards.OwnedCards(ctx, player, rarity)
	if err != nil {
		return "", nil, err
	}
	lines := []string{"**" + title + "**", ""}
	for i, c := range owned {
		if i == cardsShown {
			lines = append(lines, fmt.Sprintf(textCardMore.to(in), len(owned)-cardsShown))
			break
		}
		line := "• " + cardMarks.Replace(c.Name)
		if rarity < 0 {
			line = strings.Fields(rarityNames[c.Rarity].to(in))[0] + " " + cardMarks.Replace(c.Name)
		}
		if c.Count > 1 {
			line += fmt.Sprintf(" ×%d", c.Count)
		}
		lines = append(lines, line)
	}
	if len(owned) == 0 {
		lines = append(lines, textCardNone.to(in))
	}
	back := [][]telegram.Button{{{Text: textCardBack.to(in), Data: cardData + strconv.FormatInt(player, 36) + ":b"}}}
	return strings.Join(lines, "\n"), back, nil
}

// cardsStart is the /start payload a group's button sends to open the
// Mini App in a private chat, where Telegram lets it open.
const cardsStart = "kartalar"

// cardsAppButton is the button that opens the Mini App: itself in a
// private chat, and in a group a link to the private chat, as a group's
// message cannot carry a Mini App's button; none without a page.
func (s *Service) cardsAppButton(in incoming) [][]telegram.Button {
	switch {
	case s.opts.CardsURL == "":
		return nil
	case !in.group:
		return [][]telegram.Button{{{Text: textCardsAppButton.to(in), WebApp: &telegram.WebAppInfo{URL: s.opts.CardsURL}}}}
	case s.opts.Bot.Username != "":
		return [][]telegram.Button{{{Text: textCardsAppButton.to(in), URL: "https://t.me/" + s.opts.Bot.Username + "?start=" + cardsStart}}}
	}
	return nil
}

// startCards answers "/start kartalar", which a group's button sends, with
// the Mini App's button; ok is false for any other /start.
func (s *Service) startCards(in incoming) (outcome, bool) {
	payload := strings.TrimSpace(strings.TrimPrefix(in.text, commandName(in.text)))
	if in.group || payload != cardsStart || s.opts.Cards == nil || s.opts.CardsURL == "" {
		return outcome{}, false
	}
	return outcome{text: textCardsApp.to(in), buttons: s.cardsAppButton(in)}, true
}

// cardTop answers /kartatop: the players with the most points.
func (s *Service) cardTop(ctx context.Context, in incoming) outcome {
	top, err := s.opts.Cards.CardTop(ctx, cardRankSize)
	if err != nil {
		s.opts.Logger.Error("could not read the card table", "error", err)
		return say(textCardsUnavailable.to(in))
	}
	if len(top) == 0 {
		return say(textCardTopEmpty.to(in))
	}
	medals := []string{"🥇", "🥈", "🥉"}
	lines := []string{textCardTopTitle.to(in), ""}
	mine := -1
	for i, p := range top {
		if p.UserID == in.userID {
			mine = i
		}
		if i >= cardTopSize {
			continue
		}
		place := fmt.Sprintf("%d.", i+1)
		if i < len(medals) {
			place = medals[i]
		}
		name := truncate(strings.TrimSpace(cardMarks.Replace(p.Name)), cardNameLength)
		if i < len(medals) {
			name = "**" + name + "**"
		}
		row := fmt.Sprintf(textCardTopRow.to(in), place, name, spaced(p.Points), p.Cards)
		if i == mine {
			row += textCardTopYou.to(in)
		}
		lines = append(lines, row)
		if i == len(medals)-1 && len(top) > len(medals) {
			lines = append(lines, "")
		}
	}
	if mine >= cardTopSize {
		lines = append(lines, "", fmt.Sprintf(textCardTopMine.to(in), mine+1, spaced(top[mine].Points), top[mine].Cards))
	}
	lines = append(lines, "", textCardTopHint.to(in))
	return outcome{text: strings.Join(lines, "\n"), buttons: s.cardsAppButton(in)}
}

var (
	textCardsUnavailable = localText{
		uz: "Kartalarni hozir olib bo'lmadi, birozdan keyin urinib ko'ring.",
		ru: "Карты сейчас недоступны, попробуйте чуть позже.",
		en: "Cards are not available right now; try again in a bit.",
	}
	textCardWait = localText{
		uz: "⏳ Keyingi karta %d daqiqadan keyin. Kolleksiya: /kartalarim",
		ru: "⏳ Следующая карта через %d мин. Коллекция: /kartalarim",
		en: "⏳ Your next card in %d min. Collection: /kartalarim",
	}
	textCardAttack     = localText{uz: "🗡 Hujum: %s", ru: "🗡 Атака: %s", en: "🗡 Attack: %s"}
	textCardHealth     = localText{uz: "❤️ Sog'liq: %s", ru: "❤️ Здоровье: %s", en: "❤️ Health: %s"}
	textCardPoints     = localText{uz: "💠 +%s ochko (jami %s)", ru: "💠 +%s очков (всего %s)", en: "💠 +%s points (%s in all)"}
	textCardNew        = localText{uz: "🆕 Yangi karta!", ru: "🆕 Новая карта!", en: "🆕 A new card!"}
	textCardAgain      = localText{uz: "🔁 Sizda endi ×%d · ♻️ /eritish", ru: "🔁 Теперь у вас ×%d · ♻️ /eritish", en: "🔁 You now have ×%d · ♻️ /eritish"}
	textCardGuaranteed = localText{uz: "✨ Kafolatlangan karta!", ru: "✨ Гарантированная карта!", en: "✨ A guaranteed card!"}
	textCardPity       = localText{
		uz: "🎯 Kafolat: 🔥 %d · 👑 %d kartadan keyin",
		ru: "🎯 Гарантия: 🔥 через %d · 👑 через %d карт",
		en: "🎯 Sure: 🔥 in %d · 👑 in %d cards",
	}
	textCardAll        = localText{uz: "🗂 Hammasi - %d/%d", ru: "🗂 Все - %d/%d", en: "🗂 All - %d/%d"}
	textCardAllTitle   = localText{uz: "🗂 Barcha kartalaringiz", ru: "🗂 Все ваши карты", en: "🗂 All your cards"}
	textCardCollection = localText{
		uz: "🎴 %s, qaysi kartalarni ko'rasiz?\n💠 Ochko: %s\n💎 Kristal va duel: /kristal",
		ru: "🎴 %s, какие карты посмотреть?\n💠 Очки: %s\n💎 Кристаллы и дуэль: /kristal",
		en: "🎴 %s, which cards do you want to see?\n💠 Points: %s\n💎 Crystals and duels: /kristal",
	}
	textCardNotYours = localText{
		uz: "Bu sizning kolleksiyangiz emas: o'zingiznikini /kartalarim bilan oching.",
		ru: "Это не ваша коллекция: откройте свою через /kartalarim.",
		en: "This is not your collection: open yours with /kartalarim.",
	}
	textCardMore     = localText{uz: "…va yana %d ta", ru: "…и ещё %d", en: "…and %d more"}
	textCardNone     = localText{uz: "Hali bunday karta yo'q. Olish: /karta", ru: "Таких карт пока нет. Получить: /karta", en: "No such cards yet. Draw one: /karta"}
	textCardBack     = localText{uz: "⬅️ Orqaga", ru: "⬅️ Назад", en: "⬅️ Back"}
	textCardTopTitle = localText{uz: "🏆 **Kartalar reytingi**", ru: "🏆 **Рейтинг карт**", en: "🏆 **Card ranking**"}
	textCardTopRow   = localText{uz: "%s %s\n      💠 %s · 🎴 %d", ru: "%s %s\n      💠 %s · 🎴 %d", en: "%s %s\n      💠 %s · 🎴 %d"}
	textCardTopYou   = localText{uz: "  ← siz", ru: "  ← вы", en: "  ← you"}
	textCardTopMine  = localText{uz: "📍 Siz: %d-o'rin · 💠 %s · 🎴 %d", ru: "📍 Вы: %d-е место · 💠 %s · 🎴 %d", en: "📍 You: #%d · 💠 %s · 🎴 %d"}
	textCardTopHint  = localText{uz: "💠 ochko · 🎴 kartalar · yangi karta: /karta", ru: "💠 очки · 🎴 карты · новая карта: /karta", en: "💠 points · 🎴 cards · a new card: /karta"}
	textCardsApp     = localText{
		uz: "🎴 Kartalar ilovasi: kolleksiyangiz, karta olish va reyting bir joyda.",
		ru: "🎴 Приложение карт: коллекция, получение карт и рейтинг в одном месте.",
		en: "🎴 The cards app: your collection, drawing cards and the ranking in one place.",
	}
	textCardsAppButton = localText{uz: "📱 Ilovada ochish", ru: "📱 Открыть приложение", en: "📱 Open the app"}
	textCardTopEmpty   = localText{uz: "Hali hech kim karta olmagan. Birinchi bo'ling: /karta", ru: "Ещё никто не получил карту. Будьте первым: /karta", en: "Nobody has drawn a card yet. Be the first: /karta"}
)
