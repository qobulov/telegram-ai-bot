package placename

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// adjectiveEndings are the Russian adjective endings of names such as
// "Китабский район" or "Farishskiy Rayon", in Latin letters, longest
// first.
var adjectiveEndings = []string{"skogo", "skaya", "skoye", "skiye", "skiy", "skoy", "skoe", "skom", "sky"}

// Nouns reads a Russian adjective before a place word as the nouns it
// can come from, without the place word: "Китабский район" is kitab,
// "Ферганская область" fergan or fergana, "Кашкадарьинская область"
// kashkadarya. Russian makes adjectives in several ways, so it returns
// every likely noun, in Latin letters; nothing when name has no such
// adjective.
func Nouns(name string) []string {
	all := allWords(name)
	if len(all) < 2 {
		return nil
	}
	last := strings.ReplaceAll(all[len(all)-1], "'", "")
	if w, found := placeWords[last]; !found || w.kind == Town {
		return nil
	}
	adjective := all[len(all)-2]
	for _, ending := range adjectiveEndings {
		stem, found := strings.CutSuffix(adjective, ending)
		if !found || utf8.RuneCountInString(stem) < 2 {
			continue
		}
		before := strings.Join(all[:len(all)-2], " ")
		var nouns []string
		for _, noun := range stems(stem) {
			nouns = append(nouns, strings.TrimSpace(before+" "+noun))
		}
		return nouns
	}
	return nil
}

// stems are the nouns a stem may come from: Китаб-ский, Ферган-ская
// (Фергана), Кегейли-н-ский, Амударь-ин-ский (Амударья), Зангиат-ин-ский
// (Зангиата), Пахтачи-й-ский.
func stems(stem string) []string {
	nouns := []string{stem, stem + "a"}
	if base, found := strings.CutSuffix(stem, "in"); found {
		nouns = append(nouns, base+"i", base+"a", base+"ya")
	}
	if base, found := strings.CutSuffix(stem, "iy"); found {
		nouns = append(nouns, base+"i", base+"iya")
	}
	return nouns
}

// Cyrillic writes a Latin name in Russian letters the way Russian spells
// it (Xyuston is хьюстон, Venetsiya венеция, Qarag'anda караганда), so
// the geocoder can be asked by the Russian names its Latin ones lack.
// Uzbek o' is read у and g' г, as Russian writes them.
func Cyrillic(name string) string {
	runes := []rune(strings.ToLower(name))
	var b strings.Builder
	for i := 0; i < len(runes); {
		rest := string(runes[i:])
		previous := rune(0)
		if i > 0 {
			previous = runes[i-1]
		}
		if r := runes[i]; isApostrophe(r) {
			i++
			continue
		}
		if r := runes[i]; r == 'y' && i+1 < len(runes) {
			if iotated, found := iotatedVowels[runes[i+1]]; found {
				if isLatinConsonant(previous) {
					b.WriteRune('ь')
				}
				b.WriteString(iotated)
				i += 2
				continue
			}
		}
		if r := runes[i]; r == 'e' && !isLatinConsonant(previous) {
			b.WriteRune('э')
			i++
			continue
		}
		if r := runes[i]; r == 'y' {
			if isLatinVowel(previous) {
				b.WriteRune('й')
			} else {
				b.WriteRune('ы')
			}
			i++
			continue
		}
		matched := false
		for _, pair := range cyrillicDigraphs {
			if hasPrefixFolded(rest, pair[0]) {
				b.WriteString(pair[1])
				i += utf8.RuneCountInString(pair[0])
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		r := runes[i]
		if letter, found := cyrillicLetters[r]; found {
			b.WriteString(letter)
		} else if unicode.IsLetter(r) {
			b.WriteString(plainLatinCyrillic(r))
		} else {
			b.WriteRune(r)
		}
		i++
	}
	return b.String()
}

// hasPrefixFolded is strings.HasPrefix with any apostrophe in prefix
// matching any apostrophe in s.
func hasPrefixFolded(s, prefix string) bool {
	sr, pr := []rune(s), []rune(prefix)
	if len(sr) < len(pr) {
		return false
	}
	for i, p := range pr {
		if sr[i] != p && !(p == '\'' && isApostrophe(sr[i])) {
			return false
		}
	}
	return true
}

var iotatedVowels = map[rune]string{'a': "я", 'u': "ю", 'e': "е", 'o': "ё"}

// cyrillicDigraphs are tried in order, before single letters.
var cyrillicDigraphs = [][2]string{
	{"shch", "щ"}, {"o'", "у"}, {"g'", "г"}, {"dzh", "дж"}, {"zh", "ж"}, {"kh", "х"},
	{"gh", "г"}, {"sh", "ш"}, {"ch", "ч"}, {"ts", "ц"},
}

var cyrillicLetters = map[rune]string{
	'a': "а", 'b': "б", 'v': "в", 'g': "г", 'd': "д", 'e': "е", 'j': "ж", 'z': "з",
	'i': "и", 'k': "к", 'l': "л", 'm': "м", 'n': "н", 'o': "о", 'p': "п", 'r': "р",
	's': "с", 't': "т", 'u': "у", 'f': "ф", 'x': "х", 'h': "х", 'q': "к", 'w': "в", 'c': "к",
}

// plainLatinCyrillic writes a Latin letter with a diacritic as its plain
// letter's Cyrillic one.
func plainLatinCyrillic(r rune) string {
	plain := plainLatin(r)
	var b strings.Builder
	for _, p := range plain {
		if letter, found := cyrillicLetters[p]; found {
			b.WriteString(letter)
		}
	}
	return b.String()
}

func isLatinVowel(r rune) bool {
	return strings.ContainsRune("aeiouy", r)
}

func isLatinConsonant(r rune) bool {
	return r >= 'a' && r <= 'z' && !isLatinVowel(r)
}

// Latin writes a Cyrillic name in Uzbek Latin letters, with o‘ and g‘ as
// GeoNames writes them (U+2018), so the geocoder can be asked by the
// Uzbek names of cities abroad that it has no Russian name for: Дамашқ
// is Damashq. A Latin name comes back as it is.
func Latin(name string) string {
	var b strings.Builder
	start := true
	runes := []rune(name)
	for i, r := range runes {
		lower := unicode.ToLower(r)
		letter, found := cyrillic[lower]
		switch {
		case lower == 'д' && i+1 < len(runes) && unicode.ToLower(runes[i+1]) == 'ж':
			letter, found = "j", true
		case lower == 'ж' && i > 0 && unicode.ToLower(runes[i-1]) == 'д':
			letter, found = "", true
		case lower == 'ъ':
			letter = "’"
		}
		if !found {
			b.WriteRune(r)
			start = !unicode.IsLetter(r)
			continue
		}
		letter = strings.ReplaceAll(letter, "'", "‘")
		if start && letter != "" {
			first, size := utf8.DecodeRuneInString(letter)
			letter = string(unicode.ToUpper(first)) + letter[size:]
		}
		b.WriteString(letter)
		if letter != "" {
			start = false
		}
	}
	return b.String()
}
