// Package placename folds the ways people write a place name into keys
// that compare equal. The weather tool and the generator of its Uzbekistan
// table share it, so a GeoNames name and a member's spelling of it fold
// the same way.
package placename

import (
	"strings"
	"unicode"
)

// Key folds case, apostrophes, spaces, hyphens, diacritics and words such
// as "shahri" or "district", reads Cyrillic as Uzbek Latin, and folds the
// letters Uzbek and Russian spellings differ in (q and k, x and kh, g and
// gh, j and dzh), so Qo'ng'irot, Қўнғирот and Kongirot meet.
func Key(name string) string {
	var b strings.Builder
	for _, word := range words(name) {
		b.WriteString(loose(word))
	}
	return b.String()
}

// Strict is Key without the apostrophe and letter folding: Bo'ston and
// Boston, or Qamashi and Kamashi, stay apart.
func Strict(name string) string {
	return strings.Join(words(name), "")
}

// placeWords are dropped, so "Toshkent shahri", "Forish tumani" and
// "г. Москва" find the place. They are compared without apostrophes.
var placeWords = map[string]bool{
	"shahri": true, "shahar": true, "shaharchasi": true, "qishlogi": true,
	"tumani": true, "tuman": true, "viloyati": true, "viloyat": true, "respublikasi": true,
	"city": true, "town": true, "district": true, "region": true, "province": true,
	"gorod": true, "g": true, "rayon": true, "rayoni": true, "oblast": true, "oblasti": true, "respublika": true,
}

// words lowers name, writes its Cyrillic letters in Latin, drops
// diacritics and splits it at spaces and punctuation. Apostrophes stay,
// all written as '.
func words(name string) []string {
	var b strings.Builder
	runes := []rune(strings.ToLower(name))
	for i, r := range runes {
		switch {
		case r == 'д' && i+1 < len(runes) && runes[i+1] == 'ж':
			b.WriteRune('j')
		case r == 'ж' && i > 0 && runes[i-1] == 'д':
		case isApostrophe(r):
			b.WriteRune('\'')
		case unicode.Is(unicode.Cyrillic, r):
			b.WriteString(cyrillic[r])
		case unicode.Is(unicode.Mn, r):
			// A combining mark, such as the dot of a lowered İ.
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteString(plainLatin(r))
		default:
			b.WriteRune(' ')
		}
	}
	var kept []string
	for _, word := range strings.Fields(b.String()) {
		word = strings.Trim(word, "'")
		if word != "" && !placeWords[strings.ReplaceAll(word, "'", "")] {
			kept = append(kept, word)
		}
	}
	return kept
}

// looseFolds turn Russian and old romanizations into Uzbek Latin letters
// and drop apostrophes. Longer patterns come first.
var looseFolds = strings.NewReplacer("'", "", "dzh", "j", "dj", "j", "kh", "x", "gh", "g", "q", "k", "w", "v")

func loose(word string) string {
	return looseFolds.Replace(word)
}

func isApostrophe(r rune) bool {
	return strings.ContainsRune("'`´ʻʼ‘’", r)
}

// cyrillic writes Russian, Uzbek, Kazakh, Kyrgyz, Tajik and Ukrainian
// letters as Uzbek Latin; ы, which Uzbek lacks, as y like Russian
// romanizations (Besharyk).
var cyrillic = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "yo", 'ж': "j",
	'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
	'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "x", 'ц': "ts",
	'ч': "ch", 'ш': "sh", 'щ': "sh", 'ъ': "'", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu",
	'я': "ya", 'ў': "o'", 'ғ': "g'", 'қ': "q", 'ҳ': "h", 'ә': "a", 'ө': "o", 'ү': "u",
	'ұ': "u", 'ң': "ng", 'һ': "h", 'ӣ': "i", 'ӯ': "u", 'ҷ': "j", 'і': "i", 'ї': "yi",
	'є': "ye", 'ґ': "g", 'ј': "j",
}

// plainLatin drops the diacritics of Latin letters: ŭ, GeoNames' ў, is u
// as in its ASCII names, and Turkish ı is i.
func plainLatin(r rune) string {
	if plain, found := latin[r]; found {
		return plain
	}
	return string(r)
}

var latin = map[rune]string{
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'ā': "a", 'ă': "a", 'ą': "a", 'ə': "a",
	'ç': "c", 'ć': "c", 'č': "c", 'ď': "d", 'đ': "d",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ē': "e", 'ė': "e", 'ę': "e", 'ě': "e",
	'ğ': "g", 'ģ': "g", 'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ī': "i", 'į': "i", 'ı': "i",
	'ķ': "k", 'ł': "l", 'ľ': "l", 'ļ': "l", 'ñ': "n", 'ń': "n", 'ň': "n", 'ņ': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ø': "o", 'ō': "o", 'ő': "o",
	'ř': "r", 'ş': "s", 'ś': "s", 'š': "s", 'ș': "s", 'ß': "ss", 'ţ': "t", 'ť': "t", 'ț': "t",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ū': "u", 'ů': "u", 'ű': "u", 'ų': "u", 'ŭ': "u",
	'ý': "y", 'ÿ': "y", 'ž': "z", 'ź': "z", 'ż': "z", 'æ': "ae", 'œ': "oe",
}
