package service

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/cards"
	"telegram-ai-bot/internal/telegram"
)

// CardBook keeps Kartalar's catalog of cards. The game itself is played in
// the Mini App alone, which reads the rest of it.
type CardBook interface {
	cards.CatalogStore
}

// cardsHTTP fetches the catalog from AniList and the cards' pictures.
var cardsHTTP = &http.Client{Timeout: 10 * time.Second}

// cardCatalog is the catalog of cards, made once.
func (s *Service) cardCatalog() *cards.Catalog {
	s.cardsOnce.Do(func() {
		s.cardsCatalog = &cards.Catalog{Store: s.opts.Cards, Client: cardsHTTP, AniList: s.opts.CardsAPI, Site: siteOf(s.opts.CardsURL), Logger: s.opts.Logger}
	})
	return s.cardsCatalog
}

// siteOf is the scheme and host of page, "" for none: where the bot's own
// pages, and the card pictures kept with them, are.
func siteOf(page string) string {
	u, err := url.Parse(page)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// cardMarks are what Telegram's formatting would read in a character's
// name, which is outside data, as a mark of its own.
var cardMarks = strings.NewReplacer("*", "", "`", "", "[", "(", "]", ")")

// isCardDraw reports a message asking for a card: /card anywhere, or the
// word alone in a private chat. Cards open in the Mini App alone, so it
// answers with the app's button.
func isCardDraw(in incoming) bool {
	if commandName(in.text) == "/card" {
		return true
	}
	word := strings.ToLower(strings.TrimSpace(in.text))
	return !in.group && (word == "karta" || word == "карта" || word == "получить карту")
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

// cardsStart is the /start payload a group's button sends to open the
// Mini App in a private chat, where Telegram lets it open; marketStart opens
// it at the market.
const (
	cardsStart  = "kartalar"
	marketStart = "bozor"
)

// cardsAppButton is the button that opens the Mini App: itself in a
// private chat, and in a group a link to the private chat, as a group's
// message cannot carry a Mini App's button; none without a page.
func (s *Service) cardsAppButton(in incoming) [][]telegram.Button {
	return s.appButton(in, textCardsAppButton, cardsStart, "")
}

// marketButton opens the Mini App at its market, as cardsAppButton does.
func (s *Service) marketButton(in incoming) [][]telegram.Button {
	return s.appButton(in, textMarketButton, marketStart, "market")
}

// appButton opens the Mini App at tab, "" for its first, under label: itself
// in a private chat, in a group a link sending start to the private chat.
func (s *Service) appButton(in incoming, label localText, start, tab string) [][]telegram.Button {
	switch {
	case s.opts.CardsURL == "":
		return nil
	case !in.group:
		page := s.opts.CardsURL
		if tab != "" {
			page += "?tab=" + tab
		}
		return [][]telegram.Button{{{Text: label.to(in), WebApp: &telegram.WebAppInfo{URL: page}}}}
	case s.opts.Bot.Username != "":
		return [][]telegram.Button{{{Text: label.to(in), URL: "https://t.me/" + s.opts.Bot.Username + "?start=" + start}}}
	}
	return nil
}

// startCards answers "/start kartalar", which a group's button sends, with
// the Mini App's button; ok is false for any other /start.
func (s *Service) startCards(in incoming) (outcome, bool) {
	payload := strings.TrimSpace(strings.TrimPrefix(in.text, commandName(in.text)))
	if in.group || s.opts.Cards == nil || s.opts.CardsURL == "" {
		return outcome{}, false
	}
	switch payload {
	case cardsStart:
		return outcome{text: textCardsApp.to(in), buttons: s.cardsAppButton(in)}, true
	case marketStart:
		return outcome{text: textMarket.to(in), buttons: s.marketButton(in)}, true
	}
	return outcome{}, false
}

var (
	textMarket = localText{
		uz: "🔄 Kartalar bozori: kartangizni qo'ying va evaziga kerakli kartani tanlang. Kimda o'sha karta bo'lsa, bir bosishda almashadi.",
		ru: "🔄 Рынок карт: выставьте свою карту и выберите, какую хотите взамен. У кого она есть — обменяется в одно нажатие.",
		en: "🔄 The card market: put up a card and pick the one you want for it. Whoever holds it trades with one press.",
	}
	textMarketButton = localText{uz: "🔄 Bozorni ochish", ru: "🔄 Открыть рынок", en: "🔄 Open the market"}
	textCardsApp     = localText{
		uz: "🎴 Kartalar ilovada ochiladi: har 2 soatda token, 1x va 5x ochish, kolleksiya va reyting.",
		ru: "🎴 Карты открываются в приложении: токены дня, открыть 1 или 5, коллекция и рейтинг.",
		en: "🎴 Cards open in the app: the day's tokens, open 1 or 5, the collection and the ranking.",
	}
	textCardsAppButton = localText{uz: "📱 Ilovada ochish", ru: "📱 Открыть приложение", en: "📱 Open the app"}
)
