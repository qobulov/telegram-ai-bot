package cards

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"slices"
	"time"

	"telegram-ai-bot/internal/arena"
	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/storage"
)

// The boss, as the owners asked on 2026-11-23: a foe of endless health that
// grows stronger. Each week, Monday to Monday in Tashkent, one Mythic of
// the catalog is the boss of everyone. It never falls: the player's saved
// arena team fights it until the team falls or arena.MaxRounds are over,
// and what counts is the damage dealt it. It hits every card of the team
// each turn, BossRage harder each round, and the week's damage of all the
// players raises its level, every BossLevelDamage, each level BossLevelGrowth
// more attack. A player has BossTries fights a day; each gives arena coins
// by its damage, and the week's table ranks the best fights.
const (
	BossTries       = 3
	BossAttack      = 1500
	BossRage        = 0.12
	BossLevelDamage = 3_000_000
	BossLevelGrowth = 0.06
	BossCoinsBase   = 5
	BossCoinsPer    = 25_000
	BossCoinsMost   = 40
)

// BossWeekPrizes are the boss's weekly prizes, as the owners asked on
// 2026-11-23: on Monday at 00:00 in Tashkent the players of the week's best
// fights get card tokens, and the top 3 diamond boxes, in the mail.
var BossWeekPrizes = []storage.ArenaPrize{
	{From: 1, To: 1, Tokens: 60, Boxes: 2, Box: arenaWeekBox},
	{From: 2, To: 2, Tokens: 45, Boxes: 1, Box: arenaWeekBox},
	{From: 3, To: 3, Tokens: 30, Boxes: 1, Box: arenaWeekBox},
	{From: 4, To: 5, Tokens: 20},
	{From: 6, To: 10, Tokens: 15},
	{From: 11, To: 30, Tokens: 10},
	{From: 31, To: 50, Tokens: 5},
}

// BossWeekPayer pays a boss week.
type BossWeekPayer interface {
	PayBossWeek(ctx context.Context, week time.Time, prizes []storage.ArenaPrize, note func(place int) string) (int, bool, error)
}

// PayBossWeek pays the boss's last week ended by now, if it is not paid
// yet, and gives the players paid.
func PayBossWeek(ctx context.Context, store BossWeekPayer, now time.Time) (int, error) {
	players, _, err := store.PayBossWeek(ctx, arenaWeekStart(now).AddDate(0, 0, -7), BossWeekPrizes, bossWeekNote)
	return players, err
}

func bossWeekNote(place int) string {
	return fmt.Sprintf("👹 Boss haftalik mukofoti: %d-o'rin", place)
}

// payBossWeek pays the boss week that ended the first time anyone opens the
// boss after Monday's midnight; the daily cron pays it too, if no one came.
func (h *handler) payBossWeek(ctx context.Context) {
	week := arenaWeekStart(h.Now()).Unix()
	if h.paidBossWeek.Load() == week {
		return
	}
	if _, err := PayBossWeek(ctx, h.Store, h.Now()); err != nil {
		h.Logger.Warn("could not pay the boss week", "error", err)
		return
	}
	h.paidBossWeek.Store(week)
}

// bossLevel is the boss's level after a week's damage of total.
func bossLevel(total int64) int { return 1 + int(total/BossLevelDamage) }

// bossAttack is the boss's attack, before its rage, at level.
func bossAttack(level int) int {
	return int(math.Round(BossAttack * (1 + BossLevelGrowth*float64(level-1))))
}

// bossCoins are the arena coins a fight dealing damage gives.
func bossCoins(damage int) int { return min(BossCoinsBase+damage/BossCoinsPer, BossCoinsMost) }

// bossCard is the week's boss: a card of the highest rarity in the catalog,
// not a limited one, picked by the week's date; ok is false for none.
func bossCard(catalog []storage.Card, week time.Time) (storage.Card, bool) {
	var pool []storage.Card
	top := 0
	for _, c := range catalog {
		if storage.CardLimited(c.ID) || c.Attack <= 0 || c.Rarity < top {
			continue
		}
		if c.Rarity > top {
			top, pool = c.Rarity, nil
		}
		pool = append(pool, c)
	}
	if len(pool) == 0 {
		return storage.Card{}, false
	}
	slices.SortFunc(pool, func(a, b storage.Card) int { return int(a.ID - b.ID) })
	h := fnv.New32a()
	fmt.Fprintf(h, "boss:%s", week.Format(time.DateOnly))
	return pool[h.Sum32()%uint32(len(pool))], true
}

// bossFighter is the boss of card at level: the card's role, by its stats at
// the most stars, with the boss's attack and no skill.
func bossFighter(c storage.Card, level int) (arena.Fighter, fighterView) {
	attack, health := Boosted(c, MaxStars)
	f := fighterOf(c.ID, c.Name, c.Rarity, MaxStars, attack, health)
	f.Attack, f.Skill = bossAttack(level), arena.NoSkill
	view := fighterView{ID: c.ID, Name: c.Name, Image: c.ImageURL, Rarity: c.Rarity, Stars: MaxStars,
		Attack: f.Attack, Class: classNames[f.Class], Anime: Franchise(c.Anime), Boss: true}
	return f, view
}

// bossSeed is a boss fight's seed, from the player, the day and its fight
// that day, so that a fight is the same however it is asked for.
func bossSeed(userID int64, day time.Time, try int) int64 {
	h := fnv.New64a()
	fmt.Fprintf(h, "boss:%d:%s:%d", userID, day.Format(time.DateOnly), try)
	return int64(h.Sum64())
}

