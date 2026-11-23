package service

import (
	"strings"

	"telegram-ai-bot/internal/tools"
	"unicode"
	"unicode/utf8"
)

// languageHint tells the model, right before the message, which language
// and script to answer it in, as replyLanguage reads them; "" when the
// message has too few letters to tell. The persona asks the same, but the
// model drifted into the language of the chat around the message.
func languageHint(text string) string {
	language := replyLanguage(text)
	switch language {
	case "":
	case latinUzbek:
		// The model drifted to Cyrillic in groups that write it.
		return "[Answer in " + language + ", as they wrote, even where the chat or your earlier answers use Cyrillic.]"
	default:
		return "[Answer in " + language + ".]"
	}
	return ""
}

// replyLanguage reads the language and script of text from its letters and
// common words: Uzbek Cyrillic by its own letters, Russian by letters or
// words Uzbek does not have, English by common words and nothing Uzbek, and
// Uzbek Latin otherwise. The script is the one most words are written in,
// so names typed in Latin letters, such as ChatGPT, do not turn a Russian
// message Uzbek. It is "" for fewer than two Latin or Cyrillic letters.
func replyLanguage(text string) string {
	lower := strings.ToLower(text)
	latin, cyrillic := 0, 0
	for _, r := range lower {
		switch {
		case unicode.Is(unicode.Latin, r):
			latin++
		case unicode.Is(unicode.Cyrillic, r):
			cyrillic++
		}
	}
	words := strings.FieldsFunc(strings.Map(oneApostrophe, lower), func(r rune) bool { return !unicode.IsLetter(r) && r != '\'' })
	// Э and ъ are Uzbek Cyrillic letters too; ы and щ are Russian only.
	russian := strings.ContainsAny(lower, "ыщ") || hasAny(words, russianWords)
	switch {
	case latin+cyrillic < 2:
		return ""
	case strings.ContainsAny(lower, "ўқғҳ"):
		return "Uzbek, in Cyrillic script"
	case !strings.ContainsAny(lower, "ыщ") && uzbekCyrillicCount(words) > 0 && uzbekCyrillicCount(words) >= countIn(words, russianWords):
		// Uzbek typed in Cyrillic without its own letters, with a Russian
		// "да" or "не" in it, as "Да айтма" and "Не блдик нмалигини" were,
		// which got Russian answers on 2026-11-22.
		return "Uzbek, in Cyrillic script"
	case mostlyCyrillic(words, cyrillic > latin || russian) && russian:
		return "Russian"
	case mostlyCyrillic(words, cyrillic > latin):
		return "the language of the message, in Cyrillic script"
	case looksEnglish(words):
		return "English"
	}
	return latinUzbek
}

// toolLanguage is the language tools answer the writer of in in, as
// languageOf reads it, with Uzbek typed in Cyrillic told apart.
func toolLanguage(in incoming) string {
	switch language := languageOf(in); {
	case language == "ru":
		return tools.LanguageRussian
	case language == "en":
		return tools.LanguageEnglish
	case strings.HasSuffix(replyLanguage(in.text), "Cyrillic script"):
		return tools.LanguageUzbekCyrillic
	}
	return tools.LanguageUzbek
}

// latinUzbek is replyLanguage's word for Uzbek in the Latin alphabet.
const latinUzbek = "Uzbek, in Latin script"

// mostlyCyrillic reports more Cyrillic words than Latin ones, and tie when
// there are as many of each.
func mostlyCyrillic(words []string, tie bool) bool {
	cyrillic, latin := 0, 0
	for _, w := range words {
		switch r, _ := utf8.DecodeRuneInString(w); {
		case unicode.Is(unicode.Cyrillic, r):
			cyrillic++
		case unicode.Is(unicode.Latin, r):
			latin++
		}
	}
	if cyrillic == latin {
		return tie
	}
	return cyrillic > latin
}

