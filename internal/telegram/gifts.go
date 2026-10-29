package telegram

import "context"

// Gift is one of the gifts a bot can buy for a user with its Stars.
type Gift struct {
	ID      string  `json:"id"`
	Sticker Sticker `json:"sticker"`
	// StarCount is what it costs. RemainingCount is how many are left of a
	// limited gift, whose TotalCount is set; an unlimited one has neither.
	StarCount      int `json:"star_count"`
	TotalCount     int `json:"total_count"`
	RemainingCount int `json:"remaining_count"`
}

type gifts struct {
	Gifts []Gift `json:"gifts"`
}

// GetAvailableGifts lists the gifts the bot can send.
func (c *Client) GetAvailableGifts(ctx context.Context) ([]Gift, error) {
	var available gifts
	if err := c.call(ctx, "getAvailableGifts", struct{}{}, &available); err != nil {
		return nil, err
	}
	return available.Gifts, nil
}

type sendGiftParams struct {
	UserID int64  `json:"user_id"`
	GiftID string `json:"gift_id"`
	Text   string `json:"text,omitempty"`
}

// SendGift buys a gift with the bot's Stars and sends it to a user, with
// text, at most 128 characters, shown with it.
func (c *Client) SendGift(ctx context.Context, userID int64, giftID, text string) error {
	return c.call(ctx, "sendGift", sendGiftParams{UserID: userID, GiftID: giftID, Text: text}, nil)
}

type starAmount struct {
	Amount int `json:"amount"`
}

// GetMyStarBalance is how many whole Stars the bot has.
func (c *Client) GetMyStarBalance(ctx context.Context) (int, error) {
	var balance starAmount
	if err := c.call(ctx, "getMyStarBalance", struct{}{}, &balance); err != nil {
		return 0, err
	}
	return balance.Amount, nil
}
