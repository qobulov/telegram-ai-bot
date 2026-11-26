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
	if err := store.SetGroupSetting(ctx, -100, SettingCleanService, "on", 1); err != nil {
		t.Fatal(err)
	}
	if v, _ := store.GroupSetting(ctx, -100, SettingCleanService); v != "on" {
		t.Errorf("setting = %q", v)
	}
	_ = store.SetGroupSetting(ctx, -100, SettingCleanService, "yes", 1)
	if v, _ := store.GroupSetting(ctx, -100, SettingCleanService); v != "yes" {
		t.Errorf("changed = %q", v)
	}
	_ = store.SetGroupSetting(ctx, -100, SettingCleanService, "", 1)
	if v, _ := store.GroupSetting(ctx, -100, SettingCleanService); v != "" {
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
	if top, _ := store.RepTop(ctx, chat, 10); len(top) != 2 || top[0].UserID != b {
		t.Errorf("top = %+v", top)
	}
}
