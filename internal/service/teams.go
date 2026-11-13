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

	"telegram-ai-bot/internal/telegram"
)

// /teams splits a group's MLBB players into two teams of five, at random,
// without the model. It opens a lobby whose buttons let members join and
// leave; once ten are in, whoever opened it, or an admin, splits them: ten
// of them at random into the teams and any others to the bench, and may
// split again. The lobby is kept in the database, so that presses at once
// neither lose a member nor split twice; a newer /teams closes the one
// before.

// teamsData starts the data of a lobby's buttons: "tm:j:<token>" joins,
// "tm:l:<token>" leaves, "tm:s:<token>" splits, or splits again.
const teamsData = "tm:"

const (
	// teamSize is the players of a side, as MLBB has.
	teamSize = 5
	// teamsMax bounds a lobby, so that its message stays short.
	teamsMax = 30
)

// TeamLobbyBook keeps each group's lobby.
type TeamLobbyBook interface {
	ChangeTeamLobby(ctx context.Context, chatID int64, change func(state []byte) ([]byte, error)) error
}

// teamLobby is a group's lobby: its token, which its buttons carry, who
// opened it and in which language, who joined, and once split its teams.
type teamLobby struct {
	Token   string       `json:"token"`
	Owner   int64        `json:"owner"`
	Lang    string       `json:"lang"`
	Players []teamPlayer `json:"players"`
	Split   bool         `json:"split"`
	TeamA   []teamPlayer `json:"team_a,omitempty"`
	TeamB   []teamPlayer `json:"team_b,omitempty"`
	Bench   []teamPlayer `json:"bench,omitempty"`
}

type teamPlayer struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Lane is the MLBB lane the split gave a player of a team.
	Lane string `json:"lane,omitempty"`
}

// mlbbLanes are MLBB's five lanes, one for each player of a team.
var mlbbLanes = []string{"⚔️ EXP", "🌲 Jungle", "🔮 Mid", "💰 Gold", "🛡 Roam"}

// teamsCommand opens a lobby in a group, its opener in it.
func (s *Service) teamsCommand(ctx context.Context, in incoming) outcome {
	if !in.group {
		return say(textTeamsGroupOnly.to(in))
	}
	lobby := teamLobby{
		Token:   strconv.FormatInt(rand.Int64N(1<<40), 36),
		Owner:   in.userID,
		Lang:    appLanguage(in.msg.From),
		Players: []teamPlayer{{ID: in.userID, Name: doozName(in.msg.From)}},
	}
	err := s.opts.Teams.ChangeTeamLobby(ctx, in.msg.Chat.ID, func([]byte) ([]byte, error) { return json.Marshal(lobby) })
	if err != nil {
		s.opts.Logger.Error("could not open a team lobby", "chat_id", in.msg.Chat.ID, "error", err)
		return say(textError.to(in))
	}
	return outcome{text: teamsText(lobby), buttons: teamsButtons(lobby)}
}

// errTeamsRefused carries the reason a press is refused, shown to the
// presser alone.
type errTeamsRefused struct{ reason string }

func (e errTeamsRefused) Error() string { return e.reason }

// pressedTeams answers a press of a lobby's button and shows the lobby as
// it then is; a press refused is told to the presser alone.
func (s *Service) pressedTeams(q *telegram.CallbackQuery) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	action, token, _ := strings.Cut(strings.TrimPrefix(q.Data, teamsData), ":")
	var lobby teamLobby
	err := s.opts.Teams.ChangeTeamLobby(ctx, q.Message.Chat.ID, func(state []byte) ([]byte, error) {
		if err := json.Unmarshal(state, &lobby); err != nil {
			return nil, err
		}
		if lobby.Token == "" || lobby.Token != token {
			return nil, errTeamsRefused{textTeamsClosed.inLanguage(appLanguage(q.From))}
		}
		if reason := s.teamsPress(ctx, q, &lobby, action); reason != "" {
			return nil, errTeamsRefused{reason}
		}
		return json.Marshal(lobby)
	})
	var refused errTeamsRefused
	switch {
	case errors.As(err, &refused):
		s.answerTeams(ctx, q, refused.reason)
		return
	case err != nil:
		s.opts.Logger.Warn("could not change a team lobby", "chat_id", q.Message.Chat.ID, "error", err)
		s.answerTeams(ctx, q, textTeamsUnavailable.inLanguage(appLanguage(q.From)))
		return
	}
	s.answerTeams(ctx, q, "")
	if err := s.opts.Buttons.EditMessageMenu(ctx, q.Message.Chat.ID, q.Message.MessageID, teamsText(lobby), teamsButtons(lobby)); err != nil {
		s.opts.Logger.Warn("could not show a team lobby", "chat_id", q.Message.Chat.ID, "error", err)
	}
}

