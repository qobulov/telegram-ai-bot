package service

import (
	"context"
	"strings"
	"sync"
	"testing"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// memoryExtras keeps reputation, notes and settings in memory.
type memoryExtras struct {
	mu       sync.Mutex
	settings map[string]string
	rep      map[int64]int
	given    map[[2]int64]bool
	notes    map[string]storage.GroupNote
}

func newMemoryExtras() *memoryExtras {
	return &memoryExtras{settings: map[string]string{}, rep: map[int64]int{}, given: map[[2]int64]bool{}, notes: map[string]storage.GroupNote{}}
}

func (m *memoryExtras) GroupSetting(_ context.Context, _ int64, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings[key], nil
}
func (m *memoryExtras) SetGroupSetting(_ context.Context, _ int64, key, value string, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings[key] = value
	return nil
}
func (m *memoryExtras) GiveRep(_ context.Context, _, from, to int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.given[[2]int64{from, to}] {
		return 0, storage.ErrRepTooSoon
	}
	m.given[[2]int64{from, to}] = true
	m.rep[to]++
	return m.rep[to], nil
}
func (m *memoryExtras) RepOf(_ context.Context, _, userID int64) (int, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rep[userID] == 0 {
		return 0, 0, nil
	}
	return m.rep[userID], 1, nil
}
func (m *memoryExtras) RepTop(context.Context, int64, int) ([]storage.MemberRep, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var top []storage.MemberRep
	for id, p := range m.rep {
		top = append(top, storage.MemberRep{UserID: id, Name: "Sam", Points: p})
	}
	return top, nil
}
func (m *memoryExtras) SaveNote(_ context.Context, n storage.GroupNote) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notes[n.Name] = n
	return nil
}
func (m *memoryExtras) Note(_ context.Context, _ int64, name string) (storage.GroupNote, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.notes[name]
	return n, ok, nil
}
func (m *memoryExtras) NoteNames(context.Context, int64) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var names []string
	for n := range m.notes {
		names = append(names, n)
	}
	return names, nil
}
func (m *memoryExtras) DeleteNote(_ context.Context, _ int64, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.notes[name]
	delete(m.notes, name)
	return ok, nil
}

type fakeCopier struct{ copied []int64 }

func (f *fakeCopier) CopyMessage(_ context.Context, _, _, messageID int64, _ string) (int64, error) {
	f.copied = append(f.copied, messageID)
	return 1, nil
}

// "+" or "rahmat" to a member gives them a point once, marked with a
// reaction and unanswered; /rep and /toprep show it; a "+" to oneself or
// to the bot counts nothing.
func TestReputationByThanks(t *testing.T) {
	f := newFixture()
	extras, reactor := newMemoryExtras(), &fakeReactor{}
	f.opts.Extras, f.opts.Reactor = extras, reactor
	sent := f.run(from(20, "+", 30), from(20, "rahmat", 30), from(21, "Rahmat", 30), from(30, "+", 30))
	if extras.rep[30] != 2 || len(reactor.recorded()) != 2 || len(sent) != 0 {
		t.Fatalf("rep = %v, reactions %d, sent %+v", extras.rep, len(reactor.recorded()), sent)
	}
	sent = f.run(from(20, "/rep", 30))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "2 reputatsiya") {
		t.Errorf("/rep = %q", last)
	}
	sent = f.run(from(20, "/toprep", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "Sam — 2") {
		t.Errorf("/toprep = %q", last)
	}
}

