package service

import (
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// A post on behalf of a channel names the channel for moderation; one the
// group posts as itself, and one Telegram forwards from the linked channel,
// name none.
func TestTargetNamesTheChannelAPostIsFrom(t *testing.T) {
	f := newFixture()
	s := New(f.opts)
	defer s.Close()
	channelBot := &telegram.User{ID: 136817688, IsBot: true, FirstName: "Channel", Username: "Channel_Bot"}
	channel := &telegram.Chat{ID: -1009999, Type: telegram.ChatTypeChannel, Title: "Promo", Username: "promo_ch"}

	posted := s.target(&telegram.Message{MessageID: 29, From: channelBot, SenderChat: channel, Text: "arzon kripto"})
	if posted == nil || posted.ChannelID != channel.ID || posted.Name != "Promo @promo_ch" || posted.MessageID != 29 || posted.Text != "arzon kripto" {
		t.Errorf("channel post = %+v", posted)
	}

	forwarded := s.target(&telegram.Message{MessageID: 30, From: &telegram.User{ID: 777000}, SenderChat: channel, IsAutomaticForward: true})
	asGroup := s.target(&telegram.Message{MessageID: 31, From: &telegram.User{ID: 1087968824, IsBot: true},
		SenderChat: &telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup, Title: "Lobby"}})
	if forwarded == nil || forwarded.ChannelID != 0 || asGroup == nil || asGroup.ChannelID != 0 {
		t.Errorf("forwarded = %+v, as the group = %+v; want no channel", forwarded, asGroup)
	}
}
