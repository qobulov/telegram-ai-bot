package service

import (
	"testing"

	"telegram-ai-bot/internal/telegram"
)

func withSticker(m *telegram.Message) {
	m.Sticker = &telegram.Sticker{FileID: "sticker", Emoji: "😂"}
}

// withStickerOf sets a sticker that Telegram gives the emoji.
func withStickerOf(emoji string) func(*telegram.Message) {
	return func(m *telegram.Message) {
		m.Sticker = &telegram.Sticker{FileID: "sticker", Emoji: emoji}
	}
}

// withGIF sets Document as well, as Telegram does on a GIF.
func withGIF(m *telegram.Message) {
	m.Animation = &telegram.Animation{FileID: "gif"}
	m.Document = &telegram.Document{FileID: "gif", MimeType: "video/mp4"}
}

func TestIsApplause(t *testing.T) {
	cases := map[string]struct {
		text   string
		change func(*telegram.Message)
		want   bool
	}{
		"latin laughter":                {"xaxaxa", nil, true},
		"cyrillic laughter":             {"Хахаха", nil, true},
		"laughter and smiles":           {"ахахах)))", nil, true},
		"uzbek cyrillic laughter":       {"ҳаҳаҳа", nil, true},
		"laughing abbreviation":         {"LOL", nil, true},
		"laughter split by a hyphen":    {"ха-ха", nil, true},
		"laughter split by spaces":      {"xa xa xa", nil, true},
		"two laughs apart":              {"ha ha", nil, true},
		"a double x":                    {"хх", nil, true},
		"one laugh and smiles":          {"ха)))", nil, true},
		"one laugh and an emoji":        {"xa 😂", nil, true},
		"a chuckle":                     {"хе-хе", nil, true},
		"a laughing word and laughter":  {"lol xaxa", nil, true},
		"emoji only":                    {"😂😂", nil, true},
		"three emoji":                   {"😂😂😂", nil, true},
		"zero":                          {"0", nil, true},
		"one":                           {"1", nil, true},
		"plus one":                      {"+1", nil, true},
		"ok":                            {"ok", nil, true},
		"a sticker":                     {"", withSticker, true},
		"a GIF without caption":         {"", withGIF, true},
		"a GIF with a laughing caption": {"xaxa", withGIF, true},
		"a question":                    {"nima gap", nil, false},
		"a short question":              {"what?", nil, false},
		"a question mark":               {"?", nil, false},
		"what, in Uzbek":                {"Ha?", nil, false},
		"a time asked":                  {"18:30?", nil, false},
		"a sum":                         {"2+2", nil, false},
		"a long yes":                    {"haa", nil, false},
		// "ha" and "xa" are Uzbek for yes; one syllable is no laughter.
		"yes in Uzbek":          {"ha", nil, false},
		"yes in capitals":       {"Ha", nil, false},
		"yes with an x":         {"xa", nil, false},
		"yes in Cyrillic":       {"Ҳа", nil, false},
		"yes in Russian":        {"Да", nil, false},
		"yes with a mark":       {"ha!", nil, false},
		"no":                    {"no", nil, false},
		"no in Russian":         {"Не", nil, false},
		"laughter and words":    {"xa xa nima", nil, false},
		"emoji and words":       {"😂 bu nima", nil, false},
		"a GIF with a question": {"bu nima?", withGIF, false},
		// Someone sad is not applauding.
		"crying":                   {"😢😢", nil, false},
		"a broken heart":           {"💔", nil, false},
		"a crying sticker":         {"", withStickerOf("😭"), false},
		"a photo without caption":  {"", func(m *telegram.Message) { m.Photo = photo() }, false},
		"a photo with an emoji":    {"😂", func(m *telegram.Message) { m.Photo = photo() }, false},
		"a voice message":          {"", func(m *telegram.Message) { m.Voice = &telegram.Voice{FileID: "voice", Duration: 3} }, false},
		"a video":                  {"", func(m *telegram.Message) { m.Video = &telegram.Video{FileID: "video", Duration: 9} }, false},
		"a poll":                   {"", func(m *telegram.Message) { m.Poll = &telegram.Poll{Question: "pizza?"} }, false},
		"nothing the bot can read": {"", nil, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			msg := replyingTo(groupMessage(tc.text), botID, "answer")
			if tc.change != nil {
				tc.change(msg)
			}
			if got := isApplause(msg, tc.text); got != tc.want {
				t.Errorf("isApplause(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

// When the bot asked something, a number is the answer: "Nechi yoshdasan?"
// gets "25". To a statement, "0" and "1" are the baits of the audit.
func TestNumberAnsweringTheBotsQuestionIsNoApplause(t *testing.T) {
	cases := map[string]struct {
		text, asked string
		want        bool
	}{
		"a number to a question":    {"25", "Nechi yoshdasan?", false},
		"a digit to a question":     {"0", "Nechta qoldi?", false},
		"a digit to a statement":    {"0", "Sen hali ham shu yerdamisan.", true},
		"laughter to a question":    {"xaxaxa", "Tayyormisan?", true},
		"a thumbs up to a question": {"👍", "Tayyormisan?", true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			msg := replyingTo(groupMessage(tc.text), botID, tc.asked)
			if got := isApplause(msg, tc.text); got != tc.want {
				t.Errorf("isApplause(%q) after %q = %v, want %v", tc.text, tc.asked, got, tc.want)
			}
		})
	}
}

func TestApplauseReaction(t *testing.T) {
	cases := []struct {
		text      string
		messageID int64
		change    func(*telegram.Message)
		want      string
	}{
		{"xaxa", 6, nil, "🤣"},
		{"ха-ха", 7, nil, "🤣"},
		{"😂😂", 5, nil, "🤣"},
		{"", 5, withSticker, "🤣"}, // a 😂 sticker laughs
		{"", 5, withStickerOf("🤣"), "🤣"},
		{"", 4, withStickerOf("😺"), "👍"},
		{"", 5, withStickerOf("😺"), "🔥"},
		{"", 6, withStickerOf("😺"), "😁"},
		{"", 7, withStickerOf("😺"), "👌"},
		{"ok", 8, nil, "👍"},
		{"!", -3, nil, "🔥"}, // never sent by Telegram; must still not panic
	}
	for _, tc := range cases {
		msg := groupMessage(tc.text)
		msg.MessageID = tc.messageID
		if tc.change != nil {
			tc.change(msg)
		}
		got := applauseReaction(msg, tc.text)
		if got != tc.want {
			t.Errorf("reaction to %q, message %d = %s, want %s", tc.text, tc.messageID, got, tc.want)
		}
		if !telegram.IsReactionEmoji(got) {
			t.Errorf("%s is not a reaction Telegram allows", got)
		}
	}
}
