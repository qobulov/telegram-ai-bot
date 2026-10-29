package service

import (
	"fmt"
	"strings"

	"telegram-ai-bot/internal/telegram"
)

// Fixed messages the bot sends itself. Answers from the model follow the
// chat's language and the character's voice. The messages a member may get
// are in Uzbek, Russian and English, sent in the language of their Telegram
// app; the owners' and admins' commands answer in English.
const (
	textForbidden  = "This command is restricted to administrators."
	textMuted      = "Fine, I'll be quiet here. An admin can bring me back with /unmute@%s." // the bot's username
	textUnmuted    = "I'm back."
	textAdminsOnly = "Only this group's admins can do that."
	textGroupsOnly = "That command is for groups."
	textModel      = "Current model: %s"

	textIgnored       = "Okay, I won't answer %s for the next 24 hours."
	textUnignored     = "I'll answer %s again."
	textReplyToMember = "Reply to that member's message with this command."
	textRepliedToMe   = "That's my own message, and I can't tell whom it answered. Reply to the member's own message with this command."
	textCannotIgnore  = "I can't ignore admins, my owners or myself."
	textNotIgnored    = "%s wasn't on my ignore list."
)

// localText is a fixed message in Uzbek, Russian and English.
type localText struct{ uz, ru, en string }

// to is the text in the language of whoever wrote in.
func (t localText) to(in incoming) string {
	return t.inLanguage(languageOf(in))
}

// inLanguage is the text in language, "uz", "ru" or "en"; Uzbek for any
// other.
func (t localText) inLanguage(language string) string {
	switch language {
	case "ru":
		return t.ru
	case "en":
		return t.en
	}
	return t.uz
}

// languageOf is "uz", "ru" or "en": the language a message is written in,
// as replyLanguage reads it, or else that of its writer's Telegram app.
// Many write Uzbek with the app in English or Russian. A word or two does
// not tell: "ok" is not Uzbek.
func languageOf(in incoming) string {
	if len(strings.Fields(in.text)) < 2 {
		return appLanguage(in.msg.From)
	}
	switch language := replyLanguage(in.text); {
	case strings.HasPrefix(language, "Uzbek"):
		return "uz"
	case language == "Russian":
		return "ru"
	case language == "English":
		return "en"
	case language != "":
		// Cyrillic with nothing plainly Russian in it, as Uzbek is often
		// typed without its own letters.
		if uzbekInCyrillic(in.text) {
			return "uz"
		}
		return "ru"
	}
	return appLanguage(in.msg.From)
}

// appLanguage is "uz", "ru" or "en" for the language of user's Telegram
// app, as Telegram tells it: Russian also for the neighbouring languages
// whose speakers read it, English for any other but Uzbek, and Uzbek when
// Telegram does not say.
func appLanguage(user *telegram.User) string {
	if user == nil {
		return "uz"
	}
	code, _, _ := strings.Cut(strings.ToLower(user.LanguageCode), "-")
	switch code {
	case "", "uz":
		return "uz"
	case "ru", "uk", "be", "kk", "ky", "tg":
		return "ru"
	}
	return "en"
}

