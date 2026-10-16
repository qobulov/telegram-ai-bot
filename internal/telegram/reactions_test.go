package telegram

import "testing"

func TestReactionEmojiList(t *testing.T) {
	if len(ReactionEmoji) != 73 {
		t.Errorf("ReactionEmoji has %d entries, want Telegram's 73", len(ReactionEmoji))
	}
	seen := make(map[string]bool, len(ReactionEmoji))
	for _, emoji := range ReactionEmoji {
		if seen[emoji] {
			t.Errorf("%q is listed twice", emoji)
		}
		seen[emoji] = true
	}

	tests := []struct {
		name  string
		emoji string
		want  bool
	}{
		{"fire", "🔥", true},
		{"heart on fire, a joined sequence", "❤‍\U0001F525", true},
		{"technologist, a joined sequence", "\U0001F468‍\U0001F4BB", true},
		{"slightly smiling face is not allowed", "🙂", false},
		{"empty", "", false},
		{"two emoji", "🔥🔥", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReactionEmoji(tt.emoji); got != tt.want {
				t.Errorf("IsReactionEmoji(%q) = %v, want %v", tt.emoji, got, tt.want)
			}
		})
	}
}
