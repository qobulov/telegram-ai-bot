package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"telegram-ai-bot/internal/cards"
	"telegram-ai-bot/internal/ranks"
	"telegram-ai-bot/internal/telegram"
)

// profileTopRead bounds the table read to find a player's place.
const profileTopRead = 1000

// profile answers /profile: the asker's card game card, or that of the
// member replied to; in a group also their rank and warnings there.
func (s *Service) profile(ctx context.Context, in incoming) outcome {
	who := in.msg.From
	if r := in.msg.ReplyToMessage; r != nil && r.From != nil && !r.From.IsBot {
		who = r.From
	}
	lines, err := s.cardProfile(ctx, who.ID)
	if err != nil {
		s.opts.Logger.Error("could not read a profile", "user_id", who.ID, "error", err)
		return say(textCardsUnavailable.to(in))
	}
	lines = append([]string{"👤 **" + cardMarks.Replace(doozName(who)) + "**", ""}, lines...)
	if in.group {
		lines = append(lines, s.chatProfile(ctx, in, who)...)
	}
	return outcome{text: strings.Join(lines, "\n"), buttons: s.cardsAppButton(in)}
}

func (s *Service) cardProfile(ctx context.Context, userID int64) ([]string, error) {
	total, owned, points, err := s.opts.Cards.CardCounts(ctx, userID)
	if err != nil {
		return nil, err
	}
	player, _, err := s.opts.Cards.CardPlayerOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	shards, _, err := s.opts.Cards.CardWallet(ctx, userID)
	if err != nil {
		return nil, err
	}
	place, err := s.cardPlace(ctx, userID)
	if err != nil {
		return nil, err
	}
	lines := []string{"🏆 Ochko: " + spaced(points) + " · o'rin: " + place}
	var held []string
	for _, r := range cards.Rarities {
		held = append(held, fmt.Sprintf("%s %d/%d", strings.Fields(rarityNames[r].uz)[0], owned[int(r)], total[int(r)]))
	}
	lines = append(lines, "🃏 "+strings.Join(held, " · "))
	if best, err := s.bestCards(ctx, userID); err != nil {
		return nil, err
	} else if best != "" {
		lines = append(lines, "⭐ Eng zo'rlari: "+best)
	}
	return append(lines, fmt.Sprintf("🎟 Token: %d · 💎 Kristall: %d", cards.Tokens(player, time.Now()), shards)), nil
}

// cardPlace is the player's place on the table, "—" before their first card.
func (s *Service) cardPlace(ctx context.Context, userID int64) (string, error) {
	top, err := s.opts.Cards.CardTop(ctx, profileTopRead)
	if err != nil {
		return "", err
	}
	for i, p := range top {
		if p.UserID == userID {
			return fmt.Sprintf("%d/%d", i+1, len(top)), nil
		}
	}
	return "—", nil
}

// bestCards names up to three of the player's rarest cards.
func (s *Service) bestCards(ctx context.Context, userID int64) (string, error) {
	var names []string
	for i := len(cards.Rarities) - 1; i >= 0 && len(names) < 3; i-- {
		owned, err := s.opts.Cards.OwnedCards(ctx, userID, int(cards.Rarities[i]))
		if err != nil {
			return "", err
		}
		for _, c := range owned {
			if len(names) == 3 {
				break
			}
			names = append(names, strings.Fields(rarityNames[c.Rarity].uz)[0]+" "+cardMarks.Replace(c.Name))
		}
	}
	return strings.Join(names, ", "), nil
}

func (s *Service) chatProfile(ctx context.Context, in incoming, who *telegram.User) []string {
	var lines []string
	if s.opts.Ranks != nil {
		if rank, err := s.opts.Ranks.RankOf(ctx, in.scope.ChatID, who.ID); err == nil && rank > 0 {
			lines = append(lines, fmt.Sprintf("🎖 Daraja: %d", rank))
		}
	}
	if s.opts.WarnList != nil {
		if warns, err := s.opts.WarnList.Warns(ctx, in.scope.ChatID, who.ID, 1); err == nil && len(warns) > 0 {
			lines = append(lines, fmt.Sprintf("⚠️ Ogohlantirish: %d/3", warns[0].Count))
		}
	}
	if len(lines) > 0 {
		lines = append([]string{""}, lines...)
	}
	return lines
}

// myRights answers /rights or "huquqlarim": the asker's rank in the group
// and the commands it opens them.
func (s *Service) myRights(ctx context.Context, in incoming) outcome {
	mine, err := s.askerRank(ctx, in)
	if err != nil {
		return say(textError.to(in))
	}
	if mine >= ranks.Above {
		return say("👑 Siz guruh egasisiz: barcha buyruqlar sizga ochiq.")
	}
	admin := false
	if s.opts.Members != nil {
		admin, _ = s.opts.Members.IsGroupAdmin(ctx, in.scope.ChatID, in.userID)
	}
	overrides, err := s.opts.Ranks.CommandRanks(ctx, in.scope.ChatID)
	if err != nil {
		return say(textError.to(in))
	}
	var open []string
	for _, c := range ranks.Commands {
		if mine > 0 && mine >= ranks.Need(c.Code, overrides) {
			open = append(open, "• "+c.Name)
		}
	}
	switch {
	case len(open) > 0:
		head := fmt.Sprintf("🎖 Darajangiz: %d. Sizga ochiq buyruqlar:", mine)
		if admin {
			head = fmt.Sprintf("🛡 Siz Telegram adminisiz, darajangiz: %d. Darajangiz ochgan buyruqlar:", mine)
		}
		return say(head + "\n" + strings.Join(open, "\n"))
	case admin:
		return say("🛡 Siz Telegram adminisiz: o'z huquqlaringiz doirasidagi moderatsiya buyruqlari ochiq.")
	}
	return say("Sizda daraja yo'q: oddiy a'zo buyruqlari ochiq — /report, /voteban, /admins, /profile. Darajalar: /ranks")
}

// rightsWords ask what the asker may do in the group.
var rightsWords = map[string]bool{"huquqlarim": true, "huquqim": true, "huquqlarim qanday": true, "мои права": true}

func (s *Service) isRightsQuestion(text string) bool {
	return commandName(text) == "/rights" || rightsWords[strings.Join(s.orderWords(text), " ")]
}