var (
	// textAbout names no model or company: who runs her is a secret.
	textAbout = localText{
		uz: "Maria — Telegram uchun AI suhbatdosh. Guruhda gaplashadi, savollarga javob beradi, Mobile Legends, kino, musiqa va boshqa narsalarni topadi.\n/help — buyruqlar va imkoniyatlar",
		ru: "Maria — AI-собеседник для Telegram. Болтает в группе, отвечает на вопросы, находит Mobile Legends, фильмы, музыку и многое другое.\n/help — команды и возможности",
		en: "Maria is an AI chat character for Telegram. She chats in groups, answers questions and finds Mobile Legends stats, films, music and more.\n/help — commands and usage",
	}
	textUnknown = localText{
		uz: "Bunday buyruqni bilmayman. Nima qila olishimni ko'rish uchun /help yuboring.",
		ru: "Такой команды я не знаю. Отправьте /help, чтобы узнать, что я умею.",
		en: "I don't know that command. Send /help to see what I can do.",
	}
	textWelcome = localText{
		uz: "Salom! Yozing, gaplashamiz.\n\n/help — buyruqlar va imkoniyatlar",
		ru: "Привет! Пиши, поболтаем.\n\n/help — команды и возможности",
		en: "Hi! Say something and we'll talk.\n\n/help — commands and usage",
	}
	textBusy = localText{
		uz: "Hozir xabarlar juda ko'p, birozdan keyin qayta yozing.",
		ru: "Сейчас слишком много сообщений, напишите чуть позже.",
		en: "Too many messages right now, write again in a moment.",
	}
	textRateLimited = localText{
		uz: "Juda tez yozyapsiz, biroz kutib qayta yozing.",
		ru: "Вы пишете слишком быстро, подождите немного.",
		en: "You're writing too fast, wait a moment and try again.",
	}
	textTimeout = localText{
		uz: "Javob berish juda cho'zilib ketdi, qayta yozing.",
		ru: "Ответ занял слишком много времени, напишите ещё раз.",
		en: "The answer took too long, please write again.",
	}
	textOverloaded = localText{
		uz: "Hozir juda band ekanman, bir daqiqadan keyin qayta yozing.",
		ru: "Сейчас я очень занята, напишите через минуту.",
		en: "I'm very busy right now, write again in a minute.",
	}
	textError = localText{
		uz: "Nimadir xato ketdi, birozdan keyin qayta yozing.",
		ru: "Что-то пошло не так, напишите чуть позже.",
		en: "Something went wrong, please try again in a moment.",
	}
	// textTooLong takes the longest message allowed.
	textTooLong = localText{
		uz: "Xabaringiz juda uzun, %d belgidan qisqaroq yozing.",
		ru: "Сообщение слишком длинное, напишите короче %d символов.",
		en: "Your message is too long; keep it under %d characters.",
	}
	textNoGIF = localText{
		uz: "Bunga mos GIF topilmadi.",
		ru: "Подходящий GIF не нашёлся.",
		en: "I couldn't find a GIF for that.",
	}
	textCutOff = localText{
		uz: "Javob juda uzun chiqib ketdi, qisqaroq qilib so'rang.",
		ru: "Ответ получился слишком длинным, спросите покороче.",
		en: "The answer came out too long; ask for something shorter.",
	}
	// textDailyLimit takes the private chats' daily limit.
	textDailyLimit = localText{
		uz: "Bugungi limit tugadi: kuniga %d ta xabar. Ertaga yana yozing.",
		ru: "Лимит на сегодня исчерпан: %d сообщений в день. Напишите завтра.",
		en: "That's today's limit: %d messages a day. Write again tomorrow.",
	}
	textJoinChannel = localText{
		uz: "❌ Botdan foydalanish uchun avval kanalga a'zo bo'ling, keyin «✅ Tasdiqlash» ni bosing.",
		ru: "❌ Чтобы пользоваться ботом, подпишитесь на канал и нажмите «✅ Проверить».",
		en: "❌ To use the bot, join the channel first, then tap «✅ Check».",
	}
	textJoinChannels = localText{
		uz: "❌ Botdan foydalanish uchun avval ushbu kanallarga a'zo bo'ling, keyin «✅ Tasdiqlash» ni bosing.",
		ru: "❌ Чтобы пользоваться ботом, подпишитесь на эти каналы и нажмите «✅ Проверить».",
		en: "❌ To use the bot, join these channels first, then tap «✅ Check».",
	}
	textJoinButton = localText{uz: "📢 Kanalga a'zo bo'lish", ru: "📢 Подписаться на канал", en: "📢 Join the channel"}
	// textJoinButtonN takes the channel's number.
	textJoinButtonN  = localText{uz: "%d - kanal", ru: "Канал %d", en: "Channel %d"}
	textCheckButton  = localText{uz: "✅ Tasdiqlash", ru: "✅ Проверить", en: "✅ Check"}
	textNotJoinedYet = localText{
		uz: "Hali hamma kanallarga a'zo bo'lmadingiz. Avval a'zo bo'ling, keyin qayta bosing.",
		ru: "Вы ещё не подписались на все каналы. Подпишитесь и нажмите снова.",
		en: "You haven't joined every channel yet. Join, then tap again.",
	}
	textChannelsJoined = localText{
		uz: "✅ Rahmat! Endi bemalol yozishingiz mumkin.",
		ru: "✅ Спасибо! Теперь можете писать.",
		en: "✅ Thanks! You can write now.",
	}
)

