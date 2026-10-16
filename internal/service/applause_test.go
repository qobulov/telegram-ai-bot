package service

import (
	"testing"

	"telegram-ai-bot/internal/telegram"
)

func withSticker(m *telegram.Message) {
	m.Sticker = &telegram.Sticker{FileID: "sticker", Emoji: "😂"}
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
		"emoji only":                    {"😂😂", nil, true},
		"three emoji":                   {"😂😂😂", nil, true},
		"zero":                          {"0", nil, true},
		"one":                           {"1", nil, true},
		"two letters":                   {"ok", nil, true},
		"a sticker":                     {"", withSticker, true},
		"a GIF without caption":         {"", withGIF, true},
		"a GIF with a laughing caption": {"xaxa", withGIF, true},
		"a question":                    {"nima gap", nil, false},
		"a short question":              {"what?", nil, false},
		"one laugh means yes":           {"haa", nil, false},
		"emoji and words":               {"😂 bu nima", nil, false},
		"a GIF with a question":         {"bu nima?", withGIF, false},
		"a photo without caption":       {"", func(m *telegram.Message) { m.Photo = photo() }, false},
		"a photo with an emoji":         {"😂", func(m *telegram.Message) { m.Photo = photo() }, false},
		"a voice message":               {"", func(m *telegram.Message) { m.Voice = &telegram.Voice{FileID: "voice", Duration: 3} }, false},
		"a video":                       {"", func(m *telegram.Message) { m.Video = &telegram.Video{FileID: "video", Duration: 9} }, false},
		"a poll":                        {"", func(m *telegram.Message) { m.Poll = &telegram.Poll{Question: "pizza?"} }, false},
		"nothing the bot can read":      {"", nil, false},
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

func TestApplauseReaction(t *testing.T) {
	cases := []struct {
		text      string
		messageID int64
		change    func(*telegram.Message)
		want      string
	}{
		{"xaxa", 6, nil, "🤣"},
		{"😂😂", 5, nil, "🤣"},
		{"", 4, withSticker, "👍"},
		{"", 5, withSticker, "🔥"},
		{"", 6, withSticker, "😁"},
		{"", 7, withSticker, "👌"},
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