func (s *Service) answerTeams(ctx context.Context, q *telegram.CallbackQuery, reason string) {
	if err := s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, reason, reason != ""); err != nil {
		s.opts.Logger.Debug("could not answer a team lobby press", "user_id", q.From.ID, "error", err)
	}
}

// teamsPress changes the lobby as action asks, or says why it may not.
func (s *Service) teamsPress(ctx context.Context, q *telegram.CallbackQuery, lobby *teamLobby, action string) string {
	lang := appLanguage(q.From)
	in := slices.IndexFunc(lobby.Players, func(p teamPlayer) bool { return p.ID == q.From.ID })
	switch action {
	case "j":
		if in >= 0 {
			return textTeamsAlreadyIn.inLanguage(lang)
		}
		if len(lobby.Players) >= teamsMax {
			return fmt.Sprintf(textTeamsFull.inLanguage(lang), teamsMax)
		}
		lobby.Players = append(lobby.Players, teamPlayer{ID: q.From.ID, Name: doozName(q.From)})
		lobby.unsplit()
	case "l":
		if in < 0 {
			return textTeamsNotIn.inLanguage(lang)
		}
		lobby.Players = slices.Delete(lobby.Players, in, in+1)
		lobby.unsplit()
	case "s":
		if q.From.ID != lobby.Owner && !s.isOwner(q.From) && !s.adminPressed(ctx, q) {
			return textTeamsOwnerOnly.inLanguage(lang)
		}
		if len(lobby.Players) < 2*teamSize {
			return fmt.Sprintf(textTeamsTooFew.inLanguage(lang), 2*teamSize-len(lobby.Players))
		}
		lobby.split()
	default:
		return textTeamsClosed.inLanguage(lang)
	}
	return ""
}

// adminPressed reports a press by one of the group's admins, who may split
// a lobby whose opener is away.
func (s *Service) adminPressed(ctx context.Context, q *telegram.CallbackQuery) bool {
	admin, err := s.opts.Members.IsGroupAdmin(ctx, q.Message.Chat.ID, q.From.ID)
	return err == nil && admin
}

// split puts ten of the players, at random, into two teams of five, each
// with a lane of MLBB's five given at random, and the rest on the bench.
func (l *teamLobby) split() {
	shuffled := slices.Clone(l.Players)
	rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	l.TeamA = withLanes(shuffled[:teamSize])
	l.TeamB = withLanes(shuffled[teamSize : 2*teamSize])
	l.Bench = shuffled[2*teamSize:]
	l.Split = true
}

// withLanes gives each player of a team one of the lanes, at random, and
// lists them in the lanes' order.
func withLanes(team []teamPlayer) []teamPlayer {
	lanes := rand.Perm(len(mlbbLanes))
	laned := make([]teamPlayer, len(team))
	for i, p := range team {
		p.Lane = mlbbLanes[lanes[i]]
		laned[lanes[i]] = p
	}
	return laned
}

// unsplit takes the teams back when the players change.
func (l *teamLobby) unsplit() {
	l.Split, l.TeamA, l.TeamB, l.Bench = false, nil, nil, nil
}

// teamsText is the lobby's message: who joined, or the teams.
func teamsText(l teamLobby) string {
	names := func(players []teamPlayer) string {
		lines := make([]string, len(players))
		for i, p := range players {
			lines[i] = strconv.Itoa(i+1) + ". " + cardMarks.Replace(p.Name)
			if p.Lane != "" {
				lines[i] = p.Lane + " — " + cardMarks.Replace(p.Name)
			}
		}
		return strings.Join(lines, "\n")
	}
	if l.Split {
		text := textTeamsSplitTitle.inLanguage(l.Lang) + "\n\n🔵 " + textTeamsTeamA.inLanguage(l.Lang) + "\n" + names(l.TeamA) +
			"\n\n🔴 " + textTeamsTeamB.inLanguage(l.Lang) + "\n" + names(l.TeamB)
		if len(l.Bench) > 0 {
			text += "\n\n🪑 " + textTeamsBench.inLanguage(l.Lang) + "\n" + names(l.Bench)
		}
		return text
	}
	text := textTeamsTitle.inLanguage(l.Lang) + "\n\n" + fmt.Sprintf(textTeamsJoined.inLanguage(l.Lang), len(l.Players), 2*teamSize) + "\n" + names(l.Players) + "\n\n"
	if len(l.Players) < 2*teamSize {
		return text + fmt.Sprintf(textTeamsNeed.inLanguage(l.Lang), 2*teamSize)
	}
	return text + textTeamsReady.inLanguage(l.Lang)
}

