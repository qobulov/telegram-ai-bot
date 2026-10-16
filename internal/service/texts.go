package service

// Fixed messages the bot sends itself. Answers from the model follow the
// chat's language and the character's voice; these are plain, short and kept
// in English, except the welcome.
const (
	textWelcome = `Salom! Yozing, gaplashamiz.
Привет! Пиши, поболтаем.
Hi! Say something and we'll talk.

/help — commands and usage`

	textAbout = `An AI chat character for Telegram, written in Go.
It answers with an AI model (Gemini or OpenRouter), remembers the conversation, and can check the time and do arithmetic.
Send /reset to delete the conversation history.`

	textResetDone   = "The conversation history here has been deleted."
	textUnknown     = "I don't know that command. Send /help to see what I can do."
	textForbidden   = "This command is restricted to administrators."
	textTextOnly    = "I can't read this kind of message. Send me text, a photo, a video or a voice message instead."
	textBusy        = "I'm handling a lot of messages right now. Please try again in a moment."
	textRateLimited = "You're sending messages too quickly. Please wait a little and try again."
	textMuted       = "Fine, I'll be quiet here. An admin can bring me back with /unmute."
	textUnmuted     = "I'm back."
	textAdminsOnly  = "Only this group's admins can do that."
	textGroupsOnly  = "That command is for groups."
	textTimeout     = "That took too long to answer. Please try again."
	textOverloaded  = "Hozir juda band ekanman, bir daqiqadan keyin qayta yozing."
	textError       = "Something went wrong on my side. Please try again later."
	textTooLong     = "Your message is too long. Please keep it under %d characters."
	textModel       = "Current model: %s"
)

const helpText = `Talk to me and I will answer, in Uzbek, Russian or English, matching your language.

Commands:
/start — welcome message
/help — this help
/reset — delete the conversation history
/model — show the AI model in use
/about — about this bot
/mute, /unmute — group admins: make me stay quiet in a group, or bring me back

In a group, call me by name, mention me (@username) or reply to one of my messages. Commands there need my username, like /help@username. A group shares one conversation with me; /reset there is for the group's admins.

I remember the recent part of our conversation, and I can look up the current time in any timezone and calculate arithmetic expressions.`

// HelpText describes the bot's commands and capabilities. It is shown by
// /help and returned to the model by the get_bot_help tool.
func HelpText() string {
	return helpText
}
