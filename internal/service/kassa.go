package service

import (
	"strings"

	"telegram-ai-bot/internal/telegram"
)

// kassaStart is the /start payload a group's /kassa button sends to open
// Kassa in a private chat, where Telegram lets a Mini App open.
const kassaStart = "kassa"

// kassaCommand answers /kassa, and "/start kassa", with the button that
// opens Kassa, the cash book: itself in a private chat, in a group a link
// to the private chat, as each user's book is their own.
func (s *Service) kassaCommand(in incoming) outcome {
	switch {
	case s.opts.KassaURL == "":
		return say(textUnknown.to(in))
	case !in.group:
		button := telegram.Button{Text: textKassaButton.to(in), WebApp: &telegram.WebAppInfo{URL: s.opts.KassaURL}}
		return outcome{text: textKassa.to(in), buttons: [][]telegram.Button{{button}}}
	case s.opts.Bot.Username != "":
		button := telegram.Button{Text: textKassaButton.to(in), URL: "https://t.me/" + s.opts.Bot.Username + "?start=" + kassaStart}
		return outcome{text: textKassa.to(in), buttons: [][]telegram.Button{{button}}}
	}
	return say(textUnknown.to(in))
}

// isKassa reports /kassa, or the /start a group's Kassa button sends.
func isKassa(in incoming) bool {
	switch commandName(in.text) {
	case "/kassa":
		return true
	case "/start":
		return !in.group && strings.TrimSpace(strings.TrimPrefix(in.text, commandName(in.text))) == kassaStart
	}
	return false
}

var (
	textKassa = localText{
		uz: "💰 Kassa: kirim va chiqimlaringizni yozib boring, qoldiq o'zi hisoblanadi.",
		ru: "💰 Касса: записывайте приходы и расходы, остаток считается сам.",
		en: "💰 Kassa: write down your money in and out, the balance is counted for you.",
	}
	textKassaButton = localText{uz: "💰 Kassani ochish", ru: "💰 Открыть кассу", en: "💰 Open Kassa"}
)
