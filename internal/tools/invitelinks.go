package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// An invite link the bot makes is kept in its audit record's undo, with
// its limits: undo revokes it, and revoke_link and edit_link find it there.
// They act only on links the bot made in that group, never on the
// group's other links, whatever link the model passes.

// The moderate tool's actions on a link the bot made.
const (
	actionRevokeLink = "revoke_link"
	actionEditLink   = "edit_link"
)

// maxMemberLimit is the most people Telegram lets join through one link.
const maxMemberLimit = 99999

const (
	limitWithRequest = "Telegram does not let a link both admit a number of people and ask the admins to approve each: nothing was done; ask them which one they want"
	noLinkMade       = "I have made no invite link in this group that still works; I can revoke or change only links I made myself"
	notMyLink        = "that is not an invite link I made in this group; I can revoke or change only links I made myself"
	nothingToEdit    = "say what to change: text for its name, minutes for its end, member_limit or join_request"
	linkLimitsNote   = "Done. Give them the link exactly as it is, in one short line, in their language, with its limits as member_limit and join_request say."
)

// InviteLinks finds an invite link the bot made in a chat and has not
// revoked: link, or the latest when link is "".
type InviteLinks interface {
	InviteLink(ctx context.Context, chatID int64, link string) (storage.ModerationAction, bool, error)
}

// linkWords ask for something about an invite link: "link ber", "havola",
// "ссылка".
var linkWords = []string{"link", "havola", "ssilka", "silka", "ссылк", "taklif", "invite"}

// revokeWords ask to stop a link from working, and editWords to change its
// limits, besides naming the link.
var (
	revokeWords = []string{"bekor", "revoke", "ochir", "yop", "ishlamasin", "отзов", "отозв", "отмен", "удал", "аннул", "закр"}
	editWords   = append([]string{"limit", "лимит", "chekla", "cheklov", "огранич", "kishi", "odam", "человек", "tasdiq", "одобр", "заявк", "sorov", "ruxsat", "muddat", "срок", "vaqt", "nom", "назв"}, changeWords...)
)

// inviteRequest is what an invite link action asks for besides its name
// and end. memberLimit and joinRequest are nil when not given: an edit
// keeps what the link had.
type inviteRequest struct {
	memberLimit *int
	joinRequest *bool
	// link is the link to revoke or edit, as the asker wrote it; "" for the
	// latest the bot made in the group.
	link string
}

// linkState is an invite link the bot made, as its audit record keeps it.
// Link is the field undo revokes; the limits are what an edit starts from,
// as Telegram sets every limit anew in an edit.
type linkState struct {
	Link        string `json:"link"`
	Name        string `json:"name,omitempty"`
	Expires     int64  `json:"expires,omitempty"`
	MemberLimit int    `json:"member_limit,omitempty"`
	JoinRequest bool   `json:"join_request,omitempty"`
}

// onInviteLink reports an action on an invite link, which runLink carries
// out.
func onInviteLink(action string) bool {
	return action == actionInviteLink || action == actionRevokeLink || action == actionEditLink
}

// setLinkArgs reads a link action's limits and link, and, for revoke and
// edit, whether the asker's words say to.
func (o *order) setLinkArgs(args moderationArgs) error {
	if limit := args.MemberLimit; limit != nil && (*limit < 0 || *limit > maxMemberLimit || (*limit == 0 && o.action != actionEditLink)) {
		return &ArgumentError{Message: fmt.Sprintf("member_limit must be 1 to %d, or 0 for edit_link to remove it", maxMemberLimit)}
	}
	o.invite = inviteRequest{memberLimit: args.MemberLimit, joinRequest: args.JoinRequest, link: normalLink(args.Link)}
	switch o.action {
	case actionRevokeLink:
		if !says(o.caller.Words, revokeWords) {
			return &DeniedError{Message: notAsked}
		}
	case actionEditLink:
		if !says(o.caller.Words, editWords) {
			return &DeniedError{Message: notAsked}
		}
		if o.invite.memberLimit == nil && o.invite.joinRequest == nil && o.minutes == 0 && strings.TrimSpace(args.Text) == "" {
			return &ArgumentError{Message: nothingToEdit}
		}
	}
	return nil
}

// normalLink is a link as Telegram gives it back: "t.me/+abc" is
// "https://t.me/+abc".
func normalLink(link string) string {
	link = strings.TrimSpace(link)
	if strings.HasPrefix(link, "t.me/") {
		return "https://" + link
	}
	return link
}

