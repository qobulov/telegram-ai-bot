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
// member's message with a command: "/mute 4h spam", "/ban", "/kick". It is
// done without the model, through the moderate tool, which checks who may
// ask and what the bot may do as it does for a request in words. /mute and
// /unmute without such a reply still quiet the bot itself.

// modCommands are the commands and the moderate tool's action each does.
var modCommands = map[string]string{
	"/mute": "mute", "/unmute": "unmute", "/ban": "ban", "/unban": "unban", "/kick": "kick", "/del": "delete",
}

// modTimeout bounds one command's action, Telegram's answer included.
const modTimeout = 20 * time.Second

// isModCommand reports a moderation command replying to a member's message.
func (s *Service) isModCommand(in incoming) bool {
	_, ok := modCommands[commandName(in.text)]
	return ok && in.group && repliesToAMember(in, s.opts.Bot.ID)
}

// modCommand does an admin's command to the member they replied to, and
// says what was done, or why it was not.
func (s *Service) modCommand(ctx context.Context, in incoming) outcome {
	minutes, reason := modArgs(commandArgument(in.text))
	args, err := json.Marshal(map[string]any{"action": modCommands[commandName(in.text)], "minutes": minutes, "reason": reason})
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
