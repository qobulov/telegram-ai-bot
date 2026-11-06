// Package maydon is Maydon, the bot's team battle: up to five members of a
// group against five, each a fighter of a class, their strength from their
// Dev jangi levels. In the fight each player picks a target and strikes as
// their attack comes round, harder when they strike right on time; a
// skill fires when their energy is full, a block takes most of a blow, and
// a fallen fighter comes back once. A fighter whose player does nothing
// strikes by itself, weaker and slower, so that no team plays one short.
//
// The server plays the fight: each request plays it on to the moment it
// came, in steps of 50 ms, then does what the player asked, so every page
// shows the same fight and no one can strike more often than their class.
package maydon

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Phase is where a room is.
type Phase string

// The phases: fighters join in the lobby; the battle; the end.
const (
	Lobby  Phase = "lobby"
	Battle Phase = "battle"
	Over   Phase = "over"
)

// How many may fight, five a side at most, and the fight's timing in
// milliseconds: its length, a step of it, the window after an attack comes
// round in which a blow is a precise one, what striking early costs, how
// long an idle fighter waits before striking by itself, the block and its
// wait, and the wait to come back.
const (
	MaxPlayers = 10
	MinPlayers = 2
	lobbyFor   = 15 * time.Minute
	battleMs   = 90000
	stepMs     = 50
	perfectMs  = 450
	earlyMs    = 150
	idleMs     = 3000
	blockMs    = 1500
	blockEvery = 6000
	reviveMs   = 8000
	maxHits    = 80
)

// The multipliers of a precise blow, of an idle fighter's own, and of a
// critical one.
const (
	perfectTimes = 1.6
	idleTimes    = 0.6
	critTimes    = 2
)

// Class is what a fighter is: their life and blow, how often they strike,
// and whether they stand in front.
type Class struct {
	HP, Atk, Every int
	Front          bool
}

// The classes.
const (
	Warrior = "jangchi"
	Archer  = "kamonchi"
	Mage    = "sehrgar"
	Healer  = "tabib"
)

var classes = map[string]Class{
	Warrior: {HP: 2100, Atk: 62, Every: 1000, Front: true},
	Archer:  {HP: 1150, Atk: 78, Every: 900},
	Mage:    {HP: 1050, Atk: 56, Every: 1300},
	Healer:  {HP: 1250, Atk: 32, Every: 1150},
}

var (
	ErrStarted = errors.New("maydon: the battle has started")
	ErrFull    = errors.New("maydon: the room is full")
	ErrClass   = errors.New("maydon: no such class")
	ErrNotIn   = errors.New("maydon: not a fighter of the room")
	ErrTooFew  = errors.New("maydon: too few fighters")
	ErrPhase   = errors.New("maydon: not now")
	ErrDead    = errors.New("maydon: the fighter is down")
	ErrEarly   = errors.New("maydon: the attack is not ready")
	ErrEnergy  = errors.New("maydon: the energy is not full")
	ErrBlock   = errors.New("maydon: the block is not ready")
	ErrTarget  = errors.New("maydon: not a target")
)

// Fighter is a player in a room: their class and levels, and in the
// battle their side and place, life, how long until their attack comes
// round (below zero, how long it has been ready), energy, block, whether
// they came back yet, their target, and what they did.
type Fighter struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Class      string `json:"class"`
	Sword      int    `json:"sword"`
	Crit       int    `json:"crit"`
	Team       int    `json:"team"`
	Col        int    `json:"col"`
	Row        int    `json:"row"`
	HP         int    `json:"hp"`
	Max        int    `json:"max"`
	CD         int    `json:"cd"`
	Energy     int    `json:"energy"`
	Alive      bool   `json:"alive"`
	BlockUntil int    `json:"block_until"`
	BlockReady int    `json:"block_ready"`
	Revived    bool   `json:"revived"`
	ReviveAt   int    `json:"revive_at"`
	Target     int64  `json:"target"`
	Dmg        int    `json:"dmg"`
	Heal       int    `json:"heal"`
}

// Hit is one thing that happened in the battle, for the pages to draw: a
// blow, a cure, a fall or a return, from whom to whom, at T ms.
type Hit struct {
	ID      int    `json:"id"`
	T       int    `json:"t"`
	From    int64  `json:"from"`
	To      int64  `json:"to"`
	Amount  int    `json:"amount"`
	Kind    string `json:"kind"`
	Crit    bool   `json:"crit,omitempty"`
	Perfect bool   `json:"perfect,omitempty"`
	Blocked bool   `json:"blocked,omitempty"`
}

