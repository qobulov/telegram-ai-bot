package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"telegram-ai-bot/internal/ranks"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

const (
	infoUnavailable = "that could not be read just now; try again later"
	pinnedRunes     = 300
	memberInfoNote  = "Tell the asker what they asked for from these facts alone, briefly, in their language; never add a number or a rank that is not here. " +
		"Messages are those the bot saw; rank 0 means no moderator rank; warnings lapse after 30 days, the third mutes for a day."
)

// GroupInfoReader asks Telegram about a group.
type GroupInfoReader interface {
	GetChat(ctx context.Context, chatID int64) (telegram.Chat, error)
	GetChatMemberCount(ctx context.Context, chatID int64) (int, error)
	GetChatMember(ctx context.Context, chatID, userID int64) (telegram.ChatMember, error)
	GetChatAdministrators(ctx context.Context, chatID int64) ([]telegram.ChatMember, error)
}

// CommandRanker reads the ranks a group's creator set for its commands.
type CommandRanker interface {
	CommandRanks(ctx context.Context, chatID int64) (map[string]int, error)
}

// GroupInfoTool tells anyone in a group about it: its description, link,
// size, what members may send, its pinned message, what the bot may do
// there and, with commandRanks, what each moderator rank opens there.
func GroupInfoTool(telegramAPI GroupInfoReader, botID int64, commandRanks CommandRanker) Tool {
	return Tool{
		Name: "group_info",
		Description: "This group as Telegram has it now: title, public link, description (bio), member count, admins count, whether it has topics, what members may send, its latest pinned message, its sticker pack, which admin rights you yourself hold here, and rank_table: what each moderator rank (1 to 5) opens in this group, as its /access sets it. " +
			"Call it before you say what a rank gives or anything about the group, even when the chat or your earlier answers seem to say it, whenever someone asks about the group's bio or description, link, size, rules of what may be sent, the pinned message, what you are able to do here, or what a rank gives (\"guruh biosi\", \"guruhda nechta odam\", \"qanday huquqing bor\", \"3-daraja nima beradi\", \"описание группы\"); never guess.",
		Schema:    json.RawMessage(`{"type": "object", "properties": {}, "additionalProperties": false}`),
		GroupOnly: true,
		Run: func(ctx context.Context, _ json.RawMessage) (any, error) {
			caller, ok := CallerFrom(ctx)
			if !ok || caller.ChatID >= 0 {
				return nil, &DeniedError{Message: statsNotInAGroup}
			}
			out, err := groupInfo(ctx, telegramAPI, botID, caller.ChatID)
			if err != nil || commandRanks == nil {
				return out, err
			}
			if overrides, err := commandRanks.CommandRanks(ctx, caller.ChatID); err == nil {
				out["rank_table"], out["rank_note"] = rankTable(overrides), rankNote
			}
			return out, nil
		},
	}
}

// rankNote reads the rank table for the model.
const rankNote = "A rank opens its own commands and every lower rank's, never on a member of the same rank or higher; rank 6 is the group's creator and your owners alone. " +
	"Telegram admins use the commands their admin rights allow, whatever the table says. Say what a rank gives only from this table."

// rankLevel is what one moderator rank opens in a group.
type rankLevel struct {
	Rank  int      `json:"rank"`
	Opens []string `json:"opens"`
}

// rankTable is what each rank opens in a group whose creator set
// overrides, lowest first; a rank opens its own commands and every lower
// rank's. Commands kept above every rank are the creator's and the owners'.
func rankTable(overrides map[string]int) []rankLevel {
	table := make([]rankLevel, 0, ranks.Above)
	for rank := 1; rank <= ranks.Above; rank++ {
		level := rankLevel{Rank: rank, Opens: []string{}}
		for _, c := range ranks.Commands {
			if ranks.Need(c.Code, overrides) == rank {
				level.Opens = append(level.Opens, c.Name)
			}
		}
		if len(level.Opens) > 0 {
			table = append(table, level)
		}
	}
	return table
}

func groupInfo(ctx context.Context, api GroupInfoReader, botID, chatID int64) (map[string]any, error) {
	chat, err := api.GetChat(ctx, chatID)
	if err != nil {
		return nil, &InternalError{Err: err, Message: infoUnavailable}
	}
	out := map[string]any{"title": chat.Title, "has_topics": chat.IsForum}
	if chat.Username != "" {
		out["link"] = "https://t.me/" + chat.Username
	}
	if chat.Description != "" {
		out["description"] = chat.Description
	}
	if chat.StickerSetName != "" {
		out["sticker_pack"] = "https://t.me/addstickers/" + chat.StickerSetName
	}
	if chat.PinnedMessage != nil {
		if text, _ := chat.PinnedMessage.Content(); text != "" {
			out["pinned_message"] = cutRunes(text, pinnedRunes)
		}
	}
	if chat.Permissions != nil {
		out["members_may"] = permissionWords(*chat.Permissions)
	}
	if n, err := api.GetChatMemberCount(ctx, chatID); err == nil {
		out["members"] = n
	}
	if admins, err := api.GetChatAdministrators(ctx, chatID); err == nil {
		out["admins"] = len(admins)
	}
	if me, err := api.GetChatMember(ctx, chatID, botID); err == nil {
		rights := me.Rights()
		if len(rights) == 0 {
			rights = []string{}
		}
		out["your_status"], out["your_rights"] = me.Status, rights
	}
	return out, nil
}

