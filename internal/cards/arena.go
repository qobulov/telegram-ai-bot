package cards

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	mrand "math/rand/v2"
	"slices"
	"time"

	"telegram-ai-bot/internal/arena"
	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/storage"
)

// The arena, as the owners asked: a player holds at most ArenaTickets
// tickets, one coming back every ArenaTicketEvery, and each attack takes
// one; they win at most ArenaTokens card tokens a day; ratings move by Elo
// with ArenaK, the attacker's and the defender's both. The arena offers
// ArenaOpponentsShown opponents, its table lists arenaTopSize players and
// the history a player's last ArenaHistory battles.
const (
	ArenaTickets        = 8
	ArenaTicketEvery    = time.Hour
	ArenaTokens         = 3
	ArenaK              = 32
	ArenaOpponentsShown = 3
	// arenaOpponentsPool is how many players nearest in rating the shown
	// opponents are drawn from, so that refreshing brings others.
	arenaOpponentsPool = 15
	ArenaHistory       = 10
	arenaTopSize       = 10
)

// Anime synergy, as the owners asked: the cards of one anime in a team,
// seasons and sequels as one (Franchise), raise each other's attack and
// health by these percents, a pair by SynergyTwo, three or more by
// SynergyThree (since 2026-11-22 half what they were, 10 and 20, as the
// owners asked: a team of one anime stood too far above the rest). The page
// works it out the same way for a team not saved.
const (
	SynergyTwo   = 5
	SynergyThree = 10
)

// arenaRules are the rules at now: the tokens' day starts at midnight in
// Tashkent, built there so that the store counts Tashkent's date.
func arenaRules(now time.Time) storage.ArenaRules {
	return storage.ArenaRules{Tickets: ArenaTickets, TicketEvery: ArenaTicketEvery, Tokens: ArenaTokens, K: ArenaK,
		CoinsWin: ArenaCoinsWin, CoinsLoss: ArenaCoinsLoss, Day: DayStart(now), Now: now}
}

var (
	classNames = map[arena.Class]string{arena.ClassTank: "tank", arena.ClassDealer: "dealer", arena.ClassSupport: "support", arena.ClassHealer: "healer"}
	skillNames = map[arena.Skill]string{arena.NoSkill: "", arena.Strike: "strike", arena.Heal: "heal", arena.Shield: "shield",
		arena.Stun: "stun", arena.Poison: "poison", arena.Provoke: "provoke"}
)

// fighterView is a card in the arena as the page draws it.
type fighterView struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	Rarity int    `json:"rarity"`
	Stars  int    `json:"stars"`
	Attack int    `json:"attack"`
	Health int    `json:"health"`
	Class  string `json:"class"`
	Skill  string `json:"skill"`
	// Kit is a Mythic's own Passive and Ultimate, in place of Skill.
	Kit *kitView `json:"kit,omitempty"`
	// Anime is the card's anime as Franchise counts it, and Synergy the
	// percent its Attack and Health are raised by for the team's others of it.
	Anime   string `json:"anime"`
	Synergy int    `json:"synergy"`
	// Theme is the percent the tower's anime of the week raises it by, 0
	// outside the tower.
	Theme int `json:"theme,omitempty"`
	// Boss marks the boss, whose health is endless.
	Boss bool `json:"boss,omitempty"`
}

// eventView is one thing that happened in a battle, for the page to replay.
type eventView struct {
	Round  int    `json:"round"`
	Side   int    `json:"side"`
	From   int    `json:"from"`
	To     int    `json:"to"`
	Kind   string `json:"kind"`
	Amount int    `json:"amount"`
	HP     int    `json:"hp"`
	Edge   bool   `json:"edge"`
	// Energy and ToEnergy are the energy of the slots From and To after it,
	// for the page's energy bars.
	Energy   int `json:"energy"`
	ToEnergy int `json:"to_energy"`
	// Ult is the Ultimate the event is of, when it is.
	Ult string `json:"ult,omitempty"`
}

// fighters is a team in the arena: its fighters, front first, and their
// cards, for the page.
type fighters struct {
	team  arena.Team
	views []fighterView
}

// fightersOf makes a team of the cards ids, of the player's held, boosted by
// their stars, then by their anime synergy; ok is false if any is not held.
func fightersOf(held []storage.OwnedCard, ids [5]int64) (fighters, bool) {
	return themedFighters(held, ids, "")
}

