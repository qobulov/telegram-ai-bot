package storage

import (
	"context"
	"reflect"
	"testing"
	"time"
)

const channel = int64(-1009001)

// A channel keeps the bot's latest standing and rights there, and its name
// when Telegram gives none; it is never one of the groups.
func TestRecordChannelKeepsTheBotsStanding(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	post := []string{"can_post_messages"}
	if err := store.RecordChannel(ctx, GroupStatus{ChatID: channel, Title: "News", Username: "news_uz", BotStatus: "administrator", Rights: post}); err != nil {
		t.Fatalf("RecordChannel: %v", err)
	}
	channels, err := store.Channels(ctx, 10)
	want := []ChannelSummary{{ChatID: channel, Title: "News", Username: "news_uz", BotStatus: "administrator", Rights: post}}
	if err != nil || !reflect.DeepEqual(channels, want) {
		t.Errorf("channels = %+v, err = %v; want %+v", channels, err, want)
	}

	if err := store.RecordChannel(ctx, GroupStatus{ChatID: channel, BotStatus: "kicked"}); err != nil {
		t.Fatal(err)
	}
	channels, _ = store.Channels(ctx, 10)
	if len(channels) != 1 || channels[0].BotStatus != "kicked" || channels[0].Title != "News" || len(channels[0].Rights) != 0 {
		t.Errorf("after removal, channels = %+v", channels)
	}
	if groups, err := store.Groups(ctx, time.Now().Add(-time.Hour), 10); err != nil || len(groups) != 0 {
		t.Errorf("groups = %+v, err = %v; a channel is no group", groups, err)
	}
}
