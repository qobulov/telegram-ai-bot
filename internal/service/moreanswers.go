package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/tools"
)

// Every item of «📋 Ko'proq» is answered in code: its tool is run on the
// sender's turn and its result written here, so an item is answered in a
// second or two and costs no call to the model. Labels are in the user's
// language; the data stays as the tool gives it, an English fact or a
// Russian headline, since only the model could translate it.

func init() {
	for key, answer := range map[string]codeAnswer{
		"prayer": prayerAnswer, "quran": quranAnswer,
		"news_uz": newsAnswer(false), "news_world": newsAnswer(true),
		"movie": movieAnswer, "anime": animeAnswer, "song": songAnswer, "youtube": youtubeAnswer,
		"cat": catAnswer, "trivia": triviaAnswer, "fact": factAnswer,
		"table": tableAnswer, "matches": matchesAnswer, "scorers": scorersAnswer,
		"tarozi": gameAnswer("tarozi"), "ilon": gameAnswer("ilon"), "otinchi": gameAnswer("otinchi"), "boss": gameAnswer("boss"), "mafiya": gameAnswer("mafiya"), "maydon": gameAnswer("maydon"), "free_games": freeGamesAnswer, "steam": steamAnswer,
		"wiki": wikiAnswer, "english": englishAnswer, "synonyms": synonymsAnswer, "papers": papersAnswer,
		"qr": qrAnswer, "draw": drawAnswer, "place": placeAnswer, "holidays": holidaysAnswer,
		"crypto": cryptoAnswer, "quakes": quakesAnswer, "search": searchAnswer,
	} {
		codeAnswers[key] = answer
	}
}

// itemResult runs an item's tool with args and decodes its result into
// out; it gives the sink with what the tool made, pictures or a song, and
// why the tool failed, "" when it did not (see toolFailure).
func (s *Service) itemResult(ctx context.Context, in incoming, name string, args, out any) (*tools.Sink, string) {
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, "unreadable"
	}
	sink := &tools.Sink{}
	return sink, s.toolFailure(ctx, in, name, string(encoded), out, sink)
}

// itemText answers with a tool's result written by write. A failure, or a
// result write finds nothing in, is said plainly.
func itemText[T any](ctx context.Context, s *Service, in incoming, name string, args any, write func(incoming, T) string) outcome {
	var result T
	if _, failure := s.itemResult(ctx, in, name, args, &result); failure != "" {
		return say(failedText(in, failure))
	}
	if text := write(in, result); text != "" {
		return say(text)
	}
	return say(textNothingFound.to(in))
}

// failedText says why a tool gave nothing: a search that found nothing,
// which the registry reports as invalid arguments, or a service that is
// down.
func failedText(in incoming, failure string) string {
	if failure == "invalid_arguments" {
		return textNothingFound.to(in)
	}
	return textUnavailable.to(in)
}

// --- Faith ---

type prayerDay struct {
	Region string `json:"region"`
	Date   string `json:"date"`
	Hijri  struct {
		Day   int    `json:"day"`
		Month string `json:"month"`
		Year  int    `json:"year"`
	} `json:"hijri"`
	Bomdod string `json:"bomdod"`
	Quyosh string `json:"quyosh"`
	Peshin string `json:"peshin"`
	Asr    string `json:"asr"`
	Shom   string `json:"shom"`
	Xufton string `json:"xufton"`
}

