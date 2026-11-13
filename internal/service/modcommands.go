package service

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/tools"
)

// A group's admin moderates as with other group bots, replying to a
// member's message with a command: "/mute 4h spam", "/ban", "/kick", or the
// same in a word or two, the bot's name with them or not: "мут мари 1
// минутга", "ban". It is done without the model, through the moderate
// tool, which checks who may ask and what the bot may do as it does for a
// request in words: the model once asked back instead of muting. /mute and
// /unmute without such a reply still quiet the bot itself.

// modCommands are the commands and the moderate tool's action each does.
var modCommands = map[string]string{
	"/mute": "mute", "/unmute": "unmute", "/ban": "ban", "/unban": "unban", "/kick": "kick", "/del": "delete",
}

// modTimeout bounds one command's action, Telegram's answer included.
const modTimeout = 20 * time.Second

// modRequest is the moderation a group message asks for of the member it
// replies to, with the message as the moderate tool must see it: replying
// to that member's message. A reply to the bot's own answer about a member
// asks it of that member: "Мут бер" to the bot's answer to "mute him".
// ok is false for any other message.
func (s *Service) modRequest(in incoming) (asked incoming, action string, minutes int, reason string, ok bool) {
	if !in.group {
		return in, "", 0, "", false
	}
	asked, ok = s.withModTarget(in)
	if !ok {
		return in, "", 0, "", false
	}
	if action, ok = modCommands[commandName(in.text)]; ok {
		minutes, reason = modArgs(commandArgument(in.text))
		return asked, action, minutes, reason, true
	}
	// The message as written: addressing the bot by a name in the middle
	// takes off the words before it.
	written, _ := in.msg.Content()
	action, minutes, ok = s.modWords(written)
	return asked, action, minutes, "", ok
}

// withModTarget is in replying to the member it is about: the one whose
// message it replies to, or the one the bot's answer it replies to was
// about.
func (s *Service) withModTarget(in incoming) (incoming, bool) {
	if repliesToAMember(in, s.opts.Bot.ID) {
		return in, true
	}
	replied := in.msg.ReplyToMessage
	if replied == nil || !s.isFromBot(replied) {
		return in, false
	}
	member, ok := s.answeredAbout.get([2]int64{in.msg.Chat.ID, replied.MessageID})
	if !ok || member.From == nil || member.From.ID == in.userID {
		return in, false
	}
	msg := *in.msg
	msg.ReplyToMessage = member
	in.msg = &msg
	return in, true
}

// rememberAnsweredAbout notes, for a group message replying to a member's,
// the member's message by the bot's answer to it, so that an order replying
// to the answer reaches them.
func (s *Service) rememberAnsweredAbout(in incoming, sentID int64) {
	if sentID != 0 && in.group && repliesToAMember(in, s.opts.Bot.ID) {
		s.answeredAbout.set([2]int64{in.msg.Chat.ID, sentID}, in.msg.ReplyToMessage)
	}
}

// isModCommand reports a moderation request about a member.
func (s *Service) isModCommand(in incoming) bool {
	_, _, _, _, ok := s.modRequest(in)
	return ok
}