// themedFighters is fightersOf with the cards of theme, an anime as
// Franchise counts it, raised by TowerThemeBonus besides; "" is none.
func themedFighters(held []storage.OwnedCard, ids [5]int64, theme string) (fighters, bool) {
	byID := make(map[int64]storage.OwnedCard, len(held))
	for _, o := range held {
		if o.Count > 0 {
			byID[o.ID] = o
		}
	}
	var owned [5]storage.OwnedCard
	var animes [5]string
	for i, id := range ids {
		o, ok := byID[id]
		if !ok {
			return fighters{}, false
		}
		owned[i], animes[i] = o, Franchise(o.Anime)
	}
	bonus := synergies(animes)
	var slots [5]arena.Fighter
	views := make([]fighterView, len(ids))
	for i, o := range owned {
		stars := min(max(o.Stars, 1), MaxStars)
		themed := 0
		if theme != "" && animes[i] == theme {
			themed = TowerThemeBonus
		}
		attack, health := Boosted(o.Card, stars)
		attack, health = raised(attack, bonus[i]+themed), raised(health, bonus[i]+themed)
		f := fighterOf(o.ID, o.Name, o.Rarity, stars, attack, health)
		slots[i] = f
		views[i] = fighterView{ID: o.ID, Name: o.Name, Image: o.ImageURL, Rarity: o.Rarity, Stars: stars,
			Attack: attack, Health: health, Class: classNames[f.Class], Skill: skillNames[f.Skill], Kit: kitViewOf(o.ID, o.Rarity),
			Anime: animes[i], Synergy: bonus[i], Theme: themed}
	}
	team := arena.Team{Front: [2]arena.Fighter{slots[0], slots[1]}, Back: [3]arena.Fighter{slots[2], slots[3], slots[4]}}
	return fighters{team, views}, true
}

// synergies is each slot's synergy percent by the animes of the team's
// cards; a card without an anime has none.
func synergies(animes [5]string) [5]int {
	count := map[string]int{}
	for _, a := range animes {
		count[a]++
	}
	var bonus [5]int
	for i, a := range animes {
		switch n := count[a]; {
		case a == "" || n < 2:
		case n == 2:
			bonus[i] = SynergyTwo
		default:
			bonus[i] = SynergyThree
		}
	}
	return bonus
}

// raised is n raised by percent, rounded, in whole numbers so that the
// page gets the same.
func raised(n, percent int) int { return (n*(100+percent) + 50) / 100 }

// savedFighters is the player's saved team; ok is false without one or with
// a card of it no longer held.
func (h *handler) savedFighters(ctx context.Context, userID int64) (fighters, bool, error) {
	return h.themedSaved(ctx, userID, "")
}

// The modes with a team of their own, as the owners asked on 2026-11-25;
// a player who saved none there fights with their arena team.
const (
	teamBoss  = "boss"
	teamTower = "tower"
)

// themedSaved is savedFighters with theme's cards raised, as themedFighters.
func (h *handler) themedSaved(ctx context.Context, userID int64, theme string) (fighters, bool, error) {
	team, ok, _, err := h.modeSaved(ctx, userID, "", theme)
	return team, ok, err
}

// modeSaved is the player's team for mode, teamBoss or teamTower, or their
// arena team for "" or when they saved none for mode; own reports one of
// mode's own.
func (h *handler) modeSaved(ctx context.Context, userID int64, mode, theme string) (team fighters, ok, own bool, err error) {
	var front [2]int64
	var back [3]int64
	if mode != "" {
		if front, back, own, err = h.Store.ModeTeam(ctx, userID, mode); err != nil {
			return fighters{}, false, false, err
		}
	}
	if !own {
		if front, back, ok, err = h.Store.ArenaTeam(ctx, userID); err != nil || !ok {
			return fighters{}, false, false, err
		}
	}
	ids := [5]int64{front[0], front[1], back[0], back[1], back[2]}
	held, err := h.ownedCardsOf(ctx, userID, ids[:])
	if err != nil {
		return fighters{}, false, false, err
	}
	team, ok = themedFighters(held, ids, theme)
	return team, ok, own, nil
}

// foeTeam is an opponent's saved team as an attack would meet it, so that
// the player can look at it first; nil when it cannot be read.
func (h *handler) foeTeam(ctx context.Context, userID int64) []fighterView {
	team, ok, err := h.savedFighters(ctx, userID)
	if err != nil {
		h.Logger.Warn("could not read an opponent's team", "user_id", userID, "error", err)
		return nil
	}
	if !ok {
		return nil
	}
	return team.views
}

