package storage

import (
	"context"
	"testing"
)

// A voice file's transcript is kept once, the first one standing; "" (no
// speech) is a transcript too; a file never transcribed has none.
func TestVoiceTranscriptsAreKeptOnce(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, found, err := store.VoiceTranscript(ctx, "v1"); err != nil || found {
		t.Fatalf("before = %v %v, want none", found, err)
	}
	for _, words := range []string{"salom dunyo", "boshqa"} {
		if err := store.SaveVoiceTranscript(ctx, "v1", words); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveVoiceTranscript(ctx, "v2", ""); err != nil {
		t.Fatal(err)
	}
	if words, found, err := store.VoiceTranscript(ctx, "v1"); err != nil || !found || words != "salom dunyo" {
		t.Errorf("v1 = %q %v %v, want the first transcript", words, found, err)
	}
	if words, found, err := store.VoiceTranscript(ctx, "v2"); err != nil || !found || words != "" {
		t.Errorf("v2 = %q %v %v, want an empty transcript kept", words, found, err)
	}
}
