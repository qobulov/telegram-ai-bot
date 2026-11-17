package service

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/ranks"
	"telegram-ai-bot/internal/storage"
)

// RankBook keeps the moderators' ranks, 1 to 5, as Iris does: who has which
// in each group, who gave them, and the rank each command needs there.
type RankBook interface {
	SetRank(ctx context.Context, chatID, userID int64, rank int, by int64) error
	RankOf(ctx context.Context, chatID, userID int64) (int, error)
	Ranks(ctx context.Context, chatID int64) ([]storage.MemberRank, error)
	RankLog(ctx context.Context, chatID, userID int64, limit int) ([]storage.RankChange, error)
	CommandRanks(ctx context.Context, chatID int64) (map[string]int, error)
	SetCommandRank(ctx context.Context, chatID int64, code string, rank int) error
}

// rankLogShown is how many changes the rank log shows.
const rankLogShown = 10

// rankOrder is what a group message asks of the ranks: list them, show the
// log or the commands' ranks, set a command's rank, or give a member a rank,
// 0 taking it away.
type rankOrder struct {
	kind string // "staff", "log", "access", "give", "mine"
	rank int
	code string
}

var (
	plusRank   = regexp.MustCompile(`^\+([1-5])$`)
	rankDigit  = regexp.MustCompile(`^[0-5]$`)
	minusRank  = regexp.MustCompile(`^-[0-5]$`)
	rankWords  = map[string]bool{"daraja": true, "даража": true, "дараже": true, "rank": true, "ранг": true, "повысить": true, "повысь": true, "понизить": true, "понизь": true}
	staffWords = map[string]bool{"moderatorlar": true, "модераторы": true, "ранги": true, "darajalar": true, "staff": true}
	takeWords  = map[string]bool{"-admin": true, "-админ": true, "-модер": true, "-moder": true, "-rank": true, "-ранг": true,
		"-daraja": true, "разжаловать": true, "разжалуй": true}
	logPhrases  = map[string]bool{"kim tayinlagan": true, "kim tayinladi": true, "кто назначил": true, "ким тайинлаган": true}
	takePhrases = map[string]bool{"darajani ol": true, "darajasini ol": true, "darajani olib tashla": true, "даражани ол": true, "даражасини ол": true,
		"даражани олиб ташла": true, "снять ранг": true, "сними ранг": true}
	minePhrases = map[string]bool{"darajam": true, "darajam qaysi": true, "darajam nechchi": true, "мени darajam qaysi": true, "meni darajam qaysi": true,
		"даражам": true, "даражам қайси": true, "даражам кайси": true, "мени даражам кайси": true, "мой ранг": true, "какой у меня ранг": true, "my rank": true}
)

// isRankWord is a word that says the "+3" is a rank, not agreement: "admin",
// "модер", "daraja".
func isRankWord(w string) bool {
	return isAdminWord(w) || strings.HasPrefix(w, "модер") || strings.HasPrefix(w, "moder") || rankWords[w] ||
		strings.HasPrefix(w, "daraj") || strings.HasPrefix(w, "дараж")
}

// asksOwnRank reports "darajam qaysi" in any wording: a word of one's own
// rank, "darajam", "даражам", among a few.
func asksOwnRank(words []string) bool {
	for _, w := range words {
		if strings.HasPrefix(w, "darajam") || strings.HasPrefix(w, "даражам") {
			return true
		}
	}
	return false
}

