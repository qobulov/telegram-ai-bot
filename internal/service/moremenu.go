package service

import (
	"context"
	"fmt"
	"strings"

	"telegram-ai-bot/internal/telegram"
)

// «📋 Ko'proq» opens the rest of Maria's tools, by section: a section's
// press lists its items, and an item asks the model at once, asks for a
// value first (a city, a title, a word), or offers choices, such as the
// football leagues. The model answers each with its tool, so none takes
// from the day's limit.

// moreItem is one of a section's tools.
type moreItem struct {
	key   string
	label localText
	// request is asked of the model: at once, or with the answer to ask,
	// or with the choice pressed, in its %s.
	request localText
	ask     *askedRequest
	choices []string
}

// moreSection is a section of «📋 Ko'proq».
type moreSection struct {
	key   string
	label localText
	items []moreItem
}

func lt(uz, ru, en string) localText { return localText{uz: uz, ru: ru, en: en} }

// asking is a question with its hint, whose answer goes with request.
func asking(question, hint, request localText) *askedRequest {
	return &askedRequest{question: question, hint: hint, request: request}
}

var footballLeagues = []string{"Premier League", "La Liga", "Serie A", "Bundesliga", "Ligue 1", "Champions League"}

var moreSections = []moreSection{
	{key: "faith", label: lt("🕌 Din", "🕌 Религия", "🕌 Faith"), items: []moreItem{
		{key: "prayer", label: lt("🕋 Namoz vaqtlari", "🕋 Время намаза", "🕋 Prayer times"), ask: asking(
			lt("🕋 Qaysi shahar uchun namoz vaqtlari?", "🕋 Время намаза для какого города?", "🕋 Prayer times for which city?"),
			lt("Masalan: Toshkent", "Например: Ташкент", "For example: Tashkent"),
			lt("%s uchun bugungi namoz vaqtlari", "Время намаза на сегодня: %s", "Today's prayer times in %s"))},
		{key: "quran", label: lt("📖 Qur'on", "📖 Коран", "📖 Quran"), ask: asking(
			lt("📖 Qaysi oyat yoki mavzu?", "📖 Какой аят или тема?", "📖 Which verse or topic?"),
			lt("Masalan: 2:255 yoki sabr", "Например: 2:255 или терпение", "For example: 2:255 or patience"),
			lt("Qur'on, ma'nosi bilan: %s", "Коран, со смыслом: %s", "The Quran, with its meaning: %s"))},
	}},
	{key: "news", label: lt("📰 Yangiliklar", "📰 Новости", "📰 News"), items: []moreItem{
		{key: "news_uz", label: lt("🇺🇿 O'zbekiston", "🇺🇿 Узбекистан", "🇺🇿 Uzbekistan"),
			request: lt("O'zbekiston yangiliklari: so'nggi sarlavhalar", "Новости Узбекистана: последние заголовки", "Uzbekistan's news: the latest headlines")},
		{key: "news_world", label: lt("🌍 Jahon", "🌍 Мир", "🌍 World"),
			request: lt("Jahon yangiliklari: so'nggi sarlavhalar", "Мировые новости: последние заголовки", "World news: the latest headlines")},
	}},
	{key: "fun", label: lt("🎬 Ko'ngilochar", "🎬 Развлечения", "🎬 Fun"), items: []moreItem{
		{key: "movie", label: lt("🎬 Kino", "🎬 Кино", "🎬 Films"), ask: asking(
			lt("🎬 Qaysi kino yoki serial?", "🎬 Какой фильм или сериал?", "🎬 Which film or series?"),
			lt("Masalan: Interstellar", "Например: Interstellar", "For example: Interstellar"),
			lt("Kino: %s haqida ma'lumot", "Фильм: расскажи о %s", "Film: tell me about %s"))},
		{key: "anime", label: lt("🍥 Anime", "🍥 Аниме", "🍥 Anime"), ask: asking(
			lt("🍥 Qaysi anime?", "🍥 Какое аниме?", "🍥 Which anime?"),
			lt("Masalan: Naruto", "Например: Naruto", "For example: Naruto"),
			lt("Anime: %s haqida", "Аниме: расскажи о %s", "Anime: tell me about %s"))},
		{key: "song", label: lt("🎵 Musiqa", "🎵 Музыка", "🎵 Music"), ask: asking(
			lt("🎵 Qaysi qo'shiq yoki ijrochi?", "🎵 Какая песня или исполнитель?", "🎵 Which song or artist?"),
			lt("Masalan: Shahzoda", "Например: Shahzoda", "For example: Shahzoda"),
			lt("Qo'shiq top: %s", "Найди песню: %s", "Find the song: %s"))},
		{key: "youtube", label: lt("▶️ YouTube", "▶️ YouTube", "▶️ YouTube"), ask: asking(
			lt("▶️ YouTube'da nimani qidiray?", "▶️ Что найти на YouTube?", "▶️ What should I find on YouTube?"),
			lt("Masalan: Ling guide", "Например: Ling guide", "For example: Ling guide"),
			lt("YouTube'dan video top: %s", "Найди видео на YouTube: %s", "Find a YouTube video: %s"))},
		{key: "cat", label: lt("🐱 Mushuk", "🐱 Котик", "🐱 A cat"),
			request: lt("Mushuk rasmini yubor", "Пришли картинку котика", "Send a cat picture")},
		{key: "trivia", label: lt("❔ Viktorina", "❔ Викторина", "❔ Trivia"),
			request: lt("Viktorina savoli ber", "Задай вопрос викторины", "Ask me a trivia question")},
		{key: "fact", label: lt("💡 Qiziq fakt", "💡 Интересный факт", "💡 A fun fact"),
			request: lt("Qiziq fakt ayt", "Расскажи интересный факт", "Tell me a fun fact")},
	}},
	{key: "sport", label: lt("⚽ Sport", "⚽ Спорт", "⚽ Sport"), items: []moreItem{
		{key: "table", label: lt("🏆 Turnir jadvali", "🏆 Турнирная таблица", "🏆 League table"), choices: footballLeagues,
			request: lt("Futbol: %s turnir jadvali", "Футбол: турнирная таблица, %s", "Football: the %s table")},
		{key: "matches", label: lt("📅 O'yinlar", "📅 Матчи", "📅 Matches"), choices: footballLeagues,
			request: lt("Futbol: %s, shu turdagi o'yinlar", "Футбол: %s, матчи этого тура", "Football: %s, this matchday's matches")},
		{key: "scorers", label: lt("👟 To'purarlar", "👟 Бомбардиры", "👟 Top scorers"), choices: footballLeagues,
			request: lt("Futbol: %s to'purarlari", "Футбол: бомбардиры, %s", "Football: %s top scorers")},
	}},
	{key: "games", label: lt("🕹 O'yinlar", "🕹 Игры", "🕹 Games"), items: []moreItem{
		{key: "shashka", label: lt("♟ Shashka", "♟ Шашки", "♟ Draughts"),
			request: lt("Shashka o'ynaymiz", "Сыграем в шашки", "Let's play draughts")},
		{key: "dooz", label: lt("❌⭕ Dooz", "❌⭕ Крестики-нолики", "❌⭕ Tic-tac-toe"),
			request: lt("Dooz o'ynaymiz", "Сыграем в крестики-нолики", "Let's play tic-tac-toe")},
		{key: "boss", label: lt("👹 Dev jangi", "👹 Битва с дэвом", "👹 Boss raid"),
			request: lt("Dev jangi o'yinini yubor", "Пришли игру «Битва с дэвом»", "Send the boss raid game")},
		{key: "mafiya", label: lt("🎭 Mafia", "🎭 Мафия", "🎭 Mafia"),
			request: lt("Mafia o'yinini yubor", "Пришли игру «Мафия»", "Send the Mafia game")},
		{key: "maydon", label: lt("⚔️ Maydon", "⚔️ Майдон", "⚔️ Maydon"),
			request: lt("Maydon o'yinini yubor", "Пришли игру «Майдон»", "Send the Maydon battle")},
		{key: "ilon", label: lt("🐍 Kosmik ilon", "🐍 Космическая змейка", "🐍 Space snake"),
			request: lt("Kosmik ilon o'yinini yubor", "Пришли игру «Космическая змейка»", "Send the space snake game")},
		{key: "otinchi", label: lt("🪓 O'tinchi", "🪓 Дровосек", "🪓 Woodcutter"),
			request: lt("O'tinchi o'yinini yubor", "Пришли игру «Дровосек»", "Send the woodcutter game")},
		{key: "tarozi", label: lt("⚖️ Tarozi", "⚖️ Tarozi", "⚖️ Tarozi"),
			request: lt("Tarozi o'yinini yubor", "Пришли игру Tarozi", "Send the Tarozi game")},
		{key: "free_games", label: lt("🆓 Bepul o'yinlar", "🆓 Бесплатные игры", "🆓 Free games"),
			request: lt("Mashhur bepul o'yinlarni ko'rsat", "Покажи популярные бесплатные игры", "Show popular free games")},
		{key: "steam", label: lt("💲 Steam narxi", "💲 Цена в Steam", "💲 Steam price"), ask: asking(
			lt("💲 Qaysi o'yinning Steam narxi?", "💲 Цена какой игры в Steam?", "💲 Which game's Steam price?"),
			lt("Masalan: Dota 2", "Например: Dota 2", "For example: Dota 2"),
			lt("Steam'da %s narxi", "Цена %s в Steam", "The Steam price of %s"))},
	}},
	{key: "study", label: lt("📚 O'qish", "📚 Учёба", "📚 Study"), items: []moreItem{
		{key: "wiki", label: lt("📘 Wikipedia", "📘 Википедия", "📘 Wikipedia"), ask: asking(
			lt("📘 Wikipedia'dan nimani qidiray?", "📘 Что найти в Википедии?", "📘 What should I look up on Wikipedia?"),
			lt("Masalan: Amir Temur", "Например: Амир Темур", "For example: Amir Temur"),
			lt("Wikipedia: %s", "Википедия: %s", "Wikipedia: %s"))},
		{key: "english", label: lt("🔤 Inglizcha so'z", "🔤 Английское слово", "🔤 An English word"), ask: asking(
			lt("🔤 Qaysi inglizcha so'z?", "🔤 Какое английское слово?", "🔤 Which English word?"),
			lt("Masalan: resilient", "Например: resilient", "For example: resilient"),
			lt("Inglizcha so'z ma'nosi: %s", "Значение английского слова: %s", "The meaning of the English word %s"))},
		{key: "synonyms", label: lt("🔁 Sinonimlar", "🔁 Синонимы", "🔁 Synonyms"), ask: asking(
			lt("🔁 Qaysi inglizcha so'zga sinonim?", "🔁 Синонимы к какому английскому слову?", "🔁 Synonyms of which English word?"),
			lt("Masalan: happy", "Например: happy", "For example: happy"),
			lt("Inglizcha sinonimlar: %s", "Английские синонимы: %s", "English synonyms of %s"))},
		{key: "papers", label: lt("🎓 Ilmiy maqolalar", "🎓 Научные статьи", "🎓 Papers"), ask: asking(
			lt("🎓 Qaysi mavzuda maqolalar?", "🎓 Статьи на какую тему?", "🎓 Papers on which topic?"),
			lt("Masalan: machine learning", "Например: machine learning", "For example: machine learning"),
			lt("Ilmiy maqolalar: %s", "Научные статьи: %s", "Scholarly papers on %s"))},
	}},
	{key: "useful", label: lt("🛠 Foydali", "🛠 Полезное", "🛠 Useful"), items: []moreItem{
		{key: "qr", label: lt("🔳 QR kod", "🔳 QR-код", "🔳 QR code"), ask: asking(
			lt("🔳 QR kodga nima yozay?", "🔳 Что записать в QR-код?", "🔳 What should the QR code hold?"),
			lt("Masalan: https://t.me/...", "Например: https://t.me/...", "For example: https://t.me/..."),
			lt("QR kod yasa: %s", "Сделай QR-код: %s", "Make a QR code of: %s"))},
		{key: "draw", label: lt("🎨 Rasm chizish", "🎨 Нарисовать", "🎨 Draw"), ask: asking(
			lt("🎨 Nima chizay?", "🎨 Что нарисовать?", "🎨 What should I draw?"),
			lt("Masalan: tog'dagi qal'a", "Например: замок в горах", "For example: a castle in the mountains"),
			lt("Rasm chiz: %s", "Нарисуй: %s", "Draw: %s"))},
		{key: "place", label: lt("📍 Xaritada joy", "📍 Место на карте", "📍 A place on the map"), ask: asking(
			lt("📍 Qaysi joy?", "📍 Какое место?", "📍 Which place?"),
			lt("Masalan: Samarqand", "Например: Самарканд", "For example: Samarkand"),
			lt("%s xaritada qayerda? Lokatsiya tashla", "Где на карте %s? Пришли локацию", "Where is %s on the map? Send the location"))},
		{key: "holidays", label: lt("🎉 Bayramlar", "🎉 Праздники", "🎉 Holidays"),
			request: lt("Bu yilgi bayramlar va dam olish kunlari", "Праздники и выходные в этом году", "This year's holidays and days off")},
		{key: "crypto", label: lt("₿ Kripto", "₿ Крипта", "₿ Crypto"),
			request: lt("Bitcoin, TON va Ethereum narxi", "Цена Bitcoin, TON и Ethereum", "The price of Bitcoin, TON and Ethereum")},
		{key: "quakes", label: lt("🌋 Zilzilalar", "🌋 Землетрясения", "🌋 Earthquakes"),
			request: lt("So'nggi zilzilalar, O'rta Osiyoda", "Последние землетрясения в Центральной Азии", "The latest earthquakes in Central Asia")},
		{key: "search", label: lt("🔎 Internetdan qidirish", "🔎 Поиск в интернете", "🔎 Web search"), ask: asking(
			lt("🔎 Internetdan nimani qidiray?", "🔎 Что найти в интернете?", "🔎 What should I search the web for?"),
			lt("Masalan: MLBB yangi patch", "Например: новый патч MLBB", "For example: the new MLBB patch"),
			lt("Internetdan qidir: %s", "Найди в интернете: %s", "Search the web for: %s"))},
	}},
}

