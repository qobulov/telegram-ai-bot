package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/tools"
)

const (
	rateTTL           = 30 * time.Minute
	gramsPerTroyOunce = 31.1035
	maxAmount         = 1e12

	cbuSource    = "Central Bank of Uzbekistan (official rate, not bank buy/sell)"
	approxSource = "currency-api, approximate (the Central Bank did not answer)"
	metalSource  = "currency-api, approximate world price (not a bank's price)"
)

type currencyArgs struct {
	Code   string   `json:"code"`
	Amount *float64 `json:"amount"`
	Date   string   `json:"date"`
}

type currencyOutput struct {
	Code         string   `json:"code"`
	Name         string   `json:"name,omitempty"`
	RateUZS      float64  `json:"rate_uzs"`
	Diff         *float64 `json:"diff,omitempty"`
	RateDate     string   `json:"rate_date"`
	ConvertedUZS *float64 `json:"converted_uzs,omitempty"`
	Source       string   `json:"source"`
}

type metalOutput struct {
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	USDPerGram float64 `json:"usd_per_gram"`
	UZSPerGram float64 `json:"uzs_per_gram"`
	RateDate   string  `json:"rate_date"`
	// ConvertedUZS is the price of the asked number of grams.
	ConvertedUZS *float64 `json:"converted_uzs,omitempty"`
	Source       string   `json:"source"`
}