// rankRequest reads a group message about ranks; ok is false for any other.
func (s *Service) rankRequest(in incoming) (order rankOrder, ok bool) {
	if !in.group || s.opts.Ranks == nil {
		return rankOrder{}, false
	}
	switch commandName(in.text) {
	case "/staff", "/ranks":
		return rankOrder{kind: "staff"}, true
	case "/ranklog":
		return rankOrder{kind: "log"}, true
	case "/access":
		return accessOrder(strings.Fields(strings.ToLower(commandArgument(in.text))))
	case "/rank":
		if arg := strings.TrimSpace(commandArgument(in.text)); rankDigit.MatchString(arg) {
			n, _ := strconv.Atoi(arg)
			return rankOrder{kind: "give", rank: n}, true
		}
		return rankOrder{}, false
	}
	written, _ := in.msg.Content()
	words := s.orderWords(written)
	if len(words) == 0 || len(words) > 4 {
		return rankOrder{}, false
	}
	joined := strings.Join(words, " ")
	switch {
	case words[0] == ".дк":
		return accessOrder(words[1:])
	case len(words) == 1 && staffWords[words[0]]:
		return rankOrder{kind: "staff"}, true
	case logPhrases[joined]:
		return rankOrder{kind: "log"}, true
	case minePhrases[joined] || asksOwnRank(words):
		return rankOrder{kind: "mine"}, true
	case takePhrases[joined] || (len(words) == 1 && takeWords[words[0]]):
		return rankOrder{kind: "give"}, true
	}
	if minusRank.MatchString(words[0]) && len(words) > 1 && allRankWords(words[1:]) {
		// "-1 admin mari" takes the rank, whatever the number.
		return rankOrder{kind: "give"}, true
	}
	if m := plusRank.FindStringSubmatch(words[0]); m != nil && len(words) > 1 && allRankWords(words[1:]) {
		n, _ := strconv.Atoi(m[1])
		return rankOrder{kind: "give", rank: n}, true
	}
	if rankDigit.MatchString(words[0]) && len(words) >= 2 && isRankWord(words[1]) && allRankWords(words[2:]) {
		n, _ := strconv.Atoi(words[0])
		return rankOrder{kind: "give", rank: n}, true
	}
	if isRankWord(words[0]) && !isAdminWord(words[0]) && len(words) >= 2 && rankDigit.MatchString(words[1]) && allRankWords(words[2:]) {
		n, _ := strconv.Atoi(words[1])
		return rankOrder{kind: "give", rank: n}, true
	}
	return rankOrder{}, false
}

// allRankWords reports words that are each a rank word or a filler.
func allRankWords(words []string) bool {
	for _, w := range words {
		if !isRankWord(w) && !modFillers[w] {
			return false
		}
	}
	return true
}

// accessOrder reads the commands' ranks: none shows them, "ban 3" sets one.
func accessOrder(args []string) (rankOrder, bool) {
	if len(args) == 0 {
		return rankOrder{kind: "access", rank: -1}, true
	}
	if len(args) != 2 {
		return rankOrder{kind: "access", rank: -2}, true
	}
	n, err := strconv.Atoi(args[1])
	if err != nil || n < 0 || n > ranks.Above {
		return rankOrder{kind: "access", rank: -2}, true
	}
	return rankOrder{kind: "access", code: args[0], rank: n}, true
}

// orderWords are the words of an order, lower case, without the bot's name
// or the punctuation around them, signs kept: "+3", "-admin".
func (s *Service) orderWords(text string) []string {
	var words []string
	for _, w := range strings.Fields(strings.ToLower(text)) {
		if w != ".дк" {
			w = strings.Trim(w, ".,!?:;«»\"'`")
		}
		if w == "" || isWakeWord(w, s.opts.WakeWords) || strings.EqualFold(w, "@"+s.opts.Bot.Username) {
			continue
		}
		if n := len(words); n > 0 && (words[n-1] == "-" || words[n-1] == "+") {
			// "- админ": a sign written apart is still the word's.
			words[n-1] += w
			continue
		}
		words = append(words, w)
	}
	return words
}

// isRankCommand reports a message about ranks.
func (s *Service) isRankCommand(in incoming) bool {
	_, ok := s.rankRequest(in)
	return ok
}

// rankCommand answers a message about ranks.
func (s *Service) rankCommand(ctx context.Context, in incoming) outcome {
	order, _ := s.rankRequest(in)
	chatID := in.scope.ChatID
	switch order.kind {
	case "staff":
		return s.staffList(ctx, chatID)
	case "log":
		var userID int64
		if asked, ok := s.withModTarget(in); ok {
			userID = asked.msg.ReplyToMessage.From.ID
		}
		return s.rankLog(ctx, chatID, userID)
	case "access":
		return s.accessCommand(ctx, in, order)
	case "mine":
		return s.myRank(ctx, in)
	}
	return s.giveRank(ctx, in, order.rank)
}

// askerRank is the rank of the person asking: above every rank for the
// group's creator and the bot's owners.
func (s *Service) askerRank(ctx context.Context, in incoming) (int, error) {
	if s.isOwner(in.msg.From) {
		return ranks.Above, nil
	}
	if s.opts.Members != nil {
		creator, err := s.opts.Members.GroupCreator(ctx, in.scope.ChatID)
		if err != nil {
			return 0, err
		}
		if creator != 0 && creator == in.userID {
			return ranks.Above, nil
		}
	}
	return s.opts.Ranks.RankOf(ctx, in.scope.ChatID, in.userID)
}

