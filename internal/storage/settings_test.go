package storage

import (
	"context"
	"maps"
	"testing"
)

// A group's settings and the bot's are kept apart; setting again replaces,
// clearing removes, and the bot's version moves with each change.
func TestSettingsAreKeptByChat(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	before, err := store.GlobalSettingsVersion(ctx)
	if err != nil || before != "none" {
		t.Fatalf("version = %q, %v", before, err)
	}
	for _, s := range []struct {
		chat        int64
		name, value string
	}{{GlobalSettings, "MAX_OUTPUT_TOKENS", "1024"}, {GlobalSettings, "MAX_OUTPUT_TOKENS", "2048"}, {-100, "GROUP_ACTIVE_DAYS", "7"}} {
		if err := store.SetSetting(ctx, s.chat, s.name, s.value, 1); err != nil {
			t.Fatal(err)
		}
	}
	global, _ := store.Settings(ctx, GlobalSettings)
	group, _ := store.Settings(ctx, -100)
	if !maps.Equal(global, map[string]string{"MAX_OUTPUT_TOKENS": "2048"}) || !maps.Equal(group, map[string]string{"GROUP_ACTIVE_DAYS": "7"}) {
		t.Errorf("global = %v, group = %v", global, group)
	}
	set, _ := store.GlobalSettingsVersion(ctx)
	if err := store.ClearSetting(ctx, GlobalSettings, "MAX_OUTPUT_TOKENS"); err != nil {
		t.Fatal(err)
	}
	cleared, _ := store.GlobalSettingsVersion(ctx)
	if set == before || cleared == set {
		t.Errorf("versions %q, %q, %q; want each change to move it", before, set, cleared)
	}
}
