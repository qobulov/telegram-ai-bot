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

// keyboardAnswer answers a press of one of the private keyboard's buttons
// that needs no model: the die, the dollar, the weather and the time are
// asked of the tools here and written in code, so that they take nothing
// from the day's answers, which pay for the model. ok is false for any
// other message, the meme button among them: a meme's words are the
// model's.
func (s *Service) keyboardAnswer(ctx context.Context, in incoming) (outcome, bool) {
	// A business account's customers have no keyboard: what they write is
	// theirs, not a button.
	if in.group || in.businessConnectionID() != "" {
		return outcome{}, false
	}
	switch {
	case pressed(in, keyboardTime):
		return say(s.timeNow(in)), true
	case s.opts.Tools == nil:
		return outcome{}, false
	case pressed(in, keyboardDie):
		return s.rollDie(ctx, in), true
	case pressed(in, keyboardDollar):
		return keyboardTool(s, ctx, in, "currency_rate", `{"code":"USD"}`, dollarText), true
	case pressed(in, keyboardWeather):
		return keyboardTool(s, ctx, in, "get_weather", `{"city":"Toshkent"}`, weatherText), true
	}
	return outcome{}, false
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

// keyboardTool answers a button with a tool's result, written by write; a
// failure says something went wrong.
func keyboardTool[T any](s *Service, ctx context.Context, in incoming, name, args string, write func(incoming, T) string) outcome {
	var result T
	if !s.runTool(ctx, in, name, args, &result) {
		return say(textError.to(in))
	}
	return say(write(in, result))
}

// rollDie rolls 🎲 in the chat; the die is the answer. A refusal, such as
// too many rolls, is said.
func (s *Service) rollDie(ctx context.Context, in incoming) outcome {
	var rolled struct {
		Value int `json:"value"`
	}
	if !s.runTool(ctx, in, "send_dice", `{"emoji":"🎲"}`, &rolled) {
		return say(textDieLater.to(in))
	}
	return outcome{}
}

type dollarResult struct {
	RateUZS  float64  `json:"rate_uzs"`
	Diff     *float64 `json:"diff"`
	RateDate string   `json:"rate_date"`
}

// dollarText is the dollar's rate, as the Central Bank gives it.
func dollarText(in incoming, r dollarResult) string {
	text := fmt.Sprintf(textDollarRate.to(in), groupDigits(r.RateUZS), r.RateDate)
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

// timeNow is the time where the bot keeps its clock, Tashkent's.
func (s *Service) timeNow(in incoming) string {
	now := s.opts.Now().In(s.opts.Location)
	return fmt.Sprintf(textTimeNow.to(in), now.Format("15:04"), weekdayIn(languageOf(in), now.Weekday()), now.Format("02.01.2006"))
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
	// textDollarRate takes the rate and its date.
	textDollarRate = localText{
		uz: "💵 1 dollar = %s so'm (Markaziy bank, %s)",
		ru: "💵 1 доллар = %s сум (ЦБ Узбекистана, %s)",
		en: "💵 1 US dollar = %s so'm (Central Bank of Uzbekistan, %s)",
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