// permissionWords are what members may send, by name.
func permissionWords(p telegram.ChatPermissions) []string {
	may := []string{}
	for _, f := range []struct {
		on   bool
		name string
	}{
		{p.CanSendMessages, "messages"}, {p.CanSendPhotos, "photos"}, {p.CanSendVideos, "videos"},
		{p.CanSendOtherMessages, "stickers and GIFs"}, {p.CanSendVoiceNotes, "voice messages"},
		{p.CanSendVideoNotes, "round videos"}, {p.CanSendAudios, "music"}, {p.CanSendDocuments, "files"},
		{p.CanSendPolls, "polls"}, {p.CanAddWebPagePreviews, "link previews"}, {p.CanInviteUsers, "add members"},
		{p.CanPinMessages, "pin"}, {p.CanChangeInfo, "change the group's info"},
	} {
		if f.on {
			may = append(may, f.name)
		}
	}
	return may
}

// MemberInfoStore reads what the bot keeps about a member.
type MemberInfoStore interface {
	MemberActivityOf(ctx context.Context, chatID, userID int64, today time.Time) (storage.MemberActivity, error)
	RankOf(ctx context.Context, chatID, userID int64) (int, error)
	Warns(ctx context.Context, chatID, userID int64, limit int) ([]storage.MemberWarn, error)
	InviteTop(ctx context.Context, chatID int64, q storage.InviteQuery) ([]storage.InviterCount, error)
	AuthorByUsername(ctx context.Context, chatID int64, username string) (int64, error)
	CardCounts(ctx context.Context, userID int64) (total, owned map[int]int, points int64, err error)
	CardPlayerOf(ctx context.Context, userID int64) (storage.CardPlayer, time.Time, error)
	CardWallet(ctx context.Context, userID int64) (shards int, err error)
	CardTop(ctx context.Context, limit int) ([]storage.CardPlayer, error)
}

// MemberInfoOptions configures member_info. Store and Telegram are required.
type MemberInfoOptions struct {
	Store    MemberInfoStore
	Telegram MemberLookup
	// Tokens is how many card tokens a player may spend now.
	Tokens func(p storage.CardPlayer, now time.Time) int
	// NextToken is when the next card token comes.
	NextToken func(now time.Time) time.Time
	Location  *time.Location
}

type memberInfoArgs struct {
	Whose string `json:"whose"`
}

// MemberInfoTool tells a member about themselves, or about the member they
// replied to or tagged: their standing in Telegram, rank, warnings, invites,
// activity and card game.
func MemberInfoTool(opts MemberInfoOptions) Tool {
	return Tool{
		Name: "member_info",
		Description: "Everything known about the asker in this group (whose: me, the default), or about the member their message replies to (replied) or the one they tagged with @ (tagged): Telegram status and admin title or tag, moderator rank (1–5), warnings with the last reason, mutes and bans received, messages written today and this week, when last seen, who brought them in, how many people they brought, and their card game points, place, cards, tokens, minutes to the next token and crystals. " +
			"Call it before you state anyone's cards, points, tokens, crystals, rank, warnings or messages, even when the chat or your earlier answers seem to say them: they change, and memory is wrong. Use it whenever someone asks about themselves or a member (\"men haqimda\", \"darajam qaysi\", \"nechta ogohlantirishim bor\", \"nechta xabar yozdim\", \"kim qo'shgan meni\", \"ochkoim\", \"мой ранг\", \"сколько у меня предупреждений\"); answer only from what it gives.",
		Schema: json.RawMessage(`{"type": "object", "properties": {
			"whose": {"type": "string", "enum": ["me", "replied", "tagged"], "description": "me, the default; replied for the member their message replies to; tagged for the one member they tagged."}
		}, "additionalProperties": false}`),
		GroupOnly: true,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return memberInfo(ctx, opts, raw)
		},
	}
}

func memberInfo(ctx context.Context, opts MemberInfoOptions, raw json.RawMessage) (any, error) {
	var args memberInfoArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, ok := CallerFrom(ctx)
	if !ok || caller.ChatID >= 0 {
		return nil, &DeniedError{Message: statsNotInAGroup}
	}
	userID, err := infoTarget(ctx, opts, caller, args.Whose)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"about": "the asker", "note": memberInfoNote}
	if userID != caller.UserID {
		out["about"] = "another member"
	}
	if member, err := opts.Telegram.GetChatMember(ctx, caller.ChatID, userID); err == nil {
		addTelegramStanding(out, member)
	}
	if err := addKeptFacts(ctx, opts, caller.ChatID, userID, out); err != nil {
		return nil, err
	}
	addCardFacts(ctx, opts, userID, out)
	return out, nil
}

