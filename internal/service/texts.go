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
	// textBlocked answers a message the model provider's filter refused;
	// textBlockedOwner tells an owner how to post an announcement anyway.
	textBlocked = localText{
		uz: "🚫 Bu xabarga javob bera olmayman: model uni taqiqlangan kontent deb to'xtatdi. Boshqacha yozib ko'ring.",
		ru: "🚫 Не могу ответить на это: модель остановила запрос как запрещённый контент. Попробуйте написать иначе.",
		en: "🚫 I can't answer this: the model stopped it as prohibited content. Try putting it another way.",
	}
	textBlockedOwner = localText{
		uz: "🚫 Gemini bu so'rovni taqiqlangan kontent deb to'xtatdi, javob yoza olmadim. E'lon bo'lsa, o'zim yuboraman: videoga reply qilib /announce hammaga <tavsif> (yoki faqat guruhlarga: /announce <tavsif>).",
		ru: "🚫 Gemini остановил запрос как запрещённый контент, я не смогла ответить. Если это объявление, отправлю сама: ответом на видео /announce всем <описание> (или только в группы: /announce <описание>).",
		en: "🚫 Gemini stopped this as prohibited content, so I couldn't answer. For an announcement, I'll post it myself: in reply to the video, /announce everyone <caption> (or groups only: /announce <caption>).",
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
	// textFreeLimit says the day's free messages are over; it takes when
	// they come back, textBackIn or textBackSoon.
	textFreeLimit = localText{
		uz: "Bugungi bepul xabarlar tugadi 🙂 Yangilari %s, yarim tunda keladi.",
		ru: "Бесплатные сообщения на сегодня закончились 🙂 Новые появятся %s, в полночь.",
		en: "That's all the free messages for today 🙂 New ones arrive %s, at midnight.",
	}
	// textDailyLimit is textFreeLimit for a premium user: it takes their
	// daily limit and when it comes back.
	textDailyLimit = localText{
		uz: "Bugungi %d ta xabaringiz tugadi 🙂 Yangilari %s, yarim tunda keladi.",
		ru: "Ваши %d сообщений на сегодня закончились 🙂 Новые появятся %s, в полночь.",
		en: "That's your %d messages for today 🙂 New ones arrive %s, at midnight.",
	}
	// textBackIn takes the hours until the day's messages come back.
	textBackIn      = localText{uz: "%d soatdan keyin", ru: "через %d ч.", en: "in about %d hours"}
	textBackSoon    = localText{uz: "bir soat ichida", ru: "меньше чем через час", en: "in under an hour"}
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
/ignore@username, /unignore@username — a'zoning xabariga reply qilib: unga 24 soat javob bermayman yoki yana javob beraman
/warn, /unwarn — ogohlantirish (3-sida 1 kunga mut), /ban sabab, /kick — a'zoga reply qilib
/rank 3 (reply) — moderatorga 1–5 daraja berish, /rank 0 — olib tashlash, /rank — darajasini ko'rish; /staff, /ranklog, /access — moderatorlar, kim tayinlagan, buyruqlar darajasi
/banlist, /amnesty, /kickinactive 30 — banlar ro'yxati, hammani bandan chiqarish, faol emaslarni chiqarish
/reports, /warns — ko'rilmagan shikoyatlar, kimda nechta ogohlantirish

Hamma uchun:
/card — kartalar o'yini (har 2 soatda token), /mylink — shaxsiy taklif havolangiz, /invites — kim nechta odam qo'shdi (tugmalar: bugun, hafta, oy, hammasi, o'zingizniki)
/admins — adminlarni chaqirish, /report va /voteban — xabarga reply qilib shikoyat yoki ban uchun ovoz, /rights — huquqlaringiz
/id, /ping, /calc 2+2, /weather Toshkent

Bo'limlar: /help mod, /help kartalar, /help azo`,
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
/ignore@username, /unignore@username — ответом на сообщение участника: не отвечаю ему 24 часа или снова отвечаю
/warn, /unwarn — предупреждение (на третьем мут на сутки), /ban причина, /kick — ответом на сообщение
/rank 3 (reply) — дать модератору ранг 1–5, /rank 0 — снять, /rank — посмотреть ранг; /staff, /ranklog, /access — модераторы, кто назначил, ранги команд
/banlist, /amnesty, /kickinactive 30 — банлист, разбан всех, кик неактивных
/reports, /warns — нерассмотренные жалобы, у кого сколько предупреждений

Для всех:
/card — игра в карты (токен каждые 2 часа), /mylink — ваша личная ссылка-приглашение, /invites — кто сколько добавил (кнопки: сегодня, неделя, месяц, всё, мои)
/admins — позвать админов, /report и /voteban — ответом на сообщение: жалоба или голосование за бан, /rights — ваши права
/id, /ping, /calc 2+2, /weather Ташкент

Разделы: /help mod, /help cards, /help member`,
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
/ignore@username, /unignore@username — as a reply to a member's message: I stop answering them for 24 hours, or answer them again
/warn, /unwarn — a warning (the third mutes for a day), /ban reason, /kick — as a reply
/rank 3 (reply) — give a moderator rank 1–5, /rank 0 — take it away, /rank — see their rank; /staff, /ranklog, /access — moderators, who gave ranks, commands' ranks
/banlist, /amnesty, /kickinactive 30 — the ban list, unban everyone, remove the inactive
/reports, /warns — open reports, who has how many warnings

For everyone:
/card — the card game (a token every 2 hours), /mylink — your own invite link, /invites — who brought how many (buttons: today, week, month, all, mine)
/admins — call the admins, /report and /voteban — as a reply: report, or vote to ban, /rights — what you may do
/id, /ping, /calc 2+2, /weather Tashkent

Sections: /help mod, /help cards, /help member`,
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
		"I can tell the time anywhere, make a poll in a group, recap what you missed there and name its most active members.",
		"I can draw a picture when you ask, tell the time anywhere, make a poll in a group, recap what you missed there and name its most active members.",
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

// helpPages are the games' commands, in each language, listed after /about
// when the pages are on. /stats, for the owners alone, is not listed.
var helpPages = map[string]string{
	"uz": "\n/woodcutter — O'tinchi: daraxtni chop, shoxlardan qoch\n/boss — Dev jangi: guruh bilan birga devni uring\n/mafia — Mafia: guruh bilan, yashirin rollar, 5+ kishi",
	"ru": "\n/woodcutter — «Дровосек»: руби дерево, уворачивайся от веток\n/boss — «Битва с дэвом»: бейте дэва всей группой\n/mafia — «Мафия»: всей группой, тайные роли, от 5 человек",
	"en": "\n/woodcutter — the woodcutter: chop the tree, dodge the branches\n/boss — the boss raid: hit the giant with the whole group\n/mafia — Mafia: with the group, secret roles, 5 or more",
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

// textMuteWhom answers a /mute with no member replied to and no word for
// the bot.
const textMuteWhom = "Kimni mut qilay? A'zoning xabariga reply qilib /mute 1h yozing.\nMeni (botni) jim qilish uchun: /mute bot"

// helpSections are /help's parts, asked for by name: "/help mod".
var helpSections = map[string]localText{
	"mod": {
		uz: `🛡 Moderatsiya (a'zoning xabariga reply qilib yoki @username bilan: /demote @user)
/warn sabab, /unwarn — ogohlantirish; 3-sida 1 kunga mut, 30 kunda o'chadi
/mute 1h, /unmute — mut; /ban sabab, /kick — ban va chiqarish
/delete, /pin, /unpin — xabarni o'chirish, qadash
/promote unvon, /demote — Telegram admin qilish (unvonsiz: «Moderator») va olish; /tag matn — teg (adminga unvon)
/rank 3 yoki /rank 0 (reply) — moderator darajasi 1–5 berish yoki olish; /staff, /ranklog, /access
/banlist — banlar va "bandan chiqarish" tugmalari; /amnesty — hammani chiqarish
/reports — ko'rilmagan shikoyatlar; /warns — kimda nechta ogohlantirish
/kickinactive 30 — 30 kun yozmaganlarni chiqarish
/cleanservice — "kirdi", "chiqdi", "qadadi" xabarlarini o'chirishni yoqish yoki o'chirish
/antispam — flood, spamerlar va yangi a'zolar havolasiga qarshi himoya: tugmalar bilan yoqish yoki o'chirish (odatda yoqilgan)
"orqaga qaytar" — oxirgi amalni bekor qilish
Meni jim qilish: /mute bot, qaytarish: /unmute`,
		ru: `🛡 Модерация (ответом на сообщение участника или с @username: /demote @user)
/warn причина, /unwarn — предупреждение; на третьем мут на сутки, сгорает за 30 дней
/mute 1h, /unmute — мут; /ban причина, /kick — бан и кик
/delete, /pin, /unpin — удалить, закрепить
/promote звание, /demote — дать (без звания: «Moderator») и снять админку Telegram; /tag текст — тег (админу — звание)
/rank 3 или /rank 0 (reply) — дать или снять ранг модератора 1–5; /staff, /ranklog, /access
/banlist — баны и кнопки разбана; /amnesty — разбан всех
/reports — нерассмотренные жалобы; /warns — предупреждения
/kickinactive 30 — кик тех, кто молчит 30 дней
/antispam — защита от флуда, спамеров и ссылок новичков: кнопки вкл/выкл (по умолчанию включена)
«верни» — отменить последнее
Чтобы я молчала: /mute bot, вернуть: /unmute`,
		en: `🛡 Moderation (as a reply to a member's message, or with their @username: /demote @user)
/warn reason, /unwarn — a warning; the third mutes for a day, they lapse in 30 days
/mute 1h, /unmute — mute; /ban reason, /kick — ban and remove
/delete, /pin, /unpin — delete, pin
/promote title, /demote — make (titled "Moderator" by default) or unmake a Telegram admin; /tag text — a tag (an admin's title)
/rank 3 or /rank 0 (reply) — give or take a moderator's rank 1–5; /staff, /ranklog, /access
/banlist — bans with unban buttons; /amnesty — unban everyone
/reports — open reports; /warns — who has warnings
/kickinactive 30 — remove those silent for 30 days
/antispam — the flood, spammer and newcomer-link guards: buttons to switch them (on by default)
"undo" — undo the last action
To quiet me: /mute bot, to bring me back: /unmute`,
	},
	"cards": {
		uz: `🃏 Kartalar
/card — o'yinni ochish: hammasi ilovada
Har 2 soatda 1 token (12 tagacha yig'iladi), 1x yoki 5x ochish, token tugasa kristallga
Ilovada: kolleksiya, bozor (almashish), reyting
💎 Do'kon (ilovada): kristallni Telegram Stars bilan sotib olish; kristallga token va kafolatli paketlar (Epik, Afsonaviy, Mifik)`,
		ru: `🃏 Карты
/card — открыть игру: всё в приложении
Токен каждые 2 часа (копится до 12), открытие 1x или 5x, без токенов — за кристаллы
В приложении: коллекция, рынок (обмен), рейтинг
💎 Магазин (в приложении): кристаллы за Telegram Stars; за кристаллы — токены и паки с гарантией (эпик, легендарка, мифик)`,
		en: `🃏 Cards
/card — open the game: it is all in the app
A token every 2 hours (up to 12 saved), open 1x or 5x, crystals once tokens run out
In the app: the collection, the market (trades), the ranking
💎 Shop (in the app): crystals for Telegram Stars; crystals buy tokens and packs with a sure Epic, Legendary or Mythic`,
	},
	"member": {
		uz: `👥 Hamma uchun
/report — xabarga reply qilib adminlarga shikoyat
/voteban — xabarga reply qilib ban uchun ovoz (5 ta "ha"); boshlagan a'zo bekor qila oladi
/admins — adminlarni chaqirish, /rights — huquqlaringiz
Xabarga reply qilib "+" yoki "rahmat" — reputatsiya; /rep, /toprep — reyting
/mylink — shaxsiy taklif havolangiz, /invites — takliflar reytingi va o'zingizniki
/id, /ping, /calc 2+2, /weather Toshkent`,
		ru: `👥 Для всех
/report — ответом на сообщение: жалоба админам
/voteban — ответом на сообщение: голосование за бан (5 «да»); начавший может отменить
/admins — позвать админов, /rights — ваши права
/mylink — личная ссылка, /invites — рейтинг приглашений и ваш счёт
/id, /ping, /calc 2+2, /weather Ташкент`,
		en: `👥 For everyone
/report — as a reply: report to the admins
/voteban — as a reply: vote to ban (5 "yes"); the one who started may cancel
/admins — call the admins, /rights — what you may do
/mylink — your invite link, /invites — the invite table and your own count
/id, /ping, /calc 2+2, /weather Tashkent`,
	},
}

// helpSectionNames are the words that name each section.
var helpSectionNames = map[string]string{
	"mod": "mod", "moderatsiya": "mod", "admin": "mod", "модерация": "mod", "moderation": "mod",
	"kartalar": "cards", "karta": "cards", "cards": "cards", "карты": "cards",
	"azo": "member", "a'zo": "member", "member": "member", "members": "member", "участник": "member", "hamma": "member",
}