// boss is what the boss's page and fight need.
type boss struct {
	rules   storage.BossRules
	player  storage.BossPlayer
	total   int64
	level   int
	fighter arena.Fighter
	view    fighterView
	hasBoss bool
	mine    fighters
	hasTeam bool
}

func (h *handler) bossOf(ctx context.Context, userID int64) (boss, error) {
	if err := h.catalog.Ensure(ctx); err != nil {
		return boss{}, fmt.Errorf("fill the card catalog: %w", err)
	}
	catalog, err := h.cardCatalog(ctx)
	if err != nil {
		return boss{}, fmt.Errorf("read the card catalog: %w", err)
	}
	now := h.Now()
	b := boss{rules: storage.BossRules{Week: arenaWeekStart(now), Day: DayStart(now), Now: now, Tries: BossTries}}
	if b.total, err = h.Store.BossWeek(ctx, b.rules.Week); err != nil {
		return boss{}, err
	}
	if b.player, err = h.Store.BossPlayerOf(ctx, userID, b.rules); err != nil {
		return boss{}, err
	}
	b.level = bossLevel(b.total)
	if card, ok := bossCard(catalog, b.rules.Week); ok {
		b.fighter, b.view = bossFighter(card, b.level)
		b.hasBoss = true
	}
	if b.mine, b.hasTeam, err = h.themedSaved(ctx, userID, ""); err != nil {
		return boss{}, err
	}
	return b, nil
}

// bossView is the week's boss for the player.
func (h *handler) bossView(ctx context.Context, user miniapp.User) (any, failure) {
	b, err := h.bossOf(ctx, user.ID)
	if err != nil {
		h.Logger.Error("could not read the boss", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	view := map[string]any{
		"boss": nil, "level": b.level, "week_damage": b.total,
		"next_level": int64(b.level) * BossLevelDamage, "growth": int(BossLevelGrowth * 100), "rage": int(BossRage * 100),
		"tries": b.player.Tries, "tries_max": BossTries, "best": b.player.Best,
		"coins": map[string]any{"base": BossCoinsBase, "per": BossCoinsPer, "most": BossCoinsMost},
		"ends":  b.rules.Week.AddDate(0, 0, 7).UnixMilli(), "team": nil, "power": 0,
	}
	if b.hasBoss {
		view["boss"] = b.view
	}
	if b.hasTeam {
		view["team"], view["power"] = b.mine.views, b.mine.team.Power()
	}
	_, place, _, err := h.Store.BossPlace(ctx, b.rules.Week, user.ID)
	if err != nil {
		h.Logger.Error("could not read a boss place", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	prize := arenaWeekPrize(BossWeekPrizes, place)
	view["week"] = map[string]any{"place": place, "prize": prize.Tokens, "boxes": prize.Boxes, "prizes": BossWeekPrizes}
	return view, failure{}
}

// bossFight fights the week's boss with the player's saved team. A fight
// refused says why: "no_team", "no_boss" (the catalog has none) or
// "no_tries" (the day's fights are used).
func (h *handler) bossFight(ctx context.Context, user miniapp.User) (any, failure) {
	b, err := h.bossOf(ctx, user.ID)
	if err != nil {
		h.Logger.Error("could not read the boss", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	switch {
	case !b.hasTeam:
		return map[string]any{"refused": "no_team"}, failure{}
	case !b.hasBoss:
		return map[string]any{"refused": "no_boss"}, failure{}
	case b.player.Tries >= BossTries:
		return map[string]any{"refused": "no_tries"}, failure{}
	}
	result := arena.BossBattle(b.mine.team, b.fighter, BossRage, bossSeed(user.ID, b.rules.Day, b.player.Tries))
	b.rules.Coins = bossCoins(result.Damage)
	out, err := h.Store.RecordBossFight(ctx, user.ID, int64(result.Damage), b.rules)
	switch {
	case errors.Is(err, storage.ErrBossNoTries):
		return map[string]any{"refused": "no_tries"}, failure{}
	case err != nil:
		h.Logger.Error("could not record a boss fight", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	return map[string]any{
		"boss": true, "won": false, "damage": result.Damage, "best": max(b.player.Best, int64(result.Damage)),
		"events": eventViews(result.Events), "attacker": b.mine.views, "defender": []fighterView{b.view},
		"tries": out.Tries, "tries_max": BossTries, "coins_won": b.rules.Coins, "coins": out.Coins,
	}, failure{}
}

// bossTop is the week's boss table: the players of the best fights, and the
// player's own place.
func (h *handler) bossTop(ctx context.Context, user miniapp.User) (any, failure) {
	week := arenaWeekStart(h.Now())
	top, err := h.Store.BossTop(ctx, week, topSize)
	if err != nil {
		h.Logger.Error("could not read the boss table", "error", err)
		return nil, errUnavailable
	}
	rows := make([]map[string]any, len(top))
	for i, r := range top {
		rows[i] = map[string]any{"name": r.Name, "best": r.Best, "me": r.UserID == user.ID}
	}
	me, place, total, err := h.Store.BossPlace(ctx, week, user.ID)
	if err != nil {
		h.Logger.Error("could not read a boss place", "user_id", user.ID, "error", err)
		return nil, errUnavailable
	}
	return map[string]any{"top": rows, "me": map[string]any{"place": place, "total": total, "name": me.Name, "best": me.Best}}, failure{}
}
