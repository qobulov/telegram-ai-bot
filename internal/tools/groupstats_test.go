package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

type fakeStats struct {
	stats    storage.GroupStats
	err      error
	chatID   int64
	since    time.Time
	timeZone string
}

func (f *fakeStats) GroupStats(_ context.Context, chatID, _ int64, since time.Time, timeZone string, _ int) (storage.GroupStats, error) {
	f.chatID, f.since, f.timeZone = chatID, since, timeZone
	return f.stats, f.err
}

func askStats(t *testing.T, store *fakeStats, ctx context.Context, args string) (envelope, groupStatsResult) {
	t.Helper()
	registry := mustRegistry(t, GroupStatsTool(GroupStatsOptions{Store: store, BotID: rulesBotID, Location: tashkent, Now: func() time.Time { return rulesNow }}))
	caller, _ := CallerFrom(ctx)
	var got envelope
	_ = json.Unmarshal([]byte(registry.Execute(ctx, caller.UserID, "group_stats", args)), &got)
	var result groupStatsResult
	if got.OK {
		if err := json.Unmarshal(got.Result, &result); err != nil {
			t.Fatalf("result %s: %v", got.Result, err)
		}
	}
	return got, result
}

func TestGroupStatsGivesTodaysTopWritersAndBusiestHours(t *testing.T) {
	store := &fakeStats{stats: storage.GroupStats{
		Messages: 120, Members: 9,
		Top:      []storage.AuthorCount{{UserID: memberID, Name: "Bek @bek", Count: 50}, {UserID: userID, Name: "Ali", Count: 30}},
		Swearers: []storage.AuthorCount{{UserID: memberID, Name: "Bek", Count: 2}},
	}}
	store.stats.Hours[13], store.stats.Hours[14], store.stats.Hours[9], store.stats.Hours[22] = 40, 55, 5, 20
	got, result := askStats(t, store, asking(1, nil), `{}`)
	if !got.OK || store.chatID != groupA || store.timeZone != "Asia/Tashkent" {
		t.Fatalf("got %+v, asked chat %d in %q", got, store.chatID, store.timeZone)
	}
	if want := time.Date(2026, 10, 16, 0, 0, 0, 0, tashkent); !store.since.Equal(want) {
		t.Errorf("since = %v, want midnight %v", store.since, want)
	}
	if result.Messages != 120 || result.Members != 9 || len(result.Top) != 2 || result.Top[0] != (statsEntry{Name: "Bek", Count: 50}) {
		t.Errorf("result = %+v", result)
	}
	hours := []string{}
	for _, h := range result.Busiest {
		hours = append(hours, h.Name)
	}
	if strings.Join(hours, ",") != "14:00,13:00,22:00" || len(result.Swearers) != 1 {
		t.Errorf("busiest %v, swearers %+v", hours, result.Swearers)
	}
}

func TestGroupStatsForAWeek(t *testing.T) {
	store := &fakeStats{}
	if got, result := askStats(t, store, asking(1, nil), `{"period":"week"}`); !got.OK || result.Period != "week" || !store.since.Equal(rulesNow.Add(-statsWeek)) {
		t.Errorf("got %+v, since %v", got, store.since)
	}
	if got, _ := askStats(t, store, asking(1, nil), `{"period":"year"}`); got.OK || got.Error.Code != "invalid_arguments" {
		t.Errorf("a year: %+v", got)
	}
}

func TestGroupStatsOnlyInAGroup(t *testing.T) {
	private := WithTurn(context.Background(), Caller{UserID: userID, ChatID: userID, Human: true, UpdateID: 1}, &Sink{})
	if got, _ := askStats(t, &fakeStats{}, private, `{}`); got.OK {
		t.Errorf("a private chat got statistics: %+v", got)
	}
	if got, _ := askStats(t, &fakeStats{err: errors.New("down")}, asking(1, nil), `{}`); got.OK || !strings.Contains(got.Error.Message, "could not be read") {
		t.Errorf("a failed read: %+v", got)
	}
}