// Room is a group's room as it is kept: the phase, when the lobby closes or
// the battle began, the battle's ms played so far, its random state, the
// fighters, the last hits, and at the end the winning side (0 red, 1 blue,
// -1 a draw) and the best fighter. Round counts the battles, Seq the
// changes of phase.
type Room struct {
	Phase    Phase     `json:"phase"`
	Round    int       `json:"round"`
	Seq      int       `json:"seq"`
	Ends     time.Time `json:"ends"`
	Started  time.Time `json:"started"`
	T        int       `json:"t"`
	Random   uint32    `json:"random"`
	Fighters []Fighter `json:"fighters"`
	Hits     []Hit     `json:"hits,omitempty"`
	NextHit  int       `json:"next_hit"`
	Winner   int       `json:"winner"`
	Best     int64     `json:"best"`
}

// Event is what the group is to be told, and when a battle ends its
// winners.
type Event struct {
	Text    string
	Winners []int64
}

// Stats is what a fighter's Dev jangi levels give them: 3% more blows a
// sword level, 1% more life a level of either, and 5% critical blows and 2%
// more a crit level; power sums it up to balance the teams with.
func Stats(sword, crit int) (atk, life float64, critPct, power int) {
	atk = 1 + 0.03*float64(max(sword, 1)-1)
	life = 1 + 0.01*float64(max(sword, 1)-1+max(crit, 0))
	critPct = 5 + 2*max(crit, 0)
	return atk, life, critPct, int(100 * atk * life * (1 + float64(critPct)/100))
}

func (r *Room) fighter(id int64) *Fighter {
	for i := range r.Fighters {
		if r.Fighters[i].ID == id {
			return &r.Fighters[i]
		}
	}
	return nil
}

func (r *Room) next(phase Phase) {
	r.Phase = phase
	r.Seq++
}

// Join adds a fighter of a class to the lobby with their Dev jangi levels,
// opening a new room when there is none or the last battle is over; one
// who is in already changes their class.
func (r *Room) Join(id int64, name, class string, sword, crit int, now time.Time) error {
	if _, ok := classes[class]; !ok {
		return ErrClass
	}
	if r.Phase == "" || r.Phase == Over {
		*r = Room{Seq: r.Seq, Round: r.Round, Winner: -1}
		r.next(Lobby)
		r.Ends = now.Add(lobbyFor)
	}
	if r.Phase != Lobby {
		return ErrStarted
	}
	if f := r.fighter(id); f != nil {
		f.Class, f.Sword, f.Crit = class, sword, crit
		return nil
	}
	if len(r.Fighters) >= MaxPlayers {
		return ErrFull
	}
	r.Fighters = append(r.Fighters, Fighter{ID: id, Name: name, Class: class, Sword: sword, Crit: crit})
	return nil
}

// Leave takes a fighter out of the lobby.
func (r *Room) Leave(id int64) error {
	if r.Phase != Lobby {
		return ErrPhase
	}
	for i, f := range r.Fighters {
		if f.ID == id {
			r.Fighters = append(r.Fighters[:i], r.Fighters[i+1:]...)
			return nil
		}
	}
	return ErrNotIn
}

func power(f Fighter) int {
	_, _, _, p := Stats(f.Sword, f.Crit)
	return p
}

// Start splits the lobby into two sides of as much power, the strongest
// left joining the weaker side, places each side's warriors in front and
// the rest behind, and begins the battle; seed starts its randomness.
func (r *Room) Start(id int64, now time.Time, seed uint32) error {
	switch {
	case r.Phase != Lobby:
		return ErrPhase
	case r.fighter(id) == nil:
		return ErrNotIn
	case len(r.Fighters) < MinPlayers:
		return ErrTooFew
	}
	order := make([]int, len(r.Fighters))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return power(r.Fighters[order[a]]) > power(r.Fighters[order[b]]) })
	var size, sum [2]int
	half := (len(r.Fighters) + 1) / 2
	for _, i := range order {
		team := 0
		if size[0] >= half || (size[1] < half && sum[1] < sum[0]) {
			team = 1
		}
		r.Fighters[i].Team = team
		size[team]++
		sum[team] += power(r.Fighters[i])
	}
	for team := range 2 {
		used := map[[2]int]bool{}
		for i := range r.Fighters {
			f := &r.Fighters[i]
			if f.Team == team {
				f.Row, f.Col = place(used, classes[f.Class].Front)
			}
		}
	}
	for i := range r.Fighters {
		f := &r.Fighters[i]
		_, life, _, _ := Stats(f.Sword, f.Crit)
		*f = Fighter{ID: f.ID, Name: f.Name, Class: f.Class, Sword: f.Sword, Crit: f.Crit, Team: f.Team, Col: f.Col, Row: f.Row,
			Max: int(float64(classes[f.Class].HP) * life), Alive: true, CD: 600}
		f.HP = f.Max
	}
	r.Round++
	r.Started, r.T, r.Random, r.Hits, r.NextHit, r.Winner, r.Best = now, 0, seed|1, nil, 0, -1, 0
	r.next(Battle)
	return nil
}

