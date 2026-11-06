package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/telegram"
)

// undoTurn is userID asking in groupA, in update updateID, with words.
func undoTurn(updateID int64, words string) context.Context {
	return groupTurn(updateID, words)
}

// restrictedMember is a member restricted to p until end, as getChatMember
// gives one: the four rights a member shares with admins in their own
// fields.
func restrictedMember(p telegram.ChatPermissions, end time.Time) telegram.ChatMember {
	return telegram.ChatMember{Status: "restricted", StillIn: true, ChatPermissions: p, UntilDate: end.Unix(),
		CanChangeInfo: p.CanChangeInfo, CanInviteUsers: p.CanInviteUsers, CanPinMessages: p.CanPinMessages, CanManageTopics: p.CanManageTopics}
}

// A renamed group gets its old name back, once; asked again, there is
// nothing older left to put back.
func TestUndoPutsBackTheGroupsName(t *testing.T) {
	env := groupAdminEnv(t)
	env.telegram.chat.Title = "BS news"
	env.act(t, groupTurn(500, "guruh nomini Test qil"), `{"action":"set_title","text":"Test"}`)

	if said := env.act(t, undoTurn(501, "orqaga qaytar"), `{"action":"undo"}`); said != "Bajarildi: guruh nomi yana «BS news»." {
		t.Errorf("said %q", said)
	}
	if env.telegram.chat.Title != "BS news" {
		t.Errorf("title = %q, want the old one back", env.telegram.chat.Title)
	}
	if got := env.moderate(undoTurn(502, "yana qaytar"), `{"action":"undo"}`); got.OK || !strings.Contains(got.Error.Message, "nothing I did") {
		t.Errorf("a second undo = %+v", got)
	}
}

// A name changed by hand since is not overwritten unless they say so.
func TestUndoAsksBeforeOverwritingAChangeByHand(t *testing.T) {
	env := groupAdminEnv(t)
	env.telegram.chat.Title = "BS news"
	env.act(t, groupTurn(510, "guruh nomini Test qil"), `{"action":"set_title","text":"Test"}`)
	env.telegram.chat.Title = "Qo'lda"

	if got := env.moderate(undoTurn(511, "orqaga qaytar"), `{"action":"undo"}`); got.OK || !strings.Contains(got.Error.Message, "changed it by hand") {
		t.Errorf("undo over a change by hand = %+v", got)
	}
	if env.telegram.chat.Title != "Qo'lda" {
		t.Errorf("title = %q, want the change by hand kept", env.telegram.chat.Title)
	}
	env.act(t, undoTurn(512, "baribir qaytar"), `{"action":"undo","force":true}`)
	if env.telegram.chat.Title != "BS news" {
		t.Errorf("title = %q, want the old one back when forced", env.telegram.chat.Title)
	}
}

// A mute is undone to the member's restriction before it, end included.
func TestUndoGivesAMemberBackTheirEarlierLimits(t *testing.T) {
	env := groupAdminEnv(t)
	stickersEnd := rulesNow.Add(48 * time.Hour)
	env.chat.set(memberID, restrictedMember(telegram.NoStickers(), stickersEnd))
	ctx := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: 520, MessageID: 520, Target: bekTarget(), Words: "mute qil"}, &Sink{})
	env.act(t, ctx, `{"action":"mute"}`)

	if said := env.act(t, undoTurn(521, "orqaga qaytar"), `{"action":"undo"}`); said != "Bajarildi: a'zoning cheklovlari avvalgi holiga qaytdi." {
		t.Errorf("said %q", said)
	}
	r := env.telegram.restricted()
	if len(r) != 2 || r[1].UserID != memberID || r[1].Permissions != telegram.NoStickers() || !r[1].Until.Equal(stickersEnd) {
		t.Errorf("restrictions = %+v, want stickers taken again until %v", r, stickersEnd)
	}
}

// A deleted message never comes back, so undo has nothing to put back.
func TestUndoCannotBringBackADeletedMessage(t *testing.T) {
	env := groupAdminEnv(t)
	ctx := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: 530, MessageID: 530, Target: bekTarget(), Words: "o'chir"}, &Sink{})
	env.act(t, ctx, `{"action":"delete"}`)
	if got := env.moderate(undoTurn(531, "qaytar"), `{"action":"undo"}`); got.OK || !strings.Contains(got.Error.Message, "deleted message never comes back") {
		t.Errorf("undo after a delete = %+v", got)
	}
}

