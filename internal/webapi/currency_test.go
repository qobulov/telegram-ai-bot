package webapi

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

type currencyResult struct {
	Code         string   `json:"code"`
	Name         string   `json:"name"`
	RateUZS      float64  `json:"rate_uzs"`
	Diff         *float64 `json:"diff"`
	RateDate     string   `json:"rate_date"`
	ConvertedUZS *float64 `json:"converted_uzs"`
	USDPerGram   float64  `json:"usd_per_gram"`
	UZSPerGram   float64  `json:"uzs_per_gram"`
	Source       string   `json:"source"`
}

func currencyRoutes(t *testing.T) map[string]stub {
	return map[string]stub{
		"/cbu/USD/":              ok(fixture(t, "cbu_usd.json")),
		"/cbu/USD/2026-10-11/":   ok(fixture(t, "cbu_usd_2026-10-11.json")),
		"/cbu/USD/2000-01-01/":   ok(fixture(t, "cbu_usd_2000-01-01.json")),
		"/cbu/IRR/":              ok(fixture(t, "cbu_irr.json")),
		"/cbu/XYZ/":              ok(`[]`),
		"/currency-api/usd.json": ok(fixture(t, "currency_api_usd.json")),
		"/mirror/usd.json":       ok(fixture(t, "currency_api_usd.json")),
	}
}

func TestCurrencyRate(t *testing.T) {
	tests := []struct {
		name     string
		override map[string]stub
		args     string
		check    func(t *testing.T, r currencyResult)
	}{
		{
			name: "official rate",
			args: `{}`,
			check: func(t *testing.T, r currencyResult) {
				if r.Code != "USD" || r.RateUZS != 12011.65 || r.Diff == nil || *r.Diff != 48.49 || r.RateDate != "2026-10-16" || r.Name != "AQSH dollari" {
					t.Errorf("result = %+v", r)
				}
				if !strings.Contains(r.Source, "Central Bank") || r.ConvertedUZS != nil {
					t.Errorf("result = %+v", r)
				}
			},
		},
		{
			name: "a word and an amount",
			args: `{"code":"dollar","amount":100}`,
			check: func(t *testing.T, r currencyResult) {
				if r.ConvertedUZS == nil || *r.ConvertedUZS != 1201165 {
					t.Errorf("converted = %v", r.ConvertedUZS)
				}
			},
		},
		{
			name: "nominal of ten",
			args: `{"code":"irr"}`,
			check: func(t *testing.T, r currencyResult) {
				if r.RateUZS != 0.007 {
					t.Errorf("rate = %v, want 0.07 per 10 rials", r.RateUZS)
				}
			},
		},
		{
			name: "a weekend date answers with the day the bank set",
			args: `{"code":"USD","date":"2026-10-11"}`,
			check: func(t *testing.T, r currencyResult) {
				if r.RateDate != "2026-10-10" || r.RateUZS != 11897.90 {
					t.Errorf("result = %+v, want the rate dated 2026-10-10", r)
				}
			},
		},
		{
			name: "an old rate without a change is still a rate",
			args: `{"date":"2000-01-01"}`,
			check: func(t *testing.T, r currencyResult) {
				if r.RateUZS != 140 || r.Diff != nil || r.RateDate != "1999-12-28" || !strings.Contains(r.Source, "Central Bank") {
					t.Errorf("result = %+v", r)
				}
			},
		},
		{
			name: "gold per gram",
			args: `{"code":"oltin","amount":10}`,
			check: func(t *testing.T, r currencyResult) {
				// 1 / 0.0002390663 USD per ounce, 31.1035 g per ounce.
				if r.Code != "XAU" || r.USDPerGram != 134.48 || r.UZSPerGram != 1611752.48 || !strings.Contains(r.Source, "approximate") {
					t.Errorf("result = %+v", r)
				}
				if r.ConvertedUZS == nil || *r.ConvertedUZS != 16117524.83 {
					t.Errorf("10 g = %v", r.ConvertedUZS)
				}
			},
		},
		{
			name:     "the bank is busy so the approximate rate is used",
			override: map[string]stub{"/cbu/USD/": status(http.StatusTooManyRequests)},
			args:     `{"code":"USD"}`,
			check: func(t *testing.T, r currencyResult) {
				if r.RateUZS != 11984.67 || r.RateDate != "2026-10-16" || !strings.Contains(r.Source, "approximate") || r.Diff != nil {
					t.Errorf("result = %+v", r)
				}
			},
		},
		{
			name:     "the mirror answers when currency-api does not",
			override: map[string]stub{"/cbu/USD/": status(http.StatusInternalServerError), "/currency-api/usd.json": status(http.StatusNotFound)},
			args:     `{"code":"rubl"}`,
			check: func(t *testing.T, r currencyResult) {
				if r.Code != "RUB" || r.RateUZS != 143.09 {
					t.Errorf("result = %+v", r)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := currencyRoutes(t)
			for path, answer := range test.override {
				routes[path] = answer
			}
			env := run(t, newUpstream(t, routes).options(), "currency_rate", test.args)
			wantFits(t, env, registryLimit)
			test.check(t, result[currencyResult](t, env))
		})
	}
}

func TestCurrencyRateFailures(t *testing.T) {
	everythingBusy := map[string]stub{
		"/cbu/USD/":              status(http.StatusTooManyRequests),
		"/currency-api/usd.json": status(http.StatusServiceUnavailable),
		"/mirror/usd.json":       status(http.StatusTooManyRequests),
	}
	everythingSlow := map[string]stub{"/cbu/USD/": slow, "/currency-api/usd.json": slow, "/mirror/usd.json": slow}
	tests := []struct {
		name   string
		routes map[string]stub
		args   string
		code   string
		words  []string
	}{
		{name: "unknown code", args: `{"code":"XYZ"}`, code: "invalid_arguments", words: []string{"XYZ"}},
		{name: "not a code", args: `{"code":"mana bu"}`, code: "invalid_arguments"},
		{name: "so'm itself", args: `{"code":"so'm"}`, code: "invalid_arguments", words: []string{"so'm"}},
		{name: "negative amount", args: `{"amount":-5}`, code: "invalid_arguments"},
		{name: "bad date", args: `{"date":"16.10.2026"}`, code: "invalid_arguments", words: []string{"YYYY-MM-DD"}},
		{name: "future date", args: `{"date":"2026-10-17"}`, code: "invalid_arguments", words: []string{"future"}},
		{name: "gold has no history", args: `{"code":"XAU","date":"2026-10-11"}`, code: "invalid_arguments"},
		{name: "everything busy", routes: everythingBusy, args: `{}`, code: "execution_failed", words: []string{"busy"}},
		{name: "a dated rate has no fallback", routes: map[string]stub{"/cbu/USD/2026-10-11/": status(http.StatusTooManyRequests)}, args: `{"date":"2026-10-11"}`, code: "execution_failed", words: []string{"busy"}},
		{name: "everything slow", routes: everythingSlow, args: `{}`, code: "timeout"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := currencyRoutes(t)
			for path, answer := range test.routes {
				routes[path] = answer
			}
			env := run(t, impatient(newUpstream(t, routes).options()), "currency_rate", test.args)
			wantError(t, env, test.code, test.words...)
		})
	}
}