// place is a free spot of a side: the front row first for a warrior, the
// back for the rest, from the middle outwards.
func place(used map[[2]int]bool, front bool) (row, col int) {
	rows := []int{1, 0}
	if front {
		rows = []int{0, 1}
	}
	for _, row := range rows {
		for _, col := range []int{2, 1, 3, 0, 4} {
			if !used[[2]int{row, col}] {
				used[[2]int{row, col}] = true
				return row, col
			}
		}
	}
	return 0, 0
}

// Advance plays the battle on to now, or closes a lobby no one started; it
// gives what the group is to be told.
func (r *Room) Advance(now time.Time) []Event {
	switch r.Phase {
	case Lobby:
		if !now.Before(r.Ends) {
			*r = Room{Seq: r.Seq + 1, Round: r.Round, Winner: -1}
			return []Event{{Text: "⚔️ Maydon xonasi yopildi: jang boshlanmadi."}}
		}
	case Battle:
		until := min(int(now.Sub(r.Started).Milliseconds()), battleMs)
		for r.T+stepMs <= until && r.Phase == Battle {
			r.T += stepMs
			r.step()
			if end := r.end(); end != nil {
				return []Event{*end}
			}
		}
	}
	return nil
}

// step is 50 ms of the battle: the fallen come back when their time comes,
// attacks come round, and an idle fighter strikes, or fires their skill, by
// itself.
func (r *Room) step() {
	for i := range r.Fighters {
		f := &r.Fighters[i]
		if !f.Alive {
			if f.ReviveAt > 0 && r.T >= f.ReviveAt {
				f.Alive, f.HP, f.ReviveAt, f.CD, f.BlockUntil = true, f.Max*6/10, 0, 600, r.T+blockMs
				r.hit(Hit{From: f.ID, To: f.ID, Amount: f.HP, Kind: "back"})
			}
			continue
		}
		f.CD = max(f.CD-stepMs, -60000)
		if f.CD <= -idleMs {
			r.act(f, f.Energy >= 100, idleTimes, false)
			f.CD = classes[f.Class].Every
		}
	}
}

// Attack is a player's blow when their attack has come round: right after
// it did, a precise one; before it did, a slip that puts it off a little.
func (r *Room) Attack(id int64) error {
	f, err := r.ready(id)
	if err != nil {
		return err
	}
	if f.CD > 0 {
		f.CD += earlyMs
		return ErrEarly
	}
	perfect := -f.CD <= perfectMs
	times := 1.0
	if perfect {
		times = perfectTimes
	}
	r.act(f, false, times, perfect)
	f.CD = classes[f.Class].Every
	return nil
}

// Skill fires a player's skill when their energy is full.
func (r *Room) Skill(id int64) error {
	f, err := r.ready(id)
	if err != nil {
		return err
	}
	if f.Energy < 100 {
		return ErrEnergy
	}
	r.act(f, true, 1, false)
	return nil
}

// Block raises a player's guard, which takes 70% of each blow for a while.
func (r *Room) Block(id int64) error {
	f, err := r.ready(id)
	if err != nil {
		return err
	}
	if r.T < f.BlockReady {
		return ErrBlock
	}
	f.BlockUntil, f.BlockReady = r.T+blockMs, r.T+blockEvery
	return nil
}

// Aim sets a player's target: a foe, or for a healer a friend to cure.
func (r *Room) Aim(id, target int64) error {
	f, err := r.ready(id)
	if err != nil {
		return err
	}
	t := r.fighter(target)
	if t == nil || !t.Alive || t.ID == id || (t.Team == f.Team && f.Class != Healer) {
		return ErrTarget
	}
	f.Target = target
	return nil
}

