// Package uzscript writes Uzbek Cyrillic in the Latin alphabet.
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
