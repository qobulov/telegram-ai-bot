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
// replies to: a command, or a short order in words; ok false for any other
// message.
func (s *Service) modRequest(in incoming) (action string, minutes int, reason string, ok bool) {
	if !in.group || !repliesToAMember(in, s.opts.Bot.ID) {
		return "", 0, "", false
	}
	if action, ok = modCommands[commandName(in.text)]; ok {
		minutes, reason = modArgs(commandArgument(in.text))
		return action, minutes, reason, true
	}
	// The message as written: addressing the bot by a name in the middle
	// takes off the words before it.
	written, _ := in.msg.Content()
	action, minutes, ok = s.modWords(written)
	return action, minutes, "", ok
}

// isModCommand reports a moderation request replying to a member's message.
func (s *Service) isModCommand(in incoming) bool {
	_, _, _, ok := s.modRequest(in)
	return ok
}

// modCommand does an admin's request to the member they replied to, and
// says what was done, or why it was not.
func (s *Service) modCommand(ctx context.Context, in incoming) outcome {
	action, minutes, reason, _ := s.modRequest(in)
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
// action each asks for.
var modWordActions = map[string]string{
	"mute": "mute", "mut": "mute", "myut": "mute", "мут": "mute", "мют": "mute",
	"unmute": "unmute", "размут": "unmute", "анмут": "unmute",
	"ban": "ban", "бан": "ban", "unban": "unban", "разбан": "unban",
	"kick": "kick", "кик": "kick",
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
	if len(words) == 0 || len(words) > 3 {
		return "", 0, false
	}
	if action, ok = modWordActions[words[0]]; !ok {
		return "", 0, false
	}
	if minutes, ok = wordLength(strings.Join(words[1:], "")); !ok {
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
		{[]string{"мин", "min", "daq", "дак"}, 1},
		{[]string{"soat", "час", "ч", "h"}, 60},
		{[]string{"kun", "дн", "ден", "сут", "d", "д"}, 24 * 60},
		{[]string{"hafta", "нед", "w", "н"}, 7 * 24 * 60},
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