// infoTarget is the member asked about.
func infoTarget(ctx context.Context, opts MemberInfoOptions, caller Caller, whose string) (int64, error) {
	switch whose {
	case "replied":
		if caller.Target == nil || caller.Target.UserID == 0 {
			return 0, &ArgumentError{Message: "their message replies to no member; use whose me or tagged"}
		}
		return caller.Target.UserID, nil
	case "tagged":
		if len(caller.Mentions) != 1 {
			return 0, &DeniedError{Message: "they tagged no one, or more than one; ask them to tag the one member, or reply to their message"}
		}
		if m := caller.Mentions[0]; m.UserID != 0 {
			return m.UserID, nil
		}
		id, err := opts.Store.AuthorByUsername(ctx, caller.ChatID, caller.Mentions[0].Username)
		if errors.Is(err, storage.ErrUnknownAuthor) {
			return 0, &DeniedError{Message: unknownMention}
		}
		if err != nil {
			return 0, &InternalError{Err: err, Message: infoUnavailable}
		}
		return id, nil
	}
	return caller.UserID, nil
}

func addTelegramStanding(out map[string]any, m telegram.ChatMember) {
	out["telegram_status"] = m.Status
	if m.User != nil {
		name := m.User.FirstName
		if m.User.Username != "" {
			name = strings.TrimSpace(name + " @" + m.User.Username)
		}
		out["name"] = name
	}
	if m.CustomTitle != "" {
		out["admin_title"] = m.CustomTitle
	}
	if m.Tag != "" {
		out["tag"] = m.Tag
	}
	if m.Status == "restricted" && m.UntilDate > 0 {
		out["restricted_until"] = time.Unix(m.UntilDate, 0).UTC().Format(time.RFC3339)
	}
	if rights := m.Rights(); len(rights) > 0 {
		out["admin_rights"] = rights
	}
}

func addKeptFacts(ctx context.Context, opts MemberInfoOptions, chatID, userID int64, out map[string]any) error {
	location := opts.Location
	if location == nil {
		location = time.UTC
	}
	now := time.Now().In(location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	activity, err := opts.Store.MemberActivityOf(ctx, chatID, userID, today)
	if err != nil {
		return &InternalError{Err: err, Message: infoUnavailable}
	}
	out["messages_today"], out["messages_this_week"] = activity.MessagesToday, activity.MessagesWeek
	out["mutes_received"], out["bans_received"] = activity.Muted, activity.Banned
	if !activity.LastSeen.IsZero() {
		out["last_seen"] = activity.LastSeen.In(location).Format("2006-01-02 15:04")
	}
	if !activity.JoinedAt.IsZero() {
		out["joined"] = activity.JoinedAt.In(location).Format("2006-01-02")
		if activity.BroughtBy != "" {
			out["brought_in_by"] = activity.BroughtBy
		}
	}
	rank, err := opts.Store.RankOf(ctx, chatID, userID)
	if err != nil {
		return &InternalError{Err: err, Message: infoUnavailable}
	}
	out["rank"] = rank
	warns, err := opts.Store.Warns(ctx, chatID, userID, 1)
	if err != nil {
		return &InternalError{Err: err, Message: infoUnavailable}
	}
	out["warnings"] = 0
	if len(warns) > 0 {
		out["warnings"] = warns[0].Count
		if warns[0].Reason != "" {
			out["last_warning_reason"] = warns[0].Reason
		}
	}
	top, err := opts.Store.InviteTop(ctx, chatID, storage.InviteQuery{Inviter: userID, Limit: 50})
	if err != nil {
		return &InternalError{Err: err, Message: infoUnavailable}
	}
	for _, c := range top {
		if c.UserID == userID {
			out["people_brought"], out["people_brought_still_here"] = c.Added, c.Stayed
		}
	}
	return nil
}

// addCardFacts adds the card game, left out when it cannot be read.
func addCardFacts(ctx context.Context, opts MemberInfoOptions, userID int64, out map[string]any) {
	_, owned, points, err := opts.Store.CardCounts(ctx, userID)
	if err != nil {
		return
	}
	held := 0
	for _, n := range owned {
		held += n
	}
	cards := map[string]any{"points": points, "different_cards": held}
	if top, err := opts.Store.CardTop(ctx, 1000); err == nil {
		for i, p := range top {
			if p.UserID == userID {
				cards["place"], cards["players"] = i+1, len(top)
			}
		}
	}
	if player, _, err := opts.Store.CardPlayerOf(ctx, userID); err == nil && opts.Tokens != nil {
		cards["tokens"] = opts.Tokens(player, time.Now())
	}
	if opts.NextToken != nil {
		cards["next_token_in_minutes"] = int(time.Until(opts.NextToken(time.Now())).Minutes()) + 1
	}
	if shards, err := opts.Store.CardWallet(ctx, userID); err == nil {
		cards["crystals"] = shards
	}
	out["card_game"] = cards
}
