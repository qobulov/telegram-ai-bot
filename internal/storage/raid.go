package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// RaidBoss is the boss a chat's members hit together, at its level.
type RaidBoss struct {
	ChatID int64
	Level  int
	HP     int64
	MaxHP  int64
}

// RaidHero is a player of Dev jangi: their coins, their upgrades' levels,
// their taps so far, which with Seed say which taps are critical, their
// skills, the day's quests and their streak of days.
type RaidHero struct {
	UserID  int64
	Coins   int64
	Sword   int
	Crit    int
	Helper  int
	Seed    uint32
	Taps    int64
	LastHit time.Time
	// The skills: when each lasts until, or how many blows it has left,
	// and when each can be used again.
	StormUntil, StormReady time.Time
	FireLeft               int
	FireReady              time.Time
	RainUntil, RainReady   time.Time
	// QuestDay is the day the quest counts are for; QuestClaimed has a bit
	// for each quest whose reward was taken.
	QuestDay                           time.Time
	QuestTaps, QuestKills, QuestSkills int
	QuestClaimed                       int
	Streak                             int
	StreakDay                          time.Time
}

// OnDay starts the hero's quests over when day is another than theirs.
func (h *RaidHero) OnDay(day time.Time) {
	if !h.QuestDay.Equal(day) {
		h.QuestDay, h.QuestTaps, h.QuestKills, h.QuestSkills, h.QuestClaimed = day, 0, 0, 0, 0
	}
}

// RaidMember is a member of a chat in its raid: what they hit its boss
// with at the boss's level, and in all.
type RaidMember struct {
	UserID int64
	Name   string
	Damage int64
	Total  int64
}

// RaidKill is a boss killed: its level, and who hit it, most first, with
// the coins each got for it.
type RaidKill struct {
	Level    int
	MaxHP    int64
	Fighters []RaidFighter
}

// RaidFighter is one who hit a killed boss, and their reward.
type RaidFighter struct {
	RaidMember
	Reward int64
}

// RaidRules are what a raid's changes are counted by; the game's rules are
// internal/raid's.
type RaidRules struct {
	// MaxHP is a boss's at level, for a chat with active members.
	MaxHP func(level, active int) int64
	// Strike is what a hero's taps do at now, given how long since their
	// last hit: how many of the taps count, and their damage. It may spend
	// the hero's skills and count the taps in their quests.
	Strike func(hero *RaidHero, taps int, since time.Duration, now time.Time) (counted int, damage int64)
	// Reward is the coins one who hit a killed boss of level with damage
	// gets.
	Reward func(level int, damage, maxHP int64) int64
	// Day is the day now is in, for the quests.
	Day func(now time.Time) time.Time
	// ScoreEvery is how often a member's total is due as their Telegram
	// score; Active how long since their last hit a member counts as
	// active.
	ScoreEvery time.Duration
	Active     time.Duration
}

// RaidHit is a hit's outcome: the boss after it, the hero and the member,
// what the taps did, the boss it killed if it did, and whether the
// member's total is due as their Telegram score.
type RaidHit struct {
	Boss     RaidBoss
	Hero     RaidHero
	Member   RaidMember
	Counted  int
	Damage   int64
	Killed   *RaidKill
	ScoreDue bool
}

var (
	ErrRaidCoins = errors.New("storage: not enough coins for the upgrade")
	ErrRaidMax   = errors.New("storage: the upgrade is at its highest level")
)

// RaidState is the chat's boss, the player's hero and their part in the
// raid, and its top members at the boss's level, a new boss and hero made
// if there were none.
func (s *Store) RaidState(ctx context.Context, chatID, userID int64, rules RaidRules, seed uint32, top int) (RaidBoss, RaidHero, RaidMember, []RaidMember, error) {
	var (
		boss    RaidBoss
		hero    RaidHero
		me      RaidMember
		members []RaidMember
	)
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		if boss, err = raidBoss(ctx, tx, chatID, rules, false); err != nil {
			return err
		}
		if hero, err = raidHero(ctx, tx, userID, seed, false); err != nil {
			return err
		}
		me = RaidMember{UserID: userID}
		var level int
		err = tx.QueryRow(ctx, `SELECT name, level, damage, total FROM raid_members WHERE chat_id = $1 AND user_id = $2`, chatID, userID).
			Scan(&me.Name, &level, &me.Damage, &me.Total)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if level != boss.Level {
			me.Damage = 0
		}
		members, err = raidTop(ctx, tx, chatID, boss.Level, top)
		return err
	})
	if err != nil {
		return RaidBoss{}, RaidHero{}, RaidMember{}, nil, fmt.Errorf("storage: raid state: %w", err)
	}
	return boss, hero, me, members, nil
}

