package storage

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Card is one card of Kartalar: an anime character, its rarity from 0, the
// most common, and what it is worth.
type Card struct {
	ID         int64
	Name       string
	Anime      string
	ImageURL   string
	Favourites int
	Rarity     int
	Attack     int
	Health     int
	Value      int
	// Collection is what it is a card of: "anime", the only one now.
	Collection string
}

// CardDraw is what a draw gave: the card, whether the player had none of
// it before, how many they hold now and their points after it; whether the
// draw's roll was raised by the player's long wait for a rare card, and the
// draws in a row since their last Legendary or better and last Mythic.
type CardDraw struct {
	Card           Card
	New            bool
	Count          int
	Points         int64
	Guaranteed     bool
	SinceLegendary int
	SinceMythic    int
	// Shards are the player's 💎 shards after it.
	Shards int
	// Free is a draw of the day's free cards, and FreeLeft how many the
	// player may still open today.
	Free     bool
	FreeLeft int
}

// CardRoll picks a draw's rarity from the player's draws in a row since
// their last Legendary or better and last Mythic, and reports a rarity it
// raised for them.
type CardRoll func(sinceLegendary, sinceMythic int) (rarity int, guaranteed bool)

// Rarities as the catalog numbers them, which a draw counts the player's
// wait for a rare card by.
const (
	legendaryRarity = 3
	mythicRarity    = 4
)

// OwnedCard is a card a player holds, how many of it, and how many of those
// are melted into shards.
type OwnedCard struct {
	Card
	Count  int
	Melted int
}

// CardPlayer is a player on the table of points.
type CardPlayer struct {
	UserID int64
	Name   string
	Points int64
	Cards  int
	// Auto is whether the auto opener opens their cards; only
	// CardPlayerOf reads it.
	Auto bool
	// Username is their @username without the @; only CardPlayersNamed
	// reads it.
	Username string
	// FreeDay is the day the player last opened a free card, as the
	// midnight that began it, and FreeUsed how many they opened that day;
	// only CardPlayerOf reads them.
	FreeDay  time.Time
	FreeUsed int
}

// CardDay is the day's free cards as a draw counts them: the midnight that
// began the day and how many a player opens free in it; a PerDay of 0
// never makes a player wait.
type CardDay struct {
	Start  time.Time
	PerDay int
}

// ErrCardWait is returned by DrawCard when the player drew too lately: the
// wait is in CardWaitError.
var ErrCardWait = errors.New("storage: too soon to draw a card")

// CardWaitError says how long the player must wait for their next card.
type CardWaitError struct{ Wait time.Duration }

func (e *CardWaitError) Error() string { return ErrCardWait.Error() }
func (e *CardWaitError) Unwrap() error { return ErrCardWait }

// CardCollections counts the cards of each collection there is.
func (s *Store) CardCollections(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT collection, count(*) FROM card_catalog GROUP BY collection`)
	if err != nil {
		return nil, fmt.Errorf("storage: count cards: %w", err)
	}
	defer rows.Close()
	sizes := map[string]int{}
	for rows.Next() {
		var collection string
		var n int
		if err := rows.Scan(&collection, &n); err != nil {
			return nil, fmt.Errorf("storage: count cards: %w", err)
		}
		sizes[collection] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: count cards: %w", err)
	}
	return sizes, nil
}

// SaveCardCatalog adds the cards, and updates those there are: a card held
// is never taken away. A card with a picture of its own in card_pictures,
// a sharper one chosen by hand, keeps it over the picture given.
func (s *Store) SaveCardCatalog(ctx context.Context, cards []Card) error {
	batch := &pgx.Batch{}
	for _, c := range cards {
		batch.Queue(`
			INSERT INTO card_catalog (id, name, anime, image_url, favourites, rarity, attack, health, value, collection)
			VALUES ($1, $2, $3, coalesce((SELECT image_url FROM card_pictures WHERE card_id = $1), $4), $5, $6, $7, $8, $9, $10)
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, anime = EXCLUDED.anime, image_url = EXCLUDED.image_url,
				favourites = EXCLUDED.favourites, rarity = EXCLUDED.rarity, attack = EXCLUDED.attack,
				health = EXCLUDED.health, value = EXCLUDED.value, collection = EXCLUDED.collection, updated_at = now()`,
			c.ID, c.Name, c.Anime, c.ImageURL, c.Favourites, c.Rarity, c.Attack, c.Health, c.Value, cmp.Or(c.Collection, "anime"))
	}
	if err := s.pool.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("storage: save cards: %w", err)
	}
	return nil
}

// DrawCard gives the player a free card of the rarity roll picks, at
// random, unless they opened day.PerDay of them since day.Start, when it
// returns a *CardWaitError with the wait until the next midnight. The
// player's row is locked, so that two draws at once are two draws, the
// second waiting its turn. A player who turned on the card reminders, and
// has opened the day's last, gets one for the next midnight.
func (s *Store) DrawCard(ctx context.Context, userID int64, name string, roll CardRoll, day CardDay, now time.Time) (CardDraw, error) {
	var draw CardDraw
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		player, err := lockCardPlayer(ctx, tx, userID, name)
		if err != nil {
			return err
		}
		used := 0
		if player.freeDay != nil && player.freeDay.Equal(day.Start) {
			used = player.freeUsed
		}
		next := day.Start.AddDate(0, 0, 1)
		if day.PerDay > 0 && used >= day.PerDay {
			return &CardWaitError{Wait: next.Sub(now)}
		}
		if draw, err = giveCard(ctx, tx, userID, name, "", roll, player); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE card_players SET last_draw_at = $2, free_day = $3, free_used = $4 WHERE user_id = $1`,
			userID, now, day.Start, used+1); err != nil {
			return err
		}
		draw.Free, draw.FreeLeft = true, max(0, day.PerDay-used-1)
		if day.PerDay <= 0 || draw.FreeLeft > 0 {
			return nil
		}
		// A player who asked to be reminded of every card is reminded when
		// the next day's cards come, in place of the reminder this draw
		// made stale.
		_, err = tx.Exec(ctx, `
			INSERT INTO reminders (chat_id, user_id, user_name, kind, language, due_at)
			SELECT remind_chat_id, user_id, name, 'card', remind_language, $2
			FROM card_players WHERE user_id = $1 AND remind_chat_id IS NOT NULL
			ON CONFLICT (user_id) WHERE kind = 'card' AND claimed_at IS NULL DO UPDATE SET
				chat_id = EXCLUDED.chat_id, user_name = EXCLUDED.user_name, message_id = 0,
				language = EXCLUDED.language, due_at = EXCLUDED.due_at`, userID, next)
		return err
	})
	var wait *CardWaitError
	if errors.As(err, &wait) {
		return CardDraw{}, wait
	}
	if err != nil {
		return CardDraw{}, fmt.Errorf("storage: draw a card: %w", err)
	}
	return draw, nil
}

