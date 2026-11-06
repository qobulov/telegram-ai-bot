package tools

import (
	"context"
	"encoding/json"
	"errors"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

const (
	notCaptchaKeeper = "only the bot's owners, this group's creator and its admins who may invite users may turn the join check on or off; say so"
	captchaInGroup   = "this works only in the group whose join check it is"
	notAskedCaptcha  = "their own message does not ask to turn the join check on or off, so nothing was done; do it only when they ask in words, such as \"kapchani yoq\" or \"qo'shilish tekshiruvini o'chir\""
	captchaOffNote   = "Off within a minute: join requests go back to the group's admins, who answer them by hand; a request still waiting is left to them, not declined."
)

// captchaWords name the join check, written as says folds a message: "kapcha
// yoq", "qo'shilmoqchilarni tekshir", "заявки на вступление проверяй".
var captchaWords = []string{"captcha", "kapcha", "kaptcha", "капч", "join", "qoshil", "kirish", "sorov", "tekshir",
	"заявк", "вступ", "проверк", "human", "odamman", "robot", "робот"}

// captchaOffWords ask for it off: "o'chir", "kerak emas", "выключи". A
// bare "yo'q" is not one: "yoq" is also how "yoqib qo'y" (turn it on)
// starts.
var captchaOffWords = []string{"ochir", "uchir", "ўчир", "olib tashla", "yoq qil", "kerak emas", "kerakmas", "kerak yoq", "bekor", "toxtat", "off", "stop",
	"выключ", "отключ", "убер", "убра", "не нуж", "не надо", "отмен"}

// RightChecker asks Telegram whether a member holds an admin right in a
// group; its creator holds every one.
type RightChecker interface {
	HoldsRight(ctx context.Context, chatID, userID int64, right string) (bool, error)
}

// JoinCaptchaOptions configures the join_captcha tool. Both are required.
type JoinCaptchaOptions struct {
	Store  RuleStore
	Rights RightChecker
}

type joinCaptchaArgs struct {
	Action string `json:"action"`
}

// JoinCaptchaTool lets those who answer a group's join requests by hand
// hand them to the bot or take them back: the bot's owners, the group's
// creator and its admins who may invite users, asked of Telegram each
// time. It turns the joins guard of group_rules on and off, and nothing
// else; the other guards stay the owners'.
func JoinCaptchaTool(opts JoinCaptchaOptions) Tool {
	return Tool{
		Name: "join_captcha",
		Description: "The join check of this group, run in code without you: everyone who asks to join gets a button in a private message, is let in when they press it within 10 minutes and declined otherwise. " +
			"The bot's owners, this group's creator and its admins who may invite users only. status: whether it is on; on, off: turn it on or off when one of them asks (\"kapchani yoq\", \"qo'shilish tekshiruvini o'chir\"). Never say it is on or off unless this tool reported it.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["status", "on", "off"]}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		GroupOnly: true,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return runJoinCaptcha(ctx, opts, raw)
		},
	}
}

func runJoinCaptcha(ctx context.Context, opts JoinCaptchaOptions, raw json.RawMessage) (any, error) {
	var args joinCaptchaArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, ok := CallerFrom(ctx)
	if !ok || caller.ChatID >= 0 {
		return nil, &DeniedError{Message: captchaInGroup}
	}
	if err := mayKeepCaptcha(ctx, opts, caller); err != nil {
		return nil, err
	}
	switch args.Action {
	case "status":
		on, err := captchaOn(ctx, opts, caller.ChatID)
		if err != nil {
			return nil, err
		}
		return map[string]bool{"on": on}, nil
	case "on":
		if !says(caller.Words, captchaWords) {
			return nil, &DeniedError{Message: notAskedCaptcha}
		}
		return captchaSetOn(ctx, opts, caller)
	case "off":
		if !says(caller.Words, captchaWords) || !says(caller.Words, captchaOffWords) {
			return nil, &DeniedError{Message: notAskedCaptcha}
		}
		return captchaSetOff(ctx, opts, caller.ChatID)
	}
	return nil, &ArgumentError{Message: "action must be status, on or off"}
}

// mayKeepCaptcha lets in the bot's owners and whoever Telegram says may
// invite users here, which the creator always may. When Telegram cannot
// say, nobody else is let in.
func mayKeepCaptcha(ctx context.Context, opts JoinCaptchaOptions, caller Caller) error {
	if caller.Owner {
		return nil
	}
	holds, err := opts.Rights.HoldsRight(ctx, caller.ChatID, caller.UserID, telegram.RightInviteUsers)
	if err != nil {
		return errors.New("could not learn from Telegram whether they may invite users here; try again later")
	}
	if !holds {
		return &DeniedError{Message: notCaptchaKeeper}
	}
	return nil
}

func captchaOn(ctx context.Context, opts JoinCaptchaOptions, chatID int64) (bool, error) {
	rules, err := captchaRules(ctx, opts, chatID)
	return len(rules) > 0, err
}

// captchaSetOn keeps the group's joins guard; a group has one, and turning
// it on again replaces it.
func captchaSetOn(ctx context.Context, opts JoinCaptchaOptions, caller Caller) (any, error) {
	_, err := opts.Store.SetGroupRule(ctx, storage.GroupRule{ChatID: caller.ChatID, Kind: storage.RuleJoinCaptcha, SetBy: caller.UserID})
	if err != nil {
		return nil, err
	}
	return map[string]any{"on": true, "note": guardNotes[storage.RuleJoinCaptcha]}, nil
}

func captchaSetOff(ctx context.Context, opts JoinCaptchaOptions, chatID int64) (any, error) {
	rules, err := captchaRules(ctx, opts, chatID)
	if err != nil {
		return nil, err
	}
	for _, r := range rules {
		if _, err := opts.Store.RemoveGroupRule(ctx, chatID, r.ID); err != nil {
			return nil, err
		}
	}
	return map[string]any{"on": false, "was_on": len(rules) > 0, "note": captchaOffNote}, nil
}

// captchaRules are the group's own joins guard rules, never another
// group's or every group's.
func captchaRules(ctx context.Context, opts JoinCaptchaOptions, chatID int64) ([]storage.GroupRule, error) {
	rules, err := opts.Store.GroupRules(ctx, chatID)
	if err != nil {
		return nil, err
	}
	var captchas []storage.GroupRule
	for _, r := range rules {
		if r.ChatID == chatID && r.Kind == storage.RuleJoinCaptcha {
			captchas = append(captchas, r)
		}
	}
	return captchas, nil
}
