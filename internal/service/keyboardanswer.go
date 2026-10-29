package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"telegram-ai-bot/internal/tools"
)

// ToolRunner runs one of the model's tools for the bot itself, as the
// registry does for the model, and returns its JSON envelope.
type ToolRunner interface {
	Execute(ctx context.Context, callerID int64, name, args string) string
}

// keyboardTimeout bounds a keyboard button's tool: a live rate or forecast.
const keyboardTimeout = 20 * time.Second

// keyboardAnswer answers the private keyboard in code, so that it takes
// nothing from the day's answers, which pay for the model: the time at
// once, a button that needs a value with its question (see
// keyboardmenu.go), and the reply to such a question with the tool's
// result. A plain request typed in words, "dollar qancha", is answered as
// its button would be (see typedrequest.go). ok is false for any other
// message, a meme's topic among them: a meme's words are the model's.
func (s *Service) keyboardAnswer(ctx context.Context, in incoming) (outcome, bool) {
	// A business account's customers have no keyboard: what they write is
	// theirs, not a button.
	if in.group || in.businessConnectionID() != "" {
		return outcome{}, false
	}
	if pressed(in, keyboardTime) {
		return say(s.timeNow(pressedLanguage(in, keyboardTime))), true
	}
	if s.opts.Tools == nil {
		return outcome{}, false
	}
	if out, ok := s.menuReply(ctx, in); ok {
		return out, true
	}
	for button, asks := range map[localText]localText{
		keyboardRate: keyboardRate, keyboardOldDollar: keyboardRate,
		keyboardWeather: keyboardWeather, keyboardOldWeather: keyboardWeather,
		keyboardDie: keyboardDie, keyboardMeme: keyboardMeme, keyboardMLBB: keyboardMLBB,
	} {
		if pressed(in, button) {
			return menuQuestion(pressedLanguage(in, button), asks)
		}
	}
	if req, ok := typedRequestOf(in.text); ok {
		return s.typedAnswer(ctx, in, req)
	}
	return outcome{}, false
}

// typedAnswer answers a plain request typed in words as its button would.
// A tool that fails, as for a place the weather does not know, leaves it
// to the model, which may understand what the words meant.
func (s *Service) typedAnswer(ctx context.Context, in incoming, req typedRequest) (outcome, bool) {
	switch req.kind {
	case "time":
		return say(s.timeNow(languageOf(in))), true
	case "rate":
		args, _ := json.Marshal(map[string]string{"code": req.code})
		return keyboardTool(s, ctx, in, "currency_rate", string(args), rateText)
	case "weather":
		args, _ := json.Marshal(map[string]string{"city": req.city})
		return keyboardTool(s, ctx, in, "get_weather", string(args), weatherText)
	}
	return outcome{}, false
}

// pressedLanguage is the language of the button's label that was pressed:
// a few words such as "😂 Make a meme" are no sure sign of a language, but
// the label is. A label every language shares, "🎮 MLBB", says nothing,
// and the user's app does.
func pressedLanguage(in incoming, button localText) string {
	var languages []string
	for language, label := range map[string]string{"uz": button.uz, "ru": button.ru, "en": button.en} {
		if in.text == label {
			languages = append(languages, language)
		}
	}
	if len(languages) == 1 {
		return languages[0]
	}
	return appLanguage(in.msg.From)
}

// pressed reports a message that is button, in any of its languages.
func pressed(in incoming, button localText) bool {
	return in.text == button.uz || in.text == button.ru || in.text == button.en
}

// runTool runs a tool on the sender's turn and decodes its result into
// out, reporting whether it succeeded.
func (s *Service) runTool(ctx context.Context, in incoming, name, args string, out any) bool {
	ctx, cancel := context.WithTimeout(ctx, keyboardTimeout)
	defer cancel()
	ctx = tools.WithTurn(ctx, s.caller(in), &tools.Sink{})
	var envelope struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	raw := s.opts.Tools.Execute(ctx, in.userID, name, args)
	if json.Unmarshal([]byte(raw), &envelope) != nil || !envelope.OK || json.Unmarshal(envelope.Result, out) != nil {
		s.opts.Logger.Warn("a keyboard button's tool failed", "tool", name, "user_id", in.userID, "result", truncateLog(raw))
		return false
	}
	return true
}

// keyboardTool answers with a tool's result, written by write; ok is false
// when the tool failed.
func keyboardTool[T any](s *Service, ctx context.Context, in incoming, name, args string, write func(incoming, T) string) (outcome, bool) {
	var result T
	if !s.runTool(ctx, in, name, args, &result) {
		return outcome{}, false
	}
	return say(write(in, result)), true
}

type rateResult struct {
	Code    string  `json:"code"`
	RateUZS float64 `json:"rate_uzs"`
	// ConvertedUZS is the amount asked about, in so'm.
	ConvertedUZS *float64 `json:"converted_uzs"`
	Diff         *float64 `json:"diff"`
	RateDate     string   `json:"rate_date"`
}

