package storage

import (
	"context"
	"errors"
	"testing"
)

// A setting is kept, changed and removed; another group's stays apart.
func TestGroupSettings(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SetGroupSetting(ctx, -100, SettingLogChat, "-200", 1); err != nil {
		t.Fatal(err)
	}
	if v, _ := store.GroupSetting(ctx, -100, SettingLogChat); v != "-200" {
		t.Errorf("log chat = %q", v)
	}
	_ = store.SetGroupSetting(ctx, -100, SettingLogChat, "-300", 1)
	if v, _ := store.GroupSetting(ctx, -100, SettingLogChat); v != "-300" {
		t.Errorf("changed = %q", v)
	}
	_ = store.SetGroupSetting(ctx, -100, SettingLogChat, "", 1)
	if v, _ := store.GroupSetting(ctx, -100, SettingLogChat); v != "" {
		t.Errorf("removed = %q", v)
	}
}

// Thanks count once an hour per pair; the table and a place follow.
func TestReputation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const chat, a, b, c = int64(-1001392088384), int64(8292769065), int64(2), int64(3)
	if n, err := store.GiveRep(ctx, chat, a, b); err != nil || n != 1 {
		t.Fatalf("first = %d, %v", n, err)
	}
	if _, err := store.GiveRep(ctx, chat, a, b); !errors.Is(err, ErrRepTooSoon) {
		t.Errorf("again at once = %v", err)
	}
	if n, _ := store.GiveRep(ctx, chat, c, b); n != 2 {
		t.Errorf("from another = %d", n)
	}
	_, _ = store.GiveRep(ctx, chat, b, c)
	if points, place, _ := store.RepOf(ctx, chat, c); points != 1 || place != 2 {
		t.Errorf("c = %d, place %d", points, place)
	}
	if top, _ := store.RepTop(ctx, chat, 10); len(top) != 2 || top[0].UserID != b {
		t.Errorf("top = %+v", top)
	}
	if points, place, _ := store.RepOf(ctx, chat, 99); points != 0 || place != 0 {
		t.Errorf("none = %d %d", points, place)
	}
}

// A note is saved, replaced, listed and deleted.
func TestNotes(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_ = store.SaveNote(ctx, GroupNote{ChatID: -100, Name: "qoidalar", Text: "reklama yo'q", SavedBy: 1})
	_ = store.SaveNote(ctx, GroupNote{ChatID: -100, Name: "qoidalar", Text: "reklama taqiq", MediaKind: "photo", FileID: "F1", SavedBy: 1})
	_ = store.SaveNote(ctx, GroupNote{ChatID: -100, Name: "donat", Text: "karta", SavedBy: 1})
	n, found, err := store.Note(ctx, -100, "qoidalar")
	if err != nil || !found || n.Text != "reklama taqiq" || n.FileID != "F1" {
		t.Errorf("note = %+v %v %v", n, found, err)
	}
	if names, _ := store.NoteNames(ctx, -100); len(names) != 2 || names[0] != "donat" {
		t.Errorf("names = %v", names)
	}
	if ok, _ := store.DeleteNote(ctx, -100, "donat"); !ok {
		t.Error("not deleted")
	}
	if _, found, _ := store.Note(ctx, -100, "donat"); found {
		t.Error("still found")
	}
}