// An invite link is revoked, an old photo put back, and a lock lifted to
// the group's permissions before it.
func TestUndoRevokesLinksAndPutsBackPhotosAndPermissions(t *testing.T) {
	env := groupAdminEnv(t)
	env.moderate(groupTurn(540, "taklif havolasi ber"), `{"action":"invite_link"}`)
	env.act(t, undoTurn(541, "havolani bekor qil"), `{"action":"undo"}`)
	if calls := env.telegram.made(); calls[len(calls)-1] != fmt.Sprintf("revokeChatInviteLink https://t.me/+abc %s", inGroupA) {
		t.Errorf("last call = %q, want the link revoked", calls[len(calls)-1])
	}

	env.telegram.chat.Photo = &telegram.ChatPhoto{BigFileID: "old-photo"}
	env.act(t, groupTurn(542, "guruh rasmini olib tashla"), `{"action":"delete_photo"}`)
	env.act(t, undoTurn(543, "orqaga qaytar"), `{"action":"undo"}`)
	calls := env.telegram.made()
	if !slices.Contains(calls, "downloadFile old-photo 0 0") || calls[len(calls)-1] != "setChatPhoto 3 bytes "+inGroupA {
		t.Errorf("calls = %q, want the old photo downloaded and set again", calls)
	}

	noPolls := telegram.AllPermissions()
	noPolls.CanSendPolls = false
	env.telegram.groupPermissions = &noPolls
	env.act(t, groupTurn(544, "yozishni yop"), `{"action":"lock","kind":"messages"}`)
	env.act(t, undoTurn(545, "orqaga qaytar"), `{"action":"undo"}`)
	if p := *env.telegram.groupPermissions; p != noPolls {
		t.Errorf("permissions = %+v, want them as before the lock", p)
	}
}

// Undo needs the words and the right of the action it puts back.
func TestUndoNeedsTheWordsAndTheRight(t *testing.T) {
	env := groupAdminEnv(t)
	env.act(t, groupTurn(550, "guruh nomini Test qil"), `{"action":"set_title","text":"Test"}`)
	before := len(env.telegram.made())

	if got := env.moderate(undoTurn(551, "salom"), `{"action":"undo"}`); got.OK {
		t.Errorf("undo without being asked = %s", got.Result)
	}
	env.chat.set(userID, admin(telegram.RightDeleteMessages))
	if got := env.moderate(undoTurn(552, "orqaga qaytar"), `{"action":"undo"}`); got.OK || !strings.Contains(got.Error.Message, "change the group's info") {
		t.Errorf("undo by an admin without the right = %+v", got)
	}
	if calls := env.telegram.made(); len(calls) != before {
		t.Errorf("Telegram was asked: %q", calls[before:])
	}
	var undone int
	for _, a := range env.audit.recorded() {
		if a.Action == actionUndo {
			undone++
		}
		if a.Action == actionSetTitle {
			var state undoState
			if json.Unmarshal(a.Undo, &state) != nil || state.Title == nil || state.After == nil || *state.After != "Test" {
				t.Errorf("the rename's undo = %s", a.Undo)
			}
		}
	}
	if undone != 0 {
		t.Errorf("%d undo records, want none", undone)
	}
}

// A restriction that has ended by the time of the undo is put back as
// none: Telegram would take its end, now past, for forever.
func TestUndoDoesNotMakeAnEndedRestrictionForever(t *testing.T) {
	env := groupAdminEnv(t)
	env.chat.set(memberID, restrictedMember(telegram.NoStickers(), rulesNow.Add(-time.Minute)))
	ctx := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: 560, MessageID: 560, Target: bekTarget(), Words: "rasm tashamasin"}, &Sink{})
	env.act(t, ctx, `{"action":"no_photos"}`)
	env.act(t, undoTurn(561, "orqaga qaytar"), `{"action":"undo"}`)
	if r := env.telegram.restricted(); len(r) != 2 || r[1].Permissions != telegram.AllPermissions() || !r[1].Until.IsZero() {
		t.Errorf("restrictions = %+v, want the member left unrestricted", r)
	}
}