// rateText is a currency's rate, as the Central Bank gives it.
func rateText(in incoming, r rateResult) string {
	text := fmt.Sprintf(textRate.to(in), r.Code, groupDigits(r.RateUZS), r.RateDate)
	if r.Diff != nil && *r.Diff != 0 {
		arrow := "📈 +"
		if *r.Diff < 0 {
			arrow = "📉 "
		}
		text += fmt.Sprintf(" (%s%.2f)", arrow, *r.Diff)
	}
	return text
}

type weatherResult struct {
	Place string `json:"place"`
	Now   struct {
		TempC      float64 `json:"temp_c"`
		Conditions string  `json:"conditions"`
		WindKmh    float64 `json:"wind_kmh"`
	} `json:"now"`
	Daily []struct {
		MinC          float64 `json:"min_c"`
		MaxC          float64 `json:"max_c"`
		RainChancePct *int    `json:"rain_chance_pct"`
	} `json:"daily"`
}

// weatherText is the weather now and today's range.
func weatherText(in incoming, w weatherResult) string {
	text := fmt.Sprintf(textWeatherNow.to(in), w.Place, signed(w.Now.TempC), w.Now.Conditions, math.Round(w.Now.WindKmh))
	if len(w.Daily) > 0 {
		day := w.Daily[0]
		text += "\n" + fmt.Sprintf(textWeatherToday.to(in), signed(day.MinC), signed(day.MaxC))
		if day.RainChancePct != nil && *day.RainChancePct > 0 {
			text += fmt.Sprintf(textWeatherRain.to(in), *day.RainChancePct)
		}
	}
	return text
}

// timeNow is the time where the bot keeps its clock, Tashkent's, in
// language.
func (s *Service) timeNow(language string) string {
	now := s.opts.Now().In(s.opts.Location)
	return fmt.Sprintf(textTimeNow.inLanguage(language), now.Format("15:04"), weekdayIn(language, now.Weekday()), now.Format("02.01.2006"))
}

// signed writes a temperature with its sign, as forecasts do: +5°, -3°.
func signed(c float64) string {
	r := int(math.Round(c))
	if r > 0 {
		return fmt.Sprintf("+%d°", r)
	}
	return fmt.Sprintf("%d°", r)
}

// groupDigits writes a rate with spaces between thousands: 12 750,50.
func groupDigits(v float64) string {
	whole := fmt.Sprintf("%d", int64(v))
	var b strings.Builder
	for i, d := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(d)
	}
	cents := int(math.Round((v - math.Trunc(v)) * 100))
	return fmt.Sprintf("%s,%02d", b.String(), cents)
}

// truncateLog keeps a tool's failure short in the log.
func truncateLog(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

// weekdayIn is day's name in language, Uzbek for any but Russian and
// English.
func weekdayIn(language string, day time.Weekday) string {
	names, ok := weekdays[language]
	if !ok {
		names = weekdays["uz"]
	}
	return names[day]
}

var weekdays = map[string][7]string{
	"uz": {"yakshanba", "dushanba", "seshanba", "chorshanba", "payshanba", "juma", "shanba"},
	"ru": {"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"},
	"en": {"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
}

var (
	// textRate takes the currency's code, its rate and the rate's date.
	textRate = localText{
		uz: "💵 1 %s = %s so'm (Markaziy bank, %s)",
		ru: "💵 1 %s = %s сум (ЦБ Узбекистана, %s)",
		en: "💵 1 %s = %s so'm (Central Bank of Uzbekistan, %s)",
	}
	// textWeatherNow takes the place, the temperature, the conditions and
	// the wind.
	textWeatherNow = localText{
		uz: "🌤 %s: hozir %s, %s, shamol %.0f km/soat.",
		ru: "🌤 %s: сейчас %s, %s, ветер %.0f км/ч.",
		en: "🌤 %s: now %s, %s, wind %.0f km/h.",
	}
	textWeatherToday = localText{uz: "Bugun %s…%s", ru: "Сегодня %s…%s", en: "Today %s…%s"}
	textWeatherRain  = localText{uz: ", yomg'ir ehtimoli %d%%.", ru: ", вероятность дождя %d%%.", en: ", %d%% chance of rain."}
	// textTimeNow takes the time, the weekday and the date.
	textTimeNow = localText{
		uz: "🕐 Toshkentda soat %s, %s, %s.",
		ru: "🕐 В Ташкенте %s, %s, %s.",
		en: "🕐 It is %s in Tashkent, %s, %s.",
	}
	textDieLater = localText{
		uz: "🎲 Hozir tashlay olmadim, birozdan keyin qayta bosing.",
		ru: "🎲 Сейчас не получилось бросить, нажмите чуть позже.",
		en: "🎲 I couldn't roll just now; tap again in a little while.",
	}
)
