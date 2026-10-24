package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"telegram-ai-bot/internal/tools"
)

const (
	quranTTL = 24 * time.Hour
	// maxVerses bounds one verse range: Al-Fatiha is seven.
	maxVerses      = 7
	maxQuranQuery  = 60
	maxQuranHits   = 8
	shownQuranHit  = 220
	quranPerPage   = 50
	quranOutput    = 12000
	quranComSource = "quran.com"
	alQuranSource  = "alquran.cloud"
)

// quranTranslations are quran.com's translations by language: Muhammad
// Sodiq Muhammad Yusuf's in Latin script, Elmir Kuliev's and Saheeh
// International.
var quranTranslations = map[string]int{"uz": 55, "ru": 45, "en": 20}

// quranTranslators name the translations, for the answer to say whose
// words it quotes.
var quranTranslators = map[string]string{"uz": "Muhammad Sodiq Muhammad Yusuf", "ru": "Эльмир Кулиев", "en": "Saheeh International"}

// surahVerses is how many verses each surah has, in order.
var surahVerses = [114]int{7, 286, 200, 176, 120, 165, 206, 75, 129, 109, 123, 111, 43, 52, 99, 128, 111, 110, 98, 135,
	112, 78, 118, 64, 77, 227, 93, 88, 69, 60, 34, 30, 73, 54, 45, 83, 182, 88, 75, 85, 54, 53, 89, 59, 37, 35, 38, 29, 18, 45,
	60, 49, 62, 55, 78, 96, 29, 22, 24, 13, 14, 11, 11, 18, 12, 12, 30, 52, 52, 44, 28, 28, 20, 56, 40, 31, 50, 40, 46, 42,
	29, 19, 36, 25, 22, 17, 19, 26, 30, 20, 15, 21, 11, 8, 8, 19, 5, 8, 8, 11, 11, 8, 3, 9, 5, 4, 7, 3, 6, 3, 5, 4, 5, 6}

type quranArgs struct {
	Action   string `json:"action"`
	Ref      string `json:"ref"`
	Query    string `json:"query"`
	Language string `json:"language"`
	Arabic   bool   `json:"arabic"`
}

type quranVerse struct {
	Ref         string `json:"ref"`
	Arabic      string `json:"arabic,omitempty"`
	Translation string `json:"translation"`
}

type quranHit struct {
	Ref   string `json:"ref"`
	Surah string `json:"surah"`
	Text  string `json:"text"`
}

func (a *apis) quranTool() tools.Tool {
	return tools.Tool{
		Name: "quran",
		Description: "The Quran with its meaning in Uzbek (Latin), Russian or English, from quran.com. verse: verses by ref, \"2:255\" or a range up to 7, \"1:1-7\"; daily: the verse of the day; surah: a surah's name, verse count and where it was revealed (ref is its number); search: verses whose Uzbek meaning has the words. " +
			"Quote the translation as given, never your own, and give the ref.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["verse", "daily", "surah", "search"]},
				"ref": {"type": "string", "description": "For verse: surah:verse or surah:from-to, as \"2:255\". For surah: its number, 1 to 114."},
				"query": {"type": "string", "description": "For search: one or two Uzbek words, as \"sabr\"."},
				"language": {"type": "string", "enum": ["uz", "ru", "en"], "description": "The translation's language, the asker's; default uz."},
				"arabic": {"type": "boolean", "description": "Also give the Arabic text, when asked for it."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		Timeout:        patientTimeout,
		MaxOutputBytes: quranOutput,
		Run:            a.quran,
	}
}

func (a *apis) quran(ctx context.Context, raw json.RawMessage) (any, error) {
	var args quranArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	language := args.Language
	if language == "" {
		language = "uz"
	}
	if _, ok := quranTranslations[language]; !ok {
		return nil, argumentError("language must be uz, ru or en")
	}
	switch args.Action {
	case "verse":
		surah, from, to, err := parseVerseRef(args.Ref)
		if err != nil {
			return nil, err
		}
		return a.quranVerses(ctx, surah, from, to, language, args.Arabic)
	case "daily":
		surah, verse := verseOfTheDay(a.now().In(a.location))
		return a.quranVerses(ctx, surah, verse, verse, language, args.Arabic)
	case "surah":
		surah, err := strconv.Atoi(strings.TrimSpace(args.Ref))
		if err != nil || surah < 1 || surah > len(surahVerses) {
			return nil, argumentError("ref must be a surah number, 1 to 114")
		}
		return a.quranSurah(ctx, surah)
	case "search":
		return a.quranSearch(ctx, args.Query)
	}
	return nil, argumentError("action must be verse, daily, surah or search")
}

var verseRef = regexp.MustCompile(`^(\d{1,3})\s*:\s*(\d{1,3})(?:\s*-\s*(\d{1,3}))?$`)

// parseVerseRef reads "2:255" or "1:1-7", checking it against the surahs.
func parseVerseRef(ref string) (surah, from, to int, err error) {
	m := verseRef.FindStringSubmatch(strings.TrimSpace(ref))
	if m == nil {
		return 0, 0, 0, argumentError("ref must be surah:verse or surah:from-to, as 2:255")
	}
	surah, _ = strconv.Atoi(m[1])
	from, _ = strconv.Atoi(m[2])
	to = from
	if m[3] != "" {
		to, _ = strconv.Atoi(m[3])
	}
	switch {
	case surah < 1 || surah > len(surahVerses):
		return 0, 0, 0, argumentError("there are 114 surahs")
	case from < 1 || to < from || to > surahVerses[surah-1]:
		return 0, 0, 0, argumentError("surah %d has verses 1 to %d", surah, surahVerses[surah-1])
	case to-from+1 > maxVerses:
		return 0, 0, 0, argumentError("at most %d verses at a time", maxVerses)
	}
	return surah, from, to, nil
}

// verseOfTheDay walks through the Quran a verse a day, the same verse for
// everyone that day.
func verseOfTheDay(day time.Time) (surah, verse int) {
	total := 0
	for _, n := range surahVerses {
		total += n
	}
	index := int(time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC).Unix()/86400) % total
	for i, n := range surahVerses {
		if index < n {
			return i + 1, index + 1
		}
		index -= n
	}
	return 1, 1
}

