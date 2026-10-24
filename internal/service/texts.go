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
	textResetDone  = "The conversation history here has been deleted."
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
	switch languageOf(in) {
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
		uz: "Maria — Telegram uchun AI suhbatdosh. Guruhda gaplashadi, savollarga javob beradi, Mobile Legends, kino, musiqa va boshqa narsalarni topadi.\n/help — buyruqlar va imkoniyatlar\n/reset — suhbat tarixini o'chirish",
		ru: "Maria — AI-собеседник для Telegram. Болтает в группе, отвечает на вопросы, находит Mobile Legends, фильмы, музыку и многое другое.\n/help — команды и возможности\n/reset — удалить историю разговора",
		en: "Maria is an AI chat character for Telegram. She chats in groups, answers questions and finds Mobile Legends stats, films, music and more.\n/help — commands and usage\n/reset — delete the conversation history",
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

// helpTexts are the help in each language, with three blanks filled by
// HelpTextIn: the sentences on what she can see, what she can do and what
// she can look up. They name no model or company: who runs her is kept a
// secret.
var helpTexts = map[string]string{
	"uz": `Menga yozing: o'zbek, rus yoki ingliz tilida, siz yozgan tilda javob beraman.

Suhbatimizning oxirgi qismini eslab qolaman. %s %s
%s

Buyruqlar:
/start — salomlashish
/help — shu yordam
/reset — suhbat tarixini o'chirish
/stop — ishlayotgan testni to'xtatish
/about — bot haqida

Guruhda meni ismim bilan chaqiring, @username bilan belgilang yoki xabarimga reply qiling. U yerda buyruqqa username qo'shiladi: /help@username. Guruh men bilan bitta umumiy suhbatda.

Guruh adminlari uchun:
A'zoning xabariga reply qilib, uni o'chirishni yoki qadashni, muallifini mute yoki ban qilishni, stiker va GIF yuborishini cheklashni so'rang. Buning uchun o'sha admin huquqi sizda ham, menda ham bo'lishi kerak; men darhol bajaraman.
/mute@username, /unmute@username — guruhda jim turishim yoki qaytishim
/ignore@username, /unignore@username — a'zoning xabariga reply qilib: unga 24 soat javob bermayman yoki yana javob beraman
/reset@username — guruhning men bilan suhbat tarixini o'chirish`,
	"ru": `Пишите мне: отвечаю на узбекском, русском или английском — на вашем языке.

Я помню недавнюю часть нашего разговора. %s %s
%s

Команды:
/start — приветствие
/help — эта справка
/reset — удалить историю разговора
/stop — остановить идущий тест
/about — о боте

В группе зовите меня по имени, упоминайте (@username) или отвечайте на моё сообщение. Там к команде добавляется username: /help@username. У группы со мной один общий разговор.

Для админов группы:
Ответьте на сообщение участника и попросите удалить или закрепить его, замутить или забанить автора, ограничить ему стикеры и GIF. Нужно, чтобы это право было и у вас, и у меня; я делаю это сразу.
/mute@username, /unmute@username — чтобы я молчала в группе или вернулась
/ignore@username, /unignore@username — ответом на сообщение участника: не отвечаю ему 24 часа или снова отвечаю
/reset@username — удалить историю разговора группы со мной`,
	"en": `Talk to me and I will answer, in Uzbek, Russian or English, matching your language.

I remember the recent part of our conversation. %s %s
%s

Commands:
/start — welcome message
/help — this help
/reset — delete the conversation history
/stop — stop a running test
/about — about this bot

In a group, call me by name, mention me (@username) or reply to one of my messages. Commands there need my username, like /help@username. A group shares one conversation with me.

For a group's admins:
Reply to a member's message and ask me to delete or pin it, to mute or ban its author, or to take away their stickers and GIFs. It takes that admin right, yours and mine, and I do it at once.
/mute@username, /unmute@username — make me stay quiet in the group, or bring me back
/ignore@username, /unignore@username — as a reply to a member's message: I stop answering them for 24 hours, or answer them again
/reset@username — delete the group's conversation with me`,
}

// helpAbilities are the sentences HelpTextIn fills the help's blanks with,
// in each language: what she sees, without and with listening; what she
// does, without and with drawing; and what she looks up, without and with
// football.
var helpAbilities = map[string][6]string{
	"uz": {
		"Rasm, stiker va GIF'larni, video va dumaloq videolarning bir kadrini ko'raman, PDF, Word va matnli fayllarni o'qiyman.",
		"Rasm, stiker va GIF'larni, video va dumaloq videolarning bir kadrini ko'raman, ovozli xabarlarni eshitaman, PDF, Word va matnli fayllarni o'qiyman.",
		"Istalgan joyning vaqtini aytaman, hisoblayman, taqdimot (PowerPoint) va viktorina tayyorlayman, guruhda so'rovnoma tuzaman. Test faylini (.doc, .docx, .pdf, savollar ==== va ++++ bilan, to'g'ri javob # bilan) yuborsangiz, uni 30 talik qismlarga bo'lib, har savolga 30 soniyali test qilib beraman.",
		"So'rasangiz rasm chizaman, istalgan joyning vaqtini aytaman, hisoblayman, taqdimot (PowerPoint) va viktorina tayyorlayman, guruhda so'rovnoma tuzaman. Test faylini (.doc, .docx, .pdf, savollar ==== va ++++ bilan, to'g'ri javob # bilan) yuborsangiz, uni 30 talik qismlarga bo'lib, har savolga 30 soniyali test qilib beraman.",
		"Valyuta kursi, ob-havo, namoz vaqtlari, yangiliklar, Vikipediya va kripto narxlarini, Mobile Legends qahramonlari statistikasini topaman, qo'shiq, kino va video qidiraman, havoladagi maqolani o'qib beraman, Qur'on oyatlarini ma'nosi bilan topaman. Viktorina savoli, qiziq fakt, kitob, retsept, oltin narxi, shaxmat va Formula 1 natijalarini ham topaman. O'qish uchun: aniq hisob (hosila, matritsa, birliklar), ilmiy maqolalar va inglizcha sinonimlar.",
		"Valyuta kursi, ob-havo, namoz vaqtlari, yangiliklar, Vikipediya, futbol natijalari va kripto narxlarini, Mobile Legends qahramonlari statistikasini topaman, qo'shiq, kino va video qidiraman, havoladagi maqolani o'qib beraman, Qur'on oyatlarini ma'nosi bilan topaman. Viktorina savoli, qiziq fakt, kitob, retsept, oltin narxi, shaxmat va Formula 1 natijalarini ham topaman. O'qish uchun: aniq hisob (hosila, matritsa, birliklar), ilmiy maqolalar va inglizcha sinonimlar.",
	},
	"ru": {
		"Вижу фото, стикеры и GIF, кадр из видео и кружочков, читаю PDF, Word и текстовые файлы.",
		"Вижу фото, стикеры и GIF, кадр из видео и кружочков, слушаю голосовые, читаю PDF, Word и текстовые файлы.",
		"Скажу время в любой точке, посчитаю, сделаю презентацию (PowerPoint) и викторину, опрос в группе. Пришлите файл с тестом (.doc, .docx, .pdf: вопросы через ==== и ++++, верный ответ с #), и я разделю его на части по 30 вопросов по 30 секунд.",
		"Нарисую картинку по просьбе, скажу время в любой точке, посчитаю, сделаю презентацию (PowerPoint) и викторину, опрос в группе. Пришлите файл с тестом (.doc, .docx, .pdf: вопросы через ==== и ++++, верный ответ с #), и я разделю его на части по 30 вопросов по 30 секунд.",
		"Найду курсы валют, погоду, время намаза, новости, Википедию и цены криптовалют, статистику героев Mobile Legends, песни, фильмы и видео, прочитаю статью по ссылке, найду аяты Корана с переводом. А ещё вопрос для викторины, интересный факт, книги, рецепты, цену золота, шахматы и Формулу 1. Для учёбы: точные вычисления (производные, матрицы, единицы), научные статьи и английские синонимы.",
		"Найду курсы валют, погоду, время намаза, новости, Википедию, результаты футбола и цены криптовалют, статистику героев Mobile Legends, песни, фильмы и видео, прочитаю статью по ссылке, найду аяты Корана с переводом. А ещё вопрос для викторины, интересный факт, книги, рецепты, цену золота, шахматы и Формулу 1. Для учёбы: точные вычисления (производные, матрицы, единицы), научные статьи и английские синонимы.",
	},
	"en": {
		"I can see photos, stickers and GIFs, a frame of videos and round videos, and read PDF, Word and text files.",
		"I can see photos, stickers and GIFs, a frame of videos and round videos; I listen to voice messages and read PDF, Word and text files.",
		"I can tell the time anywhere, calculate arithmetic, make a presentation (PowerPoint) and a quiz, and a poll in a group. Send a test file (.doc, .docx, .pdf: questions split by ==== and ++++, the right answer marked #) and I will run it in parts of 30, 30 seconds a question.",
		"I can draw a picture when you ask, tell the time anywhere, calculate arithmetic, make a presentation (PowerPoint) and a quiz, and a poll in a group. Send a test file (.doc, .docx, .pdf: questions split by ==== and ++++, the right answer marked #) and I will run it in parts of 30, 30 seconds a question.",
		"I can look up exchange rates, the weather, prayer times, news headlines, Wikipedia and crypto prices, Mobile Legends hero statistics, find songs, films and videos, read an article from a link, and find Quran verses with their meaning. Also trivia questions, fun facts, books, recipes, the gold price, chess and Formula 1 results. For study: exact math (derivatives, matrices, units), scholarly papers and English synonyms.",
		"I can look up exchange rates, the weather, prayer times, news headlines, Wikipedia, football results and crypto prices, Mobile Legends hero statistics, find songs, films and videos, read an article from a link, and find Quran verses with their meaning. Also trivia questions, fun facts, books, recipes, the gold price, chess and Formula 1 results. For study: exact math (derivatives, matrices, units), scholarly papers and English synonyms.",
	},
}

// Abilities are what a deployment may lack: drawing needs the image tool,
// listening needs a provider that hears audio and football needs a
// football-data.org token. The help names only those present, because
// get_bot_help hands it to the model, which would otherwise promise a
// picture, a transcript or a score it cannot give.
type Abilities struct {
	Draw     bool
	Listen   bool
	Football bool
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
	return fmt.Sprintf(helpTexts[lang], pick(0, abilities.Listen), pick(2, abilities.Draw), pick(4, abilities.Football))
}

// The texts of tests sent as files (see quizrun.go).
var (
	textQuizUntitled = localText{uz: "Test", ru: "Тест", en: "Test"}
	textQuizFailed   = localText{
		uz: "Testni saqlab bo'lmadi. Birozdan keyin faylni qayta yuboring.",
		ru: "Не удалось сохранить тест. Пришлите файл чуть позже.",
		en: "The test could not be kept. Send the file again a little later.",
	}
	// textQuizParts takes the title, the questions, the parts and their size.
	textQuizParts = localText{
		uz: "📚 «%s»\n\n✏️ %d ta savol, %d qism (%d tadan).\nQaysi qismdan boshlaymiz?",
		ru: "📚 «%s»\n\n✏️ Вопросов: %d, частей: %d (по %d).\nС какой части начнём?",
		en: "📚 “%s”\n\n✏️ %d questions, %d parts (%d each).\nWhich part shall we start with?",
	}
	textQuizBroken = localText{
		uz: "⚠️ %d ta savolda to'g'ri javob (#) topilmadi yoki bir nechta belgilangan, ular tashlab ketildi.",
		ru: "⚠️ Пропущено вопросов: %d. В них не найден правильный ответ (#) или отмечено несколько.",
		en: "⚠️ Questions left out: %d. Their right answer (#) was not found, or more than one was marked.",
	}
	textQuizMadeOptions = localText{
		uz: "ℹ️ Faylda faqat to'g'ri javoblar bor ekan. Boshqa variantlarni shu testdagi yaqin savollarning javoblaridan oldim.",
		ru: "ℹ️ В файле только правильные ответы. Остальные варианты взяты из ответов на соседние вопросы этого теста.",
		en: "ℹ️ The file gives only the right answers. The other options are the answers to nearby questions in this test.",
	}
	// textQuizPartButton takes the part and its first and last question.
	textQuizPartButton = localText{uz: "%d-qism (%d–%d)", ru: "Часть %d (%d–%d)", en: "Part %d (%d–%d)"}
	textQuizGone       = localText{
		uz: "Bu test topilmadi. Faylni qayta yuboring.",
		ru: "Этот тест не найден. Пришлите файл снова.",
		en: "This test was not found. Send the file again.",
	}
	// textQuizReady takes the title, the part, its questions and the seconds.
	textQuizReady = localText{
		uz: "🎲 «%s», %d-qism\n\n✏️ %d ta savol\n⏱ Har bir savolga %d soniya\n\n🏁 Tayyor bo'lsangiz, tugmani bosing. To'xtatish uchun: /stop",
		ru: "🎲 «%s», часть %d\n\n✏️ Вопросов: %d\n⏱ На каждый вопрос %d секунд\n\n🏁 Когда будете готовы, нажмите кнопку. Остановить: /stop",
		en: "🎲 “%s”, part %d\n\n✏️ %d questions\n⏱ %d seconds for each\n\n🏁 Press the button when you are ready. To stop: /stop",
	}
	textQuizReadyButton = localText{uz: "Men tayyorman!", ru: "Я готов!", en: "I'm ready!"}
	textQuizPaused      = localText{
		uz: "⏸ Ikki savolga javob bo'lmadi, test to'xtatildi. Davom ettirish uchun tugmani bosing.",
		ru: "⏸ Два вопроса остались без ответа, тест приостановлен. Чтобы продолжить, нажмите кнопку.",
		en: "⏸ Two questions went unanswered, so the test is paused. Press the button to go on.",
	}
	textQuizResumeButton = localText{uz: "▶️ Davom etish", ru: "▶️ Продолжить", en: "▶️ Continue"}
	textQuizPickLetter   = localText{uz: "To'g'ri javobni tanlang", ru: "Выберите правильный ответ", en: "Pick the right answer"}
	// textQuizResults takes the title, the part, the answers, the right,
	// wrong and missed ones, and the minutes and seconds taken.
	textQuizResults = localText{
		uz: "🏁 «%s», %d-qism yakunlandi!\n\nSiz %d ta savolga javob berdingiz:\n\n✅ To'g'ri: %d\n❌ Xato: %d\n⌛ Tashlab ketilgan: %d\n⏱ %d daqiqa %d soniya",
		ru: "🏁 «%s», часть %d завершена!\n\nВы ответили на вопросы: %d\n\n✅ Верно: %d\n❌ Неверно: %d\n⌛ Пропущено: %d\n⏱ %d мин %d сек",
		en: "🏁 “%s”, part %d is over!\n\nYou answered %d questions:\n\n✅ Right: %d\n❌ Wrong: %d\n⌛ Missed: %d\n⏱ %d min %d s",
	}
	// textQuizRank takes how many took the part and the place.
	textQuizRank = localText{
		uz: "🏆 %d tadan %d-o'rin. Qayta ishlasangiz, bu o'rin o'zgarmaydi.",
		ru: "🏆 Из %d участников вы на %d-м месте. Повторные попытки его не меняют.",
		en: "🏆 Of %d people, you are number %d. Trying again does not change it.",
	}
	textQuizAgainButton = localText{uz: "🔁 Qayta urinish", ru: "🔁 Ещё раз", en: "🔁 Try again"}
	textQuizNextButton  = localText{uz: "➡️ Keyingi qism", ru: "➡️ Следующая часть", en: "➡️ Next part"}
	textNoQuizToStop    = localText{
		uz: "Hozir ishlayotgan test yo'q. Test faylini (.doc, .docx, .pdf) yuboring, men uni qismlarga bo'lib beraman.",
		ru: "Сейчас нет идущего теста. Пришлите файл с тестом (.doc, .docx, .pdf), я разделю его на части.",
		en: "No test is running. Send a test file (.doc, .docx, .pdf) and I will split it into parts.",
	}
)
