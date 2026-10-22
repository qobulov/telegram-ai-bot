package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"telegram-ai-bot/internal/storage"
)

type fakeItemCards struct {
	cards []storage.ItemCardSticker
	err   error
}

func (f fakeItemCards) ItemCards(context.Context) ([]storage.ItemCardSticker, error) {
	return f.cards, f.err
}

var someCards = fakeItemCards{cards: []storage.ItemCardSticker{
	{FileID: "f-athena", Name: "Athena's Shield"},
	{FileID: "f-magic", Name: "Magic Shoes"},
	{FileID: "f-swift", Name: "Swift Boots"},
	{FileID: "f-tough", Name: "Tough Boots"},
}}

func runItemCard(t *testing.T, cards ItemCards, args string) (envelope, *Sink) {
	t.Helper()
	sink := &Sink{}
	ctx := WithTurn(context.Background(), Caller{UserID: memberID, ChatID: groupA, Human: true}, sink)
	var env envelope
	_ = json.Unmarshal([]byte(mustRegistry(t, ItemCardTool(ItemCardOptions{Store: cards})).Execute(ctx, memberID, "mlbb_item_card", args)), &env)
	return env, sink
}

// A card is found by its name however it is written, or by a part of it
// that only one name has.
func TestItemCardFound(t *testing.T) {
	for args, want := range map[string]string{
		`{"items":["athenas shield"]}`: "f-athena",
		`{"items":["ATHENA"]}`:         "f-athena",
		`{"items":["Magic-Shoes"]}`:    "f-magic",
	} {
		env, sink := runItemCard(t, someCards, args)
		if stickers := sink.TakeStickers(); !env.OK || len(stickers) != 1 || stickers[0] != want {
			t.Errorf("%s: %+v, stickers %v, want %s", args, env, stickers, want)
		}
	}
}

// A name that is no card's, or part of several, sends nothing and lists the
// cards there are.
func TestItemCardNotFound(t *testing.T) {
	for _, args := range []string{`{"items":["boots"]}`, `{"items":["Blade of Despair"]}`} {
		env, sink := runItemCard(t, someCards, args)
		if env.OK || !strings.Contains(env.Error.Message, "Swift Boots") || len(sink.TakeStickers()) != 0 {
			t.Errorf("%s: %+v", args, env)
		}
	}
	env, _ := runItemCard(t, fakeItemCards{}, `{"items":["Athena"]}`)
	if env.OK || !strings.Contains(env.Error.Message, "none yet") {
		t.Errorf("no cards: %+v", env)
	}
	if env, _ := runItemCard(t, someCards, `{"items":[" !! "]}`); env.OK || env.Error.Code != "invalid_arguments" {
		t.Errorf("empty name: %+v", env)
	}
	if env, _ := runItemCard(t, fakeItemCards{err: errors.New("db down")}, `{"items":["Athena"]}`); env.OK || strings.Contains(env.Error.Message, "db down") {
		t.Errorf("store error: %+v, want it hidden", env)
	}
}

// A build's cards go in one call, in order and once each; an item without a
// card is named in the result.
func TestItemCardsForABuild(t *testing.T) {
	env, sink := runItemCard(t, someCards, `{"items":["Magic Shoes","Athena's Shield","Blade of Despair","athena"]}`)
	stickers := sink.TakeStickers()
	if !env.OK || strings.Join(stickers, ",") != "f-magic,f-athena" || !strings.Contains(string(env.Result), "Blade of Despair") {
		t.Errorf("%+v %s, stickers %v", env, env.Result, stickers)
	}
	if env, _ := runItemCard(t, someCards, `{"items":["a","b","c","d","e","f","g"]}`); env.OK {
		t.Errorf("seven items = %+v, want refused", env)
	}
}
