package service

import (
	"strings"
	"testing"
)

func TestTypedRequestOf(t *testing.T) {
	cases := map[string]typedRequest{
		"dollar qancha":              {kind: "rate", code: "USD"},
		"Dollar kursi?":              {kind: "rate", code: "USD"},
		"bugungi kurs":               {kind: "rate", code: "USD"},
		"evro necha":                 {kind: "rate", code: "EUR"},
		"Курс рубля":                 {kind: "rate", code: "RUB"},
		"usd":                        {kind: "rate", code: "USD"},
		"ob-havo qanday":             {kind: "weather", city: "Toshkent"},
		"ob havo":                    {kind: "weather", city: "Toshkent"},
		"Samarqand ob-havosi":        {kind: "weather", city: "Samarqand"},
		"Buxoroda havo qanday?":      {kind: "weather", city: "Buxoro"},
		"Погода":                     {kind: "weather", city: "Toshkent"},
		"soat nechi bo'ldi":          {kind: "time"},
		"Который час?":               {kind: "time"},
		"what time is it":            {kind: "time"},
		"Mari, dollar qancha":        {kind: "rate", code: "USD"},
		"100 dollar necha so'm":      {},
		"ertaga ob-havo qanday":      {},
		"dollar ko'tariladimi":       {},
		"ob-havo qanday, nima kiyay": {},
		"Moskvada soat nechi":        {},
		"salom":                      {},
		"dollar va ob-havo":          {},
	}
	for text, want := range cases {
		got, ok := typedRequestOf(text)
		if ok != (want.kind != "") || got != want {
			t.Errorf("typedRequestOf(%q) = %+v, %t; want %+v", text, got, ok, want)
		}
	}
}

// In production "dollar qancha", typed, went to the router and the main
// model to call one tool: such plain requests are answered in code, with
// what was asked of the tool, and only a tool's failure reaches the model.
func TestTypedRequestsAreAnsweredInCode(t *testing.T) {
	f := newFixture()
	runner := withToolRunner(f)

	sent := f.run(textFrom(userID, "evro kursi"), textFrom(userID, "Samarqand ob-havosi"), textFrom(userID, "dollar ko'tariladimi"))

	if asked := runner.recorded(); len(asked) != 2 || asked[0] != `currency_rate {"code":"EUR"}` || asked[1] != `get_weather {"city":"Samarqand"}` {
		t.Errorf("tools asked = %q", asked)
	}
	if len(sent) != 3 || !strings.Contains(sent[0].text, "so'm (Markaziy bank") || !strings.Contains(sent[1].text, "+15°") {
		t.Errorf("sent = %+v", sent)
	}
	if calls := f.replier.recorded(); len(calls) != 1 || !strings.Contains(calls[0].text, "dollar ko'tariladimi") {
		t.Errorf("model asked %+v; want only the question", calls)
	}

	delete(runner.answers, "get_weather")
	f.run(textFrom(userID, "Atlantida ob-havosi"))
	if calls := f.replier.recorded(); len(calls) != 2 {
		t.Errorf("a place the weather does not know went to the model %d times, want once", len(calls)-1)
	}
}
