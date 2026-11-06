package tools

import (
	"context"
	"fmt"
	"slices"
	"testing"
)

const promoChannel = int64(-1009999)

// channelPost is a message posted in groupA on behalf of a channel, as
// Telegram's account for that.
func channelPost() *Target {
	return &Target{MessageID: 29, UserID: 136817688, ChannelID: promoChannel, Name: "Promo @promo_ch", Text: "arzon kripto"}
}

func replyingWith(updateID int64, target *Target, words string) context.Context {
	return WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: updateID, MessageID: updateID, Target: target, Words: words}, &Sink{})
}

// Banning a channel's post bans the channel from posting, not the account
// Telegram posts it as; undo lets the channel post again.
func TestBanOnAChannelsPostBansTheChannel(t *testing.T) {
	env := newModerationEnv(t)

	said := env.act(t, replyingWith(100, channelPost(), "ban qil"), `{"action":"ban"}`)

	if said != "Bajarildi: endi «Promo @promo_ch» kanali nomidan bu guruhda yozib bo'lmaydi." {
		t.Errorf("said %q", said)
	}
	if want := []string{fmt.Sprintf("banChatSenderChat %d %d", groupA, promoChannel)}; !slices.Equal(env.telegram.made(), want) {
		t.Errorf("calls = %q, want %q", env.telegram.made(), want)
	}
	if audit := env.audit.recorded(); len(audit) != 1 || audit[0].TargetUserID != promoChannel || audit[0].TargetMessageID != 29 {
		t.Errorf("audit = %+v, want the channel as the target", audit)
	}

	if said := env.act(t, undoTurn(101, "orqaga qaytar"), `{"action":"undo"}`); said != "Bajarildi: kanal nomidan yana yozish mumkin." {
		t.Errorf("undo said %q", said)
	}
	if calls := env.telegram.made(); len(calls) != 2 || calls[1] != fmt.Sprintf("unbanChatSenderChat %d %d", groupA, promoChannel) {
		t.Errorf("calls = %q, want the channel unbanned", calls)
	}
}

func TestUnbanOnAChannelsPostUnbansTheChannel(t *testing.T) {
	env := newModerationEnv(t)

	env.act(t, replyingWith(100, channelPost(), "unban qil"), `{"action":"unban"}`)

	if want := []string{fmt.Sprintf("unbanChatSenderChat %d %d", groupA, promoChannel)}; !slices.Equal(env.telegram.made(), want) {
		t.Errorf("calls = %q, want %q", env.telegram.made(), want)
	}
}

// A channel's post may be deleted or pinned like a member's; nothing that
// only fits a member is done to a channel.
func TestChannelsPostsTakeOnlyChannelActions(t *testing.T) {
	env := newModerationEnv(t)
	env.act(t, replyingWith(100, channelPost(), "o'chir"), `{"action":"delete"}`)
	if want := []string{fmt.Sprintf("deleteMessage %d 29", groupA)}; !slices.Equal(env.telegram.made(), want) {
		t.Errorf("calls = %q, want %q", env.telegram.made(), want)
	}

	for i, action := range []string{"mute", "no_stickers", "no_photos", "promote", "purge"} {
		env := groupAdminEnv(t)
		got := env.moderate(replyingWith(int64(200+i), channelPost(), askingForAll+", stikerini ol, rasm, admin qil, tozala"), `{"action":"`+action+`"}`)
		expectRefusal(t, got, "forbidden", "posted on behalf of a channel")
		env.nothingActed(t)
	}
}
