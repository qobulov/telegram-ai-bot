package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

func TestShip(t *testing.T) {
	const otherID = int64(3)
	tool := ShipTool(MemberInfoOptions{Store: fakeMemberInfo{}, Telegram: fakeGroupInfo{},
		Tokens: func(p storage.CardPlayer, _ time.Time) int { return p.Tokens },
		Photo: func(_ context.Context, id int64) ([]byte, string, bool, error) {
			return []byte("JPEG"), "photo", true, nil
		},
		Eyes: &fakeEyes{}})
	replied := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, Target: &Target{UserID: otherID}}, &Sink{})
	got, err := tool.Run(replied, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	out := got.(map[string]any)
	percent := out["percent"].(int)
	if percent < 0 || percent > 100 {
		t.Errorf("percent = %d", percent)
	}
	if out["first"].(map[string]any)["photo"].(map[string]any)["shows"] == nil {
		t.Errorf("no photo in %v", out["first"])
	}
	if shipPercent(userID, otherID, time.Now()) != shipPercent(otherID, userID, time.Now()) {
		t.Error("the percent depends on who comes first")
	}
	day := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	if shipPercent(userID, otherID, day) != shipPercent(userID, otherID, day.Add(10*time.Hour)) {
		t.Error("the percent changed within the day")
	}
	alone := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true}, &Sink{})
	if _, err := tool.Run(alone, json.RawMessage(`{}`)); err == nil {
		t.Error("a match with no one named was answered")
	}
	// At random, the asker is matched with another who wrote lately.
	if got, err := tool.Run(alone, json.RawMessage(`{"random":true}`)); err != nil || got.(map[string]any)["second"] == nil {
		t.Errorf("random = %v, %v", got, err)
	}
	if a, b, err := randomPair(context.Background(), MemberInfoOptions{Store: fakeMemberInfo{}}, Caller{UserID: userID, ChatID: groupA}); err != nil || a != userID || b != 3 {
		t.Errorf("random pair = %d, %d, %v; want the asker and member 3", a, b, err)
	}
	twice := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, Mentions: []Mention{{UserID: otherID}, {UserID: otherID}}}, &Sink{})
	if _, err := tool.Run(twice, json.RawMessage(`{}`)); err == nil {
		t.Error("one member matched with themselves")
	}
}
