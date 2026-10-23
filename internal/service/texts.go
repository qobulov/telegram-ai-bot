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
	textAbout = `An AI chat character for Telegram, written in Go.
It answers with an AI model (Gemini or Grok), remembers the conversation, and can check the time and do arithmetic.
Send /reset to delete the conversation history.`

	textResetDone  = "The conversation history here has been deleted."
	textUnknown    = "I don't know that command. Send /help to see what I can do."
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

// to is the text in the language of the Telegram app of whoever wrote in.
func (t localText) to(in incoming) string {
	switch appLanguage(in.msg.From) {
	case "ru":
		return t.ru
	case "en":
		return t.en
	}
	return t.uz
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
	textJoinButtonN    = localText{uz: "%d - kanal", ru: "Канал %d", en: "Channel %d"}
	textCheckButton    = localText{uz: "✅ Tasdiqlash", ru: "✅ Проверить", en: "✅ Check"}
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

// helpText is the help with three blanks, filled by HelpText: the sentences
// on what she can see, what she can do and what she can look up.
const helpText = `Talk to me and I will answer, in Uzbek, Russian or English, matching your language.

I remember the recent part of our conversation. %s %s
%s

Commands:
/start — welcome message
/help — this help
/reset — delete the conversation history
/model — show the AI model in use
/about — about this bot

In a group, call me by name, mention me (@username) or reply to one of my messages. Commands there need my username, like /help@username. A group shares one conversation with me.

For a group's admins:
Reply to a message and ask me to delete or pin it, or to mute or ban its author. It takes that admin right, yours and mine, and I act only once you reply yes to my question about it.
/mute@username, /unmute@username — make me stay quiet in the group, or bring me back
/ignore@username, /unignore@username — as a reply to a member's message: I stop answering them for 24 hours, or answer them again
/reset@username — delete the group's conversation with me`

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
// abilities. It is shown by /help and returned to the model by the
// get_bot_help tool.
func HelpText(abilities Abilities) string {
	seeing := "I can see photos, stickers and GIFs, and a frame of videos and round videos."
	if abilities.Listen {
		seeing = "I can see photos, stickers and GIFs, a frame of videos and round videos, and I listen to voice messages."
	}
	skills := "I can tell the time anywhere, calculate arithmetic, and make a poll or a quiz in a group."
	if abilities.Draw {
		skills = "I can draw a picture when you ask, tell the time anywhere, calculate arithmetic, and make a poll or a quiz in a group."
	}
	lookups := "I can look up exchange rates, the weather, prayer times, news headlines, Wikipedia and crypto prices, Mobile Legends hero statistics, and find songs."
	if abilities.Football {
		lookups = "I can look up exchange rates, the weather, prayer times, news headlines, Wikipedia, football results and crypto prices, Mobile Legends hero statistics, and find songs."
	}
	return fmt.Sprintf(helpText, seeing, skills, lookups)
}
