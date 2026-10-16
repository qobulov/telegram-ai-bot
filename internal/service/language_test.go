package service

import (
	"strings"
	"testing"
)

func TestReplyLanguage(t *testing.T) {
	cases := map[string]string{
		"bugun havo qanday?":             "Uzbek, in Latin script",
		"nima gap, qalaysan":             "Uzbek, in Latin script",
		"o'zing kimsan o'zi":             "Uzbek, in Latin script",
		"Salom hammaga":                  "Uzbek, in Latin script",
		"rahmat, zo'r chiqibdi":          "Uzbek, in Latin script",
		"bro what qilyapsan":             "Uzbek, in Latin script",
		"hi, yaxshimisiz":                "Uzbek, in Latin script",
		"ok":                             "Uzbek, in Latin script",
		"how are you?":                   "English",
		"What is the capital of France?": "English",
		"hi":                             "English",
		"how much is it":                 "English",
		"thanks, that helps":             "English",
		"Бугун ҳаво қандай?":             "Uzbek, in Cyrillic script",
		"ЎЗИНГ КИМСАН":                   "Uzbek, in Cyrillic script",
		"нима гап":                       "the language of the message, in Cyrillic script",
		"энди нима киласан":              "the language of the message, in Cyrillic script",
		"Привет, как дела?":              "Russian",
		"что это такое":                  "Russian",
		"Мы были здесь":                  "Russian",
		"ищу работу":                     "Russian",
		"а":                              "",
		"😂😂😂":                            "",
		"?!":                             "",
		"5":                              "",
		"これは何":                           "",
		"Привет, how are you? как дела?":  "Russian",
		"salom, how are you? qalesan":     "Uzbek, in Latin script",
		"what is this? nima bu, shunaqa?": "Uzbek, in Latin script",
		"should I learn English first?":   "English",
	}
	for text, want := range cases {
		if got := replyLanguage(text); got != want {
			t.Errorf("replyLanguage(%q) = %q, want %q", text, got, want)
		}
	}
}

// Every turn's context ends with the language to answer the message in.
func TestContextEndsWithTheLanguageToAnswerIn(t *testing.T) {
	cases := map[string]struct {
		update func() *fixture
		want   string
	}{
		"in a group": {func() *fixture {
			f := newFixture()
			f.run(inGroup(groupMessage("Mira, как дела?")))
			return f
		}, "[Answer in Russian.]"},
		"in private": {func() *fixture {
			f := newFixture()
			f.run(textFrom(userID, "Бугун ҳаво қандай?"))
			return f
		}, "[Answer in Uzbek, in Cyrillic script.]"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			context := onlyRequest(t, c.update()).req.Context
			if !strings.HasSuffix(context, "\n\n"+c.want) {
				t.Errorf("context = %q, want it to end with %q", context, c.want)
			}
		})
	}

	f := newFixture()
	f.run(textFrom(userID, "👍"))
	if context := onlyRequest(t, f).req.Context; strings.Contains(context, "[Answer in") {
		t.Errorf("context = %q; a message without words says nothing of its language", context)
	}
}
