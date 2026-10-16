package webapi

import (
	"net/http"
	"strings"
	"testing"
)

type cryptoResult struct {
	Coins []struct {
		Name         string   `json:"name"`
		Symbol       string   `json:"symbol"`
		USD          float64  `json:"usd"`
		UZS          *float64 `json:"uzs"`
		Change24hPct *float64 `json:"change_24h_pct"`
	} `json:"coins"`
	NotFound []string `json:"not_found"`
	Source   string   `json:"source"`
	Note     string   `json:"note"`
}

const (
	pricePath  = "/coingecko/api/v3/simple/price"
	searchPath = "/coingecko/api/v3/search"
)

func cryptoRoutes(t *testing.T) map[string]stub {
	return map[string]stub{
		pricePath:                  ok(fixture(t, "coingecko_price.json")),
		pricePath + "?ids=pepe":    ok(fixture(t, "coingecko_price_pepe.json")),
		searchPath:                 ok(fixture(t, "coingecko_search_none.json")),
		searchPath + "?query=pepe": ok(fixture(t, "coingecko_search_pepe.json")),
		"/cbu/USD/":                ok(fixture(t, "cbu_usd.json")),
	}
}

func TestCryptoPrice(t *testing.T) {
	tests := []struct {
		name     string
		override map[string]stub
		key      string
		args     string
		check    func(t *testing.T, r cryptoResult, u *upstream)
	}{
		{
			name: "known coins in the order asked",
			args: `{"coins":["btc","TON","not","Ethereum","usdt"]}`,
			check: func(t *testing.T, r cryptoResult, u *upstream) {
				if len(r.Coins) != 5 || r.Coins[0].Name != "Bitcoin" || r.Coins[0].Symbol != "BTC" || r.Coins[0].USD != 82069 || *r.Coins[0].UZS != 981685940 || *r.Coins[0].Change24hPct != 1.37 {
					t.Fatalf("result = %+v", r)
				}
				if ton := r.Coins[1]; ton.Symbol != "TON" || ton.USD != 1.39 || r.Coins[2].Symbol != "NOT" || r.Coins[2].USD != 0.000421 {
					t.Errorf("coins = %+v", r.Coins)
				}
				if r.Source != "CoinGecko" || !strings.Contains(r.Note, "not advice") || len(r.NotFound) != 0 {
					t.Errorf("result = %+v", r)
				}
				query := u.requests(pricePath)[0].URL.Query()
				if query.Get("ids") != "bitcoin,ethereum,notcoin,tether,the-open-network" || query.Get("vs_currencies") != "usd,uzs" || query.Get("include_24hr_change") != "true" {
					t.Errorf("query = %v", query)
				}
				if len(u.requests(searchPath)) != 0 {
					t.Error("a known coin was searched for")
				}
			},
		},
		{
			name: "other coins are searched for",
			args: `{"coins":["pepe"]}`,
			check: func(t *testing.T, r cryptoResult, _ *upstream) {
				if len(r.Coins) != 1 || r.Coins[0].Name != "Pepe" || r.Coins[0].Symbol != "PEPE" || r.Coins[0].USD != 4.11e-06 {
					t.Errorf("result = %+v", r)
				}
			},
		},
		{
			name: "unknown coins are listed",
			args: `{"coins":["bitcoin","qwzxqy","btc"]}`,
			check: func(t *testing.T, r cryptoResult, _ *upstream) {
				if len(r.Coins) != 1 || len(r.NotFound) != 1 || r.NotFound[0] != "qwzxqy" {
					t.Errorf("result = %+v", r)
				}
			},
		},
		{
			name:     "so'm from the Central Bank when CoinGecko has none",
			override: map[string]stub{pricePath: ok(`{"bitcoin":{"usd":82069,"usd_24h_change":1.3718}}`)},
			args:     `{"coins":["btc"]}`,
			check: func(t *testing.T, r cryptoResult, _ *upstream) {
				if len(r.Coins) != 1 || r.Coins[0].UZS == nil || *r.Coins[0].UZS != 985784103.85 || !strings.Contains(r.Note, "Central Bank") {
					t.Errorf("result = %+v", r)
				}
			},
		},
		{
			name: "the demo key travels in a header",
			key:  "cg-secret",
			args: `{"coins":["btc"]}`,
			check: func(t *testing.T, _ cryptoResult, u *upstream) {
				request := u.requests(pricePath)[0]
				if request.Header.Get("x-cg-demo-api-key") != "cg-secret" || strings.Contains(request.URL.String(), "cg-secret") {
					t.Errorf("request = %s %v", request.URL, request.Header)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := cryptoRoutes(t)
			for path, answer := range test.override {
				routes[path] = answer
			}
			u := newUpstream(t, routes)
			opts := u.options()
			opts.CoinGeckoKey = test.key
			env := run(t, opts, "crypto_price", test.args)
			wantFits(t, env, registryLimit)
			test.check(t, result[cryptoResult](t, env), u)
		})
	}
}

func TestCryptoPriceFailures(t *testing.T) {
	tests := []struct {
		name     string
		override map[string]stub
		args     string
		code     string
		words    []string
	}{
		{name: "an empty answer means no such coin", override: map[string]stub{pricePath: ok(fixture(t, "coingecko_empty.json"))}, args: `{"coins":["doge"]}`, code: "invalid_arguments", words: []string{"doge"}},
		{name: "nothing found", args: `{"coins":["qwzxqy"]}`, code: "invalid_arguments", words: []string{"qwzxqy"}},
		{name: "busy", override: map[string]stub{pricePath: status(http.StatusTooManyRequests)}, args: `{"coins":["btc"]}`, code: "execution_failed", words: []string{"CoinGecko is busy"}},
		{name: "slow", override: map[string]stub{pricePath: slow}, args: `{"coins":["btc"]}`, code: "timeout"},
		{name: "no coins", args: `{"coins":[]}`, code: "invalid_arguments"},
		{name: "no list", args: `{}`, code: "invalid_arguments"},
		{name: "too many coins", args: `{"coins":["a","b","c","d","e","f"]}`, code: "invalid_arguments"},
		{name: "a blank coin", args: `{"coins":[" "]}`, code: "invalid_arguments"},
		{name: "a coin name that is a paragraph", args: `{"coins":["` + strings.Repeat("a", 41) + `"]}`, code: "invalid_arguments"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := cryptoRoutes(t)
			for path, answer := range test.override {
				routes[path] = answer
			}
			wantError(t, run(t, impatient(newUpstream(t, routes).options()), "crypto_price", test.args), test.code, test.words...)
		})
	}
}

func TestCryptoPriceIsCached(t *testing.T) {
	u := newUpstream(t, cryptoRoutes(t))
	registry := newRegistry(t, u.options())

	for _, args := range []string{`{"coins":["btc","ton"]}`, `{"coins":["toncoin","bitcoin"]}`} {
		result[cryptoResult](t, execute(t, registry, "crypto_price", args))
	}
	if n := len(u.requests(pricePath)); n != 1 {
		t.Errorf("CoinGecko was asked %d times, want 1", n)
	}
}