// HitRaid counts a player's taps on their chat's boss at now, with the
// boss and the hero locked: their damage comes off the boss, adds to the
// member's, and their counted taps to the hero's coins. The hit that takes
// the boss's last HP kills it: everyone who hit it is rewarded, and the
// next level's boss, as strong as the chat's active members, takes its
// place.
func (s *Store) HitRaid(ctx context.Context, chatID, userID int64, name string, taps int, now time.Time, rules RaidRules, seed uint32) (RaidHit, error) {
	var hit RaidHit
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		boss, err := raidBoss(ctx, tx, chatID, rules, true)
		if err != nil {
			return err
		}
		hero, err := raidHero(ctx, tx, userID, seed, true)
		if err != nil {
			return err
		}
		since := time.Second
		if !hero.LastHit.IsZero() {
			since = now.Sub(hero.LastHit)
		}
		counted, damage := rules.Strike(&hero, taps, since, now)
		damage = min(damage, boss.HP)
		hero.Taps += int64(counted)
		hero.Coins += int64(counted)
		hero.LastHit = now
		member := RaidMember{UserID: userID, Name: name}
		var scoredAt *time.Time
		if err := tx.QueryRow(ctx, `
			INSERT INTO raid_members (chat_id, user_id, name, level, damage, total) VALUES ($1, $2, $3, $4, $5, $5)
			ON CONFLICT (chat_id, user_id) DO UPDATE SET name = excluded.name,
				damage = CASE WHEN raid_members.level = excluded.level THEN raid_members.damage + excluded.damage ELSE excluded.damage END,
				level = excluded.level, total = raid_members.total + excluded.damage, updated_at = now()
			RETURNING damage, total, scored_at`,
			chatID, userID, name, boss.Level, damage).Scan(&member.Damage, &member.Total, &scoredAt); err != nil {
			return err
		}
		due := member.Total > 0 && (scoredAt == nil || now.Sub(*scoredAt) >= rules.ScoreEvery)
		if due {
			if _, err := tx.Exec(ctx, `UPDATE raid_members SET scored_at = $3 WHERE chat_id = $1 AND user_id = $2`, chatID, userID, now); err != nil {
				return err
			}
		}
		boss.HP -= damage
		var killed *RaidKill
		if boss.HP <= 0 {
			if killed, err = killRaidBoss(ctx, tx, boss, rules, &hero, now); err != nil {
				return err
			}
			var active int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM raid_members WHERE chat_id = $1 AND updated_at > $2`,
				chatID, now.Add(-rules.Active)).Scan(&active); err != nil {
				return err
			}
			boss = RaidBoss{ChatID: chatID, Level: boss.Level + 1, MaxHP: rules.MaxHP(boss.Level+1, max(active, 1))}
			boss.HP = boss.MaxHP
			member.Damage = 0
		}
		if _, err := tx.Exec(ctx, `UPDATE raid_bosses SET level = $2, hp = $3, max_hp = $4, updated_at = now() WHERE chat_id = $1`,
			chatID, boss.Level, boss.HP, boss.MaxHP); err != nil {
			return err
		}
		if err := saveRaidHero(ctx, tx, hero); err != nil {
			return err
		}
		hit = RaidHit{Boss: boss, Hero: hero, Member: member, Counted: counted, Damage: damage, Killed: killed, ScoreDue: due}
		return nil
	})
	if err != nil {
		return RaidHit{}, fmt.Errorf("storage: hit raid: %w", err)
	}
	return hit, nil
}

// killRaidBoss rewards everyone who hit the boss, most damage first, and
// counts the kill in their quests; the hitter, whose row is locked and
// saved after, is rewarded in hitter.
func killRaidBoss(ctx context.Context, tx pgx.Tx, boss RaidBoss, rules RaidRules, hitter *RaidHero, now time.Time) (*RaidKill, error) {
	rows, err := tx.Query(ctx, `
		SELECT user_id, name, damage, total FROM raid_members
		WHERE chat_id = $1 AND level = $2 AND damage > 0 ORDER BY damage DESC, user_id`, boss.ChatID, boss.Level)
	if err != nil {
		return nil, err
	}
	members, err := pgx.CollectRows(rows, scanRaidMember)
	if err != nil {
		return nil, err
	}
	day := rules.Day(now)
	kill := &RaidKill{Level: boss.Level, MaxHP: boss.MaxHP}
	ids, rewards := make([]int64, 0, len(members)), make([]int64, 0, len(members))
	for _, m := range members {
		reward := rules.Reward(boss.Level, m.Damage, boss.MaxHP)
		kill.Fighters = append(kill.Fighters, RaidFighter{RaidMember: m, Reward: reward})
		if m.UserID == hitter.UserID {
			hitter.Coins += reward
			hitter.OnDay(day)
			hitter.QuestKills++
			continue
		}
		ids, rewards = append(ids, m.UserID), append(rewards, reward)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE raid_heroes h SET coins = h.coins + r.reward,
			quest_kills = CASE WHEN h.quest_day = $3::date THEN h.quest_kills + 1 ELSE 1 END,
			quest_taps = CASE WHEN h.quest_day = $3::date THEN h.quest_taps ELSE 0 END,
			quest_skills = CASE WHEN h.quest_day = $3::date THEN h.quest_skills ELSE 0 END,
			quest_claimed = CASE WHEN h.quest_day = $3::date THEN h.quest_claimed ELSE 0 END,
			quest_day = $3::date, updated_at = now()
		FROM unnest($1::bigint[], $2::bigint[]) AS r(user_id, reward) WHERE h.user_id = r.user_id`, ids, rewards, day); err != nil {
		return nil, err
	}
	return kill, nil
}

