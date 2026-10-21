package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// fakeStrikes keeps the swear guard's actions in memory, as the store does:
// an update records one action once.
type fakeStrikes struct {
	mu      sync.Mutex
	actions []storage.ModerationAction
}

func (f *fakeStrikes) SwearStrikes(_ context.Context, chatID, userID int64, _ time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, a := range f.actions {
		if a.ChatID == chatID && a.TargetUserID == userID {
			n++
		}
	}
	return n, nil
}

func (f *fakeStrikes) RecordModeration(_ context.Context, a storage.ModerationAction) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, old := range f.actions {
		if old.UpdateID == a.UpdateID && old.Action == a.Action {
			return false, nil
		}
	}
	f.actions = append(f.actions, a)
	return true, nil
}

type fakeAdminList struct{ admins []telegram.ChatMember }

func (f fakeAdminList) GetChatAdministrators(context.Context, int64) ([]telegram.ChatMember, error) {
	return f.admins, nil
}

type fakeRestrictor struct {
	mu   sync.Mutex
	done []telegram.Restriction
	err  error
}

func (f *fakeRestrictor) RestrictChatMember(_ context.Context, r telegram.Restriction) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.done = append(f.done, r)
	return f.err
}

func (f *fakeRestrictor) recorded() []telegram.Restriction {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]telegram.Restriction(nil), f.done...)
}

const (
	creatorID    = int64(7007)
	guardAdminID = int64(7008)
	swearerID    = int64(4343)
)

// guardFixture is a group with the swear guard on, whose creator is @boss.
func guardFixture(tag string) (*fixture, *fakeRestrictor) {
	f := newFixture()
	restrictor := &fakeRestrictor{}
	f.opts.OwnerUsernames = []string{ownerUsername}
	f.opts.Rules = &fakeRuleBook{rules: []storage.GroupRule{{ID: 1, ChatID: groupID, Kind: storage.RuleSwearGuard, UserName: tag, SetBy: 9001}}}
	f.opts.Strikes = &fakeStrikes{}
	f.opts.Restrictor = restrictor
	f.opts.AdminList = fakeAdminList{admins: []telegram.ChatMember{
		{Status: "creator", User: &telegram.User{ID: creatorID, FirstName: "Boss", Username: "boss"}},
		{Status: "administrator", User: &telegram.User{ID: guardAdminID, FirstName: "Adm"}},
	}}
	return f, restrictor
}

func swearFrom(userID int64, text string) telegram.Update {
	msg := groupMessage(text)
	msg.From = &telegram.User{ID: userID, FirstName: "Ali", Username: "ali_s"}
	return inGroup(msg)
}

func TestSwearGuardWarnsThenMutesAndTagsTheCreator(t *testing.T) {
	f, restrictor := guardFixture("")
	start := time.Now()

	sent := f.run(swearFrom(swearerID, "bugun yana yutqazdik dnx"), swearFrom(swearerID, "o'zing qo'tog'im"))

	if len(sent) != 2 {
		t.Fatalf("sent = %+v, want a warning and a mute notice", sent)
	}
	if !strings.HasPrefix(sent[0].text, "@ali_s, guruhda so'kinish mumkin emas") || sent[0].replyTo == 0 {
		t.Errorf("warning = %+v", sent[0])
	}
	if want := "@ali_s so'kingani uchun 1 soat yozolmaydi. @boss, xabaringiz bo'lsin."; sent[1].text != want {
		t.Errorf("mute notice = %q, want %q", sent[1].text, want)
	}
	done := restrictor.recorded()
	if len(done) != 1 || done[0].UserID != swearerID || done[0].Permissions.CanSendMessages || done[0].Until.Sub(start) < 59*time.Minute {
		t.Errorf("restrictions = %+v, want the swearer muted for an hour", done)
	}
	if len(f.replier.recorded()) != 0 {
		t.Error("the model was asked")
	}
	if got := f.chatLog.texts(groupID); !strings.Contains(strings.Join(got, "\n"), "You: @ali_s so'kingani uchun") {
		t.Errorf("log = %q, want the guard's notices as the bot's lines", got)
	}
}

