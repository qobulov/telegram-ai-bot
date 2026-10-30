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
// and their taps so far, which with Seed say which taps are critical.
type RaidHero struct {
	UserID  int64
	Coins   int64
	Sword   int
	Crit    int
	Helper  int
	Seed    uint32
	Taps    int64
	LastHit time.Time
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
	// MaxHP is a boss's at level.
	MaxHP func(level int) int64
	// Strike is what a hero's taps do, given how long since their last
	// hit: how many of the taps count, and their damage.
	Strike func(hero RaidHero, taps int, since time.Duration) (counted int, damage int64)
	// Reward is the coins one who hit a killed boss with damage gets.
	Reward func(level int, damage, maxHP int64) int64
	// ScoreEvery is how often a member's total is due as their Telegram
	// score.
	ScoreEvery time.Duration
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
// next level's boss takes its place.
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
		counted, damage := rules.Strike(hero, taps, since)
		damage = min(damage, boss.HP)
		if err := tx.QueryRow(ctx, `
			UPDATE raid_heroes SET taps = taps + $2, coins = coins + $2, last_hit = $3, updated_at = now()
			WHERE user_id = $1 RETURNING taps, coins`,
			userID, counted, now).Scan(&hero.Taps, &hero.Coins); err != nil {
			return err
		}
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
			if killed, err = killRaidBoss(ctx, tx, boss, rules); err != nil {
				return err
			}
			for _, f := range killed.Fighters {
				if f.UserID == userID {
					hero.Coins += f.Reward
				}
			}
			boss = RaidBoss{ChatID: chatID, Level: boss.Level + 1, MaxHP: rules.MaxHP(boss.Level + 1)}
			boss.HP = boss.MaxHP
			member.Damage = 0
		}
		if _, err := tx.Exec(ctx, `UPDATE raid_bosses SET level = $2, hp = $3, max_hp = $4, updated_at = now() WHERE chat_id = $1`,
			chatID, boss.Level, boss.HP, boss.MaxHP); err != nil {
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

// killRaidBoss rewards everyone who hit the boss, most damage first.
func killRaidBoss(ctx context.Context, tx pgx.Tx, boss RaidBoss, rules RaidRules) (*RaidKill, error) {
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
	kill := &RaidKill{Level: boss.Level, MaxHP: boss.MaxHP}
	ids, rewards := make([]int64, 0, len(members)), make([]int64, 0, len(members))
	for _, m := range members {
		reward := rules.Reward(boss.Level, m.Damage, boss.MaxHP)
		kill.Fighters = append(kill.Fighters, RaidFighter{RaidMember: m, Reward: reward})
		ids, rewards = append(ids, m.UserID), append(rewards, reward)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE raid_heroes h SET coins = h.coins + r.reward, updated_at = now()
		FROM unnest($1::bigint[], $2::bigint[]) AS r(user_id, reward) WHERE h.user_id = r.user_id`, ids, rewards); err != nil {
		return nil, err
	}
	return kill, nil
}

// UpgradeRaidHero raises one of the hero's upgrades, "sword", "crit" or
// "helper", by a level for what cost says the next level costs, below most.
func (s *Store) UpgradeRaidHero(ctx context.Context, userID int64, item string, cost func(level int) int64, most int, seed uint32) (RaidHero, error) {
	column, ok := map[string]string{"sword": "sword", "crit": "crit", "helper": "helper"}[item]
	if !ok {
		return RaidHero{}, fmt.Errorf("storage: no raid upgrade %q", item)
	}
	var hero RaidHero
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		if hero, err = raidHero(ctx, tx, userID, seed, true); err != nil {
			return err
		}
		level := map[string]int{"sword": hero.Sword, "crit": hero.Crit, "helper": hero.Helper}[item]
		if level >= most {
			return ErrRaidMax
		}
		price := cost(level)
		if hero.Coins < price {
			return ErrRaidCoins
		}
		return tx.QueryRow(ctx, `
			UPDATE raid_heroes SET coins = coins - $2, `+column+` = `+column+` + 1, updated_at = now()
			WHERE user_id = $1 RETURNING coins, sword, crit, helper`, userID, price).
			Scan(&hero.Coins, &hero.Sword, &hero.Crit, &hero.Helper)
	})
	if errors.Is(err, ErrRaidCoins) || errors.Is(err, ErrRaidMax) {
		return RaidHero{}, err
	}
	if err != nil {
		return RaidHero{}, fmt.Errorf("storage: upgrade raid hero: %w", err)
	}
	return hero, nil
}

// raidBoss is the chat's boss, the first level's made if there is none,
// and locked when lock is set.
func raidBoss(ctx context.Context, tx pgx.Tx, chatID int64, rules RaidRules, lock bool) (RaidBoss, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO raid_bosses (chat_id, hp, max_hp) VALUES ($1, $2, $2) ON CONFLICT (chat_id) DO NOTHING`,
		chatID, rules.MaxHP(1)); err != nil {
		return RaidBoss{}, err
	}
	b := RaidBoss{ChatID: chatID}
	query := `SELECT level, hp, max_hp FROM raid_bosses WHERE chat_id = $1`
	if lock {
		query += ` FOR UPDATE`
	}
	return b, tx.QueryRow(ctx, query, chatID).Scan(&b.Level, &b.HP, &b.MaxHP)
}

// raidHero is the player's hero, a new one with seed made if there is
// none, and locked when lock is set.
func raidHero(ctx context.Context, tx pgx.Tx, userID int64, seed uint32, lock bool) (RaidHero, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO raid_heroes (user_id, seed) VALUES ($1, $2) ON CONFLICT (user_id) DO NOTHING`, userID, int64(seed)); err != nil {
		return RaidHero{}, err
	}
	h := RaidHero{UserID: userID}
	query := `SELECT coins, sword, crit, helper, seed, taps, last_hit FROM raid_heroes WHERE user_id = $1`
	if lock {
		query += ` FOR UPDATE`
	}
	var storedSeed int64
	var lastHit *time.Time
	if err := tx.QueryRow(ctx, query, userID).Scan(&h.Coins, &h.Sword, &h.Crit, &h.Helper, &storedSeed, &h.Taps, &lastHit); err != nil {
		return RaidHero{}, err
	}
	h.Seed = uint32(storedSeed)
	if lastHit != nil {
		h.LastHit = *lastHit
	}
	return h, nil
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
