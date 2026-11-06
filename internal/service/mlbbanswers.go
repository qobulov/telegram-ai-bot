package service

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The Mobile Legends menu is answered in code: the mlbb tool's result,
// written here, is the answer, as skins already were. The model read the
// same numbers back in its own words, in a few seconds more and at a cost,
// and sometimes in the wrong language. The tool's data, heroes, roles and
// items, is the game's English, as players know it; the labels around it
// are the user's language.

func init() {
	codeAnswers["meta"] = metaAnswer
	codeAnswers["hero"] = heroAnswer
	codeAnswers["build"] = buildAnswer
	codeAnswers["counter"] = counterAnswer
	codeAnswers["draft"] = draftAnswer
	codeAnswers["winrate"] = winrateAnswer
	codeAnswers["player"] = playerAnswer
}

// shownMatchups is how many heroes of each matchup a hero's profile names;
// a counter pick lists shownCounters, with their numbers, and a draft's
// pick gives shownDraftWhys reasons, so that each answer stays short.
const (
	shownMatchups  = 3
	shownCounters  = 5
	shownDraftWhys = 2
)

// mlbbRates are a hero's rates as the mlbb tool writes them, "52.1%".
type mlbbRates struct {
	Hero     string `json:"hero"`
	WinRate  string `json:"win_rate"`
	PickRate string `json:"pick_rate"`
	BanRate  string `json:"ban_rate"`
	Tier     string `json:"tier"`
}

type mlbbMatchup struct {
	Hero          string `json:"hero"`
	WinRateChange string `json:"win_rate_change"`
}

// mlbbHero is what the answers use of the mlbb tool's hero profile.
type mlbbHero struct {
	mlbbRates
	Title      string   `json:"title"`
	Roles      []string `json:"roles"`
	Lanes      []string `json:"lanes"`
	Difficulty string   `json:"difficulty"`
	Skills     []struct {
		Name string `json:"name"`
	} `json:"skills"`
	StrongAgainst []mlbbMatchup `json:"strong_against"`
	CounteredBy   []mlbbMatchup `json:"countered_by"`
	GoodPartners  []string      `json:"good_partners"`
}

// metaAnswer is the top heroes of all ranks by win rate, with their pick
// and ban rates and tier.
func metaAnswer(ctx context.Context, s *Service, in incoming, _ string) outcome {
	var meta struct {
		Measured string      `json:"measured"`
		Heroes   []mlbbRates `json:"heroes"`
	}
	if _, _, ok := s.toolResult(ctx, in, "mlbb", map[string]string{"action": "meta"}, &meta); !ok || len(meta.Heroes) == 0 {
		return say(textError.to(in))
	}
	title := fmt.Sprintf(textMetaTitle.to(in), len(meta.Heroes))
	if meta.Measured != "" {
		title += " (" + meta.Measured + ")"
	}
	lines := []string{"**" + title + "**"}
	for i, hero := range meta.Heroes {
		lines = append(lines, fmt.Sprintf("%d. **%s** — %s", i+1, hero.Hero, ratesText(in, hero)))
	}
	return say(strings.Join(lines, "\n"))
}

// heroAnswer is a hero's profile, short, with its portrait.
func heroAnswer(ctx context.Context, s *Service, in incoming, name string) outcome {
	var hero mlbbHero
	pictures, _, ok := s.toolResult(ctx, in, "mlbb", map[string]any{"action": "hero", "hero": name, "picture": true}, &hero)
	if !ok {
		return say(textHeroUnknown.to(in))
	}
	return pictureAnswer(heroText(in, hero), pictures)
}

func heroText(in incoming, hero mlbbHero) string {
	title := "🦸 **" + hero.Hero + "**"
	if hero.Title != "" {
		title += ", " + hero.Title
	}
	var skills []string
	for _, skill := range hero.Skills {
		skills = append(skills, skill.Name)
	}
	lines := nonEmpty(
		title,
		strings.Join(append(append([]string{}, hero.Roles...), hero.Lanes...), " · "),
		ratesText(in, hero.mlbbRates),
		labeled(in, textDifficulty, nonEmpty(hero.Difficulty)),
		labeled(in, textSkills, skills),
		labeled(in, textStrongAgainst, matchupHeroes(hero.StrongAgainst, shownMatchups)),
		labeled(in, textWeakAgainst, matchupHeroes(hero.CounteredBy, shownMatchups)),
		labeled(in, textPartners, hero.GoodPartners[:min(len(hero.GoodPartners), shownMatchups)]),
	)
	return strings.Join(lines, "\n")
}