// footnotes are quran.com's footnote marks in a translation.
var footnotes = regexp.MustCompile(`<sup[^>]*>.*?</sup>`)

func (a *apis) quranVerses(ctx context.Context, surah, from, to int, language string, arabic bool) (any, error) {
	var verses []quranVerse
	for page := (from-1)/quranPerPage + 1; page <= (to-1)/quranPerPage+1; page++ {
		got, err := a.quranPage(ctx, surah, page, language)
		if err != nil {
			return nil, err
		}
		verses = append(verses, got...)
	}
	var out []quranVerse
	for _, v := range verses {
		_, number, _ := strings.Cut(v.Ref, ":")
		if n, _ := strconv.Atoi(number); n >= from && n <= to {
			if !arabic {
				v.Arabic = ""
			}
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s sent no verses", quranComSource)
	}
	return map[string]any{
		"verses":     out,
		"translator": quranTranslators[language],
		"link":       fmt.Sprintf("https://quran.com/%d/%d", surah, from),
		"source":     quranComSource,
	}, nil
}

// quranPage is one page of a surah's verses with a translation, cached.
func (a *apis) quranPage(ctx context.Context, surah, page int, language string) ([]quranVerse, error) {
	link := fmt.Sprintf("%s/api/v4/verses/by_chapter/%d?translations=%d&fields=text_uthmani&per_page=%d&page=%d",
		a.urls.QuranCom, surah, quranTranslations[language], quranPerPage, page)
	return remember(a.cache, "quran:"+link, quranTTL, func() ([]quranVerse, error) {
		var answer struct {
			Verses []struct {
				Key          string `json:"verse_key"`
				Arabic       string `json:"text_uthmani"`
				Translations []struct {
					Text string `json:"text"`
				} `json:"translations"`
			} `json:"verses"`
		}
		if err := a.patient.getJSON(ctx, call{service: quranComSource, url: link, limit: largeBodyLimit}, &answer); err != nil {
			return nil, err
		}
		var verses []quranVerse
		for _, v := range answer.Verses {
			if len(v.Translations) == 0 {
				continue
			}
			verses = append(verses, quranVerse{
				Ref:         v.Key,
				Arabic:      strings.TrimSpace(v.Arabic),
				Translation: cleanText(footnotes.ReplaceAllString(v.Translations[0].Text, ""), 1500),
			})
		}
		return verses, nil
	})
}

func (a *apis) quranSurah(ctx context.Context, surah int) (any, error) {
	link := fmt.Sprintf("%s/api/v4/chapters/%d", a.urls.QuranCom, surah)
	return remember(a.cache, "quran:"+link, quranTTL, func() (any, error) {
		var answer struct {
			Chapter struct {
				Name       string `json:"name_simple"`
				Arabic     string `json:"name_arabic"`
				Revelation string `json:"revelation_place"`
				Order      int    `json:"revelation_order"`
				Verses     int    `json:"verses_count"`
				Translated struct {
					Name string `json:"name"`
				} `json:"translated_name"`
			} `json:"chapter"`
		}
		if err := a.patient.getJSON(ctx, call{service: quranComSource, url: link}, &answer); err != nil {
			return nil, err
		}
		c := answer.Chapter
		return map[string]any{
			"number": surah, "name": c.Name, "arabic": c.Arabic, "meaning": c.Translated.Name,
			"verses": c.Verses, "revealed_in": c.Revelation, "revelation_order": c.Order,
			"link": fmt.Sprintf("https://quran.com/%d", surah), "source": quranComSource,
		}, nil
	})
}

// quranSearch finds verses by words of Muhammad Sodiq's Uzbek translation,
// which alquran.cloud has in Cyrillic script only: a Latin query is
// written in Cyrillic first.
func (a *apis) quranSearch(ctx context.Context, query string) (any, error) {
	query = strings.Join(strings.Fields(query), " ")
	if query == "" || utf8.RuneCountInString(query) > maxQuranQuery {
		return nil, argumentError("query must be 1 to %d characters", maxQuranQuery)
	}
	cyrillic := uzbekCyrillic(strings.ToLower(query))
	link := a.urls.AlQuran + "/v1/search/" + url.PathEscape(cyrillic) + "/all/uz.sodik"
	return remember(a.cache, "quran:"+link, quranTTL, func() (any, error) {
		var answer struct {
			Data struct {
				Count   int `json:"count"`
				Matches []struct {
					Number int    `json:"numberInSurah"`
					Text   string `json:"text"`
					Surah  struct {
						Number int    `json:"number"`
						Name   string `json:"englishName"`
					} `json:"surah"`
				} `json:"matches"`
			} `json:"data"`
		}
		err := a.patient.getJSON(ctx, call{service: alQuranSource, url: link, limit: largeBodyLimit * 4}, &answer)
		var status *statusError
		if errors.As(err, &status) && status.status == http.StatusNotFound {
			return nil, argumentError("no verse has %q in its Uzbek meaning; try another word", query)
		}
		if err != nil {
			return nil, err
		}
		var hits []quranHit
		for _, m := range answer.Data.Matches[:min(len(answer.Data.Matches), maxQuranHits)] {
			hits = append(hits, quranHit{
				Ref:   fmt.Sprintf("%d:%d", m.Surah.Number, m.Number),
				Surah: m.Surah.Name,
				Text:  cleanText(m.Text, shownQuranHit),
			})
		}
		if len(hits) == 0 {
			return nil, argumentError("no verse has %q in its Uzbek meaning; try another word", query)
		}
		return map[string]any{
			"found": answer.Data.Count, "verses": hits, "searched": cyrillic,
			"note":   "The texts are Muhammad Sodiq's translation in Cyrillic script, cut short; verse gives a verse in full, in Latin script.",
			"source": alQuranSource,
		}, nil
	})
}

// uzbekLatin maps Uzbek Latin letters, and the pairs written as one, to
// Cyrillic. Apostrophes are made "'" first.
var uzbekLatin = []struct{ latin, cyrillic string }{
	{"yo'", "йў"}, {"o'", "ў"}, {"g'", "ғ"}, {"sh", "ш"}, {"ch", "ч"}, {"yo", "ё"}, {"yu", "ю"}, {"ya", "я"}, {"ye", "е"},
	{"a", "а"}, {"b", "б"}, {"d", "д"}, {"e", "е"}, {"f", "ф"}, {"g", "г"}, {"h", "ҳ"}, {"i", "и"}, {"j", "ж"},
	{"k", "к"}, {"l", "л"}, {"m", "м"}, {"n", "н"}, {"o", "о"}, {"p", "п"}, {"q", "қ"}, {"r", "р"}, {"s", "с"},
	{"t", "т"}, {"u", "у"}, {"v", "в"}, {"x", "х"}, {"y", "й"}, {"z", "з"}, {"'", "ъ"},
}

// uzbekCyrillic writes lower-case Uzbek Latin text in Cyrillic; an "e" that
// starts a word is "э". Anything else is left as it is.
func uzbekCyrillic(text string) string {
	text = strings.NewReplacer("ʻ", "'", "ʼ", "'", "‘", "'", "’", "'", "`", "'").Replace(text)
	var b strings.Builder
	inWord := false
	for i := 0; i < len(text); {
		if !inWord && text[i] == 'e' {
			b.WriteString("э")
			i, inWord = i+1, true
			continue
		}
		matched := false
		for _, pair := range uzbekLatin {
			if strings.HasPrefix(text[i:], pair.latin) {
				b.WriteString(pair.cyrillic)
				i += len(pair.latin)
				matched, inWord = true, true
				break
			}
		}
		if !matched {
			r, size := utf8.DecodeRuneInString(text[i:])
			b.WriteRune(r)
			i, inWord = i+size, unicode.IsLetter(r)
		}
	}
	return b.String()
}
