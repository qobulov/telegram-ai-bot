// Package mafia is the bot's Mafia: the members of a group play one game at
// a time from its Telegram game message. Each gets a secret role; at night
// the roles that act pick their targets on the page, by day the group
// argues in its chat and votes one player out, until the town has found
// every killer or the killers have the town.
//
// Only the server knows the roles: a page is told its own, its mafia
// partners', the roles of the dead and what its checks found. The phases
// end when their time is up, on the first request after it, or sooner when
// everyone who acts has acted.
package mafia

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Phase is where a game is.
type Phase string

// The phases: players join in the lobby; the night, the day and the vote
// take turns until a side wins and the game is over.
const (
	Lobby Phase = "lobby"
	Night Phase = "night"
	Day   Phase = "day"
	Vote  Phase = "vote"
	Over  Phase = "over"
)

// How many may play, and how long each phase lasts; a lobby no one starts
// closes after lobbyFor.
const (
	MinPlayers = 5
	MaxPlayers = 20
	nightFor   = 45 * time.Second
	dayFor     = 75 * time.Second
	voteFor    = 40 * time.Second
	lobbyFor   = 20 * time.Minute
)

// The roles.
const (
	Citizen   = "tinch"
	Mafioso   = "mafiya"
	Don       = "don"
	Commissar = "komissar"
	Doctor    = "shifokor"
	Courtesan = "putana"
	Maniac    = "manyak"
	Lucky     = "omadli"
)

// Team is a side that can win.
type Team string

// The sides: the town, the mafia, and the maniac alone.
const (
	Town  Team = "town"
	Mafia Team = "mafia"
	Solo  Team = "solo"
)

// The night's acts: the mafia's kill, the Don's search for the commissar,
// the commissar's check, the doctor's cure, the courtesan's visit and the
// maniac's kill.
const (
	actKill   = "kill"
	actFind   = "find"
	actCheck  = "check"
	actHeal   = "heal"
	actBlock  = "block"
	actManiac = "maniac"
)

var (
	teams = map[string]Team{Citizen: Town, Commissar: Town, Doctor: Town, Courtesan: Town, Lucky: Town, Mafioso: Mafia, Don: Mafia, Maniac: Solo}
	acts  = map[string][]string{Don: {actKill, actFind}, Mafioso: {actKill}, Commissar: {actCheck}, Doctor: {actHeal}, Courtesan: {actBlock}, Maniac: {actManiac}}
	// lineup is the roles dealt to n players, the first n of it, before
	// they are shuffled: a mafia member in every four or five.
	lineup = []string{Don, Commissar, Doctor, Citizen, Citizen, Mafioso, Courtesan, Maniac, Lucky, Citizen,
		Mafioso, Citizen, Citizen, Citizen, Mafioso, Citizen, Citizen, Citizen, Mafioso, Citizen}
	roleNames = map[string]string{Citizen: "👱‍♂️ Tinch aholi", Mafioso: "🤵 Mafiya", Don: "🤵‍♂️ Don", Commissar: "🕵️‍♂️ Komissar",
		Doctor: "👨‍⚕️ Shifokor", Courtesan: "💃 Putana", Maniac: "🦹‍♂️ Manyak", Lucky: "🍀 Omadli"}
)

var (
	ErrStarted   = errors.New("mafia: the game has started")
	ErrFull      = errors.New("mafia: the game is full")
	ErrNotIn     = errors.New("mafia: not a player of the game")
	ErrTooFew    = errors.New("mafia: too few players")
	ErrPhase     = errors.New("mafia: not now")
	ErrDead      = errors.New("mafia: the player is out")
	ErrNoAct     = errors.New("mafia: the role cannot do that")
	ErrTarget    = errors.New("mafia: not a target for that")
	ErrRepeating = errors.New("mafia: the doctor cured them last night")
)

// Player is one who plays: their role, whether they are still in, and for
// the lucky one whether their luck is still to come.
type Player struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role,omitempty"`
	Alive bool   `json:"alive"`
	Luck  bool   `json:"luck,omitempty"`
}

