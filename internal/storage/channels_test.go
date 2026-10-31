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

// Private chats are the users who wrote to the bot in private, once each,
// most recently first; a group's conversation is none.
func TestPrivateChats(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, sql := range []string{
		`INSERT INTO users (telegram_id) VALUES (11), (12)`,
		`INSERT INTO conversations (chat_id, user_id, updated_at) VALUES (11, 11, now() - interval '2 days'), (-100500, 12, now())`,
		`INSERT INTO private_messages (user_id, created_at) VALUES (13, now() - interval '1 day'), (11, now() - interval '3 days')`,
	} {
		if _, err := store.pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := store.PrivateChats(ctx, 10)
	if err != nil || !reflect.DeepEqual(ids, []int64{13, 11}) {
		t.Errorf("private chats = %v, %v; want 13 then 11", ids, err)
	}
}
