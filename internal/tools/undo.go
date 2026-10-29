package tools

import (
	"context"
	"encoding/json"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// Every action the moderate tool takes but a deletion is recorded with what
// puts it back, read from Telegram just before it is taken. "undo" puts
// back the group's latest such action, once; asked again, the one before.

const actionUndo = "undo"

// maxGroupPhotoBytes bounds the old group photo downloaded to be put back.
const maxGroupPhotoBytes = 10 << 20

const (
	nothingToUndo = "nothing I did in this group can be put back; a deleted message never comes back"
	changedSince  = "someone changed it by hand since I did, so nothing was done: tell them what it is now, and call undo with force only if they say to put it back anyway"
)

// undoState is what puts an action back: the group's or a member's state
// before it. After is the title or description the action set, and
// AfterPermissions the group's permissions, to tell whether someone changed
// them by hand since.
type undoState struct {
	Title            *string                   `json:"title,omitempty"`
	Description      *string                   `json:"description,omitempty"`
	After            *string                   `json:"after,omitempty"`
	PhotoFileID      string                    `json:"photo_file_id,omitempty"`
	NoPhoto          bool                      `json:"no_photo,omitempty"`
	Permissions      *telegram.ChatPermissions `json:"permissions,omitempty"`
	AfterPermissions *telegram.ChatPermissions `json:"after_permissions,omitempty"`
	Until            int64                     `json:"until,omitempty"`
	AdminTitle       *string                   `json:"admin_title,omitempty"`
	Rights           *telegram.AdminRights     `json:"rights,omitempty"`
	Link             string                    `json:"link,omitempty"`
	MessageID        int64                     `json:"message_id,omitempty"`
}

// snapshot is what will put o back, read before o is taken; nil when o
// cannot be undone. An invite link's is known only once it exists, and
// runLink keeps it (see linkState).
func (m *Moderation) snapshot(ctx context.Context, o order) (json.RawMessage, error) {
	var state undoState
	chatID := o.caller.ChatID
	switch o.action {
	case actionDelete:
		return nil, nil
	case actionSetTitle, actionSetDescription, actionSetPhoto, actionDeletePhoto, actionLock, actionUnlock, actionUnpinAll:
		chat, err := m.opts.Telegram.GetChat(ctx, chatID)
		if err != nil {
			return nil, err
		}
		switch o.action {
		case actionSetTitle:
			state.Title, state.After = &chat.Title, &o.text
		case actionSetDescription:
			state.Description, state.After = &chat.Description, &o.text
		case actionSetPhoto, actionDeletePhoto:
			if chat.Photo == nil {
				state.NoPhoto = true
			} else {
				state.PhotoFileID = chat.Photo.BigFileID
			}
		case actionLock, actionUnlock:
			before := groupPermissions(chat)
			after := before
			lockKinds[o.kind].set(&after, o.action == actionUnlock)
			state.Permissions, state.AfterPermissions = &before, &after
		case actionUnpinAll:
			if chat.PinnedMessage == nil {
				return nil, nil
			}
			state.MessageID = chat.PinnedMessage.MessageID
		}
	case actionMute, actionUnmute, actionNoStickers, actionStickers, actionNoPhotos, actionPhotos, actionDemote, actionAdminTitle:
		member, err := m.opts.Members.GetChatMember(ctx, chatID, o.target.UserID)
		if err != nil {
			return nil, err
		}
		switch o.action {
		case actionDemote:
			state.Rights = &telegram.AdminRights{CanDeleteMessages: member.CanDeleteMessages, CanRestrictMembers: member.CanRestrictMembers,
				CanPinMessages: member.CanPinMessages, CanInviteUsers: member.CanInviteUsers}
		case actionAdminTitle:
			state.AdminTitle = &member.CustomTitle
		default:
			permissions := member.Permissions()
			state.Permissions, state.Until = &permissions, member.UntilDate
		}
	}
	// Ban, unban, pin, unpin and promote need only their record.
	return json.Marshal(state)
}

// undo puts back the latest action in the caller's group that can be,
// asked of whoever may take that action, and only once.
func (m *Moderation) undo(ctx context.Context, caller Caller, force bool) (any, error) {
	last, found, err := m.opts.Store.LastUndoable(ctx, caller.ChatID)
	if err != nil {
		return nil, internal(err)
	}
	if !found {
		return nil, &DeniedError{Message: nothingToUndo}
	}
	var state undoState
	if err := json.Unmarshal(last.Undo, &state); err != nil {
		return nil, internal(err)
	}
	right := neededRight[last.Action]
	if err := m.checkRequester(ctx, caller, right); err != nil {
		return nil, err
	}
	if err := m.checkBot(ctx, caller.ChatID, right); err != nil {
		return nil, err
	}
	if err := m.checkPace(ctx, caller); err != nil {
		return nil, err
	}
	if !force {
		changed, err := m.changedSince(ctx, last, state)
		if err != nil {
			return nil, notCarriedOut(telegramFailure(err))
		}
		if changed {
			return nil, &DeniedError{Message: changedSince}
		}
	}
	o := order{caller: caller, action: actionUndo, target: Target{UserID: last.TargetUserID, MessageID: last.TargetMessageID}}
	first, err := m.opts.Store.RecordModeration(ctx, o.audit())
	if err != nil {
		return nil, internal(err)
	}
	if !first {
		return map[string]any{"done": true, "note": alreadyDoneNote}, nil
	}
	if err := m.putBack(ctx, last, state); err != nil {
		return nil, notCarriedOut(m.failed(ctx, o, err))
	}
	if _, err := m.opts.Store.MarkUndone(ctx, last.ID); err != nil {
		return nil, internal(err)
	}
	return map[string]any{"done": true, "said": undoneText(last.Action, state).String(), "note": doneNote}, nil
}

// changedSince reports whether the group's title, description or
// permissions are no longer what the action set: putting them back would
// undo someone else's change.
func (m *Moderation) changedSince(ctx context.Context, last storage.ModerationAction, state undoState) (bool, error) {
	switch last.Action {
	case actionSetTitle, actionSetDescription, actionLock, actionUnlock:
	default:
		return false, nil
	}
	chat, err := m.opts.Telegram.GetChat(ctx, last.ChatID)
	if err != nil {
		return false, err
	}
	switch last.Action {
	case actionSetTitle:
		return state.After != nil && chat.Title != *state.After, nil
	case actionSetDescription:
		return state.After != nil && chat.Description != *state.After, nil
	}
	return state.AfterPermissions != nil && groupPermissions(chat) != *state.AfterPermissions, nil
}

// putBack asks Telegram for the opposite of last.
func (m *Moderation) putBack(ctx context.Context, last storage.ModerationAction, state undoState) error {
	tg := m.opts.Telegram
	chatID, userID, messageID := last.ChatID, last.TargetUserID, last.TargetMessageID
	switch last.Action {
	case actionSetTitle:
		return tg.SetChatTitle(ctx, chatID, *state.Title)
	case actionSetDescription:
		return tg.SetChatDescription(ctx, chatID, *state.Description)
	case actionSetPhoto, actionDeletePhoto:
		if state.NoPhoto {
			return tg.DeleteChatPhoto(ctx, chatID)
		}
		photo, err := tg.DownloadFile(ctx, state.PhotoFileID, maxGroupPhotoBytes)
		if err != nil {
			return err
		}
		return tg.SetChatPhoto(ctx, chatID, photo)
	case actionLock, actionUnlock:
		return tg.SetChatPermissions(ctx, chatID, *state.Permissions)
	case actionUnpinAll:
		return tg.PinChatMessage(ctx, chatID, state.MessageID)
	case actionInviteLink:
		return tg.RevokeChatInviteLink(ctx, chatID, state.Link)
	case actionMute, actionUnmute, actionNoStickers, actionStickers, actionNoPhotos, actionPhotos:
		return tg.RestrictChatMember(ctx, m.restrictionBefore(chatID, userID, state))
	case actionBan:
		return tg.UnbanChatMember(ctx, chatID, userID)
	case actionUnban:
		return tg.BanChatMember(ctx, chatID, userID)
	case actionPin:
		return tg.UnpinChatMessage(ctx, chatID, messageID)
	case actionUnpin:
		return tg.PinChatMessage(ctx, chatID, messageID)
	case actionPromote:
		return tg.PromoteChatMember(ctx, chatID, userID, telegram.AdminRights{})
	case actionDemote:
		me, err := m.opts.Members.GetChatMember(ctx, chatID, m.opts.BotID)
		if err != nil {
			return err
		}
		// The bot can give back only the rights it holds itself.
		return tg.PromoteChatMember(ctx, chatID, userID, telegram.AdminRights{
			CanDeleteMessages:  state.Rights.CanDeleteMessages && me.CanDeleteMessages,
			CanRestrictMembers: state.Rights.CanRestrictMembers && me.CanRestrictMembers,
			CanPinMessages:     state.Rights.CanPinMessages && me.CanPinMessages,
			CanInviteUsers:     state.Rights.CanInviteUsers && me.CanInviteUsers,
		})
	case actionAdminTitle:
		return tg.SetChatAdministratorCustomTitle(ctx, chatID, userID, *state.AdminTitle)
	}
	return nil
}

// restrictionBefore is the member's restriction before the action. One
// that has ended since is none: Telegram takes an end in the past for
// forever.
func (m *Moderation) restrictionBefore(chatID, userID int64, state undoState) telegram.Restriction {
	r := telegram.Restriction{ChatID: chatID, UserID: userID, Permissions: *state.Permissions}
	if state.Until > 0 {
		r.Until = time.Unix(state.Until, 0)
		if !r.Until.After(m.opts.Now()) {
			r.Permissions, r.Until = telegram.AllPermissions(), time.Time{}
		}
	}
	return r
}

// undoneText says, for the chat, what undo put back.
func undoneText(action string, state undoState) chatText {
	switch action {
	case actionSetTitle:
		return chatText{"Bajarildi: guruh nomi yana «" + *state.Title + "».", "Готово: группа снова называется «" + *state.Title + "»."}
	case actionSetDescription:
		return chatText{"Bajarildi: guruh tavsifi qaytarildi.", "Готово: описание группы возвращено."}
	case actionSetPhoto, actionDeletePhoto:
		return chatText{"Bajarildi: guruh rasmi qaytarildi.", "Готово: фото группы возвращено."}
	case actionLock, actionUnlock:
		return chatText{"Bajarildi: guruh ruxsatlari avvalgi holiga qaytdi.", "Готово: права в группе снова как раньше."}
	case actionUnpinAll, actionUnpin:
		return chatText{"Bajarildi: xabar yana qadaldi.", "Готово: сообщение снова закреплено."}
	case actionInviteLink:
		return chatText{"Bajarildi: taklif havolasi bekor qilindi.", "Готово: ссылка-приглашение отозвана."}
	case actionBan:
		return chatText{"Bajarildi: a'zo blokdan chiqarildi, guruhga o'zi qaytishi mumkin.", "Готово: участник разбанен и может вернуться сам."}
	case actionUnban:
		return chatText{"Bajarildi: a'zo yana bloklandi.", "Готово: участник снова забанен."}
	case actionPin:
		return chatText{"Bajarildi: xabar qadashdan olindi.", "Готово: сообщение откреплено."}
	case actionPromote:
		return chatText{"Bajarildi: a'zo adminlikdan olindi.", "Готово: участник больше не админ."}
	case actionDemote:
		return chatText{"Bajarildi: a'zo yana admin.", "Готово: участник снова админ."}
	case actionAdminTitle:
		return chatText{"Bajarildi: admin unvoni qaytarildi.", "Готово: звание админа возвращено."}
	}
	return chatText{"Bajarildi: a'zoning cheklovlari avvalgi holiga qaytdi.", "Готово: ограничения участника снова как раньше."}
}
