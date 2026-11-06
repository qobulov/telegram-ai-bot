package service

import (
	"context"
	"errors"
	"testing"

	"telegram-ai-bot/internal/telegram"
)

// fakeBoosts keeps the boosts noted, as the store's table does.
type fakeBoosts struct {
	seen map[boostKey]bool
	err  error
}

type boostKey struct {
	chatID int64
	id     string
}

func (f *fakeBoosts) NoteBoost(_ context.Context, chatID int64, boostID string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	key := boostKey{chatID, boostID}
	if f.seen[key] {
		return false, nil
	}
	f.seen[key] = true
	return true, nil
}

func boostUpdate(chatType, boostID string, booster *telegram.User) telegram.Update {
	nextUpdateID++
	return telegram.Update{UpdateID: nextUpdateID, ChatBoost: &telegram.ChatBoostUpdated{
		Chat:  telegram.Chat{ID: groupID, Type: chatType},
		Boost: telegram.ChatBoost{BoostID: boostID, Source: telegram.ChatBoostSource{Source: "premium", User: booster}},
	}}
}

// A boost is thanked for once, in the group, by the booster's name in their
// app's language, never as a reply and never by the model; a boost sent
// again is not, nor one in a channel.
func TestBoostIsThankedOnce(t *testing.T) {
	f := newFixture()
	f.opts.Boosts = &fakeBoosts{seen: map[boostKey]bool{}}
	ali := &telegram.User{ID: 42, FirstName: "Ali"}
	ivan := &telegram.User{ID: 43, FirstName: "Иван", LanguageCode: "ru"}

	sent := f.run(boostUpdate(telegram.ChatTypeSupergroup, "b1", ali), boostUpdate(telegram.ChatTypeSupergroup, "b1", ali),
		boostUpdate(telegram.ChatTypeSupergroup, "b2", ivan), boostUpdate(telegram.ChatTypeSupergroup, "b3", nil),
		boostUpdate(telegram.ChatTypeChannel, "b4", ali))

	want := []string{"Rahmat, Ali, guruhga boost berganingiz uchun! 🚀", "Спасибо, Иван, за буст группы! 🚀", "Guruhga boost uchun rahmat! 🚀"}
	if len(sent) != len(want) {
		t.Fatalf("sent = %+v, want %q", sent, want)
	}
	for i, w := range want {
		if sent[i].text != w || sent[i].chatID != groupID || sent[i].replyTo != 0 {
			t.Errorf("sent[%d] = %+v, want %q in the group", i, sent[i], w)
		}
	}
	if len(f.replier.recorded()) != 0 {
		t.Error("a boost reached the model")
	}
}

// A muted group, or a boost that cannot be noted, gets no thanks.
func TestBoostThanksStayQuiet(t *testing.T) {
	f := newFixture()
	f.opts.Boosts = &fakeBoosts{seen: map[boostKey]bool{}}
	s := serverless(f, &fakeMuteStore{muted: map[int64]bool{groupID: true}})
	s.HandleNow(boostUpdate(telegram.ChatTypeSupergroup, "b1", &telegram.User{ID: 42, FirstName: "Ali"}))
	s.Close()

	g := newFixture()
	g.opts.Boosts = &fakeBoosts{err: errors.New("database down")}
	sent := g.run(boostUpdate(telegram.ChatTypeSupergroup, "b1", &telegram.User{ID: 42, FirstName: "Ali"}))

	if len(f.sender.messages()) != 0 || len(sent) != 0 {
		t.Errorf("muted: %+v; unnoted: %+v; want no thanks", f.sender.messages(), sent)
	}
}
