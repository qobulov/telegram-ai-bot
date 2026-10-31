package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// fakeStrikes keeps the swear guard's actions in memory, as the store does:
// an update records one action once.
type fakeStrikes struct {
	mu      sync.Mutex
	actions []storage.ModerationAction
}

func (f *fakeStrikes) SwearStrikes(_ context.Context, chatID, userID int64, action string, _ time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, a := range f.actions {
		if a.ChatID == chatID && a.TargetUserID == userID && a.Action == action {
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

// fakeJudge answers whom a message is aimed at with answer, and counts the
// questions.
type fakeJudge struct {
	mu     sync.Mutex
	answer string
	err    error
	asked  []string
}

func (f *fakeJudge) Complete(_ context.Context, req ai.Request) (ai.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, req.Messages[0].Content)
	return ai.Message{Role: ai.RoleAssistant, Content: f.answer}, f.err
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
	f.opts.Judge = &fakeJudge{answer: "PERSON"}
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

// Owners and the creator are never warned, a group without the guard is
// left alone, and neither is ordinary talk.
func TestSwearGuardLeavesAlone(t *testing.T) {
	owner := groupMessage("dnx")
	fromOwner(owner)
	for name, c := range map[string]struct {
		update telegram.Update
		off    bool
	}{
		"the creator":    {update: swearFrom(creatorID, "dnx")},
		"an owner":       {update: inGroup(owner)},
		"no swearing":    {update: swearFrom(swearerID, "kecha yomg'ir yog'di, mushuk ham ho'l bo'ldi, I am home")},
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
		"dnx":                 true,
		"ДНХ":                 true,
		"Иди нахуй":           true,
		"бля, yana yutqazdik": true,
		"qoʻtogʻimni ye":      true,
		"jalablar":            true,
		"пиздец":              true,
		"Gandonlar":           true,
		"Pawolnaxuy":          true,
		"Pawolnaххuy":         true,
		"pasholnaxuiya":       true,
		"o'zing jala":         true,
		"kot":                 true,
		"I am here":           false,
		"Уйга заебал":         true,
		"Кайси долбаеб оберади?":        true,
		"Нихуя себе":                    true,
		"Пздц":                          true,
		"Idinaxuy":                      true,
		"Выебоны":                       true,
		"Zaybal buldim anime kurib":     true,
		"Pulm bolsa auditoriya nax":     true,
		"Pashole bettan":                true,
		"самый уебищный бот":            true,
		"mol bozori":                    false,
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

// The owners asked that the group's admins may swear: the guard neither
// warns nor tags nor restricts them.
func TestSwearGuardLeavesAdminsAlone(t *testing.T) {
	f, restrictor := guardFixture("@Bakxtyorvch")

	sent := f.run(swearFrom(guardAdminID, "dnx"), swearFrom(guardAdminID, "jala"))

	for _, m := range sent {
		if strings.Contains(m.text, "so'kin") || strings.Contains(m.text, "Bakxtyorvch") {
			t.Errorf("an admin's swear got %q", m.text)
		}
	}
	if done := restrictor.recorded(); len(done) != 0 {
		t.Errorf("an admin was restricted: %+v", done)
	}
}

// A word that only looks like swearing is left alone, and so is a message
// the model cannot judge: the model answers it if it was asked.
func TestSwearGuardLeavesWhatIsNoSwearing(t *testing.T) {
	for name, judge := range map[string]*fakeJudge{
		"no swearing":       {answer: "NONE"},
		"the model fails":   {err: errors.New("ai: 503")},
		"an unclear answer": {answer: "maybe"},
	} {
		t.Run(name, func(t *testing.T) {
			f, restrictor := guardFixture("")
			f.opts.Judge = judge
			sent := f.run(swearFrom(swearerID, "Mira, давай хуярить, ps 5 olish kerak"), swearFrom(swearerID, "Mira, давай хуярить"))
			if len(restrictor.recorded()) != 0 || len(f.replier.recorded()) != 2 {
				t.Errorf("restrictions %+v, model calls %d; want none and 2", restrictor.recorded(), len(f.replier.recorded()))
			}
			for _, m := range sent {
				if strings.Contains(m.text, "so'kinish mumkin emas") {
					t.Errorf("warned: %q", m.text)
				}
			}
		})
	}
}

// Swearing as an exclamation, aimed at nobody, gets the day's one warning
// and never a mute: the admin the guard calls unmuted every "bla".
func TestSwearGuardWarnsAnExclamationOnceAndNeverMutes(t *testing.T) {
	f, restrictor := guardFixture("")
	f.opts.Judge = &fakeJudge{answer: "EXCLAIM"}
	sent := f.run(swearFrom(swearerID, "бля, yutqazdik"), swearFrom(swearerID, "pzds"), swearFrom(swearerID, "bla"))
	if len(sent) != 1 || !strings.Contains(sent[0].text, "so'kinmaylik") || len(restrictor.recorded()) != 0 {
		t.Errorf("sent %+v, restrictions %+v; want one warning and no mute", sent, restrictor.recorded())
	}
}

// An exclamation does not count towards the mute for insults.
func TestSwearGuardKeepsExclamationsApartFromInsults(t *testing.T) {
	f, restrictor := guardFixture("")
	judge := &fakeJudge{answer: "EXCLAIM"}
	f.opts.Judge = judge
	f.run(swearFrom(swearerID, "бля"))
	judge.answer = "PERSON"
	sent := f.run(swearFrom(swearerID, "sen jalab"))
	if last := sent[len(sent)-1]; len(restrictor.recorded()) != 0 || !strings.Contains(last.text, "1 soat yozolmaysiz") {
		t.Errorf("sent %+v, restrictions %+v; want the first insult's warning", sent, restrictor.recorded())
	}
}

// The owners asked: a swear at Maria gets no warning, and the second one a
// 15-minute mute; when she cannot mute, she stays quiet.
func TestSwearGuardMutesTheSecondSwearAtTheBot(t *testing.T) {
	f, restrictor := guardFixture("@Bakxtyorvch")
	f.opts.Judge = &fakeJudge{answer: "BOT"}
	start := time.Now()

	sent := f.run(swearFrom(swearerID, "Mira dnx"), swearFrom(swearerID, "Mira jalab"))

	if calls := f.replier.recorded(); len(calls) != 1 {
		t.Errorf("model calls = %d, want the first swear answered by the model and the second not", len(calls))
	}
	for _, m := range sent {
		if strings.Contains(m.text, "ogohlantirish") {
			t.Errorf("warned: %q", m.text)
		}
	}
	want := "@ali_s menga so'kingani uchun 15 daqiqa yozolmaydi. @Bakxtyorvch, xabaringiz bo'lsin."
	if !slices.ContainsFunc(sent, func(m sentMessage) bool { return m.text == want }) {
		t.Errorf("sent = %+v, want the mute notice %q", sent, want)
	}
	done := restrictor.recorded()
	if len(done) != 1 || done[0].UserID != swearerID {
		t.Fatalf("restrictions = %+v, want the swearer muted", done)
	}
	if d := done[0].Until.Sub(start); d < 14*time.Minute || d > 16*time.Minute {
		t.Errorf("mute lasts %v, want 15 minutes", d)
	}
}

func TestSwearGuardStaysQuietWhenItCannotMuteForTheBot(t *testing.T) {
	for name, c := range map[string]struct {
		swearer int64
		failing bool
	}{"an admin": {guardAdminID, false}, "the mute fails": {swearerID, true}} {
		t.Run(name, func(t *testing.T) {
			f, restrictor := guardFixture("")
			f.opts.Judge = &fakeJudge{answer: "BOT"}
			if c.failing {
				restrictor.err = errors.New("not enough rights")
			}
			f.run(swearFrom(c.swearer, "Maria dnx"))
			before := len(f.sender.messages())
			sent := f.run(swearFrom(c.swearer, "Maria dnx"))
			if len(sent) != before {
				t.Errorf("sent %+v after the second swear, want silence", sent[before:])
			}
		})
	}
}

// Messages are lower-cased before they are looked up, so a word or stem
// with a capital in the lists would never match.
func TestSwearListsAreLowerCase(t *testing.T) {
	for w := range swearWords {
		if w != strings.ToLower(w) {
			t.Errorf("swearWords has %q; write it in lower case", w)
		}
	}
	for _, s := range swearStems {
		if s != strings.ToLower(s) {
			t.Errorf("swearStems has %q; write it in lower case", s)
		}
	}
}

type fakeClipSender struct {
	mu    sync.Mutex
	clips []telegram.Clip
}

func (f *fakeClipSender) SendClip(_ context.Context, clip telegram.Clip) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clips = append(f.clips, clip)
	return int64(700 + len(f.clips)), nil
}

// With clips, a swear at the bot gets one of them as a reply instead of the
// model's answer, and the second swear gets the mute and a clip.
func TestSwearGuardAnswersASwearAtTheBotWithAClip(t *testing.T) {
	f, restrictor := guardFixture("")
	f.opts.Judge = &fakeJudge{answer: "BOT"}
	clips := &fakeClipSender{}
	f.opts.ClipSender = clips

	f.run(swearFrom(swearerID, "Mira dnx"))

	if calls := f.replier.recorded(); len(calls) != 0 {
		t.Errorf("model calls = %d, want the clip instead of an answer", len(calls))
	}
	if len(clips.clips) != 1 || len(clips.clips[0].Data) == 0 || clips.clips[0].ReplyToMessageID == 0 || clips.clips[0].ChatID != groupID {
		t.Fatalf("clips = %+v, want one, replying to the swear", clips.clips)
	}
	if len(restrictor.recorded()) != 0 {
		t.Error("the first swear was muted")
	}
	f.run(swearFrom(swearerID, "Mira jalab"))
	if len(clips.clips) != 2 || len(restrictor.recorded()) != 1 {
		t.Errorf("clips %d, mutes %d; want a second clip with the mute", len(clips.clips), len(restrictor.recorded()))
	}
	if log := strings.Join(f.chatLog.texts(groupID), "\n"); !strings.Contains(log, "<video in answer to the swearing>") {
		t.Errorf("log = %q", log)
	}
}

// Both clips are there, and both go as GIFs.
func TestSwearClips(t *testing.T) {
	if len(swearClips) != 2 || len(blokkaClip) < 1000 || len(aqliniClip) < 1000 || swearClips[0].Round || swearClips[1].Round {
		t.Errorf("clips = %d, sizes %d and %d", len(swearClips), len(blokkaClip), len(aqliniClip))
	}
}

// The judge knows the bot by its character's name, not only by its
// username, so "Mira jalab" can be seen as aimed at the bot.
func TestSwearJudgeKnowsTheBotsName(t *testing.T) {
	f, _ := guardFixture("")
	judge := &fakeJudge{answer: "NONE"}
	f.opts.Judge = judge

	f.run(swearFrom(swearerID, "Mira jalab"))

	if len(judge.asked) != 1 {
		t.Fatalf("judge asked %d times", len(judge.asked))
	}
	want := "The group's bot is called Mira, @" + f.opts.Bot.Username + "."
	if !strings.Contains(judge.asked[0], want) {
		t.Errorf("judge prompt = %q, want %q", judge.asked[0], want)
	}
}

// The guard warns and mutes in the swearer's language: their message's,
// or their app's for a word or two.
func TestSwearGuardSpeaksTheSwearersLanguage(t *testing.T) {
	f, _ := guardFixture("")
	sent := f.run(swearFrom(swearerID, "блять опять проиграли сегодня"), swearFrom(swearerID, "да пошли вы нахуй все"))
	if len(sent) != 2 || !strings.HasPrefix(sent[0].text, "@ali_s, в группе нельзя материться") {
		t.Fatalf("sent = %+v, want a Russian warning", sent)
	}
	if want := "@ali_s не сможет писать час за мат. @boss, к вашему сведению."; sent[1].text != want {
		t.Errorf("mute notice = %q, want %q", sent[1].text, want)
	}

	f, _ = guardFixture("")
	short := swearFrom(swearerID, "dnx")
	short.Message.From.LanguageCode = "en"
	if sent := f.run(short); len(sent) != 1 || !strings.HasPrefix(sent[0].text, "@ali_s, no swearing in the group") {
		t.Errorf("sent = %+v, want an English warning", sent)
	}
}
