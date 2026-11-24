package service

import "testing"

// Tags are counted once each, by @username or by ID, and written as plain
// names when taken out; an e-mail address is no tag.
func TestMemberTags(t *testing.T) {
	text := "Kelinglar: @alisher, @Bekzod @alisher [Said](tg://user?id=7) va [Olim](tg://user?id=8)! mail: a@bob.uz"
	if n := memberTags(text); n != 4 {
		t.Errorf("tags = %d, want 4", n)
	}
	if got, want := untagged(text), "Kelinglar: alisher, Bekzod alisher Said va Olim! mail: a@bob.uz"; got != want {
		t.Errorf("untagged = %q, want %q", got, want)
	}
	if n := memberTags("@alisher bilan @bekzod"); n != 2 {
		t.Errorf("two tags = %d", n)
	}
}