// counterAnswer names the heroes that lower the hero's win rate most, by
// how much, and those it beats.
func counterAnswer(ctx context.Context, s *Service, in incoming, name string) outcome {
	var hero mlbbHero
	if _, _, ok := s.toolResult(ctx, in, "mlbb", map[string]string{"action": "hero", "hero": name}, &hero); !ok {
		return say(textHeroUnknown.to(in))
	}
	if len(hero.CounteredBy) == 0 {
		// Rone Arena, which has the counters, may be down while MLBBDex
		// still gives the profile.
		return say(textNoCounters.to(in))
	}
	lines := []string{fmt.Sprintf(textCountersOf.to(in), hero.Hero)}
	for _, m := range hero.CounteredBy[:min(len(hero.CounteredBy), shownCounters)] {
		lines = append(lines, fmt.Sprintf("- **%s** (%s)", m.Hero, m.WinRateChange))
	}
	if strong := matchupHeroes(hero.StrongAgainst, shownMatchups); len(strong) > 0 {
		lines = append(lines, "", fmt.Sprintf(textBeats.to(in), hero.Hero, strings.Join(strong, ", ")))
	}
	return say(strings.Join(lines, "\n"))
}

// buildAnswer sends the top players' build as the tool's picture of it,
// with a short caption; should the picture not be drawn, the build is
// written out.
func buildAnswer(ctx context.Context, s *Service, in incoming, name string) outcome {
	var build mlbbBuildResult
	pictures, _, ok := s.toolResult(ctx, in, "mlbb", map[string]string{"action": "build", "hero": name}, &build)
	if !ok || len(build.Builds) == 0 {
		return say(textHeroUnknown.to(in))
	}
	return pictureAnswer(buildText(in, build, len(pictures) > 0), pictures)
}

type mlbbBuildResult struct {
	Hero   string `json:"hero"`
	Lane   string `json:"lane"`
	Builds []struct {
		Spell    string   `json:"spell"`
		Emblem   string   `json:"emblem"`
		Items    []string `json:"core_items"`
		WinRate  string   `json:"win_rate"`
		PickRate string   `json:"pick_rate"`
	} `json:"builds"`
	FinishWith []string `json:"finish_with"`
}

// buildText is the first build's caption: its hero, lane and rates, and,
// with no picture to show them, its spell, emblem and items.
func buildText(in incoming, build mlbbBuildResult, pictured bool) string {
	first := build.Builds[0]
	lines := []string{
		"🛠 **" + build.Hero + "** · " + laneName(build.Lane),
		fmt.Sprintf(textBuildRates.to(in), first.WinRate, first.PickRate),
	}
	if pictured {
		return strings.Join(lines, "\n")
	}
	lines = append(lines, nonEmpty(
		labeled(in, textSpell, nonEmpty(first.Spell)),
		labeled(in, textEmblem, nonEmpty(first.Emblem)),
		labeled(in, textCoreItems, first.Items),
		labeled(in, textFinishWith, build.FinishWith),
	)...)
	return strings.Join(lines, "\n")
}

// laneName is a lane as the game names it.
func laneName(lane string) string {
	if name, ok := map[string]string{"gold": "Gold Lane", "exp": "EXP Lane", "mid": "Mid Lane", "jungle": "Jungle", "roam": "Roam"}[lane]; ok {
		return name
	}
	return lane
}

// maxDraftEnemies is the most heroes the enemy picks, and the tool takes.
const maxDraftEnemies = 5