// giveRank gives the member replied to the rank, or takes theirs with 0:
// only one whose rank opens giving ranks, and only below their own.
func (s *Service) giveRank(ctx context.Context, in incoming, rank int) outcome {
	asked, ok := s.withModTarget(in)
	if !ok {
		return say("Kimga? Moderatorning xabariga reply qilib yozing: \"+2 admin\", \"-admin\".")
	}
	target := asked.msg.ReplyToMessage.From
	if target == nil || target.IsBot || target.ID == in.userID {
		return say("⛔ Bunga daraja berib bo'lmaydi.")
	}
	mine, err := s.askerRank(ctx, in)
	if err != nil {
		return say(textError.to(in))
	}
	overrides, err := s.opts.Ranks.CommandRanks(ctx, in.scope.ChatID)
	if err != nil {
		return say(textError.to(in))
	}
	if need := ranks.Need("rank", overrides); mine < need {
		return say(fmt.Sprintf("⛔ Daraja berish uchun kamida %d-daraja kerak.", need))
	}
	theirs, err := s.opts.Ranks.RankOf(ctx, in.scope.ChatID, target.ID)
	if err != nil {
		return say(textError.to(in))
	}
	if mine < ranks.Above && (rank >= mine || theirs >= mine) {
		return say(fmt.Sprintf("⛔ Faqat o'zingizdan past darajani bera yoki ola olasiz (sizniki %d).", mine))
	}
	if err := s.opts.Ranks.SetRank(ctx, in.scope.ChatID, target.ID, rank, in.userID); err != nil {
		s.opts.Logger.Error("could not set a rank", "chat_id", in.scope.ChatID, "error", err)
		return say(textError.to(in))
	}
	name := cardMarks.Replace(target.FirstName)
	if rank == 0 {
		return say("✅ " + name + " endi moderator emas.")
	}
	return say(fmt.Sprintf("✅ %s endi %s %d-darajali moderator.", name, stars(rank), rank))
}

// myRank says the asker's rank, or that they have none and how one is
// given.
func (s *Service) myRank(ctx context.Context, in incoming) outcome {
	mine, err := s.askerRank(ctx, in)
	if err != nil {
		return say(textError.to(in))
	}
	switch {
	case mine >= ranks.Above:
		return say("👑 Siz guruh egasi yoki bot egasisiz: hamma darajadan yuqorisiz.")
	case mine > 0:
		return say(fmt.Sprintf("%s Sizning darajangiz: %d.", stars(mine), mine))
	}
	return say("Sizda daraja yo'q. Uni guruh egasi yoki 4-darajali moderator xabaringizga reply qilib \"+2 admin mari\" deb beradi.")
}

// stars is a rank drawn: ⭐⭐⭐ for 3.
func stars(rank int) string { return strings.Repeat("⭐", rank) }

// staffList is the group's moderators by rank.
func (s *Service) staffList(ctx context.Context, chatID int64) outcome {
	ranked, err := s.opts.Ranks.Ranks(ctx, chatID)
	if err != nil {
		return say("Ro'yxatni hozir ochib bo'lmadi.")
	}
	lines := s.adminLines(ctx, chatID)
	if len(ranked) == 0 {
		lines = append(lines, "", "🎖 Hozir hech kimda moderator darajasi yo'q. Berish: a'zoning xabariga reply qilib \"+1 admin mari\" (1–5).")
		return say(strings.TrimSpace(strings.Join(lines, "\n")))
	}
	lines = append(lines, "", "🎖 Moderatorlar")
	current := 0
	for _, m := range ranked {
		if m.Rank != current {
			current = m.Rank
			lines = append(lines, stars(current)+fmt.Sprintf(" %d-daraja", current))
		}
		lines = append(lines, "• "+memberLabel(m.Name, m.Username, m.UserID))
	}
	return say(strings.TrimSpace(strings.Join(lines, "\n")))
}

