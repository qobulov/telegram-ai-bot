package telegram

import "context"

// MaxDeletedAtOnce is the most messages one deleteMessages call takes.
const MaxDeletedAtOnce = 100

type deleteMessagesParams struct {
	ChatID     int64   `json:"chat_id"`
	MessageIDs []int64 `json:"message_ids"`
}

// DeleteMessages deletes up to MaxDeletedAtOnce messages of one chat in one
// call. Telegram skips those it cannot find, and those older than 48 hours.
func (c *Client) DeleteMessages(ctx context.Context, chatID int64, messageIDs []int64) error {
	return c.call(ctx, "deleteMessages", deleteMessagesParams{ChatID: chatID, MessageIDs: messageIDs}, nil)
}

type senderChatParams struct {
	ChatID       int64 `json:"chat_id"`
	SenderChatID int64 `json:"sender_chat_id"`
}

// BanChatSenderChat stops a channel from writing in a group on its own
// behalf: whoever runs it can no longer post there as the channel, under any
// of their accounts, until it is unbanned.
func (c *Client) BanChatSenderChat(ctx context.Context, chatID, senderChatID int64) error {
	return c.call(ctx, "banChatSenderChat", senderChatParams{ChatID: chatID, SenderChatID: senderChatID}, nil)
}

// UnbanChatSenderChat lets a banned channel write in a group again.
func (c *Client) UnbanChatSenderChat(ctx context.Context, chatID, senderChatID int64) error {
	return c.call(ctx, "unbanChatSenderChat", senderChatParams{ChatID: chatID, SenderChatID: senderChatID}, nil)
}

// ApproveChatJoinRequest lets someone who asked to join a group in.
func (c *Client) ApproveChatJoinRequest(ctx context.Context, chatID, userID int64) error {
	return c.call(ctx, "approveChatJoinRequest", chatMemberParams{ChatID: chatID, UserID: userID}, nil)
}

// DeclineChatJoinRequest turns down someone who asked to join a group. They
// may ask again.
func (c *Client) DeclineChatJoinRequest(ctx context.Context, chatID, userID int64) error {
	return c.call(ctx, "declineChatJoinRequest", chatMemberParams{ChatID: chatID, UserID: userID}, nil)
}
