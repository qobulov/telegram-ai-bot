// Package uzscript writes Uzbek Cyrillic in the Latin alphabet, and Uzbek
// Latin in Cyrillic.
package uzscript

import (
	"strings"
	"unicode"
)

var cyrillicToLatin = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "yo", 'ж': "j", 'з': "z",
	'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r",
	'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "x", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sh",
	'ъ': "ʼ", 'ь': "", 'ы': "i", 'э': "e", 'ю': "yu", 'я': "ya",
	'ў': "oʻ", 'қ': "q", 'ғ': "gʻ", 'ҳ': "h",
}

// Latin writes Uzbek Cyrillic in the Latin alphabet Uzbek uses, as Uzbek
// Wikipedia does. Other characters pass through.
func Latin(text string) string {
	var b strings.Builder
	var previous rune
	for _, r := range text {
		lower := unicode.ToLower(r)
		latin, found := cyrillicToLatin[lower]
		switch {
		case !found:
			latin = string(r)
		case lower == 'е' && startsSyllable(previous):
			latin = "ye"
		}
		if found && r != lower {
			latin = capitalize(latin)
		}
		b.WriteString(latin)
		previous = r
	}
	return b.String()
}

// IsUzbekCyrillic reports text written mostly in Cyrillic, names in Latin
// letters such as "Mobile Legends" aside, with letters of Uzbek's own,
// which Russian does not have.
func IsUzbekCyrillic(text string) bool {
	cyrillic, latin := 0, 0
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			cyrillic++
		case unicode.Is(unicode.Latin, r):
			latin++
		}
	}
	return cyrillic > latin && strings.ContainsAny(strings.ToLower(text), "ўқғҳ")
}

// startsSyllable reports whether е after previous is said "ye": at the
// start of a word, or after a vowel or a sign.
func startsSyllable(previous rune) bool {
	return !unicode.IsLetter(previous) || strings.ContainsRune("аеёиоуэюяўъь", unicode.ToLower(previous))
}

func capitalize(text string) string {
	runes := []rune(text)
	if len(runes) > 0 {
		runes[0] = unicode.ToUpper(runes[0])
	}
	return string(runes)
}

// latinToCyrillic writes Uzbek Latin letters in Cyrillic; the digraphs and
// the letters with an apostrophe are read first, in Cyrillic.
var latinToCyrillic = map[string]string{
	"a": "а", "b": "б", "c": "с", "d": "д", "e": "е", "f": "ф", "g": "г", "h": "ҳ", "i": "и", "j": "ж",
	"k": "к", "l": "л", "m": "м", "n": "н", "o": "о", "p": "п", "q": "қ", "r": "р", "s": "с", "t": "т",
	"u": "у", "v": "в", "w": "в", "x": "х", "y": "й", "z": "з",
	"sh": "ш", "ch": "ч", "yo": "ё", "yu": "ю", "ya": "я", "ye": "е", "o'": "ў", "g'": "ғ",
}

// apostrophes are the marks Uzbek Latin is typed with for o', g' and the
// sign ъ.
const apostrophes = "'ʻʼ’‘`"

// englishSpelled are English words Uzbek uses, written as they are said.
var englishSpelled = strings.NewReplacer("reply", "replay", "Reply", "Replay")

// Cyrillic writes Uzbek Latin in Cyrillic. A command, a @username and a
// link stay as they are, as do other characters.
func Cyrillic(text string) string {
	var b strings.Builder
	for _, word := range strings.SplitAfter(text, " ") {
		if strings.HasPrefix(word, "/") || strings.HasPrefix(word, "@") || strings.Contains(word, "://") {
			b.WriteString(word)
			continue
		}
		b.WriteString(cyrillicWord(englishSpelled.Replace(word)))
	}
	return b.String()
}

// cyrillicWord is Cyrillic for one word and the space after it.
func cyrillicWord(word string) string {
	var b strings.Builder
	runes := []rune(word)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		lower := unicode.ToLower(r)
		if i+1 < len(runes) {
			next := unicode.ToLower(runes[i+1])
			if strings.ContainsRune(apostrophes, next) {
				next = '\''
			}
			// "yo'q" is "йўқ": an o' after y is ў, not ё.
			oFollows := next == 'o' && i+2 < len(runes) && strings.ContainsRune(apostrophes, runes[i+2])
			if cyrillic, ok := latinToCyrillic[string([]rune{lower, next})]; ok && !(lower == 'y' && oFollows) {
				b.WriteString(withCase(cyrillic, r))
				i++
				continue
			}
		}
		cyrillic, ok := latinToCyrillic[string(lower)]
		switch {
		case strings.ContainsRune(apostrophes, r) && i > 0 && unicode.IsLetter(runes[i-1]):
			cyrillic, ok = "ъ", true
		case !ok:
			b.WriteRune(r)
			continue
		case lower == 'e' && (i == 0 || !unicode.IsLetter(runes[i-1])):
			cyrillic = "э"
		}
		b.WriteString(withCase(cyrillic, r))
	}
	return b.String()
}

// withCase is cyrillic in capitals when latin starts with one.
func withCase(cyrillic string, latin rune) string {
	if unicode.IsUpper(latin) {
		return capitalize(cyrillic)
	}
	return cyrillic
}