// adminLines are the group's owner and Telegram admins, bots and hidden
// ones left out; none when Telegram cannot be asked.
func (s *Service) adminLines(ctx context.Context, chatID int64) []string {
	if s.opts.AdminList == nil {
		return nil
	}
	admins, err := s.opts.AdminList.GetChatAdministrators(ctx, chatID)
	if err != nil {
		return nil
	}
	var owner, others []string
	for _, a := range admins {
		if a.User == nil || a.User.IsBot || a.IsAnonymous {
			continue
		}
		label := memberLabel(a.User.FirstName, a.User.Username, a.User.ID)
		if a.CustomTitle != "" {
			label += " — " + cardMarks.Replace(a.CustomTitle)
		}
		if a.Status == "creator" {
			owner = append(owner, "👑 Egasi: "+label)
		} else {
			others = append(others, "• "+label)
		}
	}
	lines := owner
	if len(others) > 0 {
		lines = append(lines, "🛡 Adminlar:")
		lines = append(lines, others...)
	}
	return lines
}

// memberLabel names a member as the group knows them.
func memberLabel(name, username string, userID int64) string {
	name = cardMarks.Replace(strings.TrimSpace(name))
	switch {
	case name != "" && username != "":
		return name + " (@" + username + ")"
	case name != "":
		return name
	case username != "":
		return "@" + username
	}
	return strconv.FormatInt(userID, 10)
}

// rankLog is the group's latest rank changes, of one member when userID is
// not 0.
func (s *Service) rankLog(ctx context.Context, chatID, userID int64) outcome {
	log, err := s.opts.Ranks.RankLog(ctx, chatID, userID, rankLogShown)
	if err != nil {
		return say("Jurnalni hozir ochib bo'lmadi.")
	}
	if len(log) == 0 {
		return say("📜 Hali hech kimga daraja berilmagan.")
	}
	lines := []string{"📜 Kim tayinlagan"}
	for _, c := range log {
		when := c.At.In(s.location()).Format("02.01 15:04")
		who, by := memberLabel(c.Name, "", c.UserID), memberLabel(c.ByName, "", c.GivenBy)
		what := fmt.Sprintf("%d-daraja", c.Rank)
		if c.Rank == 0 {
			what = "darajasi olindi"
		}
		lines = append(lines, fmt.Sprintf("%s · %s → %s: %s", when, by, who, what))
	}
	return say(strings.Join(lines, "\n"))
}

// accessCommand shows the rank each command needs, or sets one.
func (s *Service) accessCommand(ctx context.Context, in incoming, order rankOrder) outcome {
	chatID := in.scope.ChatID
	overrides, err := s.opts.Ranks.CommandRanks(ctx, chatID)
	if err != nil {
		return say(textError.to(in))
	}
	if order.rank == -1 {
		lines := []string{"⚙️ Buyruqlar darajasi (o'zgartirish: \".дк ban 2\")"}
		for _, c := range ranks.Commands {
			lines = append(lines, fmt.Sprintf("%d · %s — %s", ranks.Need(c.Code, overrides), c.Code, c.Name))
		}
		lines = append(lines, "", "6 — faqat guruh yaratuvchisi va bot egalari.")
		return say(strings.Join(lines, "\n"))
	}
	if _, known := ranks.Find(order.code); order.rank == -2 || !known {
		return say("Masalan: \".дк ban 2\" — ban buyrug'i 2-darajadan ochiladi. Ro'yxat: \".дк\".")
	}
	mine, err := s.askerRank(ctx, in)
	if err != nil {
		return say(textError.to(in))
	}
	if need := ranks.Need("access", overrides); mine < need {
		return say(fmt.Sprintf("⛔ Buyruqlar darajasini o'zgartirish uchun kamida %d-daraja kerak.", need))
	}
	if mine < ranks.Above && order.rank > mine {
		return say(fmt.Sprintf("⛔ O'zingizning darajangizdan (%d) yuqori qilib bo'lmaydi.", mine))
	}
	if err := s.opts.Ranks.SetCommandRank(ctx, chatID, order.code, order.rank); err != nil {
		return say(textError.to(in))
	}
	c, _ := ranks.Find(order.code)
	return say(fmt.Sprintf("✅ «%s» endi %d-darajadan ochiladi.", c.Name, order.rank))
}

// location is the group's time zone for times shown.
func (s *Service) location() *time.Location {
	if s.opts.Location != nil {
		return s.opts.Location
	}
	return time.UTC
}
