package service

// Fixed messages the bot sends itself. Answers from the model follow the
// user's language; these are short and kept in English, except the welcome.
const (
	textWelcome = `Assalomu alaykum! Men sun'iy intellekt yordamchisiman. Savolingizni yozing.
Здравствуйте! Я ИИ-ассистент. Напишите свой вопрос.
Hello! I am an AI assistant. Send me your question.

/help — commands and usage`

	textAbout = `An AI assistant for Telegram, written in Go.
It answers through OpenRouter, remembers your conversation, and can check the time and do arithmetic.
Send /reset at any time to delete your conversation history.`

	textResetDone   = "Your conversation history has been deleted."
	textUnknown     = "I don't know that command. Send /help to see what I can do."
	textForbidden   = "This command is restricted to administrators."
	textTextOnly    = "I can only read text messages for now."
	textBusy        = "I'm handling a lot of messages right now. Please try again in a moment."
	textRateLimited = "You're sending messages too quickly. Please wait a little and try again."
	textTimeout     = "That took too long to answer. Please try again."
	textError       = "Something went wrong on my side. Please try again later."
	textTooLong     = "Your message is too long. Please keep it under %d characters."
	textModel       = "Current model: %s"
)

const helpText = `Send me any message and I will answer it. I reply in Uzbek, Russian or English, matching your language.

Commands:
/start — welcome message
/help — this help
/reset — delete your conversation history
/model — show the AI model in use
/about — about this bot

I remember the recent part of our conversation, and I can look up the current time in any timezone and calculate arithmetic expressions.`

// HelpText describes the bot's commands and capabilities. It is shown by
// /help and returned to the model by the get_bot_help tool.
func HelpText() string {
	return helpText
}
