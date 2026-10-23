package service

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// fakeGuards answers as the outside services would, and counts the
// questions.
type fakeGuards struct {
	mu       sync.Mutex
	spammers map[int64]string
	badHosts map[string]string
	unsafe   string
	links    bool
	images   bool
	asked    []int64
	hosts    []string
	pictures int
}

func (f *fakeGuards) Spammer(_ context.Context, userID int64) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, userID)
	source, bad := f.spammers[userID]
	return source, bad, nil
}

func (f *fakeGuards) MaliciousHost(_ context.Context, host string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hosts = append(f.hosts, host)
	reason, bad := f.badHosts[host]
	return reason, bad, nil
}

func (f *fakeGuards) UnsafeImage(context.Context, []byte) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pictures++
	return f.unsafe, f.unsafe != "", nil
}

func (f *fakeGuards) CanCheckLinks() bool  { return f.links }
func (f *fakeGuards) CanCheckImages() bool { return f.images }

func (f *fakeGuards) spamQuestions() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.asked)
}

type fakeModerator struct {
	mu      sync.Mutex
	deleted []int64
	banned  []int64
}

func (f *fakeModerator) DeleteMessage(_ context.Context, _, messageID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, messageID)
	return nil
}

func (f *fakeModerator) BanChatMember(_ context.Context, _, userID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.banned = append(f.banned, userID)
	return nil
}

const spammerID = int64(6606)

// protectFixture is a group with the given guards on, all of whose services
// have keys; spammerID is on CAS.
func protectFixture(kinds ...string) (*fixture, *fakeGuards, *fakeModerator) {
	f, _ := guardFixture("")
	var rules []storage.GroupRule
	for i, kind := range kinds {
		rules = append(rules, storage.GroupRule{ID: int64(i + 1), ChatID: groupID, Kind: kind})
	}
	f.opts.Rules = &fakeRuleBook{rules: rules}
	guards := &fakeGuards{spammers: map[int64]string{spammerID: "CAS"}, badHosts: map[string]string{}, links: true, images: true}
	moderator := &fakeModerator{}
	f.opts.Guards, f.opts.GroupModerator = guards, moderator
	f.opts.Media = newFakeMedia()
	return f, guards, moderator
}

func messageFrom(userID int64, username, text string) *telegram.Message {
	msg := groupMessage(text)
	msg.From = &telegram.User{ID: userID, FirstName: "Sam", Username: username}
	return msg
}

func TestSpamGuardBansAListedMemberAndDeletesTheirMessage(t *testing.T) {
	f, guards, moderator := protectFixture(storage.RuleSpamGuard)

	sent := f.run(inGroup(messageFrom(spammerID, "promo", "Maria, arzon kripto!")))

	if want := "🚫 @promo spamerlar ro'yxatida (CAS), guruhdan chiqarildi."; len(sent) != 1 || sent[0].text != want {
		t.Fatalf("sent = %+v, want %q", sent, want)
	}
	if !slices.Equal(moderator.deleted, []int64{30}) || !slices.Equal(moderator.banned, []int64{spammerID}) {
		t.Errorf("deleted %v, banned %v", moderator.deleted, moderator.banned)
	}
	if len(f.replier.recorded()) != 0 {
		t.Error("the model was asked")
	}
	if got := guards.spamQuestions(); !slices.Equal(got, []int64{spammerID}) {
		t.Errorf("asked about %v", got)
	}
}

func TestSpamGuardAsksAboutAMemberOnceAWhile(t *testing.T) {
	f, guards, moderator := protectFixture(storage.RuleSpamGuard)

	f.run(inGroup(messageFrom(5151, "ok", "salom")), inGroup(messageFrom(5151, "ok", "qalaysizlar")))

	if got := guards.spamQuestions(); !slices.Equal(got, []int64{5151}) {
		t.Errorf("asked about %v, want one question", got)
	}
	if len(moderator.deleted)+len(moderator.banned) != 0 {
		t.Errorf("a clean member was punished: deleted %v, banned %v", moderator.deleted, moderator.banned)
	}
}

func TestSpamGuardBansListedJoinersAndWelcomesTheOthers(t *testing.T) {
	f, _, moderator := protectFixture(storage.RuleSpamGuard)
	book := f.opts.Rules.(*fakeRuleBook)
	book.rules = append(book.rules, storage.GroupRule{ID: 9, ChatID: groupID, Kind: storage.RuleWelcome, Text: "Xush kelibsiz, {name}!"})
	join := groupMessage("")
	join.NewChatMembers = []telegram.User{{ID: spammerID, FirstName: "Promo"}, {ID: 5252, FirstName: "Ann"}}

	sent := f.run(inGroup(join))

	texts := make([]string, len(sent))
	for i, m := range sent {
		texts[i] = m.text
	}
	if len(sent) != 2 || !strings.Contains(texts[0], "Promo spamerlar ro'yxatida (CAS)") || texts[1] != "Xush kelibsiz, Ann!" {
		t.Errorf("sent = %q, want the ban notice and a welcome for Ann alone", texts)
	}
	if !slices.Equal(moderator.banned, []int64{spammerID}) || len(moderator.deleted) != 0 {
		t.Errorf("banned %v, deleted %v, want the spammer banned and the join left", moderator.banned, moderator.deleted)
	}
}