// textTextOnly answers, in a private chat, a kind of message the bot cannot
// read. Like the help, it offers voice messages only where the bot hears
// them.
func textTextOnly(listen bool) localText {
	if listen {
		return localText{
			uz: "Bunday xabarni o'qiy olmayman. Matn, rasm, video yoki ovozli xabar yuboring.",
			ru: "Такое сообщение я не читаю. Пришлите текст, фото, видео или голосовое.",
			en: "I can't read this kind of message. Send me text, a photo, a video or a voice message instead.",
		}
	}
	return localText{
		uz: "Bunday xabarni o'qiy olmayman. Matn, rasm yoki video yuboring.",
		ru: "Такое сообщение я не читаю. Пришлите текст, фото или видео.",
		en: "I can't read this kind of message. Send me text, a photo or a video instead.",
	}
}

// helpTexts are the help in each language, with four blanks filled by
// HelpTextIn: the sentences on what she can see, what she can do and what
// she can look up, and the commands of the pages when they are on. They
// name no model or company: who runs her is kept a secret.
var helpTexts = map[string]string{
	"uz": `Menga yozing: o'zbek, rus yoki ingliz tilida, siz yozgan tilda javob beraman.

Suhbatimizning oxirgi qismini eslab qolaman. %s %s
%s

Buyruqlar:
/start — salomlashish
/help — shu yordam
/about — bot haqida%s

Guruhda meni ismim bilan chaqiring, @username bilan belgilang yoki xabarimga reply qiling. U yerda buyruqqa username qo'shiladi: /help@username. Guruh men bilan bitta umumiy suhbatda.

Guruh adminlari uchun:
A'zoning xabariga reply qilib, uni o'chirishni yoki qadashni, muallifini mute yoki ban qilishni, stiker va GIF yoki rasm yuborishini cheklashni, admin qilishni so'rang. Guruhning nomi, tavsifi, rasmi, stiker to'plami, taklif havolasi va kim nima yubora olishini ham o'zgartiraman; "orqaga qaytar" desangiz, oxirgisini qaytaraman. Buning uchun o'sha admin huquqi sizda ham, menda ham bo'lishi kerak; men darhol bajaraman.
/mute@username, /unmute@username — guruhda jim turishim yoki qaytishim
/ignore@username, /unignore@username — a'zoning xabariga reply qilib: unga 24 soat javob bermayman yoki yana javob beraman`,
	"ru": `Пишите мне: отвечаю на узбекском, русском или английском — на вашем языке.

Я помню недавнюю часть нашего разговора. %s %s
%s

Команды:
/start — приветствие
/help — эта справка
/about — о боте%s

В группе зовите меня по имени, упоминайте (@username) или отвечайте на моё сообщение. Там к команде добавляется username: /help@username. У группы со мной один общий разговор.

Для админов группы:
Ответьте на сообщение участника и попросите удалить или закрепить его, замутить или забанить автора, ограничить ему стикеры и GIF или фото, сделать админом. Ещё меняю название, описание, фото группы, её стикерпак, ссылку-приглашение и что можно отправлять; скажите «верни», и я отменю последнее. Нужно, чтобы это право было и у вас, и у меня; я делаю это сразу.
/mute@username, /unmute@username — чтобы я молчала в группе или вернулась
/ignore@username, /unignore@username — ответом на сообщение участника: не отвечаю ему 24 часа или снова отвечаю`,
	"en": `Talk to me and I will answer, in Uzbek, Russian or English, matching your language.

I remember the recent part of our conversation. %s %s
%s

Commands:
/start — welcome message
/help — this help
/about — about this bot%s

In a group, call me by name, mention me (@username) or reply to one of my messages. Commands there need my username, like /help@username. A group shares one conversation with me.

For a group's admins:
Reply to a member's message and ask me to delete or pin it, to mute or ban its author, to take away their stickers and GIFs or photos, or to make them an admin. I also change the group's name, description, photo, sticker pack, invite link and what members may send, and undo the last change when asked. It takes that admin right, yours and mine, and I do it at once.
/mute@username, /unmute@username — make me stay quiet in the group, or bring me back
/ignore@username, /unignore@username — as a reply to a member's message: I stop answering them for 24 hours, or answer them again`,
}