// Game is a chat's game as it is kept: the phase and when it ends, the
// players in the order they joined, the night's acts as act → player →
// target, the vote as voter → target, each checker's findings, whom the
// doctor cured last, and what the last night or vote brought. Seq counts
// the phases and Round the games started, for the pages.
type Game struct {
	Round   int                        `json:"round"`
	Phase   Phase                      `json:"phase"`
	Day     int                        `json:"day"`
	Ends    time.Time                  `json:"ends"`
	Seq     int                        `json:"seq"`
	Players []Player                   `json:"players"`
	Acts    map[string]map[int64]int64 `json:"acts,omitempty"`
	Votes   map[int64]int64            `json:"votes,omitempty"`
	Known   map[int64]map[int64]string `json:"known,omitempty"`
	Cured   int64                      `json:"cured,omitempty"`
	News    []string                   `json:"news,omitempty"`
	Winner  Team                       `json:"winner,omitempty"`
}

// Event is what happened when a game moved on, for the group: the news to
// tell it, and when the game ended, the side that won and its players.
type Event struct {
	Text    string
	Winner  Team
	Winners []int64
}

// TeamOf is a role's side.
func TeamOf(role string) Team { return teams[role] }

// RoleName is a role as the players read it.
func RoleName(role string) string { return roleNames[role] }

func (g *Game) player(id int64) *Player {
	for i := range g.Players {
		if g.Players[i].ID == id {
			return &g.Players[i]
		}
	}
	return nil
}

func (g *Game) alive() []*Player {
	var live []*Player
	for i := range g.Players {
		if g.Players[i].Alive {
			live = append(live, &g.Players[i])
		}
	}
	return live
}

func (g *Game) next(phase Phase, now time.Time, lasts time.Duration) {
	g.Phase, g.Ends = phase, now.Add(lasts)
	g.Seq++
}

// Join adds a player to the lobby, opening a new one when there is no game
// or the last is over; joining again changes nothing.
func (g *Game) Join(id int64, name string, now time.Time) error {
	if g.Phase == "" || g.Phase == Over {
		*g = Game{Seq: g.Seq, Round: g.Round}
		g.next(Lobby, now, lobbyFor)
	}
	if g.Phase != Lobby {
		return ErrStarted
	}
	if g.player(id) != nil {
		return nil
	}
	if len(g.Players) >= MaxPlayers {
		return ErrFull
	}
	g.Players = append(g.Players, Player{ID: id, Name: name, Alive: true})
	return nil
}

// Leave takes a player out of the lobby.
func (g *Game) Leave(id int64) error {
	if g.Phase != Lobby {
		return ErrPhase
	}
	for i, p := range g.Players {
		if p.ID == id {
			g.Players = append(g.Players[:i], g.Players[i+1:]...)
			return nil
		}
	}
	return ErrNotIn
}

// Start deals the roles, shuffled with intn, and begins the first night;
// any player of the lobby may start it once enough have joined.
func (g *Game) Start(id int64, now time.Time, intn func(n int) int) error {
	switch {
	case g.Phase != Lobby:
		return ErrPhase
	case g.player(id) == nil:
		return ErrNotIn
	case len(g.Players) < MinPlayers:
		return ErrTooFew
	}
	roles := append([]string(nil), lineup[:len(g.Players)]...)
	for i := len(roles) - 1; i > 0; i-- {
		j := intn(i + 1)
		roles[i], roles[j] = roles[j], roles[i]
	}
	for i := range g.Players {
		g.Players[i].Role, g.Players[i].Alive, g.Players[i].Luck = roles[i], true, roles[i] == Lucky
	}
	g.Day, g.News, g.Cured = 1, nil, 0
	g.Round++
	g.Acts, g.Known = map[string]map[int64]int64{}, map[int64]map[int64]string{}
	g.next(Night, now, nightFor)
	return nil
}

// Pending is what a player has still to do this night: their role's acts
// they have not done, in order.
func (g *Game) Pending(id int64) []string {
	p := g.player(id)
	if g.Phase != Night || p == nil || !p.Alive {
		return nil
	}
	var left []string
	for _, act := range acts[p.Role] {
		if _, done := g.Acts[act][id]; !done {
			left = append(left, act)
		}
	}
	return left
}