// cardPlayerRow is a player's row as a draw reads it, locked.
type cardPlayerRow struct {
	last                        *time.Time
	sinceLegendary, sinceMythic int
	shards                      int
	freeDay                     *time.Time
	freeUsed                    int
}

// lockCardPlayer makes the player's row if there is none and locks it, so
// that two draws at once are two draws, the second waiting its turn.
func lockCardPlayer(ctx context.Context, tx pgx.Tx, userID int64, name string) (cardPlayerRow, error) {
	var p cardPlayerRow
	if _, err := tx.Exec(ctx, `
		INSERT INTO card_players (user_id, name) VALUES ($1, $2)
		ON CONFLICT (user_id) DO NOTHING`, userID, name); err != nil {
		return p, err
	}
	err := tx.QueryRow(ctx, `SELECT last_draw_at, since_legendary, since_mythic, shards, free_day, free_used FROM card_players WHERE user_id = $1 FOR UPDATE`, userID).
		Scan(&p.last, &p.sinceLegendary, &p.sinceMythic, &p.shards, &p.freeDay, &p.freeUsed)
	return p, err
}

// giveCard gives the player a card of the rarity roll picks, at random, of
// collection or of any when it is "", and counts it in their wait for a
// rare card, and in their points and cards when it is new to them: a copy
// of a card held adds no points.
func giveCard(ctx context.Context, tx pgx.Tx, userID int64, name, collection string, roll CardRoll, player cardPlayerRow) (CardDraw, error) {
	var draw CardDraw
	rarity, guaranteed := roll(player.sinceLegendary, player.sinceMythic)
	draw.Guaranteed = guaranteed
	c := &draw.Card
	err := tx.QueryRow(ctx, `
		SELECT id, name, anime, image_url, favourites, rarity, attack, health, value, collection
		FROM card_catalog WHERE rarity = $1 AND ($2 = '' OR collection = $2) ORDER BY random() LIMIT 1`, rarity, collection).
		Scan(&c.ID, &c.Name, &c.Anime, &c.ImageURL, &c.Favourites, &c.Rarity, &c.Attack, &c.Health, &c.Value, &c.Collection)
	if err != nil {
		return draw, err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO card_owned (user_id, card_id) VALUES ($1, $2)
		ON CONFLICT (user_id, card_id) DO UPDATE SET count = card_owned.count + 1
		RETURNING count`, userID, c.ID).Scan(&draw.Count); err != nil {
		return draw, err
	}
	draw.New = draw.Count == 1
	newCard, gained := 0, 0
	if draw.New {
		newCard, gained = 1, c.Value
	}
	err = tx.QueryRow(ctx, `
		UPDATE card_players SET name = coalesce(nullif($4, ''), name), points = points + $2,
			cards = cards + $3, draws = draws + 1,
			since_legendary = CASE WHEN $5 THEN 0 ELSE since_legendary + 1 END,
			since_mythic = CASE WHEN $6 THEN 0 ELSE since_mythic + 1 END
		WHERE user_id = $1 RETURNING points, since_legendary, since_mythic, shards`,
		userID, gained, newCard, name, c.Rarity >= legendaryRarity, c.Rarity >= mythicRarity).
		Scan(&draw.Points, &draw.SinceLegendary, &draw.SinceMythic, &draw.Shards)
	return draw, err
}

// CardCounts gives, for each rarity, how many cards there are and how
// many of them the player holds, and the player's points.
func (s *Store) CardCounts(ctx context.Context, userID int64) (total, owned map[int]int, points int64, err error) {
	total, owned = map[int]int{}, map[int]int{}
	rows, err := s.pool.Query(ctx, `
		SELECT c.rarity, count(*), count(o.card_id) FROM card_catalog c
		LEFT JOIN card_owned o ON o.card_id = c.id AND o.user_id = $1
		GROUP BY c.rarity`, userID)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("storage: count a player's cards: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rarity, all, held int
		if err := rows.Scan(&rarity, &all, &held); err != nil {
			return nil, nil, 0, fmt.Errorf("storage: count a player's cards: %w", err)
		}
		total[rarity], owned[rarity] = all, held
	}
	if err := rows.Err(); err != nil {
		return nil, nil, 0, fmt.Errorf("storage: count a player's cards: %w", err)
	}
	err = s.pool.QueryRow(ctx, `SELECT coalesce((SELECT points FROM card_players WHERE user_id = $1), 0)`, userID).Scan(&points)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("storage: read a player's points: %w", err)
	}
	return total, owned, points, nil
}

// OwnedCards are the player's cards of rarity, or of every rarity when it
// is negative, the rarest and most loved first.
func (s *Store) OwnedCards(ctx context.Context, userID int64, rarity int) ([]OwnedCard, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, c.anime, c.image_url, c.favourites, c.rarity, c.attack, c.health, c.value, c.collection, o.count, o.melted
		FROM card_owned o JOIN card_catalog c ON c.id = o.card_id
		WHERE o.user_id = $1 AND ($2 < 0 OR c.rarity = $2)
		ORDER BY c.rarity DESC, c.favourites DESC`, userID, rarity)
	if err != nil {
		return nil, fmt.Errorf("storage: list a player's cards: %w", err)
	}
	cards, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (OwnedCard, error) {
		var o OwnedCard
		err := row.Scan(&o.ID, &o.Name, &o.Anime, &o.ImageURL, &o.Favourites, &o.Rarity, &o.Attack, &o.Health, &o.Value, &o.Collection, &o.Count, &o.Melted)
		return o, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list a player's cards: %w", err)
	}
	return cards, nil
}