// ChangeRaidHero changes the player's hero as change says, with it locked,
// a new hero with seed made if there is none. An error from change keeps
// the hero as it was and is returned as it is.
func (s *Store) ChangeRaidHero(ctx context.Context, userID int64, seed uint32, change func(hero *RaidHero) error) (RaidHero, error) {
	var hero RaidHero
	var refused error
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		if hero, err = raidHero(ctx, tx, userID, seed, true); err != nil {
			return err
		}
		if refused = change(&hero); refused != nil {
			return refused
		}
		return saveRaidHero(ctx, tx, hero)
	})
	if refused != nil {
		return RaidHero{}, refused
	}
	if err != nil {
		return RaidHero{}, fmt.Errorf("storage: change raid hero: %w", err)
	}
	return hero, nil
}

// UpgradeRaidHero raises one of the hero's upgrades, "sword", "crit" or
// "helper", by a level for what cost says the next level costs, below most.
func (s *Store) UpgradeRaidHero(ctx context.Context, userID int64, item string, cost func(level int) int64, most int, seed uint32) (RaidHero, error) {
	if _, ok := map[string]bool{"sword": true, "crit": true, "helper": true}[item]; !ok {
		return RaidHero{}, fmt.Errorf("storage: no raid upgrade %q", item)
	}
	return s.ChangeRaidHero(ctx, userID, seed, func(hero *RaidHero) error {
		level := map[string]*int{"sword": &hero.Sword, "crit": &hero.Crit, "helper": &hero.Helper}[item]
		if *level >= most {
			return ErrRaidMax
		}
		price := cost(*level)
		if hero.Coins < price {
			return ErrRaidCoins
		}
		hero.Coins -= price
		*level++
		return nil
	})
}

// RaidRanks is where the player stands in their chat's raid: their place
// by damage on its boss at level, and by damage in all, 0 before they hit,
// and the chat's limit members with the most damage in all.
func (s *Store) RaidRanks(ctx context.Context, chatID, userID int64, level, limit int) (levelRank, totalRank int, top []RaidMember, err error) {
	err = s.pool.QueryRow(ctx, `
		WITH me AS (SELECT level, damage, total FROM raid_members WHERE chat_id = $1 AND user_id = $2)
		SELECT
			CASE WHEN (SELECT level FROM me) = $3 AND (SELECT damage FROM me) > 0 THEN
				1 + (SELECT count(*) FROM raid_members WHERE chat_id = $1 AND level = $3 AND damage > (SELECT damage FROM me))
			ELSE 0 END,
			CASE WHEN (SELECT total FROM me) > 0 THEN
				1 + (SELECT count(*) FROM raid_members WHERE chat_id = $1 AND total > (SELECT total FROM me))
			ELSE 0 END`,
		chatID, userID, level).Scan(&levelRank, &totalRank)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("storage: raid ranks: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, name, damage, total FROM raid_members
		WHERE chat_id = $1 AND total > 0 ORDER BY total DESC, user_id LIMIT $2`, chatID, limit)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("storage: raid ranks: %w", err)
	}
	if top, err = pgx.CollectRows(rows, scanRaidMember); err != nil {
		return 0, 0, nil, fmt.Errorf("storage: raid ranks: %w", err)
	}
	return levelRank, totalRank, top, nil
}

// raidBoss is the chat's boss, the first level's made if there is none,
// and locked when lock is set.
func raidBoss(ctx context.Context, tx pgx.Tx, chatID int64, rules RaidRules, lock bool) (RaidBoss, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO raid_bosses (chat_id, hp, max_hp) VALUES ($1, $2, $2) ON CONFLICT (chat_id) DO NOTHING`,
		chatID, rules.MaxHP(1, 1)); err != nil {
		return RaidBoss{}, err
	}
	b := RaidBoss{ChatID: chatID}
	query := `SELECT level, hp, max_hp FROM raid_bosses WHERE chat_id = $1`
	if lock {
		query += ` FOR UPDATE`
	}
	return b, tx.QueryRow(ctx, query, chatID).Scan(&b.Level, &b.HP, &b.MaxHP)
}