// helpAbilities are the sentences HelpTextIn fills the help's blanks with,
// in each language: what she sees, without and with listening; what she
// does, without and with drawing; and what she looks up, without and with
// football.
var helpAbilities = map[string][6]string{
	"uz": {
		"Rasm, stiker va GIF'larni, video va dumaloq videolarning bir kadrini ko'raman, PDF, Word va matnli fayllarni o'qiyman.",
		"Rasm, stiker va GIF'larni, video va dumaloq videolarning bir kadrini ko'raman, ovozli xabarlarni eshitaman, PDF, Word va matnli fayllarni o'qiyman.",
		"Istalgan joyning vaqtini aytaman, guruhda so'rovnoma tuzaman va o'tkazib yuborgan suhbatingizni qisqacha aytib beraman, guruhning eng faollarini aytaman.",
		"So'rasangiz rasm chizaman, istalgan joyning vaqtini aytaman, guruhda so'rovnoma tuzaman va o'tkazib yuborgan suhbatingizni qisqacha aytib beraman, guruhning eng faollarini aytaman.",
		"Valyuta kursi, ob-havo va havo sifati, namoz vaqtlari, yangiliklar, Vikipediya va kripto narxlarini, Mobile Legends qahramonlari statistikasi, build va skinlarini topaman, qo'shiq, kino va video qidiraman, havoladagi maqolani o'qib beraman, Qur'on oyatlarini ma'nosi bilan topaman. Viktorina savoli, qiziq fakt, kitob, retsept, oltin narxi, shaxmat va Formula 1 natijalarini ham topaman. O'qish uchun: ilmiy maqolalar, inglizcha sinonimlar va davlatlar statistikasi.",
		"Valyuta kursi, ob-havo va havo sifati, namoz vaqtlari, yangiliklar, Vikipediya, futbol natijalari va kripto narxlarini, Mobile Legends qahramonlari statistikasi, build va skinlarini topaman, qo'shiq, kino va video qidiraman, havoladagi maqolani o'qib beraman, Qur'on oyatlarini ma'nosi bilan topaman. Viktorina savoli, qiziq fakt, kitob, retsept, oltin narxi, shaxmat va Formula 1 natijalarini ham topaman. O'qish uchun: ilmiy maqolalar, inglizcha sinonimlar va davlatlar statistikasi.",
	},
	"ru": {
		"Вижу фото, стикеры и GIF, кадр из видео и кружочков, читаю PDF, Word и текстовые файлы.",
		"Вижу фото, стикеры и GIF, кадр из видео и кружочков, слушаю голосовые, читаю PDF, Word и текстовые файлы.",
		"Скажу время в любой точке, сделаю опрос в группе и кратко перескажу, о чём там говорили без вас, и назову самых активных.",
		"Нарисую картинку по просьбе, скажу время в любой точке, сделаю опрос в группе и кратко перескажу, о чём там говорили без вас, и назову самых активных.",
		"Найду курсы валют, погоду и качество воздуха, время намаза, новости, Википедию и цены криптовалют, статистику, сборки и скины героев Mobile Legends, песни, фильмы и видео, прочитаю статью по ссылке, найду аяты Корана с переводом. А ещё вопрос для викторины, интересный факт, книги, рецепты, цену золота, шахматы и Формулу 1. Для учёбы: научные статьи, английские синонимы и статистика стран.",
		"Найду курсы валют, погоду и качество воздуха, время намаза, новости, Википедию, результаты футбола и цены криптовалют, статистику, сборки и скины героев Mobile Legends, песни, фильмы и видео, прочитаю статью по ссылке, найду аяты Корана с переводом. А ещё вопрос для викторины, интересный факт, книги, рецепты, цену золота, шахматы и Формулу 1. Для учёбы: научные статьи, английские синонимы и статистика стран.",
	},
	"en": {
		"I can see photos, stickers and GIFs, a frame of videos and round videos, and read PDF, Word and text files.",
		"I can see photos, stickers and GIFs, a frame of videos and round videos; I listen to voice messages and read PDF, Word and text files.",
		"I can tell the time anywhere and make a poll in a group, recap what you missed there and name its most active members.",
		"I can draw a picture when you ask, tell the time anywhere and make a poll in a group, recap what you missed there and name its most active members.",
		"I can look up exchange rates, the weather and air quality, prayer times, news headlines, Wikipedia and crypto prices, Mobile Legends hero statistics, builds and skins, find songs, films and videos, read an article from a link, and find Quran verses with their meaning. Also trivia questions, fun facts, books, recipes, the gold price, chess and Formula 1 results. For study: scholarly papers, English synonyms and country statistics.",
		"I can look up exchange rates, the weather and air quality, prayer times, news headlines, Wikipedia, football results and crypto prices, Mobile Legends hero statistics, builds and skins, find songs, films and videos, read an article from a link, and find Quran verses with their meaning. Also trivia questions, fun facts, books, recipes, the gold price, chess and Formula 1 results. For study: scholarly papers, English synonyms and country statistics.",
	},
}

