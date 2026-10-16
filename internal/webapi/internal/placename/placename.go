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
// letters Uzbek and Russian spellings differ in (q and k, x, kh and h, g
// and gh, j, zh and dzh), so Qo'ng'irot, Қўнғирот and Kongirot meet.
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

// Words are a name's words as Strict sees them, so that one word of a
// longer name can be compared: "Ansan" is a word of "Ansan-si".
func Words(name string) []string {
	return words(name)
}

// Kind is what a place word says a name is.
type Kind int

const (
	// AnyKind is a name without a place word.
	AnyKind Kind = iota
	Town
	District
	Region
)

type placeWord struct {
	kind Kind
	// uzbek is true for Uzbek words: a name with one is a place in
	// Uzbekistan ("Boston tumani" is Bo'ston), while "city" or "город"
	// say nothing about the country.
	uzbek bool
	// english words are part of names the geocoder knows ("Mexico City"),
	// so Trim keeps them.
	english bool
}

// placeWords are dropped, so "Toshkent shahri", "Forish tumani" and
// "г. Москва" find the place. They are compared without apostrophes, as
// Latin letters ("шаҳри" is "shahri", and "шахри" typed without ҳ is
// "shaxri"); "sh" is the "sh." of "Toshkent sh.".
var placeWords = map[string]placeWord{
	"shahri": {Town, true, false}, "shahar": {Town, true, false}, "shaxri": {Town, true, false}, "shaxar": {Town, true, false},
	"sh": {Town, true, false}, "shaharcha": {Town, true, false}, "shaharchasi": {Town, true, false},
	"qishloq": {Town, true, false}, "qishlogi": {Town, true, false},
	"tumani": {District, true, false}, "tuman": {District, true, false},
	"viloyati": {Region, true, false}, "viloyat": {Region, true, false}, "vil": {Region, true, false}, "respublikasi": {Region, true, false},
	"gorod": {Town, false, false}, "g": {Town, false, false}, "posyolok": {Town, false, false}, "poselok": {Town, false, false},
	"selo": {Town, false, false}, "kishlak": {Town, false, false},
	"rayon": {District, false, false}, "rayoni": {District, false, false},
	"oblast": {Region, false, false}, "oblasti": {Region, false, false}, "obl": {Region, false, false}, "respublika": {Region, false, false},
	"city": {Town, false, true}, "town": {Town, false, true}, "village": {Town, false, true},
	"district": {District, false, true}, "region": {Region, false, true}, "province": {Region, false, true},
}

// Trim drops the Uzbek and Russian place words from name as written, for
// asking the geocoder: "Keles shahri" is Keles, "Toshkent sh." Toshkent.
// English ones stay, as "Mexico City" is the city's name.
func Trim(name string) string {
	var kept []string
	for _, token := range strings.Fields(name) {
		all := allWords(token)
		placeOnly := len(all) > 0
		for _, word := range all {
			if w, found := placeWords[strings.ReplaceAll(word, "'", "")]; !found || w.english {
				placeOnly = false
			}
		}
		if !placeOnly {
			kept = append(kept, token)
		}
	}
	return strings.Join(kept, " ")
}

// KindOf says what the place words of name call it, and whether one of
// them is Uzbek.
func KindOf(name string) (kind Kind, uzbek bool) {
	for _, word := range allWords(name) {
		if w, found := placeWords[strings.ReplaceAll(word, "'", "")]; found {
			kind, uzbek = w.kind, uzbek || w.uzbek
		}
	}
	return kind, uzbek
}

// words lowers name, writes its Cyrillic letters in Latin, drops
// diacritics, splits it at spaces and punctuation and drops place words.
// Apostrophes stay, all written as '.
func words(name string) []string {
	var kept []string
	for _, word := range allWords(name) {
		if _, found := placeWords[strings.ReplaceAll(word, "'", "")]; !found {
			kept = append(kept, word)
		}
	}
	return kept
}

func allWords(name string) []string {
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
	var all []string
	for _, word := range strings.Fields(b.String()) {
		if word = strings.Trim(word, "'"); word != "" {
			all = append(all, word)
		}
	}
	return all
}

// looseFolds turn Russian and old romanizations into Uzbek Latin letters
// and drop apostrophes. Longer patterns come first; sh and ch are kept
// before a lone h, which is folded with x as members confuse the two
// (Xazorasp for Hazorasp) and Russian writes both х.
var looseFolds = strings.NewReplacer("'", "", "dzh", "j", "dj", "j", "zh", "j", "kh", "x", "gh", "g", "sh", "sh", "ch", "ch", "h", "x", "q", "k", "w", "v")

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