// The owner may name whom a mute calls instead of the creator; without the
// right to restrict, the notice says so and still calls them.
func TestSwearGuardTagsWhomTheOwnerNamed(t *testing.T) {
	f, restrictor := guardFixture("@Bakxtyorvch")
	restrictor.err = errors.New("not enough rights")

	sent := f.run(swearFrom(swearerID, "Maria dnx"), swearFrom(swearerID, "нахуй"))

	if len(sent) != 2 || !strings.Contains(sent[1].text, "qila olmadim") || !strings.HasSuffix(sent[1].text, "@Bakxtyorvch, qarab qo'ying.") {
		t.Errorf("sent = %+v", sent)
	}
	if len(f.replier.recorded()) != 0 {
		t.Error("a swear at Maria was answered by the model")
	}
}

// Admins, owners and the creator are never warned, a group without the
// guard is left alone, and neither is ordinary talk.
func TestSwearGuardLeavesAlone(t *testing.T) {
	owner := groupMessage("dnx")
	fromOwner(owner)
	for name, c := range map[string]struct {
		update telegram.Update
		off    bool
	}{
		"an admin":       {update: swearFrom(guardAdminID, "dnx")},
		"the creator":    {update: swearFrom(creatorID, "dnx")},
		"an owner":       {update: inGroup(owner)},
		"no swearing":    {update: swearFrom(swearerID, "kecha jala yog'di, kot ham ho'l bo'ldi")},
		"the guard off":  {update: swearFrom(swearerID, "dnx"), off: true},
		"a bot swearing": {update: inGroup(&telegram.Message{MessageID: 31, From: &telegram.User{ID: 555, IsBot: true}, Chat: telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup}, Text: "dnx"})},
	} {
		t.Run(name, func(t *testing.T) {
			f, restrictor := guardFixture("")
			if c.off {
				f.opts.Rules = &fakeRuleBook{}
			}
			if sent := f.run(c.update); len(sent) != 0 || len(restrictor.recorded()) != 0 {
				t.Errorf("sent %+v, restrictions %+v; want nothing", sent, restrictor.recorded())
			}
		})
	}
}

// Telegram may deliver an update twice; it is warned about once.
func TestSwearGuardHandlesAnUpdateOnce(t *testing.T) {
	f, _ := guardFixture("")
	update := swearFrom(swearerID, "dnx")
	if sent := f.run(update, update); len(sent) != 1 {
		t.Errorf("sent = %+v, want one warning", sent)
	}
}

func TestIsSwearing(t *testing.T) {
	for text, want := range map[string]bool{
		"dnx":                           true,
		"ДНХ":                           true,
		"Иди нахуй":                     true,
		"бля, yana yutqazdik":           true,
		"qoʻtogʻimni ye":                true,
		"jalablar":                      true,
		"пиздец":                        true,
		"Gandonlar":                     true,
		"kecha jala yog'di":             false,
		"I am here":                     false,
		"kot ham bor":                   false,
		"xuy-atvori yaxshi odam":        false,
		"Salom, qalaysizlar?":           false,
		"хуже некуда":                   false,
		"сукно и нахальство":            false,
		"Toshkentda ertaga +25 bo'ladi": false,
	} {
		if got := isSwearing(text); got != want {
			t.Errorf("isSwearing(%q) = %v, want %v", text, got, want)
		}
	}
}

// The model is told the guard is on, so that it neither swears back nor
// mutes for swearing itself, nor promises to.
func TestRulesHeaderSaysSwearingIsWatched(t *testing.T) {
	s := New(newFixture().opts)
	defer s.stop()
	lines := s.ruleLines([]storage.GroupRule{{ID: 3, ChatID: groupID, Kind: storage.RuleSwearGuard, UserName: "@Bakxtyorvch"}})
	if len(lines) != 1 || !strings.Contains(lines[0], "watched here in code, not by you") || !strings.Contains(lines[0], "tags @Bakxtyorvch") || !strings.Contains(lines[0], "Never swear back") {
		t.Errorf("lines = %q", lines)
	}
}