func (r *Room) ready(id int64) (*Fighter, error) {
	f := r.fighter(id)
	switch {
	case r.Phase != Battle:
		return nil, ErrPhase
	case f == nil:
		return nil, ErrNotIn
	case !f.Alive:
		return nil, ErrDead
	}
	return f, nil
}

func (r *Room) side(team int, foes bool) []*Fighter {
	var list []*Fighter
	for i := range r.Fighters {
		if o := &r.Fighters[i]; o.Alive && (o.Team != team) == foes {
			list = append(list, o)
		}
	}
	return list
}

// front is the foe nearest f in the foes' front row, or their back row
// when the front has fallen.
func (r *Room) front(f *Fighter) *Fighter {
	foes := r.side(f.Team, true)
	if len(foes) == 0 {
		return nil
	}
	row := foes[0].Row
	for _, o := range foes {
		row = min(row, o.Row)
	}
	var best *Fighter
	for _, o := range foes {
		if o.Row == row && (best == nil || abs(o.Col-f.Col) < abs(best.Col-f.Col)) {
			best = o
		}
	}
	return best
}

// aimed is f's chosen foe while it can be hit, else def: a warrior reaches
// only the foes' front.
func (r *Room) aimed(f *Fighter, def *Fighter) *Fighter {
	t := r.fighter(f.Target)
	if t == nil || !t.Alive || t.Team == f.Team {
		return def
	}
	if f.Class == Warrior && t.Row != r.front(f).Row {
		return def
	}
	return t
}

// act is a fighter's blow, or their skill: the warrior strikes the front
// and their skill the whole front row; the archer shoots their target and
// their skill four arrows; the mage strikes and splashes, and their skill
// burns every foe; the healer cures the weakest friend, or strikes, and
// their skill cures everyone.
func (r *Room) act(f *Fighter, skill bool, times float64, perfect bool) {
	if len(r.side(f.Team, true)) == 0 {
		return
	}
	if skill {
		f.Energy = 0
	} else {
		f.Energy = min(100, f.Energy+25)
	}
	switch f.Class {
	case Warrior:
		t := r.aimed(f, r.front(f))
		if !skill {
			r.strike(f, t, times, "slash", perfect)
			return
		}
		for _, o := range r.side(f.Team, true) {
			if o.Row == t.Row {
				r.strike(f, o, 1.6*times, "slash", perfect)
			}
		}
	case Archer:
		if !skill {
			r.strike(f, r.aimed(f, r.nearest(f)), times, "arrow", perfect)
			return
		}
		for range 4 {
			if foes := r.side(f.Team, true); len(foes) > 0 {
				r.strike(f, foes[r.intn(len(foes))], 1.2*times, "arrow", perfect)
			}
		}
	case Mage:
		if skill {
			for _, o := range r.side(f.Team, true) {
				r.strike(f, o, 0.9*times, "fire", perfect)
			}
			return
		}
		t := r.aimed(f, r.front(f))
		r.strike(f, t, times, "orb", perfect)
		for _, o := range r.side(f.Team, true) {
			if o.Row == t.Row && abs(o.Col-t.Col) == 1 {
				r.strike(f, o, 0.5*times, "orb", perfect)
			}
		}
	case Healer:
		if skill {
			for _, o := range r.side(f.Team, false) {
				r.cure(f, o, o.Max/4)
			}
			return
		}
		low := r.weakest(f)
		if t := r.fighter(f.Target); t != nil && t.Alive && t.Team == f.Team {
			low = t
		}
		if low.HP < low.Max && (low.ID == f.Target || low.HP*10 < low.Max*9) {
			atk, _, _, _ := Stats(f.Sword, f.Crit)
			r.cure(f, low, int(float64(classes[Healer].Atk)*3*atk*times))
			return
		}
		r.strike(f, r.aimed(f, r.front(f)), times, "leaf", perfect)
	}
}

func (r *Room) nearest(f *Fighter) *Fighter {
	var best *Fighter
	for _, o := range r.side(f.Team, true) {
		if best == nil || abs(o.Col-f.Col)*2+o.Row < abs(best.Col-f.Col)*2+best.Row {
			best = o
		}
	}
	return best
}

