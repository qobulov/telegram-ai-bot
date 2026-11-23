package service

import "testing"

// An answer sent as a reply to a member's message does not start with that
// member's @username, which the reply tags already; another member's tag,
// or one inside the words, stays.
func TestWithoutLeadingTag(t *testing.T) {
	cases := []struct {
		text, want string
		users      []string
	}{
		{"@Unbrokevb Hammasini chaqirsam, uyushma ish haqimdan ushlab qoladi.", "Hammasini chaqirsam, uyushma ish haqimdan ushlab qoladi.", []string{"Unbrokevb"}},
		{"@abutzzy, sizni chaqirishyapti.", "Sizni chaqirishyapti.", []string{"Abutzzy"}},
		{"@Furqat_Shavkatov, sizni chaqirishyapti.", "@Furqat_Shavkatov, sizni chaqirishyapti.", []string{"Unbrokevb"}},
		{"Sizda username bor-ku: @Bakxtyorvch.", "Sizda username bor-ku: @Bakxtyorvch.", []string{"Bakxtyorvch"}},
		{"@Unbrokevb", "@Unbrokevb", []string{"Unbrokevb"}},
		{"@Unbrokevb salom", "@Unbrokevb salom", nil},
	}
	for _, c := range cases {
		if got := withoutLeadingTag(c.text, c.users...); got != c.want {
			t.Errorf("withoutLeadingTag(%q, %v) = %q, want %q", c.text, c.users, got, c.want)
		}
	}
}