func TestGuardsLeaveAdminsAndOwnersAlone(t *testing.T) {
	f, guards, moderator := protectFixture(storage.RuleSpamGuard)
	guards.spammers[guardAdminID] = "CAS"
	guards.spammers[9001] = "CAS"

	f.run(inGroup(messageFrom(guardAdminID, "adm", "salom")), inGroup(messageFrom(9001, ownerUsername, "salom")))

	if len(moderator.banned)+len(moderator.deleted) != 0 {
		t.Errorf("an admin or owner was punished: deleted %v, banned %v", moderator.deleted, moderator.banned)
	}
	if got := guards.spamQuestions(); slices.Contains(got, 9001) {
		t.Errorf("an owner was looked up: %v", got)
	}
}

func TestGuardsAreOffWithoutTheirRule(t *testing.T) {
	f, guards, moderator := protectFixture()
	msg := messageFrom(spammerID, "promo", "evil.example")
	msg.Entities = []telegram.MessageEntity{entityAt("url", msg.Text, "evil.example")}

	f.run(inGroup(msg))

	if len(guards.spamQuestions())+len(guards.hosts) != 0 || len(moderator.banned)+len(moderator.deleted) != 0 {
		t.Errorf("a guard acted though none is on: asked %v %v, deleted %v", guards.asked, guards.hosts, moderator.deleted)
	}
}

func TestLinkGuardDeletesLinksToMaliciousHosts(t *testing.T) {
	f, guards, moderator := protectFixture(storage.RuleLinkGuard)
	guards.badHosts["evil.example"] = "malware_download"
	text := "ko'ring: https://Evil.example/app.apk va bu"
	bad := messageFrom(5353, "lin", text)
	bad.Entities = []telegram.MessageEntity{entityAt("url", text, "https://Evil.example/app.apk"), entityAt("text_link", text, "bu")}
	bad.Entities[1].URL = "https://good.example/"
	good := messageFrom(5353, "lin", "bu yerda")
	good.MessageID = 31
	good.Entities = []telegram.MessageEntity{entityAt("text_link", good.Text, "yerda")}
	good.Entities[0].URL = "https://good.example/page"

	sent := f.run(inGroup(bad), inGroup(good))

	if len(sent) != 1 || !strings.Contains(sent[0].text, "xavfli havola o'chirildi (evil.example, malware_download)") {
		t.Fatalf("sent = %+v", sent)
	}
	if !slices.Equal(moderator.deleted, []int64{30}) {
		t.Errorf("deleted %v, want only the bad link's message", moderator.deleted)
	}
	if !slices.Equal(guards.hosts, []string{"evil.example", "good.example"}) {
		t.Errorf("hosts asked = %v, want each once", guards.hosts)
	}
}

func TestLinkGuardNeedsItsKey(t *testing.T) {
	f, guards, moderator := protectFixture(storage.RuleLinkGuard)
	guards.links = false
	guards.badHosts["evil.example"] = "malware_download"
	msg := messageFrom(5353, "lin", "evil.example")
	msg.Entities = []telegram.MessageEntity{entityAt("url", msg.Text, "evil.example")}

	f.run(inGroup(msg))

	if len(guards.hosts) != 0 || len(moderator.deleted) != 0 {
		t.Errorf("the link guard ran without its key: %v %v", guards.hosts, moderator.deleted)
	}
}

func TestImageGuardDeletesUnsafePicturesAtTheSizeItLooksAt(t *testing.T) {
	f, guards, moderator := protectFixture(storage.RuleImageGuard)
	guards.unsafe = "nudity"
	media := f.opts.Media.(*fakeMedia)
	msg := messageFrom(5454, "pic", "")
	msg.Photo = []telegram.PhotoSize{{FileID: "photo-small", FileSize: 40 << 10}, {FileID: "photo-big", FileSize: 3 << 20}}

	sent := f.run(inGroup(msg))

	if len(sent) != 1 || !strings.Contains(sent[0].text, "nomaqbul rasm o'chirildi (nudity)") {
		t.Fatalf("sent = %+v", sent)
	}
	if !slices.Equal(moderator.deleted, []int64{30}) || guards.pictures != 1 {
		t.Errorf("deleted %v after %d checks", moderator.deleted, guards.pictures)
	}
	if !slices.Equal(media.downloads, []string{"photo-small"}) {
		t.Errorf("downloads = %v, want the size under the limit", media.downloads)
	}
}

func TestGuardRulesAreToldToTheModel(t *testing.T) {
	f, _ := guardFixture("")
	s := New(f.opts)
	defer s.Close()
	lines := s.ruleLines([]storage.GroupRule{{ChatID: groupID, Kind: storage.RuleSpamGuard}, {ChatID: groupID, Kind: storage.RuleImageGuard}})
	if len(lines) != 2 || !strings.Contains(lines[0], "spammers") || !strings.Contains(lines[1], "pictures") {
		t.Errorf("lines = %q", lines)
	}
}
