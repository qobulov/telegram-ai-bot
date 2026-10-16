package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func mustSetRule(t *testing.T, store *Store, r GroupRule) int64 {
	t.Helper()
	id, err := store.SetGroupRule(context.Background(), r)
	if err != nil {
		t.Fatalf("SetGroupRule(%+v): %v", r, err)
	}
	return id
}

func mustRules(t *testing.T, store *Store, chatID int64) []GroupRule {
	t.Helper()
	rules, err := store.GroupRules(context.Background(), chatID)
	if err != nil {
		t.Fatalf("GroupRules: %v", err)
	}
	return rules
}

func instruction(chatID int64, text string) GroupRule {
	return GroupRule{ChatID: chatID, Kind: RuleInstruction, Text: text, SetBy: owner, ExpiresAt: time.Now().Add(time.Hour)}
}

func TestGroupRulesLifecycle(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	noFinance := mustSetRule(t, store, GroupRule{ChatID: group, Kind: RuleInstruction, Text: "No finance talk.", SetBy: owner})
	address := mustSetRule(t, store, GroupRule{ChatID: 0, Kind: RuleAddress, UserID: owner, Text: "aka", SetBy: owner})
	firstIgnore := mustSetRule(t, store, GroupRule{ChatID: group, Kind: RuleIgnore, UserID: bob, UserName: "Bob", SetBy: owner, ExpiresAt: time.Now().Add(time.Hour)})
	until := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	ignore := mustSetRule(t, store, GroupRule{ChatID: group, Kind: RuleIgnore, UserID: bob, UserName: "Bob @bob", SetBy: owner, ExpiresAt: until})
	mustSetRule(t, store, GroupRule{ChatID: group, Kind: RuleInstruction, Text: "Gone already.", SetBy: owner, ExpiresAt: time.Now().Add(-time.Minute)})
	mustSetRule(t, store, instruction(otherGroup, "Elsewhere."))

	rules := mustRules(t, store, group)
	if len(rules) != 3 {
		t.Fatalf("rules = %+v, want the instruction, the address and one ignore", rules)
	}
	if r := rules[0]; r.ID != noFinance || r.ChatID != group || r.Kind != RuleInstruction || r.Text != "No finance talk." ||
		r.SetBy != owner || r.UserID != 0 || !r.ExpiresAt.IsZero() || r.CreatedAt.IsZero() {
		t.Errorf("instruction = %+v", r)
	}
	if r := rules[1]; r.ID != address || r.ChatID != 0 || r.Kind != RuleAddress || r.UserID != owner || r.Text != "aka" {
		t.Errorf("global address = %+v, want it listed in the group", r)
	}
	if r := rules[2]; r.ID != ignore || r.ID == firstIgnore || r.UserID != bob || r.UserName != "Bob @bob" || !r.ExpiresAt.Equal(until) {
		t.Errorf("ignore = %+v, want the second one to have replaced the first", r)
	}

	// Another chat's rule cannot be removed from here, and stays.
	other := mustRules(t, store, otherGroup)
	if removed, err := store.RemoveGroupRule(ctx, group, other[len(other)-1].ID); err != nil || removed {
		t.Errorf("removing another chat's rule = %v, %v; want false", removed, err)
	}
	if got := mustRules(t, store, otherGroup); len(got) != 2 {
		t.Errorf("rules elsewhere = %+v, want its instruction and the global address", got)
	}
	if removed, err := store.RemoveGroupRule(ctx, group, noFinance); err != nil || !removed {
		t.Errorf("RemoveGroupRule = %v, %v; want true", removed, err)
	}

	if removed, err := store.RemoveIgnore(ctx, group, alice); err != nil || removed {
		t.Errorf("RemoveIgnore of someone not ignored = %v, %v; want false", removed, err)
	}
	if removed, err := store.RemoveIgnore(ctx, group, bob); err != nil || !removed {
		t.Errorf("RemoveIgnore = %v, %v; want true", removed, err)
	}
	if rules := mustRules(t, store, group); len(rules) != 1 || rules[0].ID != address {
		t.Errorf("rules left = %+v, want only the address", rules)
	}
}

func TestAddressIsReplacedPerOwner(t *testing.T) {
	store := newTestStore(t)
	mustSetRule(t, store, GroupRule{Kind: RuleAddress, UserID: owner, Text: "master", SetBy: owner})
	mustSetRule(t, store, GroupRule{Kind: RuleAddress, UserID: alice, Text: "boss", SetBy: alice})
	mustSetRule(t, store, GroupRule{Kind: RuleAddress, UserID: owner, Text: "aka", SetBy: owner})

	rules := mustRules(t, store, owner) // a private chat: only the global rules
	if len(rules) != 2 || rules[0].Text != "boss" || rules[1].Text != "aka" {
		t.Errorf("addresses = %+v, want one per owner, the newest", rules)
	}
}

func TestInstructionsAreLimitedPerChat(t *testing.T) {
	store := newTestStore(t)
	// Expired instructions do not count.
	mustSetRule(t, store, GroupRule{ChatID: group, Kind: RuleInstruction, Text: "old", SetBy: owner, ExpiresAt: time.Now().Add(-time.Minute)})
	for i := range MaxInstructions {
		mustSetRule(t, store, instruction(group, "rule "+string(rune('a'+i))))
	}

	_, err := store.SetGroupRule(context.Background(), instruction(group, "one too many"))
	if !errors.Is(err, ErrTooManyRules) {
		t.Errorf("instruction %d: err = %v, want ErrTooManyRules", MaxInstructions+1, err)
	}
	mustSetRule(t, store, instruction(otherGroup, "another chat has its own"))
	mustSetRule(t, store, GroupRule{ChatID: group, Kind: RuleIgnore, UserID: bob, UserName: "Bob", SetBy: owner})
	if rules := mustRules(t, store, group); len(rules) != MaxInstructions+1 {
		t.Errorf("rules = %d, want %d instructions and the ignore", len(rules), MaxInstructions)
	}
}

func TestExpireRemovesExpiredRules(t *testing.T) {
	store := newTestStore(t)
	mustSetRule(t, store, GroupRule{ChatID: group, Kind: RuleInstruction, Text: "gone", SetBy: owner, ExpiresAt: time.Now().Add(-time.Minute)})
	mustSetRule(t, store, instruction(group, "still on"))
	mustSetRule(t, store, GroupRule{ChatID: group, Kind: RuleInstruction, Text: "forever", SetBy: owner})

	if _, err := store.Expire(context.Background(), 24*time.Hour); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if rows := countRows(t, store, "group_rules"); rows != 2 {
		t.Errorf("group_rules rows = %d, want the two still in force", rows)
	}
}
