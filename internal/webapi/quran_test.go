package webapi

import (
	"strings"
	"testing"
	"time"
)

const quranAlBaqarah = `{"verses":[
	{"verse_key":"2:254","text_uthmani":"يَـٰٓأَيُّهَا","translations":[{"text":"Ey iymon keltirganlar!"}]},
	{"verse_key":"2:255","text_uthmani":"ٱللَّهُ لَآ إِلَـٰهَ","translations":[{"text":"Alloh, Undan o<sup foot_note=1>1</sup>zga iloh yo'q."}]},
	{"verse_key":"2:256","text_uthmani":"لَآ إِكْرَاهَ","translations":[{"text":"Dinda zo'rlash yo'q."}]}]}`

type quranOut struct {
	Verses []quranVerse `json:"verses"`
	Link   string       `json:"link"`
}

func TestQuranGivesVersesInTheAskersLanguage(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/qurancom/api/v4/verses/by_chapter/2": ok(quranAlBaqarah)})

	out := result[quranOut](t, run(t, u.options(), "quran", `{"action":"verse","ref":"2:255-256"}`))

	if len(out.Verses) != 2 || out.Verses[0].Ref != "2:255" || out.Verses[0].Translation != "Alloh, Undan ozga iloh yo'q." || out.Verses[0].Arabic != "" {
		t.Errorf("verses = %+v", out.Verses)
	}
	if out.Link != "https://quran.com/2/255" {
		t.Errorf("link = %q", out.Link)
	}
	q := u.requests("/qurancom/api/v4/verses/by_chapter/2")[0].URL.Query()
	if q.Get("translations") != "55" || q.Get("page") != "6" || q.Get("per_page") != "50" {
		t.Errorf("query = %v, want the Uzbek translation's page with 255", q)
	}

	out = result[quranOut](t, run(t, u.options(), "quran", `{"action":"verse","ref":"2:254","language":"ru","arabic":true}`))
	if len(out.Verses) != 1 || out.Verses[0].Arabic == "" {
		t.Errorf("verses = %+v, want the Arabic too", out.Verses)
	}
	if q := u.requests("/qurancom/api/v4/verses/by_chapter/2")[1].URL.Query(); q.Get("translations") != "45" {
		t.Errorf("translation = %s, want Kuliev's", q.Get("translations"))
	}
}

func TestQuranChecksRefs(t *testing.T) {
	opts := newUpstream(t, nil).options()
	for ref, word := range map[string]string{"2": "surah:verse", "115:1": "114", "1:8": "1 to 7", "2:1-8": "at most 7", "2:5-3": "1 to 286"} {
		wantError(t, run(t, opts, "quran", `{"action":"verse","ref":"`+ref+`"}`), "invalid_arguments", word)
	}
	wantError(t, run(t, opts, "quran", `{"action":"surah","ref":"0"}`), "invalid_arguments", "114")
}

func TestVerseOfTheDayWalksThroughTheQuran(t *testing.T) {
	day := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	surah, verse := verseOfTheDay(day)
	if surah < 1 || surah > 114 || verse < 1 || verse > surahVerses[surah-1] {
		t.Fatalf("verse of the day = %d:%d", surah, verse)
	}
	if s2, v2 := verseOfTheDay(day.Add(13 * time.Hour)); s2 != surah || v2 != verse {
		t.Errorf("the verse changed within the day: %d:%d then %d:%d", surah, verse, s2, v2)
	}
	if s2, v2 := verseOfTheDay(day.AddDate(0, 0, 1)); s2 == surah && v2 == verse {
		t.Error("the next day has the same verse")
	}
	total := 0
	for _, n := range surahVerses {
		total += n
	}
	if total != 6236 {
		t.Errorf("the surahs have %d verses, want 6236", total)
	}
}

func TestQuranSearchesTheUzbekMeaningInCyrillic(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/alquran/v1/search/сабр/all/uz.sodik":    ok(`{"code":200,"data":{"count":101,"matches":[{"numberInSurah":153,"text":"Эй иймон келтирганлар, сабр ва намоз ила ёрдам сўранг.","surah":{"number":2,"englishName":"Al-Baqara"}}]}}`),
		"/alquran/v1/search/қўрқинч/all/uz.sodik": status(404),
	})

	out := result[struct {
		Found  int        `json:"found"`
		Verses []quranHit `json:"verses"`
	}](t, run(t, u.options(), "quran", `{"action":"search","query":"Sabr"}`))

	if out.Found != 101 || len(out.Verses) != 1 || out.Verses[0].Ref != "2:153" || !strings.Contains(out.Verses[0].Text, "сабр") {
		t.Errorf("search = %+v", out)
	}
	wantError(t, run(t, u.options(), "quran", `{"action":"search","query":"qo'rqinch"}`), "invalid_arguments", "no verse")
}

func TestUzbekCyrillic(t *testing.T) {
	for latin, want := range map[string]string{
		"sabr":         "сабр",
		"o'g'il":       "ўғил",
		"shukr qilish": "шукр қилиш",
		"ehson va yer": "эҳсон ва ер",
		"ayol yetim":   "аёл етим",
		"g‘azab":       "ғазаб",
		"choy ichdi":   "чой ичди",
		"yo'l":         "йўл",
		"сабр":         "сабр",
	} {
		if got := uzbekCyrillic(latin); got != want {
			t.Errorf("uzbekCyrillic(%q) = %q, want %q", latin, got, want)
		}
	}
}