// Abilities are what a deployment may lack: drawing needs the image tool,
// listening needs a provider that hears audio and football needs a
// football-data.org token. The help names only those present, because
// get_bot_help hands it to the model, which would otherwise promise a
// picture, a transcript or a score it cannot give. Pages needs WEBAPP_URL,
// where the dashboard and the game are served.
type Abilities struct {
	Draw     bool
	Listen   bool
	Football bool
	Pages    bool
}

// helpPages is the game's command, in each language, listed after /about
// when the pages are on. /stats, for the owners alone, is not listed.
var helpPages = map[string]string{
	"uz": "\n/game — Tarozi o'yini",
	"ru": "\n/game — игра «Tarozi»",
	"en": "\n/game — the Tarozi game",
}

// HelpText describes the bot's commands and what it can do with these
// abilities, in English. It is returned to the model by the get_bot_help
// tool, which answers in the asker's language.
func HelpText(abilities Abilities) string {
	return HelpTextIn("en", abilities)
}

// HelpTextIn is the help in lang, "uz", "ru" or "en", as /help sends it.
func HelpTextIn(lang string, abilities Abilities) string {
	phrases, ok := helpAbilities[lang]
	if !ok {
		lang, phrases = "en", helpAbilities["en"]
	}
	pick := func(i int, more bool) string {
		if more {
			return phrases[i+1]
		}
		return phrases[i]
	}
	pages := ""
	if abilities.Pages {
		pages = helpPages[lang]
	}
	return fmt.Sprintf(helpTexts[lang], pick(0, abilities.Listen), pick(2, abilities.Draw), pick(4, abilities.Football), pages)
}
