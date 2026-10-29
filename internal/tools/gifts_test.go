package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// fakeGiftShop sells two gifts and a sold-out one, and records the gifts
// sent.
type fakeGiftShop struct {
	mu      sync.Mutex
	balance int
	sent    []string
	sentTo  []int64
	err     error
}

func (f *fakeGiftShop) GetAvailableGifts(context.Context) ([]telegram.Gift, error) {
	return []telegram.Gift{
		{ID: "rose", Sticker: telegram.Sticker{Emoji: "🌹"}, StarCount: 25},
		{ID: "bear", Sticker: telegram.Sticker{Emoji: "🧸"}, StarCount: 15},
		{ID: "gone", Sticker: telegram.Sticker{Emoji: "💎"}, StarCount: 5, TotalCount: 100, RemainingCount: 0},
		{ID: "cake", Sticker: telegram.Sticker{Emoji: "🎂"}, StarCount: 50, TotalCount: 1000, RemainingCount: 9},
	}, nil
}

func (f *fakeGiftShop) SendGift(_ context.Context, userID int64, giftID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, strings.TrimSpace(giftID+" "+text))
	f.sentTo = append(f.sentTo, userID)
	return nil
}

func (f *fakeGiftShop) GetMyStarBalance(context.Context) (int, error) {
	return f.balance, nil
}

type giftEnv struct {
	shop     *fakeGiftShop
	registry *Registry
}

func newGiftEnv(t *testing.T) *giftEnv {
	t.Helper()
	chat := &fakeChat{members: map[int64]telegram.ChatMember{
		saidID: {Status: "member", User: &telegram.User{ID: saidID, FirstName: "Said", Username: "said_05"}},
	}, failing: map[int64]bool{}}
	// Tags are confirmed as the moderate tool confirms them.
	moderation := NewModeration(ModerationOptions{BotID: rulesBotID, Members: chat, Usernames: fakeUsernames{"said_05": saidID}})
	env := &giftEnv{shop: &fakeGiftShop{balance: 30}}
	env.registry = mustRegistry(t, GiftTool(GiftOptions{Shop: env.shop, Members: moderation, BotID: rulesBotID}))
	return env
}

func (env *giftEnv) gift(ctx context.Context, args string) envelope {
	caller, _ := CallerFrom(ctx)
	var result envelope
	_ = json.Unmarshal([]byte(env.registry.Execute(ctx, caller.UserID, "send_gift", args)), &result)
	return result
}

// ownerGifting is an owner's turn in the group, with their words, replying
// to target (or to nothing) and tagging mentions.
func ownerGifting(words string, target *Target, mentions ...Mention) context.Context {
	return WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Owner: true, Human: true, UpdateID: 100, MessageID: 50, Target: target, Mentions: mentions, Words: words}, &Sink{})
}

func saidsMessage() *Target {
	return &Target{MessageID: 49, UserID: saidID, Name: "Said @said_05"}
}