// draftAnswer scores the picks against the enemy's heroes, written with
// commas: "Ling, Tigreal".
func draftAnswer(ctx context.Context, s *Service, in incoming, value string) outcome {
	enemies := draftHeroes(value)
	if len(enemies) == 0 || len(enemies) > maxDraftEnemies {
		return say(textDraftHow.to(in))
	}
	var draft struct {
		Enemies []string `json:"enemies"`
		Picks   []struct {
			Hero  string   `json:"hero"`
			Score string   `json:"score"`
			Why   []string `json:"why"`
		} `json:"picks"`
	}
	if _, _, ok := s.toolResult(ctx, in, "mlbb", map[string]any{"action": "draft", "enemies": enemies}, &draft); !ok {
		return say(textHeroUnknown.to(in))
	}
	if len(draft.Picks) == 0 {
		return say(textNoPicks.to(in))
	}
	lines := []string{fmt.Sprintf(textDraftTitle.to(in), strings.Join(draft.Enemies, ", "))}
	for i, pick := range draft.Picks {
		var why []string
		for _, reason := range pick.Why[:min(len(pick.Why), shownDraftWhys)] {
			why = append(why, draftReason(in, reason))
		}
		lines = append(lines, fmt.Sprintf("%d. **%s** %s — %s", i+1, pick.Hero, pick.Score, strings.Join(why, "; ")))
	}
	return say(strings.Join(lines, "\n"))
}

// draftHeroes are the heroes of a list written with commas, semicolons or
// a hero to a line.
func draftHeroes(value string) []string {
	var heroes []string
	for _, h := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		if h = strings.TrimSpace(h); h != "" {
			heroes = append(heroes, h)
		}
	}
	return heroes
}

// draftReason writes the tool's reason, "counters Ling (+3.2%)", in the
// user's language; a reason it does not know stays as it is.
func draftReason(in incoming, reason string) string {
	for prefix, label := range map[string]localText{"counters ": textReasonCounters, "weak against ": textReasonWeak} {
		if rest, ok := strings.CutPrefix(reason, prefix); ok {
			hero, change, ok := strings.Cut(rest, " (")
			if !ok {
				return fmt.Sprintf(label.to(in), hero)
			}
			return fmt.Sprintf(label.to(in), hero) + " (" + change
		}
	}
	return reason
}

// winrateAnswer is how many wins in a row reach the win rate wanted, from
// "500 48 60": the matches, the win rate now and the one wanted.
func winrateAnswer(ctx context.Context, s *Service, in incoming, value string) outcome {
	matches, winRate, target, ok := parseWinrate(value)
	if !ok {
		return say(textWinrateHow.to(in))
	}
	var result struct {
		WinRate    string `json:"win_rate"`
		Target     string `json:"target_win_rate"`
		WinsInARow int    `json:"wins_in_a_row"`
	}
	args := map[string]any{"action": "winrate", "matches": matches, "win_rate": winRate, "target_win_rate": target}
	// The tool reckons it alone, so its failing is numbers out of range.
	if _, _, ok := s.toolResult(ctx, in, "mlbb", args, &result); !ok {
		return say(textWinrateHow.to(in))
	}
	if result.WinsInARow == 0 {
		return say(fmt.Sprintf(textWinrateReached.to(in), result.WinRate, result.Target))
	}
	return say(fmt.Sprintf(textWinsNeeded.to(in), matches, result.WinRate, result.Target, result.WinsInARow))
}

// parseWinrate reads three numbers as people write them: "500 48 60",
// "500, 48%, 60%", "500 48,5 60" or "500,48,60".
func parseWinrate(value string) (matches int, winRate, target float64, ok bool) {
	value = strings.NewReplacer("%", " ", ", ", " ", ";", " ").Replace(value)
	fields := strings.Fields(strings.Trim(value, ", "))
	if len(fields) == 1 {
		fields = strings.Split(fields[0], ",")
	}
	if len(fields) != 3 {
		return 0, 0, 0, false
	}
	var numbers [3]float64
	for i, f := range fields {
		n, err := strconv.ParseFloat(strings.ReplaceAll(strings.Trim(f, ","), ",", "."), 64)
		if err != nil {
			return 0, 0, 0, false
		}
		numbers[i] = n
	}
	if numbers[0] != float64(int(numbers[0])) {
		return 0, 0, 0, false
	}
	return int(numbers[0]), numbers[1], numbers[2], true
}

