package service

import (
	"strings"
	"testing"
)

// The tool's refusals a command can meet are told to the group in Uzbek,
// with what they name filled in.
func TestModRefusalsInUzbek(t *testing.T) {
	for message, want := range map[string]string{
		"that admin was made by another admin, not by me, and Telegram lets only the one who made them":                                     "boshqa admin tayinlagan",
		"I can't do that to admins, my owners, myself, Telegram's service accounts or the person asking":                                    "adminlarga",
		"only admins of this group who have the right to add new admins, or my owners, can ask me to do that; the person asking is neither": "«admin qo'shish»",
		"I don't have the right to ban and mute members in this group; its admins can give it to me":                                        "«a'zolarni cheklash»",
		"their rank is 2, and this needs rank 5 here":                                                                                       "Darajangiz 2, buning uchun 5-daraja",
		"that member's rank is 3, not lower than the asker's 3, so nothing was done":                                                        "Uning darajasi 3",
		"that's 20 actions in 10 minutes; wait a bit":                                                                                       "10 daqiqada 20 ta",
		"I have not seen that @username in this group, so I can't tell who it is; reply":                                                    "@username'ni guruhda ko'rmaganman",
		"a member's tag has at most 16 characters and no emoji":                                                                             "16 belgi",
	} {
		if got := modRefusalText(message); !strings.Contains(got, want) {
			t.Errorf("%q = %q, want %q in it", message, got, want)
		}
	}
	if got := modRefusalText("something new"); got != "" {
		t.Errorf("an unknown refusal = %q", got)
	}
}