// A moderator saves an answer, anyone calls it with "#name" or "!name", a
// member cannot save, and a reply to a picture is copied back.
func TestNotes(t *testing.T) {
	f := newFixture()
	extras, copier := newMemoryExtras(), &fakeCopier{}
	f.opts.Extras, f.opts.Copier = extras, copier
	f.opts.Ranks = &fakeRankBook{ranks: map[int64]int{40: 2}, overrides: map[string]int{}}
	sent := f.run(from(21, "/save qoidalar Reklama yo'q", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "⛔") {
		t.Errorf("a member's /save = %q", last)
	}
	f.run(from(40, "/save qoidalar Reklama yo'q", 0))
	sent = f.run(from(21, "#qoidalar", 0), from(22, "!Qoidalar", 0))
	if len(sent) < 2 || sent[len(sent)-1].text != "Reklama yo'q" || sent[len(sent)-2].text != "Reklama yo'q" {
		t.Fatalf("called = %+v", sent)
	}
	if before := len(sent); len(f.run(from(21, "#nimadir", 0))) != before {
		t.Errorf("an unknown #tag was answered")
	}
	picture := groupMessage("/save meme")
	picture.From = &telegram.User{ID: 40, FirstName: "M"}
	picture.ReplyToMessage = &telegram.Message{MessageID: 77, From: &telegram.User{ID: 30}, Chat: picture.Chat, Caption: "kulgili"}
	f.run(inGroup(picture))
	f.run(from(21, "#meme", 0))
	if len(copier.copied) != 1 || copier.copied[0] != 77 {
		t.Errorf("copied = %v", copier.copied)
	}
	sent = f.run(from(21, "/notes", 0))
	if last := sent[len(sent)-1].text; !strings.Contains(last, "#qoidalar") || !strings.Contains(last, "#meme") {
		t.Errorf("/notes = %q", last)
	}
	f.run(from(40, "/clear qoidalar", 0))
	if _, ok := extras.notes["qoidalar"]; ok {
		t.Error("/clear left the note")
	}
}

// /setlog links a log chat that the bot can write to, and moderation then
// goes there; a join is logged too; /setlog off stops it.
func TestGroupLog(t *testing.T) {
	f := newFixture()
	extras := newMemoryExtras()
	f.opts.Extras = extras
	f.members.creator = 10
	var lines []string
	f.opts.Journal = func(_ context.Context, _ int64, line string) { lines = append(lines, line) }
	sent := f.run(from(10, "/setlog -1009999", 0))
	if extras.settings[storage.SettingLogChat] != "-1009999" || !strings.Contains(sent[len(sent)-1].text, "ulandi") {
		t.Fatalf("setlog = %v, %+v", extras.settings, sent)
	}
	if !strings.Contains(sent[len(sent)-2].text, "jurnali bo'ldi") || sent[len(sent)-2].chatID != -1009999 {
		t.Errorf("no greeting in the log chat: %+v", sent)
	}
	join := groupMessage("")
	join.NewChatMembers = []telegram.User{{ID: 55, FirstName: "Yangi"}}
	f.run(inGroup(join))
	if len(lines) == 0 || !strings.Contains(lines[len(lines)-1], "Kirdi: Yangi") {
		t.Errorf("join lines = %v", lines)
	}
	sent = f.run(from(21, "/setlog -1008888", 0))
	if !strings.Contains(sent[len(sent)-1].text, "⛔") {
		t.Errorf("a member's /setlog = %q", sent[len(sent)-1].text)
	}
	f.run(from(10, "/setlog off", 0))
	if extras.settings[storage.SettingLogChat] != "" {
		t.Errorf("off left %v", extras.settings)
	}
}

// With /cleanservice on, a join message is deleted.
func TestCleanService(t *testing.T) {
	f := newFixture()
	extras, moderator := newMemoryExtras(), &fakeModerator{}
	f.opts.Extras, f.opts.GroupModerator = extras, moderator
	f.members.creator = 10
	f.run(from(10, "/cleanservice on", 0))
	join := groupMessage("")
	join.MessageID = 88
	join.NewChatMembers = []telegram.User{{ID: 55, FirstName: "Yangi"}}
	f.run(inGroup(join))
	if len(moderator.deleted) != 1 || moderator.deleted[0] != 88 {
		t.Errorf("deleted = %v", moderator.deleted)
	}
	f.run(from(10, "/cleanservice off", 0))
	join.MessageID = 89
	f.run(inGroup(join))
	if len(moderator.deleted) != 1 {
		t.Errorf("deleted after off = %v", moderator.deleted)
	}
}

type journalStore struct{ logChat string }

func (j journalStore) GroupSetting(context.Context, int64, string) (string, error) {
	return j.logChat, nil
}
func (journalStore) GroupTitle(context.Context, int64) (string, error) { return "BS news", nil }

// The journal writes to the log chat under the group's title, and to
// nowhere when the group has none.
func TestGroupJournal(t *testing.T) {
	sender := &fakeSender{}
	GroupJournal(journalStore{logChat: "-1009999"}, sender, discardLogger())(context.Background(), groupID, "🛡 Aziz: Bajarildi")
	if len(sender.sent) != 1 || sender.sent[0].chatID != -1009999 || sender.sent[0].text != "📋 BS news\n🛡 Aziz: Bajarildi" {
		t.Errorf("sent = %+v", sender.sent)
	}
	GroupJournal(journalStore{}, sender, discardLogger())(context.Background(), groupID, "x")
	if len(sender.sent) != 1 {
		t.Errorf("wrote without a log chat: %+v", sender.sent)
	}
}

// Thanks are read with fillers, punctuation and emoji; talk is not.
func TestIsThanks(t *testing.T) {
	for text, want := range map[string]bool{
		"raxmat": true, "Рахмат!": true, "спс": true, "rahmat aka": true, "katta rahmat 🙏": true, "спасибо бро": true, "+": true, "++": true,
		"rahmat lekin bu noto'g'ri": false, "salom": false, "": false, "+ va -": false,
	} {
		if got := isThanks(text); got != want {
			t.Errorf("isThanks(%q) = %t, want %t", text, got, want)
		}
	}
}

// /cleanservice alone switches the setting, on then off.
func TestCleanServiceToggles(t *testing.T) {
	f := newFixture()
	extras := newMemoryExtras()
	f.opts.Extras = extras
	f.members.creator = 10
	sent := f.run(from(10, "/cleanservice", 0))
	if extras.settings[storage.SettingCleanService] != "on" || !strings.Contains(sent[len(sent)-1].text, "Yoqildi") {
		t.Fatalf("first = %v, %q", extras.settings, sent[len(sent)-1].text)
	}
	sent = f.run(from(10, "/cleanservice", 0))
	if extras.settings[storage.SettingCleanService] != "" || !strings.Contains(sent[len(sent)-1].text, "O'chirildi") {
		t.Errorf("second = %v, %q", extras.settings, sent[len(sent)-1].text)
	}
}
