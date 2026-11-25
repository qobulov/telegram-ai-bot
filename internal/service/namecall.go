package service

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"unicode"

	"telegram-ai-bot/internal/agent"
)

// A message that is nothing but the bot's name, "Mari" or its @username, is
// answered here with a short line, as the owners asked on 2026-11-25: the
// model read about 20 000 tokens of persona, tools and chatter to say
// "Eshitaman" back. A call that replies to a message, or comes with a
// picture or a voice message, still goes to the model: "Mari" under
// someone's message asks about it.

// nameCallAnswer answers a bare call of the bot's name, and reports whether
// it did. It is saved through the agent, so that the update is answered
// once and the model sees the exchange in later turns.
func (s *Service) nameCallAnswer(ctx context.Context, in incoming) (outcome, bool) {
	if in.msg.ReplyToMessage != nil || s.hasMedia(in) || !s.onlyName(in) {
		return outcome{}, false
	}
	lines := nameCallLines
	if s.isOwner(in.msg.From) {
		lines = nameCallOwnerLines
	}
	answer := agent.Answer{Text: lines[rand.IntN(len(lines))].to(in)}
	saved, err := s.opts.Replier.Reply(ctx, agent.Request{
		Scope: in.scope, UserID: in.userID, UpdateID: in.updateID, MessageID: in.msg.MessageID,
		Text: in.text, Fixed: &answer,
	})
	switch {
	case errors.Is(err, agent.ErrDuplicate):
		return outcome{duplicate: true}, true
	case err != nil:
		s.opts.Logger.Warn("could not save a name call's answer; sending it anyway", "user_id", in.userID, "error", err)
		saved = answer
	}
	return outcome{text: saved.Text, answerID: saved.ID}, true
}

// onlyName reports a message whose every word is one of the bot's names or
// its username, with nothing else but punctuation, as "Mari", "mari?" or
// "@MariaBot".
func (s *Service) onlyName(in incoming) bool {
	text, _ := in.msg.Content()
	words := splitWords(text)
	if len(words) == 0 || len(words) > 2 || strings.ContainsAny(text, "/") {
		return false
	}
	for _, w := range words {
		if !isWakeWord(w.text, s.opts.WakeWords) && !(s.opts.Bot.Username != "" && strings.EqualFold(w.text, s.opts.Bot.Username)) {
			return false
		}
		text = strings.Replace(text, w.text, "", 1)
	}
	// What is left may be spaces, "@" and punctuation; an emoji says
	// something the model should answer.
	for _, r := range text {
		if !unicode.IsSpace(r) && !unicode.IsPunct(r) {
			return false
		}
	}
	return true
}

var (
	nameCallLines = []localText{
		{uz: "Eshitaman.", ru: "Слушаю.", en: "I'm listening."},
		{uz: "Labbay?", ru: "Да?", en: "Yes?"},
		{uz: "Shu yerdaman, gapiring.", ru: "Я тут, говори.", en: "I'm here, go on."},
		{uz: "Eshitaman, nima gap?", ru: "Слушаю, что такое?", en: "Listening, what's up?"},
		{uz: "Ha, nima bo'ldi?", ru: "Да, что случилось?", en: "Yeah, what happened?"},
		{uz: "Labbay, eshityapman.", ru: "Да-да, слушаю.", en: "Yes, I'm all ears."},
	}
	nameCallOwnerLines = []localText{
		{uz: "Labbay, master.", ru: "Слушаю, мастер.", en: "Yes, master."},
		{uz: "Eshitaman, master.", ru: "Да, мастер?", en: "Listening, master."},
		{uz: "Shu yerdaman, master.", ru: "Я здесь, мастер.", en: "I'm here, master."},
	}
)