func TestCurrencyRateIsCached(t *testing.T) {
	u := newUpstream(t, currencyRoutes(t))
	registry := newRegistry(t, u.options())

	for range 3 {
		result[currencyResult](t, execute(t, registry, "currency_rate", `{"code":"USD"}`))
	}
	if n := len(u.requests("/cbu/USD/")); n != 1 {
		t.Errorf("CBU was asked %d times, want 1", n)
	}
}

// cbuRow is a Central Bank answer with one row, for answers that are wrong.
func cbuRow(ccy, nominal, rate, diff string) stub {
	return ok(`[{"Ccy":"` + ccy + `","CcyNm_UZ":"AQSH dollari","Nominal":"` + nominal + `","Rate":"` + rate + `","Diff":"` + diff + `","Date":"16.10.2026"}]`)
}

func TestCurrencyRateDoesNotTrustABrokenRow(t *testing.T) {
	broken := map[string]stub{
		"not a number nominal": cbuRow("USD", "NaN", "12011.65", "48.49"),
		"infinite rate":        cbuRow("USD", "1", "Infinity", "48.49"),
		"negative rate":        cbuRow("USD", "1", "-5", "48.49"),
		"zero rate":            cbuRow("USD", "1", "0", "0"),
		"not a number diff":    cbuRow("USD", "1", "12011.65", "NaN"),
		"another currency":     cbuRow("EUR", "1", "13990.12", "10.00"),
	}
	for name, answer := range broken {
		t.Run(name, func(t *testing.T) {
			routes := currencyRoutes(t)
			routes["/cbu/USD/"] = answer
			u := newUpstream(t, routes)
			registry := newRegistry(t, u.options())

			for range 2 {
				r := result[currencyResult](t, execute(t, registry, "currency_rate", `{"code":"USD","amount":100}`))
				if r.Code != "USD" || r.RateUZS != 11984.67 || !strings.Contains(r.Source, "approximate") {
					t.Errorf("result = %+v, want currency-api's approximate rate", r)
				}
			}
			if n := len(u.requests("/cbu/USD/")); n != 2 {
				t.Errorf("the Central Bank was asked %d times; a broken answer must not be cached", n)
			}
		})
	}
}

func TestCurrencyRateReachesTheMirrorWhenEverythingElseStalls(t *testing.T) {
	t.Parallel()
	routes := currencyRoutes(t)
	routes["/cbu/USD/"] = slow
	routes["/currency-api/usd.json"] = slow
	u := newUpstream(t, routes)
	opts := u.options()
	// A caller's client without short timeouts: only the tool's own
	// sub-deadlines leave the mirror time to answer.
	opts.HTTPClient = &http.Client{Timeout: 10 * time.Second}

	r := result[currencyResult](t, run(t, opts, "currency_rate", `{}`))

	if r.RateUZS != 11984.67 || len(u.requests("/mirror/usd.json")) != 1 {
		t.Errorf("result = %+v", r)
	}
}
