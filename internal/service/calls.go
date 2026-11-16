package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"telegram-ai-bot/internal/telegram"
)

// Members call the group's admins and moderators, report a message to them,
// and vote to ban a member, as Iris lets them.

const (
	// callPause keeps a group from calling its admins more than once in it,
	// and a member from reporting more than once in reportPause.
	callPause   = 3 * time.Minute
	reportPause = 2 * time.Minute
	// voteBanNeed votes for a ban ban the member; a vote lasts voteBanLasts.
	voteBanNeed  = 5
	voteBanLasts = 30 * time.Minute
	// voteBanData starts a vote's buttons: "vb:y:<token>" for, "vb:n:<token>"
	// against.
	voteBanData = "vb:"
)

// BanVoteBook keeps each group's vote to ban a member.
type BanVoteBook interface {
	ChangeBanVote(ctx context.Context, chatID int64, change func(state []byte) ([]byte, error)) error
}

// callTimes keeps when each group last called its admins and each member
// last reported, so that neither floods the group.
type callTimes struct {
	mu   sync.Mutex
	last map[[2]int64]time.Time
}

// ready reports whether key may call again, and if so marks it.
func (c *callTimes) ready(key [2]int64, pause time.Duration, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last == nil {
		c.last = map[[2]int64]time.Time{}
	}
	if now.Sub(c.last[key]) < pause {
		return false
	}
	c.last[key] = now
	return true
}

var groupCalls callTimes

// callOrder is "/admins", "/report" or "/voteban", or the same in words.
func (s *Service) callOrder(in incoming) (string, bool) {
	if !in.group {
		return "", false
	}
	switch commandName(in.text) {
	case "/admins":
		return "admins", s.opts.AdminList != nil
	case "/report":
		return "report", s.opts.AdminList != nil
	case "/voteban":
		return "voteban", s.opts.BanVotes != nil && s.opts.GroupModerator != nil && s.opts.Buttons != nil
	}
	written, _ := in.msg.Content()
	switch strings.Join(s.orderWords(written), " ") {
	case "adminlar", "adminlarni chaqir", "админы", "позови админов", "@admin", "@admins":
		return "admins", s.opts.AdminList != nil
	case "report", "репорт", "жалоба", "shikoyat":
		return "report", s.opts.AdminList != nil
	case "voteban", "ban uchun ovoz", "голосование за бан":
		return "voteban", s.opts.BanVotes != nil && s.opts.GroupModerator != nil && s.opts.Buttons != nil
	}
	return "", false
}

func (s *Service) isCallOrder(in incoming) bool {
	_, ok := s.callOrder(in)
	return ok
}

func (s *Service) callCommand(ctx context.Context, in incoming) outcome {
	kind, _ := s.callOrder(in)
	switch kind {
	case "report":
		return s.report(ctx, in)
	case "voteban":
		return s.openBanVote(ctx, in)
	}
	return s.callAdmins(ctx, in)
}

// staffTags are the @usernames of the group's admins and ranked moderators,
// and the names of those without one; bots are left out.
func (s *Service) staffTags(ctx context.Context, chatID int64) ([]string, error) {
	admins, err := s.opts.AdminList.GetChatAdministrators(ctx, chatID)
	if err != nil {
		return nil, err
	}
	var tags []string
	seen := map[int64]bool{}
	add := func(id int64, username, name string) {
		if seen[id] {
			return
		}
		seen[id] = true
		if username != "" {
			tags = append(tags, "@"+username)
		} else if name != "" {
			tags = append(tags, cardMarks.Replace(name))
		}
	}
	for _, a := range admins {
		if a.User != nil && !a.User.IsBot && !a.IsAnonymous {
			add(a.User.ID, a.User.Username, a.User.FirstName)
		}
	}
	if s.opts.Ranks != nil {
		if ranked, err := s.opts.Ranks.Ranks(ctx, chatID); err == nil {
			for _, m := range ranked {
				add(m.UserID, m.Username, m.Name)
			}
		}
	}
	return tags, nil
}

// callAdmins tags the group's admins and moderators, once in callPause.
func (s *Service) callAdmins(ctx context.Context, in incoming) outcome {
	if !groupCalls.ready([2]int64{in.scope.ChatID, 0}, callPause, time.Now()) {
		return say("⏳ Adminlar yaqinda chaqirildi, birozdan keyin qayta urinib ko'ring.")
	}
	tags, err := s.staffTags(ctx, in.scope.ChatID)
	if err != nil || len(tags) == 0 {
		return say("Adminlar ro'yxatini hozir olib bo'lmadi.")
	}
	return say("📣 " + cardMarks.Replace(doozName(in.msg.From)) + " adminlarni chaqiryapti: " + strings.Join(tags, " "))
}

// report tells the admins and moderators about the message replied to,
// once in reportPause for each member.
func (s *Service) report(ctx context.Context, in incoming) outcome {
	replied := in.msg.ReplyToMessage
	if replied == nil || replied.From == nil || replied.From.ID == in.userID {
		return say("Qaysi xabar ustidan? Xabarga reply qilib /report yozing.")
	}
	if !groupCalls.ready([2]int64{in.scope.ChatID, in.userID}, reportPause, time.Now()) {
		return say("⏳ Yaqinda shikoyat qildingiz, birozdan keyin yana yozing.")
	}
	tags, err := s.staffTags(ctx, in.scope.ChatID)
	if err != nil {
		return say("Adminlar ro'yxatini hozir olib bo'lmadi.")
	}
	quote, _ := replied.Content()
	quote = cardMarks.Replace(strings.TrimSpace(quote))
	if r := []rune(quote); len(r) > 120 {
		quote = string(r[:120]) + "…"
	}
	if quote == "" {
		quote = "(rasm, stiker yoki boshqa xabar)"
	}
	return say(fmt.Sprintf("🚨 Shikoyat: %s → %s\n«%s»\n%s", cardMarks.Replace(doozName(in.msg.From)), cardMarks.Replace(doozName(replied.From)), quote, strings.Join(tags, " ")))
}