// runLink carries out an invite link action whose rights were checked:
// makes a link, or revokes or edits one the bot made. Like every action it
// is recorded first, so that an update delivered twice acts once.
func (m *Moderation) runLink(ctx context.Context, o order) (any, error) {
	if o.action == actionInviteLink {
		return m.createLink(ctx, o)
	}
	made, state, err := m.madeLink(ctx, o)
	if err != nil {
		return nil, err
	}
	next := state.with(o)
	if next.MemberLimit > 0 && next.JoinRequest {
		return nil, &ArgumentError{Message: limitWithRequest}
	}
	first, err := m.opts.Store.RecordModeration(ctx, o.audit())
	if err != nil {
		return nil, internal(err)
	}
	if !first {
		return map[string]any{"done": true, "note": alreadyDoneNote}, nil
	}
	if o.action == actionRevokeLink {
		if err := m.opts.Telegram.RevokeChatInviteLink(ctx, o.caller.ChatID, state.Link); err != nil {
			return nil, notCarriedOut(m.failed(ctx, o, err))
		}
		// Revoked, it is no longer the latest link, nor anything undo can
		// revoke; failing that, a second revoke only fails at Telegram.
		_, _ = m.opts.Store.MarkUndone(ctx, made.ID)
		return map[string]any{"done": true, "said": chatText{"Bajarildi: taklif havolasi bekor qilindi.", "Готово: ссылка-приглашение отозвана."}.String(), "note": doneNote}, nil
	}
	if err := m.opts.Telegram.EditChatInviteLink(ctx, state.Link, next.inviteLink(o.caller.ChatID)); err != nil {
		return nil, notCarriedOut(m.failed(ctx, o, err))
	}
	m.keepLink(ctx, made.UpdateID, next)
	return map[string]any{"done": true, "said": next.said().String(), "note": doneNote}, nil
}

// createLink makes a new link with the limits asked for, and keeps it in
// its record for undo, revoke_link and edit_link.
func (m *Moderation) createLink(ctx context.Context, o order) (any, error) {
	state := linkState{Name: o.text}.with(o)
	if state.MemberLimit > 0 && state.JoinRequest {
		return nil, &ArgumentError{Message: limitWithRequest}
	}
	first, err := m.opts.Store.RecordModeration(ctx, o.audit())
	if err != nil {
		return nil, internal(err)
	}
	if !first {
		return map[string]any{"done": true, "note": alreadyDoneNote}, nil
	}
	link, err := m.opts.Telegram.CreateChatInviteLink(ctx, state.inviteLink(o.caller.ChatID))
	if err != nil {
		return nil, notCarriedOut(m.failed(ctx, o, err))
	}
	state.Link = link
	m.keepLink(ctx, o.caller.UpdateID, state)
	if state.MemberLimit == 0 && !state.JoinRequest {
		return map[string]any{"done": true, "link": link, "note": linkNote}, nil
	}
	return map[string]any{"done": true, "link": link, "member_limit": state.MemberLimit, "join_request": state.JoinRequest, "note": linkLimitsNote}, nil
}

// keepLink stores a link's state in the record of the update that made
// it. The link exists either way; without its state it only cannot be
// revoked or edited through the bot.
func (m *Moderation) keepLink(ctx context.Context, madeIn int64, state linkState) {
	if undo, err := json.Marshal(state); err == nil {
		_ = m.opts.Store.SetModerationUndo(ctx, madeIn, actionInviteLink, undo)
	}
}

// madeLink is the link o acts on, from the audit: the one the asker named,
// or the latest, among the links the bot made in the group and has not
// revoked.
func (m *Moderation) madeLink(ctx context.Context, o order) (storage.ModerationAction, linkState, error) {
	if m.opts.Links == nil {
		return storage.ModerationAction{}, linkState{}, &DeniedError{Message: noLinkMade}
	}
	made, found, err := m.opts.Links.InviteLink(ctx, o.caller.ChatID, o.invite.link)
	if err != nil {
		return storage.ModerationAction{}, linkState{}, internal(err)
	}
	if !found && o.invite.link != "" {
		return storage.ModerationAction{}, linkState{}, &DeniedError{Message: notMyLink}
	}
	var state linkState
	if !found || json.Unmarshal(made.Undo, &state) != nil || state.Link == "" {
		return storage.ModerationAction{}, linkState{}, &DeniedError{Message: noLinkMade}
	}
	return made, state, nil
}

// with is s with what o changes: its name, end and limits when given.
func (s linkState) with(o order) linkState {
	if o.action == actionEditLink && o.text != "" {
		s.Name = o.text
	}
	if !o.until.IsZero() {
		s.Expires = o.until.Unix()
	}
	if o.invite.memberLimit != nil {
		s.MemberLimit = *o.invite.memberLimit
	}
	if o.invite.joinRequest != nil {
		s.JoinRequest = *o.invite.joinRequest
	}
	return s
}

// inviteLink is s as Telegram is asked for it.
func (s linkState) inviteLink(chatID int64) telegram.InviteLink {
	l := telegram.InviteLink{ChatID: chatID, Name: s.Name, MemberLimit: s.MemberLimit, JoinRequest: s.JoinRequest}
	if s.Expires > 0 {
		l.Expires = time.Unix(s.Expires, 0)
	}
	return l
}

// said says, for the chat, what an edited link now does.
func (s linkState) said() chatText {
	switch {
	case s.JoinRequest:
		return chatText{"Bajarildi: taklif havolasi endi adminlar tasdig'i bilan qo'shadi.", "Готово: по ссылке теперь вступают с одобрения админов."}
	case s.MemberLimit > 0:
		n := strconv.Itoa(s.MemberLimit)
		return chatText{"Bajarildi: taklif havolasi endi " + n + " kishigacha qo'shadi.", "Готово: по ссылке теперь могут вступить до " + n + " человек."}
	}
	return chatText{"Bajarildi: taklif havolasi yangilandi.", "Готово: ссылка-приглашение обновлена."}
}