const raidHeroColumns = `coins, sword, crit, helper, seed, taps, last_hit,
	storm_until, storm_ready, fire_left, fire_ready, rain_until, rain_ready,
	quest_day, quest_taps, quest_kills, quest_skills, quest_claimed, streak, streak_day`

// raidHero is the player's hero, a new one with seed made if there is
// none, and locked when lock is set.
func raidHero(ctx context.Context, tx pgx.Tx, userID int64, seed uint32, lock bool) (RaidHero, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO raid_heroes (user_id, seed) VALUES ($1, $2) ON CONFLICT (user_id) DO NOTHING`, userID, int64(seed)); err != nil {
		return RaidHero{}, err
	}
	h := RaidHero{UserID: userID}
	query := `SELECT ` + raidHeroColumns + ` FROM raid_heroes WHERE user_id = $1`
	if lock {
		query += ` FOR UPDATE`
	}
	var storedSeed int64
	var lastHit, stormUntil, stormReady, fireReady, rainUntil, rainReady, questDay, streakDay *time.Time
	if err := tx.QueryRow(ctx, query, userID).Scan(&h.Coins, &h.Sword, &h.Crit, &h.Helper, &storedSeed, &h.Taps, &lastHit,
		&stormUntil, &stormReady, &h.FireLeft, &fireReady, &rainUntil, &rainReady,
		&questDay, &h.QuestTaps, &h.QuestKills, &h.QuestSkills, &h.QuestClaimed, &h.Streak, &streakDay); err != nil {
		return RaidHero{}, err
	}
	h.Seed = uint32(storedSeed)
	for _, t := range []struct {
		from *time.Time
		to   *time.Time
	}{{lastHit, &h.LastHit}, {stormUntil, &h.StormUntil}, {stormReady, &h.StormReady}, {fireReady, &h.FireReady},
		{rainUntil, &h.RainUntil}, {rainReady, &h.RainReady}, {questDay, &h.QuestDay}, {streakDay, &h.StreakDay}} {
		if t.from != nil {
			*t.to = *t.from
		}
	}
	return h, nil
}

// saveRaidHero writes what can change of a hero.
func saveRaidHero(ctx context.Context, tx pgx.Tx, h RaidHero) error {
	_, err := tx.Exec(ctx, `
		UPDATE raid_heroes SET coins = $2, sword = $3, crit = $4, helper = $5, taps = $6, last_hit = $7,
			storm_until = $8, storm_ready = $9, fire_left = $10, fire_ready = $11, rain_until = $12, rain_ready = $13,
			quest_day = $14, quest_taps = $15, quest_kills = $16, quest_skills = $17, quest_claimed = $18,
			streak = $19, streak_day = $20, updated_at = now()
		WHERE user_id = $1`,
		h.UserID, h.Coins, h.Sword, h.Crit, h.Helper, h.Taps, timeOrNull(h.LastHit),
		timeOrNull(h.StormUntil), timeOrNull(h.StormReady), h.FireLeft, timeOrNull(h.FireReady),
		timeOrNull(h.RainUntil), timeOrNull(h.RainReady),
		timeOrNull(h.QuestDay), h.QuestTaps, h.QuestKills, h.QuestSkills, h.QuestClaimed, h.Streak, timeOrNull(h.StreakDay))
	return err
}

// timeOrNull is t, or NULL for the zero time.
func timeOrNull(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// raidTop is the chat's limit members who hit its boss at level the most.
func raidTop(ctx context.Context, tx pgx.Tx, chatID int64, level, limit int) ([]RaidMember, error) {
	rows, err := tx.Query(ctx, `
		SELECT user_id, name, damage, total FROM raid_members
		WHERE chat_id = $1 AND level = $2 AND damage > 0 ORDER BY damage DESC, user_id LIMIT $3`, chatID, level, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanRaidMember)
}

func scanRaidMember(row pgx.CollectableRow) (RaidMember, error) {
	var m RaidMember
	return m, row.Scan(&m.UserID, &m.Name, &m.Damage, &m.Total)
}
