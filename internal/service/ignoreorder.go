package service

import (
	"context"
	"slices"
	"strings"
	"unicode"

	"telegram-ai-bot/internal/tools"
)

// ignoreOrderReaction acknowledges an owner's order not to answer a member.
const ignoreOrderReaction = "👌"

// obeyedIgnoreOrder saves an owner's order not to answer a member, given in
// a group as a reply to that member's message, as /ignore does, and reports
// whether it did. The model is not asked: told "don't answer this", it often
// stayed silent once instead of saving the order. Anything else, or an order
// about someone /ignore would refuse, goes to the model as before.
func (s *Service) obeyedIgnoreOrder(ctx context.Context, in incoming) bool {
	member := s.ignoreOrderTarget(ctx, in)
	if member == nil {
		return false
	}
	if err := s.saveIgnore(ctx, in, member); err != nil {
		s.reply(in, textError.to(in))
	} else {
		s.react(in, ignoreOrderReaction)
	}
	s.logFrom(in.msg, in.text)
	return true
}

// ignoreOrderTarget is the member an owner's message orders the bot not to
// answer, or nil when it is no such order. The member is the human author of
// the message it replies to, whom /ignore could ignore.
func (s *Service) ignoreOrderTarget(ctx context.Context, in incoming) *tools.Target {
	replied := in.msg.ReplyToMessage
	if !in.group || s.opts.Rules == nil || !s.isOwner(in.msg.From) || strings.HasPrefix(in.text, "/") ||
		replied == nil || replied.From == nil || replied.From.IsBot || replied.SenderChat != nil || replied.IsService() ||
		!isIgnoreOrder(in.text, s.opts.WakeWords) {
		return nil
	}
	member := s.target(replied)
	if s.refuseUnignorable(ctx, in, member) != "" {
		return nil
	}
	return member
}

// isOthersIgnoreOrder reports whether someone other than an owner, in a
// group, replied to a message with nothing but an order not to answer its
// author. Only owners decide whom the bot ignores, so the order is passed
// over: any answer, even a refusal, would repeat it to the group.
func (s *Service) isOthersIgnoreOrder(in incoming) bool {
	return in.group && in.msg.ReplyToMessage != nil && !s.isOwner(in.msg.From) &&
		!strings.HasPrefix(in.text, "/") && isIgnoreOrder(in.text, s.opts.WakeWords)
}

// ignoreOrders are the orders not to answer someone, folded as isIgnoreOrder
// folds a message.
var ignoreOrders = map[string]bool{
	"javob berma": true, "javob bermang": true, "javob qaytarma": true, "gapirma": true, "gaplashma": true,
	"ignor qil": true, "ignore qil": true, "ignore": true, "etibor berma": true,
	"жавоб берма": true, "жавоб берманг": true, "жавоб кайтарма": true, "гапирма": true, "гаплашма": true,
	"игнор кил": true, "этибор берма": true,
	"не отвечай": true, "игнорируй": true, "не разговаривай": true,
}

// orderFillers are the words an order may carry besides the order itself:
// whom it means, and how firmly.
var orderFillers = map[string]bool{
	"bunga": true, "unga": true, "shunga": true, "shu": true, "bu": true, "u": true, "odamga": true, "bilan": true,
	"buni": true, "uni": true, "shuni": true, "odamni": true, "ushbu": true,
	"endi": true, "boshqa": true, "hech": true, "umuman": true, "iltimos": true, "him": true, "her": true, "this": true,
	"бунга": true, "унга": true, "шунга": true, "шу": true, "бу": true, "у": true, "одамга": true, "билан": true,
	"буни": true, "уни": true, "шуни": true, "одамни": true, "ушбу": true,
	"энди": true, "бошка": true, "хеч": true, "умуман": true, "илтимос": true,
	"ему": true, "ей": true, "его": true, "ее": true, "этому": true, "этой": true, "этого": true, "с": true, "ним": true, "ней": true,
	"больше": true, "вообще": true, "пожалуйста": true,
}

// orderFolding drops apostrophes ("e'tibor") and the hard sign, and writes
// Uzbek Cyrillic letters as the Russian ones people often type for them.
var orderFolding = strings.NewReplacer(
	"'", "", "’", "", "‘", "", "ʻ", "", "ʼ", "", "`", "", "ъ", "",
	"қ", "к", "ғ", "г", "ў", "у", "ҳ", "х", "ё", "е",
)

// isIgnoreOrder reports whether text, what an owner said to the bot, is
// nothing but an order not to answer someone: "bunga javob berma", "не
// отвечай ему". Words saying whom or how firmly, and the bot's wake words,
// may come with it; any other word, as in "nega javob bermading?", makes it
// something for the model to read.
func isIgnoreOrder(text string, wakeWords []string) bool {
	words := strings.FieldsFunc(orderFolding.Replace(strings.ToLower(text)), func(r rune) bool { return !unicode.IsLetter(r) })
	words = slices.DeleteFunc(words, func(w string) bool { return orderFillers[w] || slices.Contains(wakeWords, w) })
	return ignoreOrders[strings.Join(words, " ")]
}