// The list is cheapest first, without what is sold out, with the balance.
func TestGiftListShowsCostAndBalance(t *testing.T) {
	env := newGiftEnv(t)
	got := env.gift(ownerGifting("qanday sovg'alar bor?", nil), `{"action":"list"}`)
	if !got.OK {
		t.Fatalf("list = %+v", got)
	}
	var result struct {
		Balance int          `json:"balance"`
		Gifts   []listedGift `json:"gifts"`
	}
	if err := json.Unmarshal(got.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.Balance != 30 || len(result.Gifts) != 3 || result.Gifts[0].ID != "bear" || result.Gifts[2].ID != "cake" || result.Gifts[2].Left == nil || *result.Gifts[2].Left != 9 {
		t.Errorf("list = %+v", result)
	}
}

// A gift goes to the member replied to, or the one tagged once Telegram
// confirms who that is, with its text.
func TestGiftGoesToTheMemberRepliedToOrTagged(t *testing.T) {
	t.Run("reply", func(t *testing.T) {
		env := newGiftEnv(t)
		got := env.gift(ownerGifting("Maria unga sovg'a yubor", saidsMessage()), `{"action":"send","gift_id":"bear","text":"Tabriklayman!"}`)
		if !got.OK || !strings.Contains(string(got.Result), `"balance":15`) {
			t.Fatalf("send = %+v %s", got, got.Result)
		}
		if len(env.shop.sentTo) != 1 || env.shop.sentTo[0] != saidID || env.shop.sent[0] != "bear Tabriklayman!" {
			t.Errorf("sent %v to %v", env.shop.sent, env.shop.sentTo)
		}
	})
	t.Run("tag", func(t *testing.T) {
		env := newGiftEnv(t)
		got := env.gift(ownerGifting("@said_05 ga подарок", nil, Mention{Username: "said_05"}), `{"action":"send","gift_id":"bear"}`)
		if !got.OK || len(env.shop.sentTo) != 1 || env.shop.sentTo[0] != saidID {
			t.Errorf("send = %+v, to %v", got, env.shop.sentTo)
		}
	})
}

// Nothing is bought unless an owner, in a group, asks in their own words
// for a gift on sale, for one member who is a person, and the balance
// covers it.
func TestGiftRefusals(t *testing.T) {
	cases := map[string]struct {
		ctx  context.Context
		args string
		want string
	}{
		"not an owner": {
			WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: 100, Target: saidsMessage(), Words: "sovg'a yubor"}, &Sink{}),
			`{"action":"send","gift_id":"bear"}`, "restricted",
		},
		"private chat": {
			WithTurn(context.Background(), Caller{UserID: userID, ChatID: userID, Owner: true, Human: true, UpdateID: 100, Words: "sovg'a yubor"}, &Sink{}),
			`{"action":"send","gift_id":"bear"}`, "from a group",
		},
		"not asked":        {ownerGifting("Said zo'r yigit", saidsMessage()), `{"action":"send","gift_id":"bear"}`, "does not ask"},
		"nobody meant":     {ownerGifting("sovg'a yubor", nil), `{"action":"send","gift_id":"bear"}`, "reply to the message"},
		"own message":      {ownerGifting("sovg'a yubor", &Target{UserID: userID}), `{"action":"send","gift_id":"bear"}`, "reply to the message"},
		"to the bot":       {ownerGifting("sovg'a yubor", &Target{UserID: rulesBotID, IsBot: true}), `{"action":"send","gift_id":"bear"}`, "bots cannot"},
		"unknown tag":      {ownerGifting("sovg'a yubor", nil, Mention{Username: "nobody"}), `{"action":"send","gift_id":"bear"}`, "have not seen"},
		"unknown gift":     {ownerGifting("sovg'a yubor", saidsMessage()), `{"action":"send","gift_id":"car"}`, "no gift"},
		"sold out":         {ownerGifting("sovg'a yubor", saidsMessage()), `{"action":"send","gift_id":"gone"}`, "sold out"},
		"too expensive":    {ownerGifting("sovg'a yubor", saidsMessage()), `{"action":"send","gift_id":"cake"}`, "balance is 30 and that gift costs 50"},
		"text too long":    {ownerGifting("sovg'a yubor", saidsMessage()), `{"action":"send","gift_id":"bear","text":"` + strings.Repeat("a", 129) + `"}`, "128"},
		"no gift id":       {ownerGifting("sovg'a yubor", saidsMessage()), `{"action":"send"}`, "gift_id"},
		"unknown action":   {ownerGifting("sovg'a yubor", saidsMessage()), `{"action":"buy"}`, "list or send"},
		"two tags at once": {ownerGifting("sovg'a yubor", nil, Mention{Username: "said_05"}, Mention{Username: "ali"}), `{"action":"send","gift_id":"bear"}`, "more than one"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			env := newGiftEnv(t)
			got := env.gift(c.ctx, c.args)
			if got.OK || !strings.Contains(got.Error.Message, c.want) {
				t.Errorf("send = %+v, want refused with %q", got, c.want)
			}
			if len(env.shop.sent) != 0 {
				t.Errorf("sent %v", env.shop.sent)
			}
		})
	}
}

// Telegram's refusal reaches the model in its words.
func TestGiftTelegramRefuses(t *testing.T) {
	env := newGiftEnv(t)
	env.shop.err = &telegram.APIError{Code: 400, Description: "Bad Request: BALANCE_TOO_LOW"}
	got := env.gift(ownerGifting("sovg'a yubor", saidsMessage()), `{"action":"send","gift_id":"bear"}`)
	if got.OK || !strings.Contains(got.Error.Message, "BALANCE_TOO_LOW") {
		t.Errorf("send = %+v", got)
	}
}