// playerDigits are the numbers of "1407157025 (15832)": the ID, then the
// zone.
var playerDigits = regexp.MustCompile(`[0-9]+`)

// playerAnswer is an account's nickname and country, by its ID and zone.
func playerAnswer(ctx context.Context, s *Service, in incoming, value string) outcome {
	numbers := playerDigits.FindAllString(value, -1)
	if len(numbers) == 0 || len(numbers) > 2 {
		return say(textPlayerHow.to(in))
	}
	if len(numbers) == 1 {
		// The lookup needs the zone, the number in brackets by the ID.
		return say(textPlayerZone.to(in))
	}
	var player struct {
		PlayerID string `json:"player_id"`
		Zone     string `json:"zone"`
		Nickname string `json:"nickname"`
		Country  string `json:"country"`
	}
	if _, _, ok := s.toolResult(ctx, in, "mlbb", map[string]string{"action": "player", "player_id": numbers[0], "zone": numbers[1]}, &player); !ok {
		return say(textPlayerNotFound.to(in))
	}
	text := fmt.Sprintf("🆔 %s (%s): **%s**", player.PlayerID, player.Zone, player.Nickname)
	if player.Country != "" {
		text += ", " + player.Country
	}
	return say(text)
}

// ratesText is a hero's win, pick and ban rates, and its tier when known.
func ratesText(in incoming, r mlbbRates) string {
	if r.WinRate == "" {
		return ""
	}
	text := fmt.Sprintf(textRates.to(in), r.WinRate, r.PickRate, r.BanRate)
	if r.Tier != "" {
		text += " · " + fmt.Sprintf(textTier.to(in), r.Tier)
	}
	return text
}

// labeled is "label: a, b, c", or "" for no items.
func labeled(in incoming, label localText, items []string) string {
	if len(items) == 0 {
		return ""
	}
	return label.to(in) + ": " + strings.Join(items, ", ")
}

// matchupHeroes are the first n heroes of matchups.
func matchupHeroes(matchups []mlbbMatchup, n int) []string {
	var heroes []string
	for _, m := range matchups[:min(len(matchups), n)] {
		heroes = append(heroes, m.Hero)
	}
	return heroes
}

