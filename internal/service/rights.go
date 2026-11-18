package service

import (
	"context"
	"fmt"
	"strings"

	"telegram-ai-bot/internal/ranks"
)

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
	return say("Sizda daraja yo'q: oddiy a'zo buyruqlari ochiq — /report, /voteban, /admins, /myinvites. Darajalar: /ranks")
}

// rightsWords ask what the asker may do in the group.
var rightsWords = map[string]bool{"huquqlarim": true, "huquqim": true, "huquqlarim qanday": true, "мои права": true}

func (s *Service) isRightsQuestion(text string) bool {
	return commandName(text) == "/rights" || rightsWords[strings.Join(s.orderWords(text), " ")]
}