// Targets is whom a player may pick for an act: the living, but not
// themselves unless the doctor cures, not their partners for the mafia's
// kill, and not whom the doctor cured last night.
func (g *Game) Targets(id int64, act string) []int64 {
	var ids []int64
	for _, t := range g.alive() {
		if g.canTarget(id, act, t) {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

func (g *Game) canTarget(id int64, act string, t *Player) bool {
	switch {
	case !t.Alive:
		return false
	case act == actHeal:
		return t.ID != g.Cured
	case t.ID == id:
		return false
	case act == actKill:
		return TeamOf(t.Role) != Mafia
	}
	return true
}

// Act is a player's pick for one of their role's acts this night; they may
// change it until dawn.
func (g *Game) Act(id int64, act string, target int64) error {
	p, t := g.player(id), g.player(target)
	switch {
	case g.Phase != Night:
		return ErrPhase
	case p == nil:
		return ErrNotIn
	case !p.Alive:
		return ErrDead
	case !hasAct(p.Role, act):
		return ErrNoAct
	case t == nil:
		return ErrTarget
	case act == actHeal && target == g.Cured:
		return ErrRepeating
	case !g.canTarget(id, act, t):
		return ErrTarget
	}
	if g.Acts == nil {
		g.Acts = map[string]map[int64]int64{}
	}
	if g.Acts[act] == nil {
		g.Acts[act] = map[int64]int64{}
	}
	g.Acts[act][id] = target
	return nil
}

func hasAct(role, act string) bool { return slices.Contains(acts[role], act) }

// Vote is a living player's vote against another; they may change it until
// the vote ends.
func (g *Game) Vote(id, target int64) error {
	p, t := g.player(id), g.player(target)
	switch {
	case g.Phase != Vote:
		return ErrPhase
	case p == nil:
		return ErrNotIn
	case !p.Alive:
		return ErrDead
	case t == nil || !t.Alive || target == id:
		return ErrTarget
	}
	if g.Votes == nil {
		g.Votes = map[int64]int64{}
	}
	g.Votes[id] = target
	return nil
}

// Advance moves the game on as far as now allows: a night ends when its
// time is up or everyone who acts has acted, a day when its time is up, a
// vote when its time is up or everyone has voted. It gives what the group
// is to be told.
func (g *Game) Advance(now time.Time) []Event {
	var events []Event
	for {
		switch {
		case g.Phase == Lobby && !now.Before(g.Ends):
			*g = Game{Seq: g.Seq + 1, Round: g.Round}
			return append(events, Event{Text: "🎭 Mafia xonasi yopildi: o'yin boshlanmadi."})
		case g.Phase == Night && (!now.Before(g.Ends) || g.allActed()):
			events = append(events, g.dawn(now)...)
		case g.Phase == Day && !now.Before(g.Ends):
			g.Votes = map[int64]int64{}
			g.next(Vote, now, voteFor)
			events = append(events, Event{Text: "🗳 Ovoz berish boshlandi! Kimni shahardan chiqaramiz? O'yin sahifasida ovoz bering."})
		case g.Phase == Vote && (!now.Before(g.Ends) || len(g.Votes) >= len(g.alive())):
			events = append(events, g.verdict(now)...)
		default:
			return events
		}
	}
}

func (g *Game) allActed() bool {
	for _, p := range g.alive() {
		if len(g.Pending(p.ID)) > 0 {
			return false
		}
	}
	return true
}

// actor is the living player of a role, the first if there are several.
func (g *Game) actor(role string) *Player {
	for _, p := range g.alive() {
		if p.Role == role {
			return p
		}
	}
	return nil
}

// dawn plays the night out in its order: the courtesan's visit keeps its
// host from acting; then the doctor's cure, the checks, the kills, and the
// lucky one's luck. The mafia's victim is the Don's pick, or when the Don
// is gone, kept away or picked no one, another member's.
func (g *Game) dawn(now time.Time) []Event {
	var blocked int64
	if host := g.pick(actBlock, g.actor(Courtesan)); host != nil {
		blocked = host.ID
	}
	free := func(p *Player) *Player {
		if p == nil || p.ID == blocked {
			return nil
		}
		return p
	}
	cured := g.pick(actHeal, free(g.actor(Doctor)))
	g.Cured = 0
	if cured != nil {
		g.Cured = cured.ID
	}
	if kom := free(g.actor(Commissar)); kom != nil {
		if t := g.pick(actCheck, kom); t != nil {
			g.learn(kom.ID, t.ID, map[bool]string{true: "mafia", false: "town"}[TeamOf(t.Role) == Mafia])
		}
	}
	if don := free(g.actor(Don)); don != nil {
		if t := g.pick(actFind, don); t != nil {
			g.learn(don.ID, t.ID, map[bool]string{true: "commissar", false: "not"}[t.Role == Commissar])
		}
	}
	victim := g.pick(actKill, free(g.actor(Don)))
	for _, p := range g.alive() {
		if victim == nil && p.Role == Mafioso {
			victim = g.pick(actKill, free(p))
		}
	}
	var news []string
	seen := map[int64]bool{}
	for _, t := range []*Player{victim, g.pick(actManiac, free(g.actor(Maniac)))} {
		if t == nil || seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		switch {
		case cured != nil && cured.ID == t.ID:
			news = append(news, "💉 Shifokor kimnidir o'limdan qutqarib qoldi!")
		case t.Luck:
			t.Luck = false
			news = append(news, "🍀 Kimgadir omad kulib boqdi — hujumdan omon qoldi!")
		default:
			t.Alive = false
			news = append(news, fmt.Sprintf("💀 %s o'ldirildi. U %s edi.", t.Name, RoleName(t.Role)))
		}
	}
	if len(news) == 0 {
		news = append(news, "🙏 Bu kecha hech kim o'lmadi.")
	}
	g.News = append([]string{fmt.Sprintf("☀️ Tong otdi (%d-kun).", g.Day)}, news...)
	g.Acts = map[string]map[int64]int64{}
	events := []Event{{Text: strings.Join(g.News, "\n")}}
	if end := g.end(); end != nil {
		return append(events, *end)
	}
	g.next(Day, now, dayFor)
	return append(events, Event{Text: "☀️ Bahslashing: kim mafiya? Ovoz berish birozdan keyin boshlanadi."})
}

// pick is whom a player picked for an act this night, nil for no one.
func (g *Game) pick(act string, p *Player) *Player {
	if p == nil {
		return nil
	}
	target, ok := g.Acts[act][p.ID]
	if !ok {
		return nil
	}
	return g.player(target)
}

func (g *Game) learn(checker, target int64, what string) {
	if g.Known == nil {
		g.Known = map[int64]map[int64]string{}
	}
	if g.Known[checker] == nil {
		g.Known[checker] = map[int64]string{}
	}
	g.Known[checker][target] = what
}

// verdict counts the votes: the one with the most is out, and on a tie no
// one is.
func (g *Game) verdict(now time.Time) []Event {
	tally := map[int64]int{}
	for _, t := range g.Votes {
		if p := g.player(t); p != nil && p.Alive {
			tally[t]++
		}
	}
	ids := make([]int64, 0, len(tally))
	for id := range tally {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var top int64
	most, tie := 0, false
	for _, id := range ids {
		switch n := tally[id]; {
		case n > most:
			top, most, tie = id, n, false
		case n == most:
			tie = true
		}
	}
	if most == 0 || tie {
		g.News = []string{"⚖️ Ovozlar teng bo'ldi — bugun hech kim chiqarilmadi."}
	} else {
		p := g.player(top)
		p.Alive = false
		g.News = []string{fmt.Sprintf("⚖️ %s %d ta ovoz bilan shahardan chiqarildi. U %s edi.", p.Name, most, RoleName(p.Role))}
	}
	g.Votes = map[int64]int64{}
	events := []Event{{Text: g.News[0]}}
	if end := g.end(); end != nil {
		return append(events, *end)
	}
	g.Day++
	g.next(Night, now, nightFor)
	return append(events, Event{Text: fmt.Sprintf("🌙 %d-tun. Shahar uxlaydi, rollar uyg'onadi…", g.Day)})
}

// end ends the game when a side has won: the town when no killer is left,
// the maniac when they are left with at most one other and no mafia, the
// mafia when they are as many as the rest and the maniac is gone.
func (g *Game) end() *Event {
	live := g.alive()
	mafia, maniac := 0, false
	for _, p := range live {
		switch TeamOf(p.Role) {
		case Mafia:
			mafia++
		case Solo:
			maniac = true
		}
	}
	switch {
	case mafia == 0 && !maniac:
		g.Winner = Town
	case maniac && mafia == 0 && len(live) <= 2:
		g.Winner = Solo
	case !maniac && mafia >= len(live)-mafia:
		g.Winner = Mafia
	default:
		return nil
	}
	g.Phase = Over
	g.Seq++
	var winners []int64
	lines := []string{map[Team]string{Town: "🏛 Tinch aholi yutdi!", Mafia: "🩸 Mafiya yutdi!", Solo: "🦹‍♂️ Manyak yutdi!"}[g.Winner], "", "Rollar:"}
	for _, p := range g.Players {
		if TeamOf(p.Role) == g.Winner {
			winners = append(winners, p.ID)
		}
		mark := ""
		if !p.Alive {
			mark = " 💀"
		}
		lines = append(lines, fmt.Sprintf("%s — %s%s", p.Name, RoleName(p.Role), mark))
	}
	return &Event{Text: strings.Join(lines, "\n"), Winner: g.Winner, Winners: winners}
}