var (
	// textMetaTitle takes how many heroes.
	textMetaTitle = localText{uz: "🔥 MLBB meta: win rate bo'yicha top %d", ru: "🔥 Мета MLBB: топ-%d по винрейту", en: "🔥 MLBB meta: top %d by win rate"}
	// textRates takes the win, pick and ban rates.
	textRates         = localText{uz: "WR %s · pik %s · ban %s", ru: "WR %s · пик %s · бан %s", en: "WR %s · pick %s · ban %s"}
	textTier          = localText{uz: "tier %s", ru: "тир %s", en: "tier %s"}
	textDifficulty    = localText{uz: "Qiyinlik", ru: "Сложность", en: "Difficulty"}
	textSkills        = localText{uz: "Skillar", ru: "Навыки", en: "Skills"}
	textStrongAgainst = localText{uz: "Yengadi", ru: "Силён против", en: "Strong against"}
	textWeakAgainst   = localText{uz: "Qiyin raqiblar", ru: "Слаб против", en: "Weak against"}
	textPartners      = localText{uz: "Yaxshi sheriklar", ru: "Хорошие союзники", en: "Good partners"}
	// textCountersOf takes the hero.
	textCountersOf = localText{
		uz: "⚔️ **%s** ga qarshi eng yaxshi piklar (uning win rate'i qanchaga tushadi):",
		ru: "⚔️ Лучшие пики против **%s** (насколько падает его винрейт):",
		en: "⚔️ The best picks against **%s** (how much its win rate drops):",
	}
	// textBeats takes the hero and the heroes it beats.
	textBeats      = localText{uz: "%s o'zi yengadi: %s", ru: "%s сам силён против: %s", en: "%s itself beats: %s"}
	textNoCounters = localText{
		uz: "Hozir kontr-pik statistikasi kelmadi, birozdan keyin qayta urinib ko'ring.",
		ru: "Статистика контрпиков сейчас не пришла, попробуйте чуть позже.",
		en: "The counter statistics didn't come through just now; try again in a little while.",
	}
	// textBuildRates takes the build's win rate and how many builds use it.
	textBuildRates = localText{uz: "WR %s · buildlarning %s", ru: "WR %s · %s сборок", en: "WR %s · %s of builds"}
	textSpell      = localText{uz: "Spell", ru: "Заклинание", en: "Spell"}
	textEmblem     = localText{uz: "Emblema", ru: "Эмблема", en: "Emblem"}
	textCoreItems  = localText{uz: "Asosiy itemlar", ru: "Основные предметы", en: "Core items"}
	textFinishWith = localText{uz: "Oxirida", ru: "Дособрать", en: "Finish with"}
	// textDraftTitle takes the enemy's heroes.
	textDraftTitle     = localText{uz: "🎯 Dushman: %s. Eng yaxshi piklar:", ru: "🎯 Враги: %s. Лучшие пики:", en: "🎯 Enemy: %s. The best picks:"}
	textReasonCounters = localText{uz: "%s ga qarshi", ru: "против %s", en: "counters %s"}
	textReasonWeak     = localText{uz: "%s ga qarshi zaif", ru: "слаб против %s", en: "weak against %s"}
	textNoPicks        = localText{
		uz: "Bu qahramonlarga qarshi aniq pik topilmadi.",
		ru: "Явных пиков против этих героев не нашлось.",
		en: "No clear picks against these heroes.",
	}
	textDraftHow = localText{
		uz: "Dushmanning 1 tadan 5 tagacha qahramonini vergul bilan yozing. Masalan: Ling, Tigreal",
		ru: "Напишите от 1 до 5 героев врага через запятую. Например: Ling, Tigreal",
		en: "Write 1 to 5 of the enemy's heroes, separated by commas. For example: Ling, Tigreal",
	}
	textWinrateHow = localText{
		uz: "Uchta son yozing: o'yinlar soni, hozirgi win rate va kerakli win rate. Masalan: 500 48 60",
		ru: "Напишите три числа: матчей, текущий винрейт и нужный. Например: 500 48 60",
		en: "Write three numbers: your matches, your win rate and the one you want. For example: 500 48 60",
	}
	// textWinsNeeded takes the matches, the win rate, the one wanted and
	// the wins in a row.
	textWinsNeeded = localText{
		uz: "📈 %d o'yin, %s → %s: ketma-ket **%d** g'alaba kerak.",
		ru: "📈 %d матчей, %s → %s: нужно **%d** побед подряд.",
		en: "📈 %d matches, %s → %s: you need **%d** wins in a row.",
	}
	// textWinrateReached takes the win rate and the one wanted.
	textWinrateReached = localText{
		uz: "📈 Win rate'ingiz %s, kerakli %s ga allaqachon yetgan.",
		ru: "📈 Ваш винрейт %s уже не ниже нужного %s.",
		en: "📈 Your win rate of %s is already at least %s.",
	}
	textPlayerHow = localText{
		uz: "O'yinchi ID va zonasini yozing. Masalan: 1407157025 (15832)",
		ru: "Напишите ID игрока и зону. Например: 1407157025 (15832)",
		en: "Write the player's ID and zone. For example: 1407157025 (15832)",
	}
	textPlayerZone = localText{
		uz: "Zonani ham yozing: ID yonidagi qavsdagi son. Masalan: 1407157025 (15832)",
		ru: "Напишите и зону: число в скобках рядом с ID. Например: 1407157025 (15832)",
		en: "Write the zone too: the number in brackets by the ID. For example: 1407157025 (15832)",
	}
	textPlayerNotFound = localText{
		uz: "Bu ID va zona bo'yicha akkaunt topilmadi. Ikkalasini tekshiring; qidiruv xizmati vaqtincha ishlamayotgan bo'lishi ham mumkin.",
		ru: "Аккаунт по этому ID и зоне не нашёлся. Проверьте оба; возможно, сервис поиска временно не работает.",
		en: "No account was found for this ID and zone. Check both; the lookup may also be down for now.",
	}
)
