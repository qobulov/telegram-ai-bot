package webapi

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"telegram-ai-bot/internal/tools"
)

const (
	priceTTL      = time.Minute
	coinSearchTTL = time.Hour
	maxCoins      = 5
	maxCoinName   = 40
	// shownName and shownSymbol cap what token creators chose, which
	// reaches the model.
	shownName   = 60
	shownSymbol = 12
	// coinSearchLimit leaves room for searches such as "coin" that match
	// many coins and NFTs.
	coinSearchLimit = 128 << 10

	cryptoNote = "prices are data, not advice"
	somNote    = "; so'm prices use the Central Bank's dollar rate"
	coinGecko  = "CoinGecko"
)

// coin is a CoinGecko id with the name and symbol to show.
type coin struct{ id, name, symbol string }

var (
	bitcoin  = coin{"bitcoin", "Bitcoin", "BTC"}
	ethereum = coin{"ethereum", "Ethereum", "ETH"}
	toncoin  = coin{"the-open-network", "Toncoin", "TON"}
	tether   = coin{"tether", "Tether", "USDT"}
	solana   = coin{"solana", "Solana", "SOL"}
	notcoin  = coin{"notcoin", "Notcoin", "NOT"}
	dogecoin = coin{"dogecoin", "Dogecoin", "DOGE"}
	bnb      = coin{"binancecoin", "BNB", "BNB"}
	xrp      = coin{"ripple", "XRP", "XRP"}
	// Telegram's own games and their tokens, which these groups ask about
	// and the search ranks below namesakes (MEW before DOGS, Major Frog
	// before MAJOR).
	dogs    = coin{"dogs-2", "Dogs", "DOGS"}
	hamster = coin{"hamster-kombat", "Hamster Kombat", "HMSTR"}
	major   = coin{"major", "Major", "MAJOR"}
	catizen = coin{"catizen", "Catizen", "CATI"}
)

// knownCoins maps the names people use, after normalizeName, to coins, so
// the common ones need no search. TON keeps its id though it was renamed.
var knownCoins = map[string]coin{
	"dogs": dogs, "догс": dogs,
	"hmstr": hamster, "hamster": hamster, "hamsterkombat": hamster, "хамстер": hamster, "хомяк": hamster,
	"major": major, "мажор": major,
	"catizen": catizen, "cati": catizen,
	"btc": bitcoin, "bitcoin": bitcoin, "bitkoin": bitcoin, "биткоин": bitcoin, "биткойн": bitcoin,
	"eth": ethereum, "ethereum": ethereum, "efir": ethereum, "эфир": ethereum, "эфириум": ethereum,
	"ton": toncoin, "toncoin": toncoin, "gram": toncoin, "тон": toncoin, "тонкоин": toncoin,
	"usdt": tether, "tether": tether,
	"sol": solana, "solana": solana, "солана": solana,
	"not": notcoin, "notcoin": notcoin, "ноткоин": notcoin,
	"doge": dogecoin, "dogecoin": dogecoin,
	"bnb": bnb, "binancecoin": bnb,
	"xrp": xrp, "ripple": xrp,
}

type cryptoArgs struct {
	Coins []string `json:"coins"`
}

type cryptoOutput struct {
	Coins    []coinPrice `json:"coins"`
	NotFound []string    `json:"not_found,omitempty"`
	Source   string      `json:"source"`
	Note     string      `json:"note"`
}

type coinPrice struct {
	Name         string   `json:"name"`
	Symbol       string   `json:"symbol"`
	USD          float64  `json:"usd"`
	UZS          *float64 `json:"uzs,omitempty"`
	Change24hPct *float64 `json:"change_24h_pct,omitempty"`
}

