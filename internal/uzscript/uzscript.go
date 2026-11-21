// Package uzscript writes Uzbek Cyrillic in the Latin alphabet, and Uzbek
// Latin in Cyrillic.
package uzscript

import (
	"regexp"
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

// letterRun is a run of letters, with the apostrophes Uzbek Latin writes
// inside words.
var letterRun = regexp.MustCompile("[\\p{L}\\p{M}'ʻʼ’‘`]+")

// nameWithEnding is a name in Latin with an Uzbek Cyrillic ending after an
// apostrophe, "Telegram'да", which is written so on purpose.
var nameWithEnding = regexp.MustCompile("^\\p{Lu}\\p{Latin}*['ʻʼ’‘`]\\p{Cyrillic}+$")

// OneScript writes each word that mixes Cyrillic and Latin letters, as the
// model now and then writes one ("Эшиuryapman", 2026-11-21), wholly in the
// script most letters of the text's other words are in, the mixed words'
// own aside; then, in Uzbek Cyrillic, the Latin words in small letters
// ("бу buyruq озгина", the same day). A @username or /command stays.
func OneScript(text string) string {
	text = mendMixedWords(text)
	if IsUzbekCyrillic(text) {
		text = cyrillicSmallWords(text)
	}
	return text
}

// cyrillicSmallWords writes in Cyrillic the Latin words of an Uzbek
// Cyrillic text that are all small letters, names and brands having
// capitals ("Telegram"); a link, @username or /command stays.
func cyrillicSmallWords(text string) string {
	parts := strings.SplitAfter(text, " ")
	for i, part := range parts {
		if strings.Contains(part, "://") || strings.Contains(part, "www.") || strings.HasPrefix(part, "@") || strings.HasPrefix(part, "/") {
			continue
		}
		parts[i] = letterRun.ReplaceAllStringFunc(part, func(word string) string {
			if c, l := letters(word); c > 0 || l < 2 || word != strings.ToLower(word) {
				return word
			}
			return cyrillicWord(word)
		})
	}
	return strings.Join(parts, "")
}

// mendMixedWords writes each word mixing the scripts in the script of the
// text's other words.
func mendMixedWords(text string) string {
	if c, l := letters(text); c == 0 || l == 0 {
		return text
	}
	cyrillic, latin := 0, 0
	for _, word := range letterRun.FindAllString(text, -1) {
		if c, l := letters(word); c == 0 || l == 0 {
			cyrillic, latin = cyrillic+c, latin+l
		}
	}
	var b strings.Builder
	last := 0
	for _, at := range letterRun.FindAllStringIndex(text, -1) {
		word := text[at[0]:at[1]]
		c, l := letters(word)
		if c == 0 || l == 0 || nameWithEnding.MatchString(word) || at[0] > 0 && strings.ContainsRune("@/", rune(text[at[0]-1])) {
			continue
		}
		b.WriteString(text[last:at[0]])
		if cyrillic > latin {
			b.WriteString(cyrillicWord(word))
		} else {
			b.WriteString(Latin(word))
		}
		last = at[1]
	}
	b.WriteString(text[last:])
	return b.String()
}

// letters counts text's Cyrillic and Latin letters.
func letters(text string) (cyrillic, latin int) {
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			cyrillic++
		case unicode.Is(unicode.Latin, r):
			latin++
		}
	}
	return cyrillic, latin
}

// IsUzbekCyrillic reports text written mostly in Cyrillic, names in Latin
// letters such as "Mobile Legends" aside, with letters of Uzbek's own,
// which Russian does not have.
func IsUzbekCyrillic(text string) bool {
	cyrillic, latin := letters(text)
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