// looksEnglish reports Latin words with common English words and none of
// Uzbek's marks. English spells with "sh" and "ch" too, so a word with them
// only counts against it while such words are as many as its English ones.
func looksEnglish(words []string) bool {
	english, digraphs := 0, 0
	for _, w := range words {
		switch {
		case isUzbekWord(w):
			return false
		case englishWords[w]:
			english++
		case strings.Contains(w, "sh") || strings.Contains(w, "ch"):
			digraphs++
		}
	}
	return english > digraphs
}

// isUzbekWord reports a word with an Uzbek mark: o' or g', a personal or
// question ending (-man, -san, -mi), or one of a few words no English
// message has.
func isUzbekWord(w string) bool {
	return uzbekWords[w] || strings.Contains(w, "o'") || strings.Contains(w, "g'") ||
		strings.HasSuffix(w, "man") || strings.HasSuffix(w, "san") || strings.HasSuffix(w, "mi")
}

// uzbekCyrillicEndings are endings of Uzbek words typed in Cyrillic that
// Russian words do not have.
var uzbekCyrillicEndings = []string{"нинг", "ларни", "лари", "япти", "япман", "япсан", "ябди", "ворди", "ворбти", "ворман", "ибди", "дик", "дингиз", "лигини", "масин", "моқда", "вотти", "вотман", "сизми", "санми", "манми"}

// isUzbekCyrillicWord reports a common Uzbek word, or one with an Uzbek
// ending, typed in Cyrillic without Uzbek's own letters.
func isUzbekCyrillicWord(w string) bool {
	if uzbekCyrillicWords[w] {
		return true
	}
	for _, end := range uzbekCyrillicEndings {
		if strings.HasSuffix(w, end) && len([]rune(w)) > len([]rune(end)) {
			return true
		}
	}
	return false
}

// uzbekCyrillicCount is how many of words are Uzbek typed in Cyrillic.
func uzbekCyrillicCount(words []string) int {
	n := 0
	for _, w := range words {
		if isUzbekCyrillicWord(w) {
			n++
		}
	}
	return n
}

// countIn is how many of words are in set.
func countIn(words []string, set map[string]bool) int {
	n := 0
	for _, w := range words {
		if set[w] {
			n++
		}
	}
	return n
}

// uzbekInCyrillic reports Uzbek typed in Cyrillic without its own letters,
// as "слайд килиб бер": by its common words and endings.
func uzbekInCyrillic(text string) bool {
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if isUzbekCyrillicWord(w) {
			return true
		}
	}
	return false
}

// oneApostrophe writes the apostrophes Uzbek is typed with as one.
func oneApostrophe(r rune) rune {
	if isApostrophe(r) {
		return '\''
	}
	return r
}

func hasAny(words []string, set map[string]bool) bool {
	for _, w := range words {
		if set[w] {
			return true
		}
	}
	return false
}

func wordSet(words ...string) map[string]bool {
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[w] = true
	}
	return set
}

var (
	englishWords = wordSet("the", "you", "are", "what", "how", "is", "why", "please", "thanks", "thank", "hello", "hi",
		"i", "i'm", "me", "my", "your", "this", "that", "it's", "do", "does", "don't", "can", "can't", "could", "would",
		"should", "will", "have", "and", "but", "not", "to", "of", "for", "with", "in", "we", "they", "he", "she", "one")
	uzbekWords = wordSet("nima", "qanday", "rahmat", "salom")
	// uzbekCyrillicWords are common Uzbek words with no letter of Uzbek's
	// own, which Russian does not have.
	uzbekCyrillicWords = wordSet("ва", "бу", "учун", "керак", "килиб", "бер", "беринг", "нима", "кандай", "менга", "билан", "хам", "эмас", "рахмат", "салом",
		"нма", "нега", "айтма", "айт", "йок", "шу", "мана", "ана", "кил", "килдим", "бор", "борми", "йокми", "экан", "акан", "сан", "ман", "мен", "сен", "уни", "буни", "нимага", "канака", "канча", "хали", "энди", "кани", "мари", "хоп")
	russianWords = wordSet("как", "что", "это", "ты", "вы", "я", "не", "да", "нет", "привет", "спасибо", "пожалуйста",
		"где", "когда", "почему", "сегодня")
)