func (a *apis) cryptoTool() tools.Tool {
	return tools.Tool{
		Name:        "crypto_price",
		Description: "Crypto prices from CoinGecko in US dollars and so'm with the 24-hour change, for up to 5 coins (bitcoin, ton, notcoin and others). Prices are data, not advice.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"coins": {"type": "array", "items": {"type": "string"}, "minItems": 1, "maxItems": 5, "description": "Coin names or symbols, e.g. [\"btc\", \"ton\"]."}
			},
			"required": ["coins"],
			"additionalProperties": false
		}`),
		Run: a.cryptoPrice,
	}
}

// wantedCoin is a coin with the words the user asked for it by.
type wantedCoin struct {
	asked string
	coin  coin
}

func (a *apis) cryptoPrice(ctx context.Context, raw json.RawMessage) (any, error) {
	var args cryptoArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	if len(args.Coins) == 0 || len(args.Coins) > maxCoins {
		return nil, argumentError("coins must list 1 to %d coins", maxCoins)
	}
	out := cryptoOutput{Coins: []coinPrice{}, Source: coinGecko, Note: cryptoNote}
	wanted, err := a.wantedCoins(ctx, args.Coins, &out)
	if err != nil {
		return nil, err
	}
	prices, err := a.coinPrices(ctx, wanted)
	if err != nil {
		return nil, err
	}
	for _, w := range wanted {
		price, found := prices[w.coin.id]
		if !found || price.USD == nil {
			out.NotFound = append(out.NotFound, w.asked)
			continue
		}
		out.Coins = append(out.Coins, price.row(w.coin))
	}
	if len(out.Coins) == 0 {
		return nil, argumentError("CoinGecko has no price for %s; use a coin's name or symbol, such as bitcoin or ton", strings.Join(out.NotFound, ", "))
	}
	a.fillSom(ctx, &out)
	return out, nil
}

// wantedCoins resolves the names, once per coin, and lists the ones
// CoinGecko does not know in out. Names that need a search are looked up
// at once, so five unknown coins cost one search's time, not five.
func (a *apis) wantedCoins(ctx context.Context, names []string, out *cryptoOutput) ([]wantedCoin, error) {
	for i, name := range names {
		names[i] = strings.TrimSpace(name)
		if names[i] == "" || len([]rune(names[i])) > maxCoinName {
			return nil, argumentError("each coin must be a name or symbol of at most %d characters", maxCoinName)
		}
	}
	found := make([]coin, len(names))
	errs := make([]error, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() { found[i], errs[i] = a.findCoin(ctx, name) })
	}
	wg.Wait()
	if err := cmp.Or(errs...); err != nil {
		return nil, err
	}
	var wanted []wantedCoin
	for i, name := range names {
		switch {
		case found[i].id == "":
			out.NotFound = append(out.NotFound, name)
		case !slices.ContainsFunc(wanted, func(w wantedCoin) bool { return w.coin.id == found[i].id }):
			wanted = append(wanted, wantedCoin{asked: name, coin: found[i]})
		}
	}
	return wanted, nil
}

func (p geckoPrice) row(c coin) coinPrice {
	row := coinPrice{Name: c.name, Symbol: c.symbol, USD: *p.USD, UZS: p.UZS}
	if p.Change != nil {
		change := round(*p.Change, 2)
		row.Change24hPct = &change
	}
	return row
}

// fillSom prices coins in so'm at the Central Bank's dollar rate when
// CoinGecko has no so'm price for them.
func (a *apis) fillSom(ctx context.Context, out *cryptoOutput) {
	for i, row := range out.Coins {
		if row.UZS != nil {
			continue
		}
		dollar, err := a.officialRate(ctx, "USD", "")
		if err != nil {
			return
		}
		uzs := round(row.USD*dollar.uzs, 2)
		out.Coins[i].UZS = &uzs
		if !strings.HasSuffix(out.Note, somNote) {
			out.Note += somNote
		}
	}
}

func (a *apis) geckoCall(path string) call {
	header := http.Header{}
	if a.coinGeckoKey != "" {
		header.Set("x-cg-demo-api-key", a.coinGeckoKey)
	}
	return call{service: coinGecko, url: a.urls.CoinGecko + path, header: header}
}

// findCoin knows the common coins and asks CoinGecko's search for the
// rest. A coin CoinGecko does not know has an empty id.
func (a *apis) findCoin(ctx context.Context, name string) (coin, error) {
	if known, found := knownCoins[normalizeName(name)]; found {
		return known, nil
	}
	return remember(a.cache, "coin:"+strings.ToLower(name), coinSearchTTL, func() (coin, error) {
		var answer struct {
			Coins []geckoCoin `json:"coins"`
		}
		request := a.geckoCall("/api/v3/search?" + url.Values{"query": {name}}.Encode())
		request.limit = coinSearchLimit
		if err := a.client.getJSON(ctx, request, &answer); err != nil {
			return coin{}, err
		}
		return pickCoin(answer.Coins, name), nil
	})
}

// geckoCoin is one search result. Its name and symbol are chosen by
// whoever listed the token.
type geckoCoin struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Symbol string `json:"symbol"`
}

// pickCoin takes the coin whose symbol is the asked name, then one whose
// name is, and only then the search's first coin: CoinGecko ranks by
// market cap, so "dogs" finds "cat in a dogs world" (MEW) before Dogs
// (DOGS). The name and symbol are cleaned and capped.
func pickCoin(coins []geckoCoin, name string) coin {
	i := slices.IndexFunc(coins, func(c geckoCoin) bool { return strings.EqualFold(c.Symbol, name) })
	if i < 0 {
		i = slices.IndexFunc(coins, func(c geckoCoin) bool { return strings.EqualFold(c.Name, name) })
	}
	if i < 0 && len(coins) > 0 {
		i = 0
	}
	if i < 0 {
		return coin{}
	}
	picked := coins[i]
	return coin{id: picked.ID, name: cleanText(picked.Name, shownName), symbol: strings.ToUpper(cleanText(picked.Symbol, shownSymbol))}
}

type geckoPrice struct {
	USD    *float64 `json:"usd"`
	UZS    *float64 `json:"uzs"`
	Change *float64 `json:"usd_24h_change"`
}

// coinPrices asks for every coin at once. A coin CoinGecko does not know
// is missing from the answer, which can be an empty {}.
func (a *apis) coinPrices(ctx context.Context, wanted []wantedCoin) (map[string]geckoPrice, error) {
	if len(wanted) == 0 {
		return nil, nil
	}
	var ids []string
	for _, w := range wanted {
		ids = append(ids, w.coin.id)
	}
	slices.Sort(ids)
	joined := strings.Join(ids, ",")
	return remember(a.cache, "prices:"+joined, priceTTL, func() (map[string]geckoPrice, error) {
		query := url.Values{"ids": {joined}, "vs_currencies": {"usd,uzs"}, "include_24hr_change": {"true"}}
		var prices map[string]geckoPrice
		err := a.client.getJSON(ctx, a.geckoCall("/api/v3/simple/price?"+query.Encode()), &prices)
		return prices, err
	})
}