func (r *Room) weakest(f *Fighter) *Fighter {
	var low *Fighter
	for _, o := range r.side(f.Team, false) {
		if low == nil || o.HP*low.Max < low.HP*o.Max {
			low = o
		}
	}
	return low
}

// strike deals f's blow to t: their class's, their sword's, varied by a
// tenth, critical by chance, mostly kept off by a block. A fighter who falls
// for the first time comes back later.
func (r *Room) strike(f, t *Fighter, times float64, kind string, perfect bool) {
	if t == nil || !t.Alive {
		return
	}
	atk, _, critPct, _ := Stats(f.Sword, f.Crit)
	amount := int(float64(classes[f.Class].Atk) * atk * times * (0.9 + 0.2*r.float()))
	crit := r.intn(100) < critPct
	if crit {
		amount *= critTimes
	}
	blocked := t.BlockUntil > r.T
	if blocked {
		amount = amount * 3 / 10
	}
	t.HP = max(0, t.HP-amount)
	f.Dmg += amount
	t.Energy = min(100, t.Energy+8)
	r.hit(Hit{From: f.ID, To: t.ID, Amount: amount, Kind: kind, Crit: crit, Perfect: perfect, Blocked: blocked})
	if t.HP == 0 {
		t.Alive = false
		if !t.Revived {
			t.Revived, t.ReviveAt = true, r.T+reviveMs
		}
		r.hit(Hit{From: f.ID, To: t.ID, Kind: "down"})
	}
}

func (r *Room) cure(f, t *Fighter, amount int) {
	got := min(amount, t.Max-t.HP)
	if got <= 0 {
		return
	}
	t.HP += got
	f.Heal += got
	r.hit(Hit{From: f.ID, To: t.ID, Amount: got, Kind: "heal"})
}

func (r *Room) hit(h Hit) {
	r.NextHit++
	h.ID, h.T = r.NextHit, r.T
	r.Hits = append(r.Hits, h)
	if len(r.Hits) > maxHits {
		r.Hits = r.Hits[len(r.Hits)-maxHits:]
	}
}

// end ends the battle when a side has fallen and none of it is to come
// back, or when its time is up: then the side with more of its life left
// wins, or it is a draw.
func (r *Room) end() *Event {
	var standing [2]bool
	var share [2]float64
	for _, f := range r.Fighters {
		if f.Alive || f.ReviveAt > 0 {
			standing[f.Team] = true
		}
		share[f.Team] += float64(f.HP) / float64(f.Max)
	}
	switch {
	case !standing[0] && !standing[1]:
		r.Winner = -1
	case !standing[0]:
		r.Winner = 1
	case !standing[1]:
		r.Winner = 0
	case r.T >= battleMs:
		r.Winner = -1
		if share[0] > share[1] {
			r.Winner = 0
		} else if share[1] > share[0] {
			r.Winner = 1
		}
	default:
		return nil
	}
	r.next(Over)
	best := &r.Fighters[0]
	var winners []int64
	for i := range r.Fighters {
		f := &r.Fighters[i]
		if f.Dmg+f.Heal > best.Dmg+best.Heal {
			best = f
		}
		if f.Team == r.Winner {
			winners = append(winners, f.ID)
		}
	}
	r.Best = best.ID
	title := map[int]string{0: "🔴 Qizillar yutdi!", 1: "🔵 Ko'klar yutdi!", -1: "🤝 Durang!"}[r.Winner]
	heal := ""
	if best.Heal > 0 {
		heal = fmt.Sprintf(", %d davolash", best.Heal)
	}
	lines := []string{"⚔️ Maydon: " + title, "", fmt.Sprintf("⭐ Jang qahramoni: %s — %d zarar%s", best.Name, best.Dmg, heal)}
	for team, mark := range []string{"🔴", "🔵"} {
		var names []string
		for _, f := range r.Fighters {
			if f.Team == team {
				names = append(names, fmt.Sprintf("%s (%d)", f.Name, f.Dmg+f.Heal))
			}
		}
		lines = append(lines, mark+" "+strings.Join(names, ", "))
	}
	return &Event{Text: strings.Join(lines, "\n"), Winners: winners}
}

// The battle's randomness: a xorshift kept in the room, so that a fight
// played on in many requests is the same fight.
func (r *Room) next32() uint32 {
	x := r.Random
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	r.Random = x
	return x
}

func (r *Room) intn(n int) int { return int(r.next32() % uint32(n)) }
func (r *Room) float() float64 { return float64(r.next32()) / 4294967296 }

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
