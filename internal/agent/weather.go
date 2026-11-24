package agent

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/uzscript"
)

// weatherTool is the tool whose failure the model must not cover with a
// forecast of its own: asked on 2026-11-24, the model answered a failed
// get_weather with "around 18°C and partly cloudy" in one try of three.
const weatherTool = "get_weather"

// weatherFailed reports a turn that called get_weather and had no answer
// from it: every call failed.
func weatherFailed(messages []ai.Message) bool {
	names := map[string]string{}
	failed := false
	for _, msg := range messages {
		for _, call := range msg.ToolCalls {
			names[call.ID] = call.Function.Name
		}
		if msg.Role != ai.RoleTool || names[msg.ToolCallID] != weatherTool {
			continue
		}
		var result struct {
			OK bool `json:"ok"`
		}
		if json.Unmarshal([]byte(msg.Content), &result) == nil && result.OK {
			return false
		}
		failed = true
	}
	return failed
}

// weatherNumbers are what a forecast says and a failure does not: a
// temperature, a wind speed or a share of rain.
var weatherNumbers = regexp.MustCompile(`(?i)\d\s*(?:°|º|℃|daraja|даража|градус|degree|km/h|km/soat|км/ч|км/соат|m/s|м/с|mph)|\d\s*%`)

// givesWeather reports an answer that tells weather by numbers.
func givesWeather(answer string) bool { return weatherNumbers.MatchString(answer) }

// The lines said in place of a forecast made up after get_weather failed,
// in the answer's language.
const (
	weatherLineUz         = "Ob-havo ma'lumotini hozir ololmadim, birozdan keyin qayta so'rab ko'ring."
	weatherLineRu         = "Не удалось получить прогноз погоды, попробуйте чуть позже."
	weatherLineEn         = "I couldn't get the weather right now, please try again a bit later."
	englishWordsForAnswer = 2
)

// englishWords tell an English answer from an Uzbek one in Latin letters.
var englishWords = map[string]bool{"the": true, "is": true, "it": true, "will": true, "be": true, "and": true, "with": true, "in": true, "weather": true, "tomorrow": true, "today": true, "around": true}

// weatherLine is the line for a failed forecast in answer's language and
// script: Russian or Uzbek in Cyrillic, English or Uzbek in Latin.
func weatherLine(answer string) string {
	cyrillic, latin := 0, 0
	for _, r := range answer {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			cyrillic++
		case unicode.IsLetter(r):
			latin++
		}
	}
	if cyrillic > latin {
		if uzscript.IsUzbekCyrillic(answer) {
			return uzscript.Cyrillic(weatherLineUz)
		}
		return weatherLineRu
	}
	english := 0
	for _, w := range strings.FieldsFunc(strings.ToLower(answer), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if englishWords[w] {
			english++
		}
	}
	if english >= englishWordsForAnswer {
		return weatherLineEn
	}
	return weatherLineUz
}