// banVote is a group's vote to ban a member.
type banVote struct {
	Token   string  `json:"token"`
	Target  int64   `json:"target"`
	Name    string  `json:"name"`
	Starter int64   `json:"starter"`
	Yes     []int64 `json:"yes"`
	No      []int64 `json:"no"`
	Until   int64   `json:"until"`
	Done    bool    `json:"done"`
}

// openBanVote starts a vote to ban the member replied to, the asker its
// first vote for; admins and owners cannot be voted on.
func (s *Service) openBanVote(ctx context.Context, in incoming) outcome {
	replied := in.msg.ReplyToMessage
	if replied == nil || replied.From == nil || replied.From.IsBot || replied.From.ID == in.userID {
		return say("Kimni? Uning xabariga reply qilib /voteban yozing.")
	}
	target := replied.From
	if s.isOwner(target) {
		return say("⛔ Bunga ovoz berib bo'lmaydi.")
	}
	if s.opts.Members != nil {
		if admin, err := s.opts.Members.IsGroupAdmin(ctx, in.scope.ChatID, target.ID); err != nil || admin {
			return say("⛔ Adminni ovoz bilan bloklab bo'lmaydi.")
		}
	}
	vote := banVote{
		Token: strconv.FormatInt(rand.Int64N(1<<40), 36), Target: target.ID, Name: doozName(target),
		Starter: in.userID, Yes: []int64{in.userID}, Until: time.Now().Add(voteBanLasts).Unix(),
	}
	err := s.opts.BanVotes.ChangeBanVote(ctx, in.scope.ChatID, func(state []byte) ([]byte, error) {
		var open banVote
		if json.Unmarshal(state, &open) == nil && open.Token != "" && !open.Done && time.Now().Unix() < open.Until {
			return nil, errVoteOpen
		}
		return json.Marshal(vote)
	})
	if errors.Is(err, errVoteOpen) {
		return say("⏳ Guruhda boshqa ovoz berish ketyapti, u tugashini kuting.")
	}
	if err != nil {
		return say(textError.to(in))
	}
	return outcome{text: banVoteText(vote), buttons: banVoteButtons(vote)}
}

var errVoteOpen = errors.New("a vote is open")

// errVoteRefused carries why a press is refused, shown to the presser alone.
type errVoteRefused struct{ reason string }

func (e errVoteRefused) Error() string { return e.reason }

// pressedBanVote counts a press; at voteBanNeed votes for, the member is
// banned and the vote closed.
func (s *Service) pressedBanVote(q *telegram.CallbackQuery) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	side, token, _ := strings.Cut(strings.TrimPrefix(q.Data, voteBanData), ":")
	var vote banVote
	err := s.opts.BanVotes.ChangeBanVote(ctx, q.Message.Chat.ID, func(state []byte) ([]byte, error) {
		if err := json.Unmarshal(state, &vote); err != nil {
			return nil, err
		}
		switch {
		case vote.Token != token || vote.Done || time.Now().Unix() >= vote.Until:
			return nil, errVoteRefused{"Bu ovoz berish tugagan."}
		case q.From.ID == vote.Target:
			return nil, errVoteRefused{"O'zingiz haqingizdagi ovozda qatnasha olmaysiz."}
		case slices.Contains(vote.Yes, q.From.ID) || slices.Contains(vote.No, q.From.ID):
			return nil, errVoteRefused{"Siz ovoz bergansiz."}
		}
		if side == "y" {
			vote.Yes = append(vote.Yes, q.From.ID)
		} else {
			vote.No = append(vote.No, q.From.ID)
		}
		vote.Done = len(vote.Yes) >= voteBanNeed
		return json.Marshal(vote)
	})
	var refused errVoteRefused
	if errors.As(err, &refused) {
		_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, refused.reason, true)
		return
	}
	if err != nil {
		_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, "Hozir ovoz berib bo'lmadi.", true)
		return
	}
	_ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, "Ovozingiz qabul qilindi.", false)
	if !vote.Done {
		_ = s.opts.Buttons.EditMessageMenu(ctx, q.Message.Chat.ID, q.Message.MessageID, banVoteText(vote), banVoteButtons(vote))
		return
	}
	text := fmt.Sprintf("🔨 %s %d ovoz bilan bloklandi.", cardMarks.Replace(vote.Name), len(vote.Yes))
	if err := s.opts.GroupModerator.BanChatMember(ctx, q.Message.Chat.ID, vote.Target); err != nil {
		text = fmt.Sprintf("Ovoz yig'ildi, lekin %s ni bloklab bo'lmadi: menga a'zolarni cheklash huquqi kerak.", cardMarks.Replace(vote.Name))
	}
	_ = s.opts.Buttons.EditMessageText(ctx, q.Message.Chat.ID, q.Message.MessageID, text)
}

func banVoteText(v banVote) string {
	return fmt.Sprintf("🗳 %s ni bloklaymizmi?\n%d ta \"ha\" ovozi yig'ilsa, bloklanadi. Ovoz berish 30 daqiqa davom etadi.", cardMarks.Replace(v.Name), voteBanNeed)
}

func banVoteButtons(v banVote) [][]telegram.Button {
	return [][]telegram.Button{{
		{Text: fmt.Sprintf("✅ Ha (%d/%d)", len(v.Yes), voteBanNeed), Data: voteBanData + "y:" + v.Token},
		{Text: fmt.Sprintf("❌ Yo'q (%d)", len(v.No)), Data: voteBanData + "n:" + v.Token},
	}}
}
