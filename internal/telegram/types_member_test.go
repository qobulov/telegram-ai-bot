package telegram

import (
	"encoding/json"
	"testing"
)

// A restricted member's permissions come in the same object as their
// status; anyone not restricted may do everything.
func TestChatMemberPermissions(t *testing.T) {
	var restricted ChatMember
	body := `{"status":"restricted","is_member":true,"until_date":1800000000,"can_send_messages":true,"can_send_photos":false,"can_send_other_messages":false,"can_invite_users":true,"can_pin_messages":false}`
	if err := json.Unmarshal([]byte(body), &restricted); err != nil {
		t.Fatal(err)
	}
	p := restricted.Permissions()
	if !p.CanSendMessages || p.CanSendPhotos || p.CanSendOtherMessages || !p.CanInviteUsers || p.CanPinMessages {
		t.Errorf("permissions = %+v", p)
	}
	if got := (ChatMember{Status: "member"}).Permissions(); got != AllPermissions() {
		t.Errorf("a plain member's permissions = %+v, want all", got)
	}
}
