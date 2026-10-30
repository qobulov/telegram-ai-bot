package service

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// useItem is the user using an item of «📋 Ko'proq» with value: replying
// to its question, pressing its choice, or pressing it.
func useItem(t *testing.T, key, value string) telegram.Update {
	t.Helper()
	item, ok := findItem(key)
	switch {
	case !ok:
		t.Fatalf("no item %q", key)
	case item.ask != nil:
		return replyTo(item.ask.question.uz, value)
	case len(item.choices) > 0:
		return menuPress(1, fmt.Sprintf("kb:pick:%s:%d", key, slices.Index(item.choices, value)))
	}
	return menuPress(1, "kb:item:"+key)
}

// succeeded wraps a tool's result in the registry's envelope.
func succeeded(result string) string { return `{"ok":true,"result":` + result + `}` }

// Every item of «📋 Ko'proq» has its answer in code.
func TestEveryMoreItemIsAnsweredInCode(t *testing.T) {
	for _, section := range moreSections {
		for _, item := range section.items {
			if _, inCode := codeAnswers[item.key]; !inCode {
				t.Errorf("%s has no code answer", item.key)
			}
		}
	}
}

// Each item runs its tool with the arguments it needs and writes the
// result itself, with the keyboard back, never asking the model.
func TestMoreItemsAnswerWithTheirTools(t *testing.T) {
	for _, c := range []struct {
		key, value, tool, result string
		// args are in what the tool was asked, want in what was sent.
		args, want []string
		photo      bool
	}{
		{key: "prayer", value: "Qarshi", tool: "prayer_times",
			result: `{"region":"Qarshi","date":"2026-10-30","hijri":{"day":19,"month":"Jumodul avval","year":1448},"bomdod":"05:28","quyosh":"06:49","peshin":"12:05","asr":"15:20","shom":"17:20","xufton":"18:36"}`,
			args:   []string{`prayer_times {"region":"Qarshi"}`},
			want:   []string{"Qarshi, 30.10.2026", "(19 Jumodul avval 1448)", "Bomdod — 05:28", "Xufton — 18:36", "muslim.uz"}},
		{key: "quran", value: "2:255", tool: "quran",
			result: `{"verses":[{"ref":"2:255","translation":"Alloh — Undan o'zga iloh yo'q."}],"translator":"Muhammad Sodiq Muhammad Yusuf","link":"https://quran.com/2/255"}`,
			args:   []string{`"action":"verse"`, `"ref":"2:255"`},
			want:   []string{"**2:255** Alloh", "Muhammad Sodiq Muhammad Yusuf tarjimasi", "https://quran.com/2/255"}},
		{key: "news_uz", tool: "news_headlines",
			result: `{"items":[{"title":"Yangi metro bekati ochildi","source":"kun.uz","link":"https://kun.uz/1","time":"14:05"}]}`,
			args:   []string{`"lang":"uz"`, `"world":false`},
			want:   []string{"O'zbekiston yangiliklari", "• 14:05 Yangi metro bekati ochildi (kun.uz)\nhttps://kun.uz/1"}},
		{key: "news_world", tool: "news_headlines",
			result: `{"items":[{"title":"Summit opens","source":"BBC","link":"https://bbc.com/1"}]}`,
			args:   []string{`"world":true`},
			want:   []string{"Jahon yangiliklari", "• Summit opens (BBC)"}},
		{key: "movie", value: "Interstellar", tool: "movies",
			result: `{"titles":[{"id":157336,"kind":"movie","title":"Интерстеллар","year":"2014"},{"id":2,"kind":"tv","title":"Interstellar Wars","year":"2016"}],` +
				`"id":157336,"kind":"movie","title":"Интерстеллар","original_title":"Interstellar","year":"2014","rating":"8.4/10 (35000 votes)","overview":"Путешествие сквозь червоточину.",` +
				`"link":"https://www.themoviedb.org/movie/157336","genres":["фантастика"],"runtime":"169 min","directed_or_created_by":["Christopher Nolan"],"cast":["Matthew McConaughey"]}`,
			args: []string{`"action":"search"`, `"action":"details","id":157336,"kind":"movie","lang":"ru"`},
			want: []string{"🎬 **Интерстеллар** (2014) — Interstellar", "⭐ 8.4/10", "Rejissyor: Christopher Nolan", "Rollarda: Matthew McConaughey", "Путешествие", "• Interstellar Wars (2016)", "TMDB"}},
		{key: "anime", value: "Naruto", tool: "anime",
			result: `{"anime":[{"title":"Naruto","native":"ナルト","score":"79/100","episodes":220,"year":2002,"genres":["Action"],"studio":"Pierrot","about":"A ninja.","link":"https://anilist.co/anime/20"}]}`,
			args:   []string{`anime {"action":"search","query":"Naruto"}`},
			want:   []string{"🍥 **Naruto** (2002) — ナルト", "Qismlar: 220", "Studiya: Pierrot", "https://anilist.co/anime/20"}},
		{key: "song", value: "Shohruhxon", tool: "music_search",
			result: `{"tracks":[{"title":"Yurak","artist":"Shohruhxon","duration":"3:21","link":"https://www.deezer.com/track/1"}]}`,
			args:   []string{`music_search {"query":"Shohruhxon"}`},
			want:   []string{"• Shohruhxon — Yurak (3:21)\nhttps://www.deezer.com/track/1"}},
		{key: "youtube", value: "Ling", tool: "youtube_search",
			result: `{"videos":[{"title":"Ling guide","channel":"MLBB Pro","duration":"12:34","views":"1234567","link":"https://youtu.be/a"},{"title":"Ling combo","channel":"X","link":"https://youtu.be/b"}]}`,
			args:   []string{`youtube_search {"query":"Ling"}`},
			want:   []string{"▶️ **Ling guide**\nMLBB Pro · 12:34 · 1 234 567 marta ko'rilgan\nhttps://youtu.be/a", "• Ling combo — X\nhttps://youtu.be/b"}},
		{key: "cat", tool: "cat_picture", result: `{"sent":true}`, args: []string{"cat_picture {}"}, want: []string{"🐱"}, photo: true},
		{key: "trivia", tool: "fun",
			result: `{"question":"What is the capital of France?","options":["Lyon","Paris","Nice","Lille"],"answer":"Paris","category":"Geography","difficulty":"easy"}`,
			args:   []string{`fun {"action":"trivia"}`},
			want:   []string{"Viktorina** · Geography, easy\nWhat is the capital of France?", "A) Lyon\nB) Paris", "D) Lille\n\nJavob pastda", "✅ Javob: B) Paris"}},
		{key: "fact", tool: "fun", result: `{"fact":"Honey never spoils."}`, args: []string{`fun {"action":"fact"}`}, want: []string{"💡 Honey never spoils."}},
		{key: "table", value: "La Liga", tool: "football",
			result: `{"competition":"Primera Division","matchday":11,"rows":[{"pos":1,"team":"Real Madrid","played":10,"points":25,"gd":15},{"pos":2,"team":"Barça","played":10,"points":22,"gd":-1}]}`,
			args:   []string{`football {"action":"table","competition":"La Liga"}`},
			want:   []string{"🏆 **Primera Division, 11-tur**", "1) Real Madrid — 25 (10, +15)\n2) Barça — 22 (10, -1)"}},
		{key: "matches", value: "Premier League", tool: "football",
			result: `{"competition":"Premier League","matchday":10,"matches":[{"kickoff_tashkent":"Sat 01 Nov 18:00","home":"Arsenal","away":"Chelsea","score":"2-1","status":"finished"},` +
				`{"kickoff_tashkent":"Sun 02 Nov 23:00","home":"Everton","away":"Fulham","status":"scheduled"},{"kickoff_tashkent":"Sun 02 Nov 20:00","home":"Leeds","away":"Wolves","score":"1-0","status":"playing"}]}`,
			args: []string{`football {"action":"matches","competition":"Premier League"}`},
			want: []string{"📅 **Premier League, 10-tur**", "• 01.11 18:00 Arsenal 2-1 Chelsea\n", "• 02.11 23:00 Everton — Fulham", "• 02.11 20:00 Leeds 1-0 Wolves 🔴"}},
		{key: "scorers", value: "Serie A", tool: "football",
			result: `{"competition":"Serie A","scorers":[{"player":"Lautaro Martínez","team":"Inter","goals":9,"assists":3},{"player":"Dovbyk","team":"Roma","goals":7}]}`,
			args:   []string{`"action":"scorers","competition":"Serie A"`},
			want:   []string{"Serie A: to'purarlar", "1) Lautaro Martínez (Inter) — 9 ⚽ · 3 🅰️\n2) Dovbyk (Roma) — 7 ⚽"}},
		{key: "free_games", tool: "free_games",
			result: `{"games":[{"title":"Warframe","genre":"Shooter","platform":"PC (Windows)","about":"A co-op shooter.","link":"https://www.freetogame.com/open/warframe"}]}`,
			args:   []string{"free_games {}"},
			want:   []string{"Mashhur bepul o'yinlar", "• **Warframe** — Shooter, PC (Windows)\nA co-op shooter.\nhttps://www.freetogame.com/open/warframe"}},
		{key: "steam", value: "Dota", tool: "games",
			result: `{"game":"Dota 2","about":"A MOBA.","released":"9 Jul, 2013","link":"https://store.steampowered.com/app/570","genres":["Action","Strategy"],"price":"free to play","metacritic":90,"playing_now_on_steam":612345}`,
			args:   []string{`games {"action":"steam_game","name":"Dota"}`},
			want:   []string{"🎮 **Dota 2**", "O'zbekistonda narxi: bepul", "🎭 Action, Strategy", "Chiqqan: 9 Jul, 2013", "Metacritic: 90", "o'ynayapti: 612 345", "https://store.steampowered.com/app/570"}},
		{key: "wiki", value: "Temur", tool: "wiki_lookup",
			result: `{"title":"Amir Temur","extract":"Amir Temur (1336–1405) — sarkarda.","url":"https://uz.wikipedia.org/wiki/Amir_Temur","lang":"uz"}`,
			args:   []string{`wiki_lookup {"lang":"uz","query":"Temur"}`},
			want:   []string{"📘 **Amir Temur**\n\nAmir Temur (1336–1405) — sarkarda.\nhttps://uz.wikipedia.org/wiki/Amir_Temur"}},
		{key: "english", value: "resilient", tool: "english_word",
			result: `{"word":"resilient","phonetic":"/rɪˈzɪliənt/","meanings":[{"part_of_speech":"adjective","definition":"Able to recover quickly.","example":"a resilient economy"}]}`,
			args:   []string{`english_word {"word":"resilient"}`},
			want:   []string{"🔤 **resilient** /rɪˈzɪliənt/", "• adjective — Able to recover quickly.\n  Misol: «a resilient economy»"}},
		{key: "synonyms", value: "happy", tool: "study", result: `{"word":"happy","synonyms":["glad","felicitous"]}`,
			args: []string{`study {"action":"synonyms","query":"happy"}`}, want: []string{"**happy** sinonimlari: glad, felicitous"}},
		{key: "papers", value: "genetics", tool: "study",
			result: `{"papers":[{"title":"Deep learning","authors":["Yann LeCun","Yoshua Bengio"],"year":2015,"journal":"Nature","cited_by":50000,"link":"https://doi.org/10.1038/nature14539"}]}`,
			args:   []string{`study {"action":"papers","query":"genetics"}`},
			want:   []string{"Ilmiy maqolalar: genetics", "• **Deep learning** (2015)\nYann LeCun, Yoshua Bengio · Nature · 50000 marta iqtibos keltirilgan\nhttps://doi.org/10.1038/nature14539"}},
		{key: "qr", value: "https://t.me/mira", tool: "qr_code", result: `{"sent":true}`,
			args: []string{`qr_code {"text":"https://t.me/mira"}`}, want: []string{"🔳 https://t.me/mira"}, photo: true},
		{key: "draw", value: "qal'a", tool: "generate_image", result: `{"drawn":true}`,
			args: []string{`generate_image {"prompt":"qal'a"}`}, want: []string{"🎨 qal'a"}, photo: true},
		{key: "place", value: "Samarqand", tool: "send_location", result: `{"sent":true,"place":"Samarqand"}`,
			args: []string{`send_location {"place":"Samarqand"}`}, want: []string{"📍 Samarqand"}},
		{key: "holidays", tool: "holidays",
			result: `{"country":"UZ","year":2026,"holidays":[{"date":"2026-03-21","name":"Nowruz","type":"Public Holiday","in_days":-223},` +
				`{"date":"2026-12-08","name":"Constitution Day","type":"Public Holiday","in_days":39},{"date":"2026-12-12","name":"Working Day","type":"Working Day on a Weekend","in_days":43}]}`,
			args: []string{"holidays {}"},
			want: []string{"dam olish kunlari, 2026**\n🎉 08.12.2026 — Constitution Day, 39 kundan keyin\n💼 12.12.2026 — Working Day, 43 kundan keyin"}},
		{key: "crypto", tool: "crypto_price",
			result: `{"coins":[{"name":"Bitcoin","symbol":"BTC","usd":67123.45,"uzs":856000000,"change_24h_pct":2.31},{"name":"Toncoin","symbol":"TON","usd":0.5123,"change_24h_pct":-1.5}]}`,
			args:   []string{`crypto_price {"coins":["btc","ton","eth"]}`},
			want:   []string{"• **Bitcoin** (BTC): $67 123,45 · 📈 +2.31%\n  ≈ 856 000 000 so'm", "• **Toncoin** (TON): $0,5123 · 📉 -1.50%", "CoinGecko"}},
		{key: "quakes", tool: "earthquakes",
			result: `{"earthquakes":[{"time":"29.10 14:05","magnitude":4.5,"place":"10 km N of Kashgar, China","depth_km":10}]}`,
			args:   []string{`earthquakes {"area":"central_asia"}`},
			want:   []string{"So'nggi zilzilalar", "• 29.10 14:05 — M4.5, 10 km N of Kashgar, China, chuqurligi 10 km"}},
		{key: "search", value: "patch", tool: "web_search",
			result: `{"answer":"Patch 1.9.20 buffs Ling.","results":[{"title":"Patch notes","site":"m.mobilelegends.com","url":"https://m.mobilelegends.com/news"}]}`,
			args:   []string{`web_search {"query":"patch"}`},
			want:   []string{"🔎 **patch**\n\nPatch 1.9.20 buffs Ling.", "• Patch notes — m.mobilelegends.com\nhttps://m.mobilelegends.com/news"}},
	} {
		t.Run(c.key, func(t *testing.T) {
			f := newFixture()
			runner := withToolRunner(f)
			runner.answers[c.tool] = succeeded(c.result)
			f.opts.Buttons = &fakeButtons{}
			audio := &fakeAudioSender{}
			f.opts.AudioSender = audio

			sent := f.run(useItem(t, c.key, c.value))

			asked := strings.Join(runner.recorded(), "|")
			for _, want := range c.args {
				if !strings.Contains(asked, want) {
					t.Errorf("tools asked %q; want %q", asked, want)
				}
			}
			if len(sent) != 1 {
				t.Fatalf("sent = %+v; want one answer", sent)
			}
			for _, want := range c.want {
				if !strings.Contains(sent[0].text, want) {
					t.Errorf("answer = %q\nwant %q in it", sent[0].text, want)
				}
			}
			if sent[0].photo != c.photo || sent[0].keyboard == nil {
				t.Errorf("answer = %+v; want photo %v, with the keyboard", sent[0], c.photo)
			}
			if c.key == "song" && (len(audio.sent) != 1 || audio.sent[0] != "Shohruhxon — Yurak MP3") {
				t.Errorf("songs sent = %q; want the preview before the list", audio.sent)
			}
			if c.key == "holidays" && strings.Contains(sent[0].text, "Nowruz") {
				t.Errorf("a past holiday was listed: %q", sent[0].text)
			}
			if calls := f.replier.recorded(); len(calls) != 0 {
				t.Errorf("the model was asked %+v", calls)
			}
		})
	}
}

