package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"telegram-ai-bot/internal/shashkaapp"
	"telegram-ai-bot/internal/telegram"
)

// shashkaCommand answers /shashka with a way into the draughts Mini App.
// In a private chat it is the Mini App's button. A group cannot have one,
// so there the sender's invite is made, and its button is a link to the
// bot's private chat with the invite's code, where startShashka answers
// with the button into that game: for whoever takes it, and for the sender
// too, who is shown their invite waiting.
func (s *Service) shashkaCommand(ctx context.Context, in incoming) outcome {
	if s.opts.ShashkaURL == "" || s.opts.ShashkaInvites == nil || s.opts.Bot.Username == "" {
		return say(textUnknown.to(in))
	}
	if !in.group {
		return s.shashkaButton(in, s.opts.ShashkaURL, textShashkaPrivate)
	}
	invite, err := shashkaapp.NewInvite(ctx, s.opts.ShashkaInvites, in.userID)
	if err != nil {
		s.opts.Logger.Error("could not make a shashka invite", "chat_id", in.scope.ChatID, "user_id", in.userID, "error", err)
		return say(textError.to(in))
	}
	link := shashkaapp.InviteLink(s.opts.Bot.Username, invite.InviteCode)
	return outcome{
		text:    fmt.Sprintf(textShashkaGroup.to(in), doozName(in.msg.From)),
		buttons: [][]telegram.Button{{{Text: textShashkaAccept.to(in), URL: link}}},
	}
}

// startShashka answers "/start sh_<code>", which an invite's link sends,
// with the button into that game; ok is false for any other /start.
func (s *Service) startShashka(in incoming) (outcome, bool) {
	payload := strings.TrimSpace(strings.TrimPrefix(in.text, commandName(in.text)))
	code, ok := strings.CutPrefix(payload, shashkaapp.InvitePrefix)
	if in.group || !ok || code == "" || s.opts.ShashkaURL == "" {
		return outcome{}, false
	}
	return s.shashkaButton(in, s.opts.ShashkaURL+"?join="+url.QueryEscape(code), textShashkaInvited), true
}

// shashkaButton is text with the Mini App's button opening page.
func (s *Service) shashkaButton(in incoming, page string, text localText) outcome {
	button := telegram.Button{Text: textShashkaOpen.to(in), WebApp: &telegram.WebAppInfo{URL: page}}
	return outcome{text: text.to(in), buttons: [][]telegram.Button{{button}}}
}

func init() {
	codeAnswers["shashka"] = func(ctx context.Context, s *Service, in incoming, _ string) outcome {
		return s.shashkaCommand(ctx, in)
	}
}

var (
	textShashkaPrivate = lt(
		"♟ Shashka: tasodifiy raqib, do'stingiz yoki Maria bilan o'ynang. G'alabalar reytingingizni oshiradi.",
		"♟ Шашки: играйте со случайным соперником, с другом или с Марией. Победы поднимают рейтинг.",
		"♟ Draughts: play a random opponent, a friend or Maria. Wins raise your rating.")
	textShashkaGroup = lt(
		"♟ %s shashka o'ynashga chaqiryapti! Kim qabul qiladi? Tugmani bosing, bot o'yinni ochib beradi.",
		"♟ %s зовёт сыграть в шашки! Кто примет вызов? Нажмите кнопку, бот откроет игру.",
		"♟ %s wants to play draughts! Who's in? Press the button and the bot opens the game.")
	textShashkaInvited = lt(
		"♟ Sizni shashkaga chaqirishdi! O'yinga kiring.",
		"♟ Вас позвали в шашки! Откройте игру.",
		"♟ You're invited to draughts! Open the game.")
	textShashkaAccept = lt("⚔️ Qabul qilish", "⚔️ Принять", "⚔️ Accept")
	textShashkaOpen   = lt("♟ Shashka o'ynash", "♟ Играть в шашки", "♟ Play draughts")
)