// The items' questions are answered as the menus' others are: they join
// askedRequests under the item's key.
func init() {
	for _, section := range moreSections {
		for _, item := range section.items {
			if item.ask != nil {
				askedRequests[item.key] = *item.ask
			}
		}
	}
}

var textMoreMenu = lt("📋 Yana nima kerak?", "📋 Что ещё нужно?", "📋 What else do you need?")

// moreMenu is the list of sections, in language.
func moreMenu(language string) outcome {
	var labels []telegram.Button
	for _, section := range moreSections {
		labels = append(labels, telegram.Button{Text: section.label.inLanguage(language), Data: menuData + "more:" + section.key})
	}
	return outcome{text: textMoreMenu.inLanguage(language), buttons: inRows(labels, 2)}
}

// pressedMore answers a press in «📋 Ko'proq»: a section lists its items;
// an item asks, offers its choices, or asks the model at once; a choice
// asks the model.
func (s *Service) pressedMore(ctx context.Context, q *telegram.CallbackQuery, in incoming, kind, choice string) (outcome, bool) {
	language := languageOf(in)
	switch kind {
	case "more":
		if choice == "" {
			out := moreMenu(language)
			out.replace = true
			return out, true
		}
		section, ok := findSection(choice)
		if !ok {
			return outcome{}, false
		}
		var buttons []telegram.Button
		for _, item := range section.items {
			buttons = append(buttons, telegram.Button{Text: item.label.inLanguage(language), Data: menuData + "item:" + item.key})
		}
		rows := append(inRows(buttons, 2), []telegram.Button{backButton(language, "more:")})
		return outcome{text: section.label.inLanguage(language), buttons: rows, replace: true}, true
	case "item":
		item, ok := findItem(choice)
		switch {
		case !ok:
			return outcome{}, false
		case item.ask != nil:
			return askFor(language, *item.ask), true
		case len(item.choices) > 0:
			var buttons []telegram.Button
			for i, c := range item.choices {
				buttons = append(buttons, telegram.Button{Text: c, Data: fmt.Sprintf("%spick:%s:%d", menuData, item.key, i)})
			}
			rows := append(inRows(buttons, 2), []telegram.Button{backButton(language, "more:"+sectionOf(item.key))})
			return outcome{text: item.label.inLanguage(language), buttons: rows, replace: true}, true
		}
		if answer, inCode := codeAnswers[item.key]; inCode {
			return answer(ctx, s, in, ""), true
		}
		s.askAsTyped(q, item.request.inLanguage(language))
		return outcome{}, true
	case "pick":
		key, place, _ := strings.Cut(choice, ":")
		item, ok := findItem(key)
		var i int
		if _, err := fmt.Sscan(place, &i); err != nil || !ok || i < 0 || i >= len(item.choices) {
			return outcome{}, false
		}
		if answer, inCode := codeAnswers[item.key]; inCode {
			return answer(ctx, s, in, item.choices[i]), true
		}
		s.askAsTyped(q, fmt.Sprintf(item.request.inLanguage(language), item.choices[i]))
		return outcome{}, true
	}
	return outcome{}, false
}

// backButton goes back to the menu data names, in language.
func backButton(language, data string) telegram.Button {
	return telegram.Button{Text: textBack.inLanguage(language), Data: menuData + data}
}

var textBack = lt("⬅️ Orqaga", "⬅️ Назад", "⬅️ Back")

// sectionOf is the key of the section an item is in.
func sectionOf(itemKey string) string {
	for _, section := range moreSections {
		for _, item := range section.items {
			if item.key == itemKey {
				return section.key
			}
		}
	}
	return ""
}

func findSection(key string) (moreSection, bool) {
	for _, section := range moreSections {
		if section.key == key {
			return section, true
		}
	}
	return moreSection{}, false
}

func findItem(key string) (moreItem, bool) {
	for _, section := range moreSections {
		for _, item := range section.items {
			if item.key == key {
				return item, true
			}
		}
	}
	return moreItem{}, false
}

// inRows puts buttons perRow to a row.
func inRows(buttons []telegram.Button, perRow int) [][]telegram.Button {
	var rows [][]telegram.Button
	for i, b := range buttons {
		if i%perRow == 0 {
			rows = append(rows, nil)
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], b)
	}
	return rows
}
