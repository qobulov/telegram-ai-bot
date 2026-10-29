package service

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// A few requests are so common and so plain that they are answered as the
// keyboard's buttons are, in code and without the model, when typed in a
// private chat: the rate of a currency, the weather somewhere, the time.
// Only a message that is nothing but such a request is taken, word by
// word: "dollar qancha", "evro kursi", "Samarqand ob-havosi", "soat nechi".
// Anything more, "ertaga ob-havo qanday, nima kiyay", is the model's.

// typedRequest is a plain request recognised in a message: which, and its
// one argument.
type typedRequest struct {
	kind string // "rate", "weather" or "time"
	// code is the currency of a rate, city the place of the weather.
	code, city string
}

// currencyWords name a currency, by its code.
var currencyWords = map[string]string{
	"dollar": "USD", "dollari": "USD", "dollarni": "USD", "usd": "USD", "доллар": "USD", "доллара": "USD", "долларов": "USD", "бакс": "USD",
	"evro": "EUR", "yevro": "EUR", "euro": "EUR", "eur": "EUR", "евро": "EUR",
	"rubl": "RUB", "rubli": "RUB", "rub": "RUB", "рубль": "RUB", "рубля": "RUB", "рублей": "RUB",
	"tenge": "KZT", "тенге": "KZT", "yuan": "CNY", "юань": "CNY", "юаня": "CNY",
}

var (
	rateWords    = []string{"kurs", "kursi", "kursini", "курс", "курса", "rate"}
	weatherWords = []string{"obhavo", "obhavosi", "obhavoni", "havo", "погода", "погоду", "погоде", "weather"}
	timeWords    = []string{"soat", "vaqt", "час", "времени", "time"}
	// fillers ask, or say when, without asking anything more.
	fillers = []string{
		"qancha", "necha", "nechi", "nechchi", "nechpul", "bugun", "bugungi", "hozir", "hozirgi", "qanday", "qanaqa", "boldi",
		"bilan", "endi", "mari", "maria", "iltimos", "ayt", "aytchi", "ayting", "1", "bir", "som", "sum",
		"сколько", "стоит", "сегодня", "сейчас", "какая", "какой", "который", "в", "скажи", "мари",
		"what", "is", "the", "it", "now", "today", "how", "much", "in", "please",
	}
	// laterWords ask about another day, which the plain answers do not
	// give: the model does.
	laterWords = []string{"ertaga", "kecha", "indinga", "завтра", "вчера", "tomorrow", "yesterday", "hafta", "неделю"}
)

// typedRequestOf is the plain request a private message is, if it is one.
func typedRequestOf(text string) (typedRequest, bool) {
	words := requestWords(text)
	if len(words) == 0 || len(words) > 5 || slices.ContainsFunc(words, func(w string) bool { return slices.Contains(laterWords, w) }) {
		return typedRequest{}, false
	}
	var req typedRequest
	var others []string
	for _, w := range words {
		code, isCurrency := currencyWords[w]
		switch {
		case isCurrency:
			req.code = code
		case slices.Contains(rateWords, w):
			req.kind = setKind(req.kind, "rate")
		case slices.Contains(weatherWords, w):
			req.kind = setKind(req.kind, "weather")
		case slices.Contains(timeWords, w):
			req.kind = setKind(req.kind, "time")
		case slices.Contains(fillers, w):
		default:
			others = append(others, w)
		}
	}
	if req.code != "" && (req.kind == "" || req.kind == "rate") {
		req.kind = "rate"
	}
	switch {
	case req.kind == "rate" && len(others) == 0:
		req.code = cmp.Or(req.code, "USD")
		return req, true
	case req.kind == "weather" && req.code == "" && len(others) <= 1:
		req.city = "Toshkent"
		if len(others) == 1 {
			req.city = cityOf(others[0])
		}
		return req, true
	case req.kind == "time" && req.code == "" && len(others) == 0:
		return req, true
	}
	return typedRequest{}, false
}

// setKind keeps the first kind a message names; a second makes it none of
// them, "invalid".
func setKind(have, kind string) string {
	if have == "" || have == kind {
		return kind
	}
	return "invalid"
}

// requestWords are a message's words in lower case, without punctuation or
// apostrophes, and "ob-havo" as one word.
func requestWords(text string) []string {
	text = strings.Map(oneApostrophe, strings.ToLower(text))
	text = strings.NewReplacer("'", "", "-", "", "ob havo", "obhavo").Replace(text)
	return strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// cityOf is the place a word names, without the endings a sentence gives
// it: "samarqandda" is Samarqand.
func cityOf(word string) string {
	for _, ending := range []string{"dagi", "da", "ning"} {
		if trimmed, ok := strings.CutSuffix(word, ending); ok && len([]rune(trimmed)) >= 3 {
			word = trimmed
			break
		}
	}
	runes := []rune(word)
	return fmt.Sprintf("%c%s", unicode.ToUpper(runes[0]), string(runes[1:]))
}
