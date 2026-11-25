package service

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"telegram-ai-bot/internal/agent"
)

// A short praise of the bot, "Молодец" or "zo'rsan" in reply to it, is
// answered here with one of its own GIFs, as the owners asked on 2026-11-25:
// left to the model, half the praises got words instead. Only a message of
// praise words alone (and the bot's name, emoji or punctuation), at most
// praiseMostWords of them, replying to the bot or calling it, counts:
// anything longer, or praise of someone else, goes to the model.
const praiseMostWords = 4

// praiseWords are the words of a praise, folded to lower case.
var praiseWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`zo'r zor zo'rsan zorsan zo'rsiz zorsiz zo'r-ku barakalla barakallo malades molodes molodets maladets
		qoyil qoyilmaqom aqlli aqllisan aqllisiz ofarin rahmat raxmat yashavor yashang super kuchli ajoyib klass klassno imba
		молодец молодчина умница умничка класс красава супер браво спасибо
		good nice great smart awesome amazing thanks the best eng zo'risiz zo'risan zorisan zorisiz zo'risiz`) {
		praiseWords[w] = true
	}
	// a thanks alone is not praise enough to be answered with a GIF
	for _, w := range []string{"rahmat", "raxmat", "спасибо", "thanks"} {
		delete(praiseWords, w)
	}
}

// praiseAnswer answers a short praise of the bot with one of its GIFs, and
// reports whether it did; saved through the agent, as a name call's line.
func (s *Service) praiseAnswer(ctx context.Context, in incoming) (outcome, bool) {
	if len(s.opts.PraiseGIFs) == 0 || s.hasMedia(in) || !s.toBot(in) || !s.onlyPraise(in) {
		return outcome{}, false
	}
	answer := agent.Answer{GIF: PraiseGIF}
	saved, err := s.opts.Replier.Reply(ctx, agent.Request{
		Scope: in.scope, UserID: in.userID, UpdateID: in.updateID, MessageID: in.msg.MessageID,
		Text: in.text, Fixed: &answer,
	})
	switch {
	case errors.Is(err, agent.ErrDuplicate):
		return outcome{duplicate: true}, true
	case err != nil:
		s.opts.Logger.Warn("could not save a praise's GIF; sending it anyway", "user_id", in.userID, "error", err)
		saved = answer
	}
	return outcome{gif: PraiseGIF, answerID: saved.ID}, true
}

// toBot reports a message meant for the bot alone: a reply to one of its
// messages, or one calling it by name, in a private chat any.
func (s *Service) toBot(in incoming) bool {
	if !in.group {
		return true
	}
	return s.isFromBot(in.msg.ReplyToMessage) || in.trigger == triggerWakeWord || in.trigger == triggerMention
}

// onlyPraise reports a message of praise words alone, at most
// praiseMostWords of them, with the bot's name, emoji and punctuation.
func (s *Service) onlyPraise(in incoming) bool {
	text, _ := in.msg.Content()
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\'' && r != 'ʻ' && r != '’' && r != '-'
	})
	praised := 0
	for _, w := range words {
		w = strings.NewReplacer("ʻ", "'", "’", "'").Replace(strings.Trim(w, "-'"))
		switch {
		case w == "":
		case praiseWords[w]:
			praised++
		case isWakeWord(w, s.opts.WakeWords), s.opts.Bot.Username != "" && strings.EqualFold(w, s.opts.Bot.Username), w == "apa", w == "siz", w == "sen", w == "san", w == "ты", w == "you", w == "are", w == "bot", w == "бот":
		default:
			return false
		}
	}
	return praised > 0 && praised <= praiseMostWords && len(words) <= praiseMostWords+2
}
