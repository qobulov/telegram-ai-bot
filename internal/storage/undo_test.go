package storage

import (
	"context"
	"encoding/json"
	"testing"
)

// The latest action of a chat that can be undone is found, marked once,
// and then the one before it; deletions and other chats are never found,
// and an invite link's undo is added once the link exists.
func TestModerationUndoIsFoundLatestFirstAndMarkedOnce(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	record := func(a ModerationAction) {
		t.Helper()
		if _, err := store.RecordModeration(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	record(ModerationAction{ChatID: -1, Action: "set_title", RequesterID: 7, UpdateID: 1, Undo: json.RawMessage(`{"title":"Old"}`)})
	record(ModerationAction{ChatID: -1, Action: "mute", RequesterID: 7, TargetUserID: 9, UpdateID: 2, Undo: json.RawMessage(`{}`)})
	record(ModerationAction{ChatID: -1, Action: "delete", RequesterID: 7, TargetMessageID: 5, UpdateID: 3})
	record(ModerationAction{ChatID: -2, Action: "set_title", RequesterID: 7, UpdateID: 4, Undo: json.RawMessage(`{}`)})

	last, found, err := store.LastUndoable(ctx, -1)
	if err != nil || !found || last.Action != "mute" || last.TargetUserID != 9 || string(last.Undo) != "{}" {
		t.Fatalf("last = %+v %v %v, want the mute", last, found, err)
	}
	if first, err := store.MarkUndone(ctx, last.ID); err != nil || !first {
		t.Fatalf("MarkUndone = %v %v", first, err)
	}
	if again, err := store.MarkUndone(ctx, last.ID); err != nil || again {
		t.Errorf("a second MarkUndone = %v %v, want false", again, err)
	}
	last, found, err = store.LastUndoable(ctx, -1)
	var state struct{ Title string }
	if err != nil || !found || last.Action != "set_title" || json.Unmarshal(last.Undo, &state) != nil || state.Title != "Old" {
		t.Fatalf("then last = %+v %v %v, want the rename", last, found, err)
	}

	record(ModerationAction{ChatID: -3, Action: "invite_link", RequesterID: 7, UpdateID: 5})
	if _, found, _ := store.LastUndoable(ctx, -3); found {
		t.Error("a link with no undo yet was found")
	}
	if err := store.SetModerationUndo(ctx, 5, "invite_link", json.RawMessage(`{"link":"https://t.me/+x"}`)); err != nil {
		t.Fatal(err)
	}
	if last, found, _ := store.LastUndoable(ctx, -3); !found || last.Action != "invite_link" {
		t.Errorf("after SetModerationUndo, last = %+v %v", last, found)
	}
}

// The bot's links are found by link, or the latest without one; a revoked
// link, another chat's or one not yet made is not.
func TestInviteLinkFindsTheBotsOwnLinks(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, a := range []ModerationAction{
		{ChatID: -1, Action: "invite_link", RequesterID: 7, UpdateID: 1, Undo: json.RawMessage(`{"link":"https://t.me/+old","member_limit":5}`)},
		{ChatID: -1, Action: "invite_link", RequesterID: 7, UpdateID: 2, Undo: json.RawMessage(`{"link":"https://t.me/+new"}`)},
		{ChatID: -1, Action: "invite_link", RequesterID: 7, UpdateID: 3},
		{ChatID: -1, Action: "set_title", RequesterID: 7, UpdateID: 4, Undo: json.RawMessage(`{"title":"Old"}`)},
		{ChatID: -2, Action: "invite_link", RequesterID: 7, UpdateID: 5, Undo: json.RawMessage(`{"link":"https://t.me/+other"}`)},
	} {
		if _, err := store.RecordModeration(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	latest, found, err := store.InviteLink(ctx, -1, "")
	if err != nil || !found || latest.UpdateID != 2 {
		t.Fatalf("latest = %+v %v %v, want the newer link", latest, found, err)
	}
	old, found, err := store.InviteLink(ctx, -1, "https://t.me/+old")
	if err != nil || !found || old.UpdateID != 1 || len(old.Undo) == 0 {
		t.Fatalf("old = %+v %v %v", old, found, err)
	}
	if _, found, _ := store.InviteLink(ctx, -1, "https://t.me/+other"); found {
		t.Error("another chat's link was found")
	}
	if _, err := store.MarkUndone(ctx, latest.ID); err != nil {
		t.Fatal(err)
	}
	if again, found, _ := store.InviteLink(ctx, -1, ""); !found || again.UpdateID != 1 {
		t.Errorf("after a revoke, latest = %+v %v, want the older link", again, found)
	}
}