// Each game, Tarozi to Dev jangi, is the game's own message; nothing else is
// sent with it.
func TestGamesAreTheGameAlone(t *testing.T) {
	for _, name := range []string{"tarozi", "ilon", "gilam", "boss"} {
		f := newFixture()
		runner := withToolRunner(f)
		runner.answers["send_game"] = succeeded(`{"sent":true}`)
		f.opts.Buttons = &fakeButtons{}

		sent := f.run(useItem(t, name, ""))

		if asked := runner.recorded(); len(sent) != 0 || len(asked) != 1 || asked[0] != `send_game {"game":"`+name+`"}` {
			t.Errorf("%s: sent = %+v, tools asked %q; want the game alone", name, sent, asked)
		}
	}
}

// What a tool could not find, a service that is down, the day's pictures
// used up and a quiet week are each said plainly.
func TestMoreItemsSayFailures(t *testing.T) {
	refused := func(code string) string { return `{"ok":false,"error":{"code":"` + code + `","message":"no"}}` }
	for _, c := range []struct {
		name, key, value, tool, answer string
		want                           localText
	}{
		{"not found", "movie", "Qwzx", "movies", refused("invalid_arguments"), textNothingFound},
		{"down", "fact", "", "fun", refused("execution_failed"), textUnavailable},
		{"no tool", "crypto", "", "", "", textUnavailable},
		{"empty", "news_uz", "", "news_headlines", succeeded(`{"items":[]}`), textNothingFound},
		{"unknown town", "prayer", "Parij", "prayer_times", refused("invalid_arguments"), textPrayerUnknown},
		{"unknown place", "place", "Atlantida", "send_location", refused("invalid_arguments"), textPlaceUnknown},
		{"day's pictures", "draw", "qal'a", "generate_image", refused("daily_limit"), textDrawLimit},
		{"picture refused", "draw", "qal'a", "generate_image", refused("invalid_arguments"), textDrawRefused},
		{"two games", "tarozi", "", "send_game", refused("invalid_arguments"), textGameTooSoon},
		{"quiet week", "quakes", "", "earthquakes", succeeded(`{"earthquakes":[]}`), textNoQuakes},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture()
			runner := withToolRunner(f)
			if c.tool != "" {
				runner.answers[c.tool] = c.answer
			}
			f.opts.Buttons = &fakeButtons{}

			sent := f.run(useItem(t, c.key, c.value))

			if len(sent) != 1 || sent[0].text != c.want.uz || sent[0].photo {
				t.Errorf("sent = %+v; want %q", sent, c.want.uz)
			}
			if len(f.replier.recorded()) != 0 {
				t.Errorf("the model was asked")
			}
		})
	}
}