func (a *apis) currencyTool() tools.Tool {
	return tools.Tool{
		Name:        "currency_rate",
		Description: "Official Central Bank of Uzbekistan rate of a currency in so'm, optionally converting an amount; also gold and silver per gram. Rates are data, not advice.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"code": {"type": "string", "description": "ISO code or name, e.g. USD, EUR, RUB, KZT, CNY, dollar, rubl, yevro, oltin (gold), kumush (silver). Default USD."},
				"amount": {"type": "number", "description": "How much of it to convert to so'm (grams for gold and silver)."},
				"date": {"type": "string", "description": "YYYY-MM-DD for a past rate; omit for today."}
			},
			"additionalProperties": false
		}`),
		Run: a.currencyRate,
	}
}

func (a *apis) currencyRate(ctx context.Context, raw json.RawMessage) (any, error) {
	args, err := a.currencyArgs(raw)
	if err != nil {
		return nil, err
	}
	if isMetal(args.Code) {
		return a.metalPrice(ctx, args)
	}
	return a.exchangeRate(ctx, args)
}

// currencyArgs decodes and checks the arguments, leaving an ISO code and
// an empty or YYYY-MM-DD date.
func (a *apis) currencyArgs(raw json.RawMessage) (currencyArgs, error) {
	var args currencyArgs
	if err := decodeArgs(raw, &args); err != nil {
		return args, err
	}
	code, err := currencyCode(args.Code)
	if err != nil {
		return args, err
	}
	args.Code = code
	if args.Amount != nil && (*args.Amount <= 0 || *args.Amount > maxAmount) {
		return args, argumentError("amount must be positive and at most %.0f", float64(maxAmount))
	}
	if args.Date == "" {
		return args, nil
	}
	if isMetal(code) {
		return args, argumentError("gold and silver have only today's price")
	}
	day, err := a.parseDay(args.Date)
	if err != nil {
		return args, err
	}
	// The bank silently answers a future date with today's rate.
	if day.After(a.today()) {
		return args, argumentError("%s is in the future", args.Date)
	}
	args.Date = day.Format(time.DateOnly)
	return args, nil
}

func isMetal(code string) bool { return code == "XAU" || code == "XAG" }

// exchangeRate asks the Central Bank and, for today's rate only, falls
// back to currency-api, labelled approximate.
func (a *apis) exchangeRate(ctx context.Context, args currencyArgs) (any, error) {
	primary, cancel := context.WithTimeout(ctx, primaryTimeout)
	defer cancel()
	official, err := a.officialRate(primary, args.Code, args.Date)
	if err == nil {
		return official.output(args.Amount), nil
	}
	if args.Date != "" || !shouldFallBack(ctx, err) {
		return nil, err
	}
	approximate, fallbackErr := a.approximateRate(ctx, args.Code)
	if fallbackErr != nil {
		return nil, fmt.Errorf("%w; %w", err, fallbackErr)
	}
	return approximate.output(args.Amount), nil
}

// rate is one currency's price in so'm.
type rate struct {
	code   string
	name   string
	uzs    float64
	diff   *float64
	date   string
	source string
}

func (r rate) output(amount *float64) currencyOutput {
	out := currencyOutput{Code: r.code, Name: r.name, RateUZS: roundRate(r.uzs), RateDate: r.date, Source: r.source}
	if r.diff != nil {
		diff := roundRate(*r.diff)
		out.Diff = &diff
	}
	if amount != nil {
		converted := round(*amount*r.uzs, 2)
		out.ConvertedUZS = &converted
	}
	return out
}

// roundRate keeps two decimals, or more for currencies worth less than a
// so'm.
func roundRate(value float64) float64 {
	if value < 1 && value > -1 {
		return round(value, 6)
	}
	return round(value, 2)
}

// cbuRate is one row of the Central Bank's answer. Its numbers are
// strings, and Rate is for Nominal units (10 for rials and dong).
type cbuRate struct {
	Ccy     string `json:"Ccy"`
	Name    string `json:"CcyNm_UZ"`
	Nominal string `json:"Nominal"`
	Rate    string `json:"Rate"`
	Diff    string `json:"Diff"`
	Date    string `json:"Date"`
}

// officialRate asks the Central Bank for code on date, or for the latest
// rate when date is empty.
func (a *apis) officialRate(ctx context.Context, code, date string) (rate, error) {
	path := code + "/"
	if date != "" {
		path += date + "/"
	}
	return remember(a.cache, "cbu:"+path, rateTTL, func() (rate, error) {
		var rows []cbuRate
		if err := a.client.getJSON(ctx, call{service: "the Central Bank", url: a.urls.CBU + "/" + path}, &rows); err != nil {
			return rate{}, err
		}
		if len(rows) == 0 {
			return rate{}, argumentError("the Central Bank has no rate for %s; use an ISO code such as USD, EUR or RUB", code)
		}
		return rows[0].parse()
	})
}

func (row cbuRate) parse() (rate, error) {
	nominal, errNominal := strconv.ParseFloat(row.Nominal, 64)
	value, errRate := strconv.ParseFloat(row.Rate, 64)
	diff, errDiff := strconv.ParseFloat(row.Diff, 64)
	date, errDate := time.Parse("02.01.2006", row.Date)
	if errNominal != nil || errRate != nil || errDiff != nil || errDate != nil || nominal <= 0 {
		return rate{}, fmt.Errorf("the Central Bank sent a rate that could not be read")
	}
	diff /= nominal
	return rate{
		code:   row.Ccy,
		name:   row.Name,
		uzs:    value / nominal,
		diff:   &diff,
		date:   date.Format(time.DateOnly),
		source: cbuSource,
	}, nil
}

// usdRates is currency-api's answer: how much of each currency (lower-case
// code) one US dollar buys. Metals are in troy ounces.
type usdRates struct {
	Date string             `json:"date"`
	USD  map[string]float64 `json:"usd"`
}

// usdRates asks currency-api, then its mirror on jsDelivr.
func (a *apis) usdRates(ctx context.Context) (usdRates, error) {
	return remember(a.cache, "currency-api", rateTTL, func() (usdRates, error) {
		var rates usdRates
		err := a.client.getJSON(ctx, call{service: "currency-api", url: a.urls.CurrencyAPI}, &rates)
		if err != nil && ctx.Err() == nil {
			err = a.client.getJSON(ctx, call{service: "currency-api's mirror", url: a.urls.CurrencyAPIMirror}, &rates)
		}
		if err == nil && rates.USD["uzs"] <= 0 {
			err = fmt.Errorf("currency-api sent no so'm rate")
		}
		return rates, err
	})
}

// perUSD is how much of code one US dollar buys.
func (r usdRates) perUSD(code string) (float64, error) {
	value := r.USD[strings.ToLower(code)]
	if value <= 0 {
		return 0, fmt.Errorf("currency-api has no rate for %s", code)
	}
	return value, nil
}

func (a *apis) approximateRate(ctx context.Context, code string) (rate, error) {
	rates, err := a.usdRates(ctx)
	if err != nil {
		return rate{}, err
	}
	perUSD, err := rates.perUSD(code)
	if err != nil {
		return rate{}, err
	}
	return rate{code: code, uzs: rates.USD["uzs"] / perUSD, date: rates.Date, source: approxSource}, nil
}

func (a *apis) metalPrice(ctx context.Context, args currencyArgs) (any, error) {
	rates, err := a.usdRates(ctx)
	if err != nil {
		return nil, err
	}
	ouncesPerUSD, err := rates.perUSD(args.Code)
	if err != nil {
		return nil, err
	}
	usdPerGram := 1 / ouncesPerUSD / gramsPerTroyOunce
	uzsPerGram := usdPerGram * rates.USD["uzs"]
	out := metalOutput{
		Code:       args.Code,
		Name:       map[string]string{"XAU": "oltin", "XAG": "kumush"}[args.Code],
		USDPerGram: round(usdPerGram, 2),
		UZSPerGram: round(uzsPerGram, 2),
		RateDate:   rates.Date,
		Source:     metalSource,
	}
	if args.Amount != nil {
		converted := round(*args.Amount*uzsPerGram, 2)
		out.ConvertedUZS = &converted
	}
	return out, nil
}

// currencyWords maps the names people use, after normalizeName, to ISO
// codes.
var currencyWords = map[string]string{
	"dollar": "USD", "dollor": "USD", "dollari": "USD", "aqshdollari": "USD", "доллар": "USD", "долар": "USD", "$": "USD",
	"rubl": "RUB", "rubli": "RUB", "рубль": "RUB", "рубл": "RUB", "рубли": "RUB", "₽": "RUB",
	"yevro": "EUR", "evro": "EUR", "euro": "EUR", "евро": "EUR", "€": "EUR",
	"tenge": "KZT", "тенге": "KZT",
	"yuan": "CNY", "юань": "CNY", "юан": "CNY",
	"lira": "TRY", "лира": "TRY",
	"funt": "GBP", "фунт": "GBP", "pound": "GBP", "£": "GBP",
	"yena": "JPY", "iyena": "JPY", "yen": "JPY", "иена": "JPY", "йена": "JPY",
	"von": "KRW", "won": "KRW", "вона": "KRW",
	"dirham": "AED", "дирхам": "AED",
	"frank": "CHF", "франк": "CHF",
	"oltin": "XAU", "олтин": "XAU", "gold": "XAU", "золото": "XAU",
	"kumush": "XAG", "кумуш": "XAG", "silver": "XAG", "серебро": "XAG",
}

var somWords = map[string]bool{"uzs": true, "som": true, "сум": true, "сўм": true, "сом": true}

func currencyCode(input string) (string, error) {
	word := normalizeName(input)
	if word == "" {
		return "USD", nil
	}
	if somWords[word] {
		return "", argumentError("rates are given in so'm; ask for the currency to compare with it")
	}
	if code, found := currencyWords[word]; found {
		return code, nil
	}
	if len(word) == 3 && strings.Trim(word, "abcdefghijklmnopqrstuvwxyz") == "" {
		return strings.ToUpper(word), nil
	}
	return "", argumentError("unknown currency %q; use an ISO code such as USD, EUR or RUB", input)
}