// prayerAnswer gives today's prayer times for the town written, which the
// tool finds among Uzbekistan's region centres.
func prayerAnswer(ctx context.Context, s *Service, in incoming, city string) outcome {
	var day prayerDay
	if _, failure := s.itemResult(ctx, in, "prayer_times", map[string]string{"region": city}, &day); failure != "" {
		if failure == "invalid_arguments" {
			return say(textPrayerUnknown.to(in))
		}
		return say(textUnavailable.to(in))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🕋 **%s, %s**", day.Region, dotDate(day.Date))
	if day.Hijri.Year > 0 {
		fmt.Fprintf(&b, " (%d %s %d)", day.Hijri.Day, day.Hijri.Month, day.Hijri.Year)
	}
	b.WriteString("\n")
	for i, at := range []string{day.Bomdod, day.Quyosh, day.Peshin, day.Asr, day.Shom, day.Xufton} {
		fmt.Fprintf(&b, "\n%s — %s", prayerNames[i].to(in), at)
	}
	return say(b.String() + "\n\n" + textPrayerNote.to(in))
}

var prayerNames = [6]localText{
	lt("🌅 Bomdod", "🌅 Фаджр", "🌅 Fajr"), lt("☀️ Quyosh", "☀️ Восход", "☀️ Sunrise"), lt("🕛 Peshin", "🕛 Зухр", "🕛 Dhuhr"),
	lt("🌤 Asr", "🌤 Аср", "🌤 Asr"), lt("🌇 Shom", "🌇 Магриб", "🌇 Maghrib"), lt("🌙 Xufton", "🌙 Иша", "🌙 Isha"),
}

var (
	// verseRef is a verse, "2:255", or verses, "1:1-7"; surahNumber a
	// surah alone.
	verseRef    = regexp.MustCompile(`^\d{1,3}\s*:\s*\d{1,3}(\s*-\s*\d{1,3})?$`)
	surahNumber = regexp.MustCompile(`^\d{1,3}$`)
)

type quranVerses struct {
	Verses []struct {
		Ref         string `json:"ref"`
		Translation string `json:"translation"`
		Surah       string `json:"surah"`
		Text        string `json:"text"`
	} `json:"verses"`
	Translator string `json:"translator"`
	Link       string `json:"link"`
	Found      int    `json:"found"`
}

type quranSurah struct {
	Number     int    `json:"number"`
	Name       string `json:"name"`
	Meaning    string `json:"meaning"`
	Verses     int    `json:"verses"`
	RevealedIn string `json:"revealed_in"`
	Link       string `json:"link"`
}

// quranAnswer reads what was written as a verse, "2:255", a surah's number,
// or words to find in the Uzbek meaning, as "sabr".
func quranAnswer(ctx context.Context, s *Service, in incoming, value string) outcome {
	language := languageOf(in)
	switch {
	case verseRef.MatchString(value):
		args := map[string]string{"action": "verse", "ref": value, "language": language}
		return itemText(ctx, s, in, "quran", args, versesText)
	case surahNumber.MatchString(value):
		return itemText(ctx, s, in, "quran", map[string]string{"action": "surah", "ref": value}, surahText)
	}
	return itemText(ctx, s, in, "quran", map[string]string{"action": "search", "query": value}, foundVersesText)
}

func versesText(in incoming, q quranVerses) string {
	if len(q.Verses) == 0 {
		return ""
	}
	var lines []string
	for _, v := range q.Verses {
		lines = append(lines, fmt.Sprintf("📖 **%s** %s", v.Ref, v.Translation))
	}
	return strings.Join(lines, "\n\n") + "\n\n" + fmt.Sprintf(textTranslator.to(in), q.Translator) + "\n" + q.Link
}

func surahText(in incoming, q quranSurah) string {
	place := q.RevealedIn
	if where, ok := revelationPlaces[place]; ok {
		place = where.to(in)
	}
	return fmt.Sprintf(textSurah.to(in), q.Number, q.Name, q.Meaning, q.Verses, place) + "\n" + q.Link
}

var revelationPlaces = map[string]localText{
	"makkah":  lt("Makkada", "Мекке", "Mecca"),
	"madinah": lt("Madinada", "Медине", "Medina"),
}

// foundVersesText lists the verses whose meaning has the words, as the
// tool gives them: Muhammad Sodiq's translation in Cyrillic, cut short.
func foundVersesText(in incoming, q quranVerses) string {
	if len(q.Verses) == 0 {
		return ""
	}
	lines := []string{fmt.Sprintf(textVersesFound.to(in), q.Found)}
	for _, v := range q.Verses {
		lines = append(lines, fmt.Sprintf("• **%s** (%s) %s", v.Ref, v.Surah, v.Text))
	}
	return strings.Join(lines, "\n\n")
}

// --- News ---

type headlines struct {
	Items []struct {
		Title  string `json:"title"`
		Source string `json:"source"`
		Link   string `json:"link"`
		Time   string `json:"time"`
	} `json:"items"`
}

// newsAnswer gives the latest headlines of Uzbek sites, in Uzbek or
// Russian, or with world those of the BBC in the user's language.
func newsAnswer(world bool) codeAnswer {
	return func(ctx context.Context, s *Service, in incoming, _ string) outcome {
		language := languageOf(in)
		args := map[string]any{"lang": language, "world": world, "limit": shownHeadlines}
		heading := textWorldNews
		if !world {
			heading = textUzbekNews
			if language != "ru" {
				args["lang"] = "uz" // the Uzbek sites write in Uzbek and Russian
			}
		}
		return itemText(ctx, s, in, "news_headlines", args, func(in incoming, news headlines) string {
			if len(news.Items) == 0 {
				return ""
			}
			lines := []string{heading.to(in)}
			for _, h := range news.Items {
				line := "• " + strings.TrimSpace(h.Time+" "+h.Title) + " (" + h.Source + ")"
				if h.Link != "" {
					line += "\n" + h.Link
				}
				lines = append(lines, line)
			}
			return strings.Join(lines, "\n\n")
		})
	}
}

const shownHeadlines = 6

// --- Fun ---

type movieTitle struct {
	ID       int      `json:"id"`
	Kind     string   `json:"kind"`
	Title    string   `json:"title"`
	Original string   `json:"original_title"`
	Year     string   `json:"year"`
	Rating   string   `json:"rating"`
	Overview string   `json:"overview"`
	Link     string   `json:"link"`
	Tagline  string   `json:"tagline"`
	Genres   []string `json:"genres"`
	Runtime  string   `json:"runtime"`
	Seasons  int      `json:"seasons"`
	Director []string `json:"directed_or_created_by"`
	Cast     []string `json:"cast"`
}

// movieAnswer finds a film or series and gives the best match in full,
// its plot, genres, length and cast, and names the other matches.
func movieAnswer(ctx context.Context, s *Service, in incoming, title string) outcome {
	lang := "ru" // TMDB has few Uzbek titles; Russian is the one most know here
	if languageOf(in) == "en" {
		lang = "en"
	}
	var found struct {
		Titles []movieTitle `json:"titles"`
	}
	if _, failure := s.itemResult(ctx, in, "movies", map[string]string{"action": "search", "query": title, "lang": lang}, &found); failure != "" {
		return say(failedText(in, failure))
	}
	if len(found.Titles) == 0 {
		return say(textNothingFound.to(in))
	}
	best := found.Titles[0]
	var details movieTitle
	args := map[string]any{"action": "details", "id": best.ID, "kind": best.Kind, "lang": lang}
	if _, failure := s.itemResult(ctx, in, "movies", args, &details); failure == "" {
		best = details
	}
	return say(movieText(in, best, found.Titles[1:]))
}

func movieText(in incoming, m movieTitle, others []movieTitle) string {
	lines := []string{"🎬 **" + m.Title + "**" + inBrackets(m.Year)}
	if m.Original != "" {
		lines[0] += " — " + m.Original
	}
	lines = appendIf(lines, m.Rating != "", "⭐ "+m.Rating)
	lines = appendIf(lines, len(m.Genres) > 0, "🎭 "+strings.Join(m.Genres, ", "))
	lines = appendIf(lines, m.Runtime != "", "⏱ "+m.Runtime)
	lines = appendIf(lines, m.Seasons > 0, fmt.Sprintf(textSeasons.to(in), m.Seasons))
	lines = appendIf(lines, len(m.Director) > 0, fmt.Sprintf(textDirector.to(in), strings.Join(m.Director, ", ")))
	lines = appendIf(lines, len(m.Cast) > 0, fmt.Sprintf(textCast.to(in), strings.Join(m.Cast, ", ")))
	text := strings.Join(lines, "\n")
	if m.Overview != "" {
		text += "\n\n" + m.Overview
	}
	text += "\n" + m.Link
	if len(others) > 0 {
		var names []string
		for _, o := range others {
			names = append(names, "• "+o.Title+inBrackets(o.Year))
		}
		text += "\n\n" + textAlsoFound.to(in) + "\n" + strings.Join(names, "\n")
	}
	return text + "\n\n" + textFromTMDB.to(in)
}

// animeTitle is a title as either source gives it: AniList, in English,
// or Shikimori, with its Russian name.
type animeTitle struct {
	Title    string   `json:"title"`
	Native   string   `json:"native"`
	Russian  string   `json:"russian"`
	Name     string   `json:"name"`
	Score    string   `json:"score"`
	Episodes int      `json:"episodes"`
	Year     int      `json:"year"`
	Aired    string   `json:"aired"`
	Genres   []string `json:"genres"`
	Studio   string   `json:"studio"`
	About    string   `json:"about"`
	Link     string   `json:"link"`
}

// name is the title to show, and other its name in another script.
func (a animeTitle) name() (name, other string) {
	if a.Russian != "" {
		return a.Russian, a.Name
	}
	if a.Title != "" {
		return a.Title, a.Native
	}
	return a.Name, ""
}

// animeAnswer finds an anime, with Russian titles and description for a
// Russian speaker, and gives the best match in full.
func animeAnswer(ctx context.Context, s *Service, in incoming, title string) outcome {
	args := map[string]string{"action": "search", "query": title}
	if languageOf(in) == "ru" {
		args["language"] = "ru"
	}
	return itemText(ctx, s, in, "anime", args, func(in incoming, found struct {
		Anime []animeTitle `json:"anime"`
	}) string {
		if len(found.Anime) == 0 {
			return ""
		}
		return animeText(in, found.Anime[0], found.Anime[1:])
	})
}

func animeText(in incoming, a animeTitle, others []animeTitle) string {
	name, other := a.name()
	year := a.Aired
	if a.Year > 0 {
		year = strconv.Itoa(a.Year)
	}
	lines := []string{"🍥 **" + name + "**" + inBrackets(year)}
	if other != "" && other != name {
		lines[0] += " — " + other
	}
	lines = appendIf(lines, a.Score != "", "⭐ "+a.Score)
	lines = appendIf(lines, a.Episodes > 0, fmt.Sprintf(textEpisodes.to(in), a.Episodes))
	lines = appendIf(lines, len(a.Genres) > 0, "🎭 "+strings.Join(a.Genres, ", "))
	lines = appendIf(lines, a.Studio != "", fmt.Sprintf(textStudio.to(in), a.Studio))
	text := strings.Join(lines, "\n")
	if a.About != "" {
		text += "\n\n" + a.About
	}
	text += "\n" + a.Link
	if len(others) > 0 {
		var names []string
		for _, o := range others {
			n, _ := o.name()
			names = append(names, "• "+n)
		}
		text += "\n\n" + textAlsoFound.to(in) + "\n" + strings.Join(names, "\n")
	}
	return text
}

type songs struct {
	Tracks []struct {
		Title    string `json:"title"`
		Artist   string `json:"artist"`
		Duration string `json:"duration"`
		Link     string `json:"link"`
	} `json:"tracks"`
}

// songAnswer sends the best match's 30-second preview, then the matches
// with their links. The preview goes first by itself, not as the answer:
// an answer of a song alone would leave the keyboard hidden after the
// question, and the links are where the whole song is.
func songAnswer(ctx context.Context, s *Service, in incoming, query string) outcome {
	var found songs
	sink, failure := s.itemResult(ctx, in, "music_search", map[string]string{"query": query}, &found)
	if failure != "" {
		return say(failedText(in, failure))
	}
	if len(found.Tracks) == 0 {
		return say(textNothingFound.to(in))
	}
	s.sendAudios(in, sink.TakeAudios())
	lines := []string{textSongs.to(in)}
	for _, t := range found.Tracks {
		lines = append(lines, fmt.Sprintf("• %s — %s (%s)\n%s", t.Artist, t.Title, t.Duration, t.Link))
	}
	return say(strings.Join(lines, "\n\n"))
}

type videos struct {
	Videos []struct {
		Title    string `json:"title"`
		Channel  string `json:"channel"`
		Duration string `json:"duration"`
		Views    string `json:"views"`
		Link     string `json:"link"`
	} `json:"videos"`
}

// youtubeAnswer gives the best video first, its link on a line of its
// own, which Telegram shows as a player, and the others after it.
func youtubeAnswer(ctx context.Context, s *Service, in incoming, query string) outcome {
	return itemText(ctx, s, in, "youtube_search", map[string]string{"query": query}, func(in incoming, found videos) string {
		if len(found.Videos) == 0 {
			return ""
		}
		best := found.Videos[0]
		details := []string{best.Channel}
		details = appendIf(details, best.Duration != "", best.Duration)
		if views, err := strconv.ParseInt(best.Views, 10, 64); err == nil {
			details = append(details, fmt.Sprintf(textViews.to(in), groupWhole(views)))
		}
		text := "▶️ **" + best.Title + "**\n" + strings.Join(details, " · ") + "\n" + best.Link
		if len(found.Videos) > 1 {
			var others []string
			for _, v := range found.Videos[1:] {
				others = append(others, "• "+v.Title+" — "+v.Channel+"\n"+v.Link)
			}
			text += "\n\n" + textMoreVideos.to(in) + "\n" + strings.Join(others, "\n")
		}
		return text
	})
}

// catAnswer sends a cat.
func catAnswer(ctx context.Context, s *Service, in incoming, _ string) outcome {
	return pictureItem(ctx, s, in, "cat_picture", struct{}{}, "🐱")
}

// pictureItem answers with the picture a tool made, captioned.
func pictureItem(ctx context.Context, s *Service, in incoming, name string, args any, caption string) outcome {
	var sent struct{}
	sink, failure := s.itemResult(ctx, in, name, args, &sent)
	if failure != "" {
		return say(failedText(in, failure))
	}
	if pictures := sink.Take(); len(pictures) > 0 {
		return pictureAnswer(caption, pictures)
	}
	return say(textUnavailable.to(in))
}

type triviaQuestion struct {
	Question   string   `json:"question"`
	Options    []string `json:"options"`
	Answer     string   `json:"answer"`
	Category   string   `json:"category"`
	Difficulty string   `json:"difficulty"`
}

// triviaAnswer asks a question with its options, lettered, and gives the
// answer below a few lines, to be read after guessing: the formatter has
// no spoiler to hide it in.
func triviaAnswer(ctx context.Context, s *Service, in incoming, _ string) outcome {
	return itemText(ctx, s, in, "fun", map[string]string{"action": "trivia"}, func(in incoming, q triviaQuestion) string {
		if q.Question == "" || len(q.Options) == 0 {
			return ""
		}
		head := textTrivia.to(in)
		if about := strings.Join(nonEmpty(q.Category, q.Difficulty), ", "); about != "" {
			head += " · " + about
		}
		lines := []string{head, q.Question, ""}
		answer := q.Answer
		for i, option := range q.Options {
			letter := string(rune('A' + i))
			lines = append(lines, letter+") "+option)
			if option == q.Answer {
				answer = letter + ") " + option
			}
		}
		lines = append(lines, "", textTriviaBelow.to(in), "·", "·", "·", fmt.Sprintf(textTriviaAnswer.to(in), answer))
		return strings.Join(lines, "\n")
	})
}

// factAnswer tells a random fact.
func factAnswer(ctx context.Context, s *Service, in incoming, _ string) outcome {
	return itemText(ctx, s, in, "fun", map[string]string{"action": "fact"}, func(_ incoming, f struct {
		Fact string `json:"fact"`
	}) string {
		if f.Fact == "" {
			return ""
		}
		return "💡 " + f.Fact
	})
}

// --- Sport ---

type leagueTable struct {
	Competition string `json:"competition"`
	Group       string `json:"group"`
	Matchday    int    `json:"matchday"`
	Rows        []struct {
		Pos    int    `json:"pos"`
		Team   string `json:"team"`
		Played int    `json:"played"`
		Points int    `json:"points"`
		GD     int    `json:"gd"`
	} `json:"rows"`
}

// tableAnswer gives the league's top ten.
func tableAnswer(ctx context.Context, s *Service, in incoming, league string) outcome {
	args := map[string]string{"action": "table", "competition": league}
	return itemText(ctx, s, in, "football", args, func(in incoming, t leagueTable) string {
		if len(t.Rows) == 0 {
			return ""
		}
		lines := []string{"🏆 **" + competitionHeading(in, t.Competition, t.Group, t.Matchday) + "**", textTableColumns.to(in)}
		for _, r := range t.Rows {
			lines = append(lines, fmt.Sprintf("%d) %s — %d (%d, %+d)", r.Pos, r.Team, r.Points, r.Played, r.GD))
		}
		return strings.Join(lines, "\n")
	})
}

type matchday struct {
	Competition string `json:"competition"`
	Matchday    int    `json:"matchday"`
	Matches     []struct {
		Kickoff string `json:"kickoff_tashkent"`
		Home    string `json:"home"`
		Away    string `json:"away"`
		Score   string `json:"score"`
		Status  string `json:"status"`
	} `json:"matches"`
}

// matchesAnswer gives the matchday's matches, in Tashkent time: a score
// for those played or playing, the kickoff for the rest.
func matchesAnswer(ctx context.Context, s *Service, in incoming, league string) outcome {
	args := map[string]string{"action": "matches", "competition": league}
	return itemText(ctx, s, in, "football", args, func(in incoming, m matchday) string {
		if len(m.Matches) == 0 {
			return ""
		}
		lines := []string{"📅 **" + competitionHeading(in, m.Competition, "", m.Matchday) + "**"}
		for _, match := range m.Matches {
			teams := match.Home + " — " + match.Away
			if match.Score != "" {
				teams = match.Home + " " + match.Score + " " + match.Away
			}
			line := "• " + kickoff(match.Kickoff) + " " + teams
			if mark, ok := matchMarks[match.Status]; ok {
				line += " " + mark.to(in)
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	})
}

// matchMarks show a match that is not simply scheduled or over.
var matchMarks = map[string]localText{
	"playing":   lt("🔴", "🔴", "🔴"),
	"half-time": lt("⏸ tanaffus", "⏸ перерыв", "⏸ half-time"),
	"postponed": lt("(qoldirildi)", "(перенесён)", "(postponed)"),
	"suspended": lt("(to'xtatildi)", "(приостановлен)", "(suspended)"),
	"cancelled": lt("(bekor qilindi)", "(отменён)", "(cancelled)"),
}

// kickoff writes the tool's "Sat 01 Nov 18:00" as "01.11 18:00", without
// the English day.
func kickoff(at string) string {
	t, err := time.Parse("Mon 02 Jan 15:04", at)
	if err != nil {
		return at
	}
	return t.Format("02.01 15:04")
}

type topScorers struct {
	Competition string `json:"competition"`
	Scorers     []struct {
		Player  string `json:"player"`
		Team    string `json:"team"`
		Goals   int    `json:"goals"`
		Assists *int   `json:"assists"`
	} `json:"scorers"`
}

// scorersAnswer gives the league's top five scorers.
func scorersAnswer(ctx context.Context, s *Service, in incoming, league string) outcome {
	args := map[string]string{"action": "scorers", "competition": league}
	return itemText(ctx, s, in, "football", args, func(in incoming, t topScorers) string {
		if len(t.Scorers) == 0 {
			return ""
		}
		lines := []string{fmt.Sprintf(textScorers.to(in), t.Competition)}
		for i, scorer := range t.Scorers {
			line := fmt.Sprintf("%d) %s (%s) — %d ⚽", i+1, scorer.Player, scorer.Team, scorer.Goals)
			if scorer.Assists != nil && *scorer.Assists > 0 {
				line += fmt.Sprintf(" · %d 🅰️", *scorer.Assists)
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	})
}

// competitionHeading names a competition with its group and matchday.
func competitionHeading(in incoming, competition, group string, day int) string {
	heading := strings.Join(nonEmpty(competition, group), ", ")
	if day > 0 {
		heading += fmt.Sprintf(textMatchday.to(in), day)
	}
	return heading
}

// --- Games ---

// gameAnswer posts the game named, which is the answer by itself: its
// message has the Play button.
func gameAnswer(name string) codeAnswer {
	return func(ctx context.Context, s *Service, in incoming, _ string) outcome {
		var sent struct{}
		args := struct {
			Game string `json:"game"`
		}{name}
		switch _, failure := s.itemResult(ctx, in, "send_game", args, &sent); failure {
		case "":
			return outcome{}
		case "invalid_arguments":
			return say(textGameTooSoon.to(in)) // two games in ten minutes
		}
		return say(textGameLater.to(in))
	}
}

type freeGames struct {
	Games []struct {
		Title    string `json:"title"`
		Genre    string `json:"genre"`
		Platform string `json:"platform"`
		About    string `json:"about"`
		Link     string `json:"link"`
	} `json:"games"`
}

// freeGamesAnswer lists the most popular free games.
func freeGamesAnswer(ctx context.Context, s *Service, in incoming, _ string) outcome {
	return itemText(ctx, s, in, "free_games", struct{}{}, func(in incoming, found freeGames) string {
		if len(found.Games) == 0 {
			return ""
		}
		lines := []string{textFreeGames.to(in)}
		for _, g := range found.Games {
			line := "• **" + g.Title + "** — " + strings.Join(nonEmpty(g.Genre, g.Platform), ", ")
			if g.About != "" {
				line += "\n" + g.About
			}
			lines = append(lines, line+"\n"+g.Link)
		}
		return strings.Join(lines, "\n\n")
	})
}

type steamGame struct {
	Game       string   `json:"game"`
	About      string   `json:"about"`
	Released   string   `json:"released"`
	Link       string   `json:"link"`
	Genres     []string `json:"genres"`
	Price      string   `json:"price"`
	Normal     string   `json:"normal_price"`
	Discount   string   `json:"discount"`
	Metacritic int      `json:"metacritic"`
	Players    int64    `json:"playing_now_on_steam"`
}

// steamAnswer gives a game's Steam page, priced for Uzbekistan.
func steamAnswer(ctx context.Context, s *Service, in incoming, name string) outcome {
	args := map[string]string{"action": "steam_game", "name": name}
	return itemText(ctx, s, in, "games", args, func(in incoming, g steamGame) string {
		if g.Game == "" {
			return ""
		}
		lines := []string{"🎮 **" + g.Game + "**"}
		if g.Price != "" {
			price := g.Price
			if said, ok := steamPrices[price]; ok {
				price = said.to(in)
			}
			if g.Discount != "" {
				price += fmt.Sprintf(textDiscount.to(in), g.Discount, g.Normal)
			}
			lines = append(lines, fmt.Sprintf(textSteamPrice.to(in), price))
		}
		lines = appendIf(lines, len(g.Genres) > 0, "🎭 "+strings.Join(g.Genres, ", "))
		lines = appendIf(lines, g.Released != "", fmt.Sprintf(textReleased.to(in), g.Released))
		lines = appendIf(lines, g.Metacritic > 0, fmt.Sprintf("⭐ Metacritic: %d", g.Metacritic))
		lines = appendIf(lines, g.Players > 0, fmt.Sprintf(textPlayingNow.to(in), groupWhole(g.Players)))
		text := strings.Join(lines, "\n")
		if g.About != "" {
			text += "\n\n" + g.About
		}
		return text + "\n" + g.Link
	})
}

// steamPrices are the prices the tool writes in words.
var steamPrices = map[string]localText{
	"free to play": lt("bepul", "бесплатно", "free to play"),
	"not out yet":  lt("hali chiqmagan", "ещё не вышла", "not out yet"),
}

// --- Study ---

// wikiAnswer gives the best article's introduction, in the user's
// language when Wikipedia has it.
func wikiAnswer(ctx context.Context, s *Service, in incoming, query string) outcome {
	args := map[string]string{"query": query, "lang": languageOf(in)}
	return itemText(ctx, s, in, "wiki_lookup", args, func(_ incoming, w struct {
		Title   string `json:"title"`
		Extract string `json:"extract"`
		URL     string `json:"url"`
	}) string {
		if w.Extract == "" {
			return ""
		}
		return "📘 **" + w.Title + "**\n\n" + w.Extract + "\n" + w.URL
	})
}

type dictionaryEntry struct {
	Word     string `json:"word"`
	Phonetic string `json:"phonetic"`
	Meanings []struct {
		PartOfSpeech string `json:"part_of_speech"`
		Definition   string `json:"definition"`
		Example      string `json:"example"`
	} `json:"meanings"`
}

// englishAnswer gives an English word's pronunciation and a meaning for
// each part of speech, with an example where there is one.
func englishAnswer(ctx context.Context, s *Service, in incoming, word string) outcome {
	return itemText(ctx, s, in, "english_word", map[string]string{"word": word}, func(in incoming, e dictionaryEntry) string {
		if len(e.Meanings) == 0 {
			return ""
		}
		lines := []string{strings.TrimSpace("🔤 **" + e.Word + "** " + e.Phonetic)}
		for _, m := range e.Meanings {
			line := "• " + m.PartOfSpeech + " — " + m.Definition
			if m.Example != "" {
				line += "\n  " + fmt.Sprintf(textExample.to(in), m.Example)
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	})
}

// synonymsAnswer gives an English word's synonyms.
func synonymsAnswer(ctx context.Context, s *Service, in incoming, word string) outcome {
	args := map[string]string{"action": "synonyms", "query": word}
	return itemText(ctx, s, in, "study", args, func(in incoming, found struct {
		Word     string   `json:"word"`
		Synonyms []string `json:"synonyms"`
	}) string {
		if len(found.Synonyms) == 0 {
			return ""
		}
		return fmt.Sprintf(textSynonyms.to(in), found.Word, strings.Join(found.Synonyms, ", "))
	})
}

type papers struct {
	Papers []struct {
		Title   string   `json:"title"`
		Authors []string `json:"authors"`
		Year    int      `json:"year"`
		Journal string   `json:"journal"`
		Cited   int      `json:"cited_by"`
		Link    string   `json:"link"`
	} `json:"papers"`
}

// papersAnswer lists scholarly articles on the topic, from Crossref.
func papersAnswer(ctx context.Context, s *Service, in incoming, topic string) outcome {
	args := map[string]string{"action": "papers", "query": topic}
	return itemText(ctx, s, in, "study", args, func(in incoming, found papers) string {
		if len(found.Papers) == 0 {
			return ""
		}
		lines := []string{fmt.Sprintf(textPapers.to(in), topic)}
		for _, p := range found.Papers {
			year := ""
			if p.Year > 0 {
				year = strconv.Itoa(p.Year)
			}
			line := "• **" + p.Title + "**" + inBrackets(year)
			details := nonEmpty(strings.Join(p.Authors, ", "), p.Journal)
			if p.Cited > 0 {
				details = append(details, fmt.Sprintf(textCited.to(in), p.Cited))
			}
			if len(details) > 0 {
				line += "\n" + strings.Join(details, " · ")
			}
			lines = append(lines, line+"\n"+p.Link)
		}
		return strings.Join(lines, "\n\n")
	})
}

// --- Useful ---

// qrAnswer sends the QR code of what was written, with it as the caption
// so that a mistyped link is seen.
func qrAnswer(ctx context.Context, s *Service, in incoming, text string) outcome {
	return pictureItem(ctx, s, in, "qr_code", map[string]string{"text": text}, "🔳 "+truncate(text, 200))
}

// drawAnswer draws what was written. The tool keeps the day's limit of
// pictures, and a refusal says which it was: the limit, a picture the
// image model will not make, or a model that is busy.
func drawAnswer(ctx context.Context, s *Service, in incoming, prompt string) outcome {
	var drawn struct{}
	sink, failure := s.itemResult(ctx, in, "generate_image", map[string]string{"prompt": prompt}, &drawn)
	switch failure {
	case "":
		if pictures := sink.Take(); len(pictures) > 0 {
			return pictureAnswer("🎨 "+truncate(prompt, 200), pictures)
		}
	case "daily_limit":
		return say(textDrawLimit.to(in))
	case "invalid_arguments":
		return say(textDrawRefused.to(in))
	}
	return say(textDrawFailed.to(in))
}

// placeAnswer sends the place's pin, then names the place found, so that
// a wrong match is seen, with the keyboard the question hid.
func placeAnswer(ctx context.Context, s *Service, in incoming, place string) outcome {
	var sent struct {
		Place string `json:"place"`
	}
	switch _, failure := s.itemResult(ctx, in, "send_location", map[string]string{"place": place}, &sent); failure {
	case "":
		return say("📍 " + sent.Place)
	case "invalid_arguments":
		return say(textPlaceUnknown.to(in))
	}
	return say(textUnavailable.to(in))
}

type holidayList struct {
	Year     int `json:"year"`
	Holidays []struct {
		Date   string `json:"date"`
		Name   string `json:"name"`
		Type   string `json:"type"`
		InDays int    `json:"in_days"`
	} `json:"holidays"`
}

// holidaysAnswer gives Uzbekistan's next holidays and days off, and the
// Saturdays made working days, counting the days to each; late in the
// year, when none is left, next year's.
func holidaysAnswer(ctx context.Context, s *Service, in incoming, _ string) outcome {
	var list holidayList
	if _, failure := s.itemResult(ctx, in, "holidays", struct{}{}, &list); failure != "" {
		return say(failedText(in, failure))
	}
	if text := holidaysText(in, list); text != "" {
		return say(text)
	}
	var next holidayList
	if _, failure := s.itemResult(ctx, in, "holidays", map[string]int{"year": list.Year + 1}, &next); failure == "" {
		if text := holidaysText(in, next); text != "" {
			return say(text)
		}
	}
	return say(textNothingFound.to(in))
}

// shownHolidays is how many of the coming holidays are listed.
const shownHolidays = 8

func holidaysText(in incoming, list holidayList) string {
	lines := []string{fmt.Sprintf(textHolidays.to(in), list.Year)}
	for _, h := range list.Holidays {
		if h.InDays < 0 || len(lines) > shownHolidays {
			continue
		}
		mark := "📌"
		switch h.Type {
		case "Public Holiday":
			mark = "🎉"
		case "Working Day on a Weekend":
			mark = "💼"
		}
		lines = append(lines, fmt.Sprintf("%s %s — %s, %s", mark, dotDate(h.Date), h.Name, inDays(in, h.InDays)))
	}
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}

// inDays says how far off a day is: today, tomorrow, in so many days.
func inDays(in incoming, days int) string {
	switch days {
	case 0:
		return textToday.to(in)
	case 1:
		return textTomorrow.to(in)
	}
	return fmt.Sprintf(textInDays.to(in), days)
}

type coinPrices struct {
	Coins []struct {
		Name   string   `json:"name"`
		Symbol string   `json:"symbol"`
		USD    float64  `json:"usd"`
		UZS    *float64 `json:"uzs"`
		Change *float64 `json:"change_24h_pct"`
	} `json:"coins"`
}

// cryptoAnswer gives the coins asked about most, in dollars and so'm, with
// the day's change.
func cryptoAnswer(ctx context.Context, s *Service, in incoming, _ string) outcome {
	args := map[string][]string{"coins": {"btc", "ton", "eth"}}
	return itemText(ctx, s, in, "crypto_price", args, func(in incoming, prices coinPrices) string {
		if len(prices.Coins) == 0 {
			return ""
		}
		lines := []string{textCrypto.to(in)}
		for _, c := range prices.Coins {
			line := fmt.Sprintf("• **%s** (%s): $%s", c.Name, c.Symbol, dollars(c.USD))
			if c.Change != nil {
				arrow := "📈 +"
				if *c.Change < 0 {
					arrow = "📉 "
				}
				line += fmt.Sprintf(" · %s%.2f%%", arrow, *c.Change)
			}
			if c.UZS != nil {
				line += "\n  ≈ " + groupWhole(int64(math.Round(*c.UZS))) + " " + textSom.to(in)
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n") + "\n\n" + textCryptoNote.to(in)
	})
}

// dollars writes a price as rates are written, 67 123,45, and a coin worth
// less than a dollar to its first figures, 0,004512.
func dollars(v float64) string {
	if v >= 1 {
		return groupDigits(v)
	}
	return strings.ReplaceAll(strconv.FormatFloat(v, 'g', 4, 64), ".", ",")
}

type earthquakes struct {
	Earthquakes []struct {
		Time      string  `json:"time"`
		Magnitude float64 `json:"magnitude"`
		Place     string  `json:"place"`
		DepthKM   float64 `json:"depth_km"`
	} `json:"earthquakes"`
}

// quakesAnswer gives the week's earthquakes of magnitude 3 or more in
// Central Asia, newest first; a week without any is said to be.
func quakesAnswer(ctx context.Context, s *Service, in incoming, _ string) outcome {
	var found earthquakes
	if _, failure := s.itemResult(ctx, in, "earthquakes", map[string]string{"area": "central_asia"}, &found); failure != "" {
		return say(failedText(in, failure))
	}
	if len(found.Earthquakes) == 0 {
		return say(textNoQuakes.to(in))
	}
	lines := []string{textQuakes.to(in)}
	for _, q := range found.Earthquakes {
		lines = append(lines, fmt.Sprintf("• %s — M%.1f, %s, %s", q.Time, q.Magnitude, q.Place, fmt.Sprintf(textDepth.to(in), q.DepthKM)))
	}
	return say(strings.Join(lines, "\n"))
}

type webResults struct {
	Answer  string `json:"answer"`
	Results []struct {
		Title string `json:"title"`
		Site  string `json:"site"`
		URL   string `json:"url"`
	} `json:"results"`
}

// shownResults is how many of a search's sites are listed.
const shownResults = 3

// searchAnswer gives the search's short answer, as the search engine
// wrote it, and the first sites with their links.
func searchAnswer(ctx context.Context, s *Service, in incoming, query string) outcome {
	return itemText(ctx, s, in, "web_search", map[string]string{"query": query}, func(in incoming, found webResults) string {
		if found.Answer == "" && len(found.Results) == 0 {
			return ""
		}
		text := "🔎 **" + query + "**"
		if found.Answer != "" {
			text += "\n\n" + found.Answer
		}
		if len(found.Results) > 0 {
			var sites []string
			for _, r := range found.Results[:min(len(found.Results), shownResults)] {
				sites = append(sites, "• "+r.Title+" — "+r.Site+"\n"+r.URL)
			}
			text += "\n\n" + textSources.to(in) + "\n" + strings.Join(sites, "\n")
		}
		return text
	})
}

// --- Writing ---

// appendIf appends line when ok.
func appendIf(lines []string, ok bool, line string) []string {
	if ok {
		return append(lines, line)
	}
	return lines
}

// inBrackets is " (text)", or "" for no text.
func inBrackets(text string) string {
	if text == "" {
		return ""
	}
	return " (" + text + ")"
}

// dotDate writes a tool's 2026-10-30 as people here do, 30.10.2026.
func dotDate(date string) string {
	t, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return date
	}
	return t.Format("02.01.2006")
}

// groupWhole writes a count with spaces between thousands: 1 234 567.
func groupWhole(n int64) string {
	grouped, _, _ := strings.Cut(groupDigits(float64(n)), ",")
	return grouped
}

var (
	textNothingFound = lt("🤷 Hech narsa topilmadi. Boshqacha yozib ko'ring.", "🤷 Ничего не нашла. Попробуйте написать иначе.", "🤷 I found nothing. Try writing it another way.")
	textUnavailable  = lt("⚠️ Hozir ma'lumot olib bo'lmadi, birozdan keyin qayta urinib ko'ring.", "⚠️ Сейчас не получилось узнать, попробуйте чуть позже.", "⚠️ I couldn't get that just now; try again in a little while.")

	textPrayerUnknown = lt(
		"🕋 Bu joyni topa olmadim. Namoz vaqtlarini O'zbekiston shahar va viloyatlari uchun bilaman, masalan Toshkent yoki Qarshi.",
		"🕋 Не нашла это место. Время намаза я знаю для городов и областей Узбекистана, например Ташкент или Карши.",
		"🕋 I couldn't find that place. I know the prayer times of Uzbekistan's towns and regions, such as Tashkent or Qarshi.")
	textPrayerNote = lt(
		"Vaqtlar taxminiy, bir necha daqiqa farq qilishi mumkin; rasmiysi muslim.uz'da.",
		"Время примерное, может отличаться на несколько минут; официальное — на muslim.uz.",
		"The times are approximate, within a few minutes; the official ones are on muslim.uz.")
	// textTranslator takes the translator's name.
	textTranslator = lt("— %s tarjimasi", "— перевод: %s", "— translated by %s")
	// textSurah takes the surah's number, name, meaning, verses and where
	// it was revealed.
	textSurah = lt("📖 **%d. %s** (%s): %d oyat, %s nozil bo'lgan.", "📖 **%d. %s** (%s): аятов — %d, ниспослана в %s.", "📖 **%d. %s** (%s): %d verses, revealed in %s.")
	// textVersesFound takes how many verses have the words.
	textVersesFound = lt("📖 Topilgan oyatlar: %d", "📖 Найдено аятов: %d", "📖 Verses found: %d")

	textUzbekNews = lt("📰 **O'zbekiston yangiliklari**", "📰 **Новости Узбекистана**", "📰 **Uzbekistan's news**")
	textWorldNews = lt("🌍 **Jahon yangiliklari**", "🌍 **Мировые новости**", "🌍 **World news**")

	textSeasons   = lt("📺 Fasllar: %d", "📺 Сезонов: %d", "📺 Seasons: %d")
	textDirector  = lt("🎬 Rejissyor: %s", "🎬 Режиссёр: %s", "🎬 Directed by %s")
	textCast      = lt("👥 Rollarda: %s", "👥 В ролях: %s", "👥 Starring %s")
	textAlsoFound = lt("Yana topildi:", "Ещё нашла:", "Also found:")
	textFromTMDB  = lt("Ma'lumot: TMDB", "Данные: TMDB", "Data: TMDB")
	textEpisodes  = lt("📺 Qismlar: %d", "📺 Серий: %d", "📺 Episodes: %d")
	textStudio    = lt("🏢 Studiya: %s", "🏢 Студия: %s", "🏢 Studio: %s")
	textSongs     = lt("🎵 Topilgan qo'shiqlar:", "🎵 Нашла песни:", "🎵 The songs I found:")
	// textViews takes the count of views.
	textViews        = lt("%s marta ko'rilgan", "%s просмотров", "%s views")
	textMoreVideos   = lt("Yana:", "Ещё:", "More:")
	textTrivia       = lt("❔ **Viktorina**", "❔ **Викторина**", "❔ **Trivia**")
	textTriviaBelow  = lt("Javob pastda, avval o'ylab ko'ring 👇", "Ответ ниже, сначала подумайте 👇", "The answer is below; think first 👇")
	textTriviaAnswer = lt("✅ Javob: %s", "✅ Ответ: %s", "✅ Answer: %s")

	textTableColumns = lt("O'rin) jamoa — ochko (o'yinlar, to'p farqi)", "Место) команда — очки (игры, разница мячей)", "Place) team — points (played, goal difference)")
	// textMatchday takes the matchday's number.
	textMatchday = lt(", %d-tur", ", %d-й тур", ", matchday %d")
	// textScorers takes the competition.
	textScorers = lt("👟 **%s: to'purarlar**", "👟 **%s: бомбардиры**", "👟 **%s: top scorers**")

	textGameTooSoon = lt(
		"⚖️ So'nggi 10 daqiqada o'yin yuborilgan edi: yuqoridagisini o'ynang.",
		"⚖️ Игру уже присылала за последние 10 минут: играйте в ту, что выше.",
		"⚖️ I sent the game within the last 10 minutes: play the one above.")
	textGameLater = lt("⚖️ Hozir o'yinni yubora olmadim, birozdan keyin qayta bosing.", "⚖️ Сейчас не получилось отправить игру, нажмите чуть позже.", "⚖️ I couldn't send the game just now; tap again in a little while.")
	textFreeGames = lt("🆓 **Mashhur bepul o'yinlar**", "🆓 **Популярные бесплатные игры**", "🆓 **Popular free games**")
	// textSteamPrice takes the price, textDiscount the discount and the
	// usual price.
	textSteamPrice = lt("💲 O'zbekistonda narxi: %s", "💲 Цена в Узбекистане: %s", "💲 Price in Uzbekistan: %s")
	textDiscount   = lt(" (−%s, odatda %s)", " (−%s, обычно %s)", " (−%s, usually %s)")
	textReleased   = lt("📅 Chiqqan: %s", "📅 Вышла: %s", "📅 Released: %s")
	textPlayingNow = lt("👥 Hozir Steam'da o'ynayapti: %s", "👥 Сейчас играют в Steam: %s", "👥 Playing on Steam now: %s")

	textExample  = lt("Misol: «%s»", "Пример: «%s»", "Example: “%s”")
	textSynonyms = lt("🔁 **%s** sinonimlari: %s", "🔁 Синонимы к **%s**: %s", "🔁 Synonyms of **%s**: %s")
	// textPapers takes the topic, textCited how often a paper is cited.
	textPapers = lt("🎓 **Ilmiy maqolalar: %s**", "🎓 **Научные статьи: %s**", "🎓 **Papers on %s**")
	textCited  = lt("%d marta iqtibos keltirilgan", "цитирований: %d", "cited %d times")

	textDrawLimit    = lt("🎨 Bugungi rasmlaringiz tugadi. Yarim tundan keyin yana chizaman.", "🎨 Картинки на сегодня закончились. После полуночи нарисую снова.", "🎨 You've had today's pictures. I'll draw again after midnight.")
	textDrawRefused  = lt("🎨 Buni chiza olmayman. Boshqa narsani so'rang.", "🎨 Это я нарисовать не могу. Попросите что-нибудь другое.", "🎨 I can't draw that. Ask for something else.")
	textDrawFailed   = lt("🎨 Hozir chiza olmadim, birozdan keyin qayta urinib ko'ring.", "🎨 Сейчас не получилось нарисовать, попробуйте чуть позже.", "🎨 I couldn't draw just now; try again in a little while.")
	textPlaceUnknown = lt(
		"📍 Bunday joyni topa olmadim. Shahar, tuman yoki viloyat nomini yozing.",
		"📍 Не нашла такое место. Напишите название города, района или области.",
		"📍 I couldn't find that place. Write the name of a town, district or region.")
	// textHolidays takes the year.
	textHolidays = lt("🎉 **Bayramlar va dam olish kunlari, %d**", "🎉 **Праздники и выходные, %d**", "🎉 **Holidays and days off, %d**")
	textToday    = lt("bugun", "сегодня", "today")
	textTomorrow = lt("ertaga", "завтра", "tomorrow")
	// textInDays takes the days to wait.
	textInDays     = lt("%d kundan keyin", "через %d дн.", "in %d days")
	textCrypto     = lt("₿ **Kripto narxlari**", "₿ **Курсы криптовалют**", "₿ **Crypto prices**")
	textSom        = lt("so'm", "сум", "so'm")
	textCryptoNote = lt("CoinGecko narxlari; maslahat emas.", "Цены CoinGecko; это не совет.", "CoinGecko's prices; not advice.")
	textQuakes     = lt("🌋 **So'nggi zilzilalar, O'rta Osiyo** (3+, Toshkent vaqti)", "🌋 **Последние землетрясения, Центральная Азия** (3+, время ташкентское)", "🌋 **Latest earthquakes, Central Asia** (3+, Tashkent time)")
	textNoQuakes   = lt("🌋 So'nggi 7 kunda O'rta Osiyoda 3 va undan kuchli zilzila bo'lmadi.", "🌋 За последние 7 дней в Центральной Азии не было землетрясений магнитудой от 3.", "🌋 No earthquake of magnitude 3 or more in Central Asia in the last 7 days.")
	// textDepth takes the depth in km.
	textDepth   = lt("chuqurligi %.0f km", "глубина %.0f км", "%.0f km deep")
	textSources = lt("Manbalar:", "Источники:", "Sources:")
)
