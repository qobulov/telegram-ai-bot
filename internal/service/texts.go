package service

import "fmt"

// Fixed messages the bot sends itself. Answers from the model follow the
// chat's language and the character's voice; these are plain, short and kept
// in English, except the welcome, the overload notice and the daily limit.
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
	textBusy        = "I'm handling a lot of messages right now. Please try again in a moment."
	textRateLimited = "You're sending messages too quickly. Please wait a little and try again."
	textMuted       = "Fine, I'll be quiet here. An admin can bring me back with /unmute@%s." // the bot's username
	textUnmuted     = "I'm back."
	textAdminsOnly  = "Only this group's admins can do that."
	textGroupsOnly  = "That command is for groups."
	textTimeout     = "That took too long to answer. Please try again."
	textOverloaded  = "Hozir juda band ekanman, bir daqiqadan keyin qayta yozing."
	textError       = "Something went wrong on my side. Please try again later."
	textTooLong     = "Your message is too long. Please keep it under %d characters."
	textModel       = "Current model: %s"
	// textDailyLimit takes the private chats' daily limit three times, once
	// for each language.
	textDailyLimit = "Bugungi limit tugadi: kuniga %d ta xabar. Ertaga yana yozing.\nЛимит на сегодня исчерпан: %d сообщений в день. Напишите завтра.\nThat's today's limit: %d messages a day. Write again tomorrow."

	textIgnored       = "Okay, I won't answer %s for the next 24 hours."
	textUnignored     = "I'll answer %s again."
	textReplyToMember = "Reply to that member's message with this command."
	textRepliedToMe   = "That's my own message, and I can't tell whom it answered. Reply to the member's own message with this command."
	textCannotIgnore  = "I can't ignore admins, my owners or myself."
	textNotIgnored    = "%s wasn't on my ignore list."
)

// textTextOnly answers, in a private chat, a kind of message the bot cannot
// read. Like the help, it offers voice messages only where the bot hears
// them.
func textTextOnly(listen bool) string {
	if listen {
		return "I can't read this kind of message. Send me text, a photo, a video or a voice message instead."
	}
	return "I can't read this kind of message. Send me text, a photo or a video instead."
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
	skills := "I can tell the time anywhere, calculate arithmetic, and make a poll in a group."
	if abilities.Draw {
		skills = "I can draw a picture when you ask, tell the time anywhere, calculate arithmetic, and make a poll in a group."
	}
	lookups := "I can look up exchange rates, the weather, prayer times, news headlines, Wikipedia and crypto prices."
	if abilities.Football {
		lookups = "I can look up exchange rates, the weather, prayer times, news headlines, Wikipedia, football results and crypto prices."
	}
	return fmt.Sprintf(helpText, seeing, skills, lookups)
}