// A Quran item reads a surah's number as the surah and words as a search
// of the meaning.
func TestQuranReadsWhatWasWritten(t *testing.T) {
	f := newFixture()
	runner := withToolRunner(f)
	runner.answers["quran"] = succeeded(`{"number":2,"name":"Al-Baqarah","meaning":"The Cow","verses":286,"revealed_in":"madinah","link":"https://quran.com/2"}`)

	sent := f.run(useItem(t, "quran", "2"))
	if len(sent) != 1 || !strings.Contains(sent[0].text, "**2. Al-Baqarah** (The Cow): 286 oyat, Madinada nozil bo'lgan.") {
		t.Errorf("surah = %+v", sent)
	}

	runner.answers["quran"] = succeeded(`{"found":12,"verses":[{"ref":"2:153","surah":"Al-Baqara","text":"Эй, иймон келтирганлар, сабр ва намоз ила ёрдам сўранглар."}]}`)
	sent = f.run(useItem(t, "quran", "sabr"))
	if last := sent[len(sent)-1]; !strings.Contains(last.text, "Topilgan oyatlar: 12") || !strings.Contains(last.text, "• **2:153** (Al-Baqara) Эй") {
		t.Errorf("search = %+v", last)
	}
	if asked := runner.recorded(); !strings.Contains(asked[0], `"action":"surah","ref":"2"`) || !strings.Contains(asked[1], `"action":"search","query":"sabr"`) {
		t.Errorf("tools asked %q", asked)
	}
}