// CardTop is the players with the most points.
func (s *Store) CardTop(ctx context.Context, limit int) ([]CardPlayer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, name, points, cards FROM card_players
		WHERE points > 0 ORDER BY points DESC, user_id LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: read the card table: %w", err)
	}
	top, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CardPlayer, error) {
		var p CardPlayer
		err := row.Scan(&p.UserID, &p.Name, &p.Points, &p.Cards)
		return p, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read the card table: %w", err)
	}
	return top, nil
}

// CardCatalog is every card, the rarest and most loved first.
func (s *Store) CardCatalog(ctx context.Context) ([]Card, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, anime, image_url, favourites, rarity, attack, health, value, collection
		FROM card_catalog ORDER BY rarity DESC, favourites DESC`)
	if err != nil {
		return nil, fmt.Errorf("storage: read the card catalog: %w", err)
	}
	catalog, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Card, error) {
		var c Card
		err := row.Scan(&c.ID, &c.Name, &c.Anime, &c.ImageURL, &c.Favourites, &c.Rarity, &c.Attack, &c.Health, &c.Value, &c.Collection)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read the card catalog: %w", err)
	}
	return catalog, nil
}

// CardPlayerOf is the player's points and cards held, the day's free cards
// they opened, and when they last drew one; zero for one who never did.
func (s *Store) CardPlayerOf(ctx context.Context, userID int64) (CardPlayer, time.Time, error) {
	p := CardPlayer{UserID: userID}
	var last, freeDay *time.Time
	err := s.pool.QueryRow(ctx, `SELECT name, points, cards, last_draw_at, auto_draw, free_day, free_used FROM card_players WHERE user_id = $1`, userID).
		Scan(&p.Name, &p.Points, &p.Cards, &last, &p.Auto, &freeDay, &p.FreeUsed)
	if freeDay != nil {
		p.FreeDay = *freeDay
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return p, time.Time{}, nil
	}
	if err != nil {
		return CardPlayer{}, time.Time{}, fmt.Errorf("storage: read a card player: %w", err)
	}
	if last != nil {
		return p, *last, nil
	}
	return p, time.Time{}, nil
}