// teamsButtons are the lobby's buttons: join and leave, and once ten are
// in, split, or split again.
func teamsButtons(l teamLobby) [][]telegram.Button {
	rows := [][]telegram.Button{{
		{Text: textTeamsJoin.inLanguage(l.Lang), Data: teamsData + "j:" + l.Token},
		{Text: textTeamsLeave.inLanguage(l.Lang), Data: teamsData + "l:" + l.Token},
	}}
	switch {
	case l.Split:
		rows = append(rows, []telegram.Button{{Text: textTeamsAgain.inLanguage(l.Lang), Data: teamsData + "s:" + l.Token}})
	case len(l.Players) >= 2*teamSize:
		rows = append(rows, []telegram.Button{{Text: textTeamsSplit.inLanguage(l.Lang), Data: teamsData + "s:" + l.Token}})
	}
	return rows
}

var (
	textTeamsTitle       = localText{uz: "🎮 MLBB: jamoalarga bo'lish", ru: "🎮 MLBB: деление на команды", en: "🎮 MLBB: split into teams"}
	textTeamsSplitTitle  = localText{uz: "🎮 MLBB jamoalari (tasodifiy)", ru: "🎮 Команды MLBB (случайно)", en: "🎮 MLBB teams (random)"}
	textTeamsJoined      = localText{uz: "Qo'shilganlar (%d/%d):", ru: "Записались (%d/%d):", en: "Joined (%d/%d):"}
	textTeamsNeed        = localText{uz: "Kamida %d kishi kerak: «Qo'shilish»ni bosing.", ru: "Нужно минимум %d человек: нажмите «Записаться».", en: "At least %d are needed: press «Join»."}
	textTeamsReady       = localText{uz: "Hammasi tayyor: lobbini ochgan kishi yoki admin «Bo'lish»ni bossin.", ru: "Всё готово: тот, кто открыл лобби, или админ жмёт «Разделить».", en: "Ready: whoever opened the lobby, or an admin, presses «Split»."}
	textTeamsTeamA       = localText{uz: "1-jamoa", ru: "Команда 1", en: "Team 1"}
	textTeamsTeamB       = localText{uz: "2-jamoa", ru: "Команда 2", en: "Team 2"}
	textTeamsBench       = localText{uz: "Zaxira", ru: "Запасные", en: "Bench"}
	textTeamsJoin        = localText{uz: "➕ Qo'shilish", ru: "➕ Записаться", en: "➕ Join"}
	textTeamsLeave       = localText{uz: "➖ Chiqish", ru: "➖ Выйти", en: "➖ Leave"}
	textTeamsSplit       = localText{uz: "🎲 Bo'lish", ru: "🎲 Разделить", en: "🎲 Split"}
	textTeamsAgain       = localText{uz: "🔀 Qayta bo'lish", ru: "🔀 Разделить заново", en: "🔀 Split again"}
	textTeamsAlreadyIn   = localText{uz: "Siz allaqachon ro'yxatdasiz.", ru: "Вы уже в списке.", en: "You are in already."}
	textTeamsNotIn       = localText{uz: "Siz ro'yxatda yo'qsiz.", ru: "Вас нет в списке.", en: "You are not in the list."}
	textTeamsFull        = localText{uz: "Ro'yxat to'lgan: %d kishi.", ru: "Список полон: %d человек.", en: "The list is full: %d players."}
	textTeamsOwnerOnly   = localText{uz: "Faqat lobbini ochgan kishi yoki admin bo'la oladi.", ru: "Делить может только открывший лобби или админ.", en: "Only whoever opened the lobby, or an admin, may split it."}
	textTeamsTooFew      = localText{uz: "Yana %d kishi kerak.", ru: "Нужно ещё %d человек.", en: "%d more are needed."}
	textTeamsClosed      = localText{uz: "Bu lobbi yopilgan: yangisini /teams bilan oching.", ru: "Это лобби закрыто: откройте новое через /teams.", en: "This lobby is closed: open a new one with /teams."}
	textTeamsUnavailable = localText{uz: "Hozir bo'lmadi, yana bosing.", ru: "Сейчас не вышло, нажмите ещё раз.", en: "That did not work; press again."}
	textTeamsGroupOnly   = localText{uz: "🎮 /teams guruhda ishlaydi.", ru: "🎮 /teams работает в группе.", en: "🎮 /teams works in a group."}
)