// modCommand does an admin's request to the member they replied to, and
// says what was done, or why it was not.
func (s *Service) modCommand(ctx context.Context, in incoming) outcome {
	in, action, minutes, reason, _ := s.modRequest(in)
	args, err := json.Marshal(map[string]any{"action": action, "minutes": minutes, "reason": reason})
	if err != nil {
		return say(textError.to(in))
	}
	ctx, cancel := context.WithTimeout(ctx, modTimeout)
	defer cancel()
	ctx = tools.WithTurn(ctx, s.caller(in), &tools.Sink{})
	var envelope struct {
		OK     bool `json:"ok"`
		Result struct {
			Said string `json:"said"`
		} `json:"result"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(s.opts.Tools.Execute(ctx, in.userID, "moderate", string(args))), &envelope); err != nil {
		s.opts.Logger.Error("could not read a moderation command's result", "chat_id", in.scope.ChatID, "error", err)
		return say(textError.to(in))
	}
	switch {
	case envelope.OK && envelope.Result.Said != "":
		return say(envelope.Result.Said)
	case envelope.OK:
		return outcome{}
	case envelope.Error.Message != "" && envelope.Error.Code != "execution_failed":
		return say("⛔ " + envelope.Error.Message)
	}
	s.opts.Logger.Warn("a moderation command failed", "chat_id", in.scope.ChatID, "code", envelope.Error.Code)
	return say(textError.to(in))
}

// modDuration reads a mute's length: "30m", "4h", "2d", "1w", or a number
// of minutes, in English, Uzbek or Russian letters.
var modDuration = regexp.MustCompile(`^(\d{1,6})(m|min|daq|daqiqa|м|мин|h|soat|ч|час|d|kun|д|день|дн|w|hafta|н|нед)?$`)

// modUnits are the minutes in each unit modDuration reads.
var modUnits = map[string]int{
	"": 1, "m": 1, "min": 1, "daq": 1, "daqiqa": 1, "м": 1, "мин": 1,
	"h": 60, "soat": 60, "ч": 60, "час": 60,
	"d": 24 * 60, "kun": 24 * 60, "д": 24 * 60, "день": 24 * 60, "дн": 24 * 60,
	"w": 7 * 24 * 60, "hafta": 7 * 24 * 60, "н": 7 * 24 * 60, "нед": 7 * 24 * 60,
}

// modArgs reads a command's argument: a length first, if there is one, in
// minutes, and the rest as the reason.
func modArgs(arg string) (minutes int, reason string) {
	words := strings.Fields(arg)
	if len(words) == 0 {
		return 0, ""
	}
	match := modDuration.FindStringSubmatch(strings.ToLower(words[0]))
	if match == nil {
		return 0, strings.Join(words, " ")
	}
	n, _ := strconv.Atoi(match[1])
	return n * modUnits[match[2]], strings.Join(words[1:], " ")
}

// modWordActions are the words an order in words starts with, and the
// action each asks for, in the languages the groups write: Uzbek in both
// alphabets, Russian, English, Turkish and Kazakh.
var modWordActions = map[string]string{
	// mute
	"mute": "mute", "mut": "mute", "myut": "mute", "silence": "mute", "shush": "mute",
	"мут": "mute", "мют": "mute", "мьют": "mute", "замуть": "mute", "замутить": "mute", "мутни": "mute", "мутануть": "mute",
	"заглуши": "mute", "заткни": "mute", "ovozsiz": "mute", "овозсиз": "mute", "jimla": "mute", "жимла": "mute",
	"sustur": "mute", "susturun": "mute", "үнсіз": "mute", "өшір": "mute",
	// unmute
	"unmute": "unmute", "размут": "unmute", "размуть": "unmute", "размутить": "unmute", "анмут": "unmute",
	"разглуши": "unmute", "susturmayı": "unmute",
	// ban
	"ban": "ban", "бан": "ban", "забань": "ban", "забанить": "ban", "банни": "ban", "заблокируй": "ban",
	"blok": "ban", "блок": "ban", "yasakla": "ban", "banla": "ban", "банла": "ban", "бұғатта": "ban",
	// unban
	"unban": "unban", "разбан": "unban", "разбань": "unban", "разбанить": "unban", "разблокируй": "unban",
	"yasağı": "unban",
	// kick
	"kick": "kick", "кик": "kick", "кикни": "kick", "кикнуть": "kick", "выгони": "kick", "выкинь": "kick",
	"chiqar": "kick", "чиқар": "kick", "чикар": "kick", "hayda": "kick", "ҳайда": "kick", "хайда": "kick",
	"remove": "kick", "шығар": "kick", "қу": "kick",
	// delete the message replied to
	"del": "delete", "delete": "delete", "удали": "delete", "удалить": "delete", "o'chir": "delete", "ochir": "delete", "ўчир": "delete", "sil": "delete",
}

// modFillers are the words an order in words may have besides its action
// and length: "мут бер", "ban qil", "замуть его на 5 минут", "mute him for
// 2 hours".
var modFillers = map[string]bool{
	"бер": true, "ber": true, "qil": true, "қил": true, "кил": true, "qilvor": true, "қилвор": true, "килвор": true,
	"qiling": true, "қилинг": true, "bering": true, "беринг": true,
	"дай": true, "сделай": true, "его": true, "её": true, "ее": true, "их": true, "на": true, "пожалуйста": true,
	"him": true, "her": true, "them": true, "for": true, "please": true,
	"iltimos": true, "илтимос": true, "uni": true, "уни": true, "shuni": true, "шуни": true, "буни": true, "buni": true,
	"onu": true, "lütfen": true, "boyunca": true, "оны": true, "ет": true,
	"kaldır": true,
}

// modWords reads a short order in words: an action, then perhaps a length
// ("1 минутга", "4soat"), with the bot's name or @username anywhere and
// nothing else, so that talk about muting is never taken for an order.
func (s *Service) modWords(text string) (action string, minutes int, ok bool) {
	var words []string
	for _, w := range strings.Fields(strings.ToLower(text)) {
		w = strings.Trim(w, ".,!?:;«»\"'`")
		if w == "" || isWakeWord(w, s.opts.WakeWords) || strings.EqualFold(w, "@"+s.opts.Bot.Username) {
			continue
		}
		words = append(words, w)
	}
	if len(words) == 0 || len(words) > 6 {
		return "", 0, false
	}
	if action, ok = modWordActions[words[0]]; !ok {
		return "", 0, false
	}
	var length []string
	for _, w := range words[1:] {
		if !modFillers[w] {
			length = append(length, w)
		}
	}
	if len(length) > 2 {
		return "", 0, false
	}
	if minutes, ok = wordLength(strings.Join(length, "")); !ok {
		return "", 0, false
	}
	return action, minutes, true
}

// wordLength reads a length said in words, its unit's ending whatever it
// is: "1минутга", "4soat", "2kun", "30" (minutes); "" is none.
var wordLength = func() func(string) (int, bool) {
	pattern := regexp.MustCompile(`^(\d{1,6})(\pL*)$`)
	units := []struct {
		prefixes []string
		minutes  int
	}{
		{[]string{"мин", "min", "daq", "дақ", "дак", "dak"}, 1},
		{[]string{"soat", "соат", "saat", "сағ", "час", "ч", "h"}, 60},
		{[]string{"kun", "кун", "күн", "gün", "дн", "ден", "сут", "d", "д"}, 24 * 60},
		{[]string{"hafta", "ҳафта", "хафта", "hafta", "апта", "нед", "w", "н"}, 7 * 24 * 60},
		{[]string{"m", "м"}, 1},
	}
	return func(said string) (int, bool) {
		if said == "" {
			return 0, true
		}
		match := pattern.FindStringSubmatch(said)
		if match == nil {
			return 0, false
		}
		n, _ := strconv.Atoi(match[1])
		if match[2] == "" {
			return n, true
		}
		for _, unit := range units {
			for _, prefix := range unit.prefixes {
				if strings.HasPrefix(match[2], prefix) {
					return n * unit.minutes, true
				}
			}
		}
		return 0, false
	}
}()