// arenaView is the arena: the player, their team (null without one they
// hold), a few opponents near their rating, the table and the player's
// last battles.
// arenaView is the arena for the player; shown is the opponents they see
// now, whom a refresh leaves out while there are others to offer.
func (h *handler) arenaView(ctx context.Context, user miniapp.User, shown []int64) (any, failure) {
	player, err := h.Store.ArenaPlayerOf(ctx, user.ID, arenaRules(h.Now()))
	if err != nil {
		h.Logger.Error("could not read an arena player", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	team, ok, err := h.savedFighters(ctx, user.ID)
	if err != nil {
		h.Logger.Error("could not read an arena team", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	pool, err := h.Store.ArenaOpponents(ctx, user.ID, arenaOpponentsPool)
	if err != nil {
		h.Logger.Error("could not read arena opponents", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	opponents := pickOpponents(pool, shown, ArenaOpponentsShown, mrand.Shuffle)
	top, err := h.Store.ArenaTop(ctx, arenaTopSize)
	if err != nil {
		h.Logger.Error("could not read the arena table", "error", err)
		return nil, errUnavailable
	}
	tablePlace, tableSize, err := h.Store.ArenaPlace(ctx, user.ID)
	if err != nil {
		h.Logger.Error("could not read an arena place", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	weekStart := arenaWeekStart(h.Now())
	weekPrizes := arenaWeekPrizes(weekStart)
	place, err := h.Store.ArenaWeekPlace(ctx, user.ID, weekStart)
	if err != nil {
		h.Logger.Error("could not read an arena week place", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	battles, err := h.Store.ArenaBattles(ctx, user.ID, ArenaHistory)
	if err != nil {
		h.Logger.Error("could not read arena battles", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	history := make([]map[string]any, len(battles))
	for i, b := range battles {
		history[i] = map[string]any{"at": b.At.UnixMilli(), "attacked": b.Attacked, "opponent": b.Opponent, "won": b.Won, "rating_change": b.RatingChange}
	}
	others := make([]map[string]any, len(opponents))
	for i, p := range opponents {
		others[i] = map[string]any{"id": p.UserID, "name": p.Name, "rating": p.Rating, "power": p.Power, "team": h.foeTeam(ctx, p.UserID)}
	}
	rows := make([]map[string]any, len(top))
	for i, p := range top {
		rows[i] = map[string]any{"name": p.Name, "rating": p.Rating, "wins": p.Wins, "losses": p.Losses, "power": p.Power, "me": p.UserID == user.ID}
	}
	var views []fighterView
	if ok {
		views = team.views
	}
	return map[string]any{
		"player": map[string]any{
			"name": player.Name, "rating": player.Rating, "wins": player.Wins, "losses": player.Losses, "power": player.Power,
			"tickets": player.Tickets, "tickets_max": ArenaTickets, "next_ticket_in": player.NextTicket.Milliseconds(),
			"tokens_left": player.TokensLeft, "tokens": ArenaTokens, "coins": player.Coins,
		},
		"shop": map[string]any{
			"items": ArenaShopItems, "tokens_daily": ArenaShopTokensDaily, "tokens_left": max(ArenaShopTokensDaily-player.ShopTokens, 0),
			"coins_win": ArenaCoinsWin, "coins_loss": ArenaCoinsLoss,
		},
		"team": views, "opponents": others, "top": rows, "history": history,
		"me": map[string]any{"place": tablePlace, "total": tableSize},
		"week": map[string]any{
			"place": place, "prize": arenaWeekPrize(weekPrizes, place).Tokens, "boxes": arenaWeekPrize(weekPrizes, place).Boxes, "prizes": weekPrizes,
			"event": weekStart.Equal(ArenaEventWeek),
			"ends":  weekStart.AddDate(0, 0, 7).UnixMilli(),
		},
	}, failure{}
}

// arenaTeam saves the player's team, two in front and three behind, its
// power worked out here from the cards held and their stars, and gives the
// arena. A team refused says why: "size", "repeat" or "not_held".
func (h *handler) arenaTeam(ctx context.Context, user miniapp.User, mode string, front, back []int64) (any, failure) {
	if mode != "" && mode != "arena" && mode != teamBoss && mode != teamTower {
		return nil, errBadRequest
	}
	if len(front) != 2 || len(back) != 3 {
		return map[string]any{"refused": "size"}, failure{}
	}
	ids := [5]int64{front[0], front[1], back[0], back[1], back[2]}
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			return map[string]any{"refused": "repeat"}, failure{}
		}
		seen[id] = true
	}
	held, err := h.ownedCardsOf(ctx, user.ID, ids[:])
	if err != nil {
		h.Logger.Error("could not read a player's cards", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	team, ok := fightersOf(held, ids)
	if !ok {
		return map[string]any{"refused": "not_held"}, failure{}
	}
	if mode == teamBoss || mode == teamTower {
		err = h.Store.SaveModeTeam(ctx, user.ID, mode, [2]int64{ids[0], ids[1]}, [3]int64{ids[2], ids[3], ids[4]})
	} else {
		err = h.Store.SaveArenaTeam(ctx, user.ID, [2]int64{ids[0], ids[1]}, [3]int64{ids[2], ids[3], ids[4]}, team.team.Power())
	}
	switch {
	case errors.Is(err, storage.ErrCardNotHeld):
		return map[string]any{"refused": "not_held"}, failure{}
	case errors.Is(err, storage.ErrArenaTeamRepeat):
		return map[string]any{"refused": "repeat"}, failure{}
	case err != nil:
		h.Logger.Error("could not save a team", "user_id", user.ID, "mode", mode, "error", err)
		return nil, errUnavailable
	}
	switch mode {
	case teamBoss:
		return h.bossView(ctx, user)
	case teamTower:
		return h.towerView(ctx, user)
	}
	return h.arenaView(ctx, user, nil)
}

// arenaAttack fights the player's saved team against the opponent's and
// keeps the outcome. An attack refused says why: "self", "no_team" (the
// player's team is missing or holds a card no longer held), "opponent_gone"
// (so is the opponent's) or "no_tickets" (none left).
func (h *handler) arenaAttack(ctx context.Context, user miniapp.User, opponent int64) (any, failure) {
	switch {
	case opponent == 0:
		return nil, errBadRequest
	case opponent == user.ID:
		return map[string]any{"refused": "self"}, failure{}
	}
	mine, ok, err := h.savedFighters(ctx, user.ID)
	if err != nil {
		h.Logger.Error("could not read an arena team", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	if !ok {
		return map[string]any{"refused": "no_team"}, failure{}
	}
	theirs, ok, err := h.savedFighters(ctx, opponent)
	if err != nil {
		h.Logger.Error("could not read an arena team", "user_id", opponent, "error", err)
		return nil, errUnavailable
	}
	if !ok {
		return map[string]any{"refused": "opponent_gone"}, failure{}
	}
	seed, err := arenaSeed()
	if err != nil {
		h.Logger.Error("could not draw an arena seed", "error", err)
		return nil, errUnavailable
	}
	result := arena.Battle(mine.team, theirs.team, seed)
	now := h.Now()
	outcome, err := h.Store.RecordArenaBattle(ctx, user.ID, opponent, seed, result.AttackerWon, arenaRules(now), questOf(QuestArena, now))
	switch {
	case errors.Is(err, storage.ErrArenaNoTickets):
		return map[string]any{"refused": "no_tickets"}, failure{}
	case err != nil:
		h.Logger.Error("could not record an arena battle", "user_id", user.ID, "opponent", opponent, "error", err)
		return nil, errUnavailable
	}
	return map[string]any{
		"won": result.AttackerWon, "events": eventViews(result.Events), "attacker": mine.views, "defender": theirs.views,
		"rating_change": outcome.RatingChange, "token": outcome.Token,
		"tickets": outcome.Tickets, "next_ticket_in": outcome.NextTicket.Milliseconds(), "tokens_left": outcome.TokensLeft,
		"coins": outcome.Coins, "coins_won": outcome.CoinsWon,
	}, failure{}
}

func eventViews(events []arena.Event) []eventView {
	views := make([]eventView, len(events))
	for i, e := range events {
		views[i] = eventView{Round: e.Round, Side: e.Side, From: e.From, To: e.To, Kind: e.Kind, Amount: e.Amount, HP: e.HP, Edge: e.Edge, Energy: e.Energy, ToEnergy: e.ToEnergy, Ult: e.Ult}
	}
	return views
}

// arenaSeed draws a battle's seed, which replays it, from crypto/rand.
func arenaSeed() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(b[:])), nil
}

// pickOpponents draws n of pool, the players nearest in rating, in a random
// order by shuffle, the ones not in shown first; they are then listed
// nearest first, as pool was.
func pickOpponents(pool []storage.ArenaPlayer, shown []int64, n int, shuffle func(int, func(int, int))) []storage.ArenaPlayer {
	seen := map[int64]bool{}
	for _, id := range shown {
		seen[id] = true
	}
	order := make([]int, len(pool))
	for i := range order {
		order[i] = i
	}
	shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Compare(b2i(seen[pool[a].UserID]), b2i(seen[pool[b].UserID]))
	})
	order = order[:min(n, len(order))]
	slices.Sort(order)
	picked := make([]storage.ArenaPlayer, len(order))
	for i, at := range order {
		picked[i] = pool[at]
	}
	return picked
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
