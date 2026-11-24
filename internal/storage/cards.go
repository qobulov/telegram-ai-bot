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
	// Serial is which copy of the card in the whole game this one is: the
	// copies of it ever drawn, this one with them.
	Serial int
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

// OwnedCard is a card a player holds, how many of it, how many of those
// are used up (melted once into shards, now by fusion), and its stars, ★1
// to ★5, which fusion raises.
type OwnedCard struct {
	Card
	Count  int
	Melted int
	Stars  int
	// Serial is, for a limited card, which holder of it the player is, by
	// when each first got it: their copy's number. 0 for other cards.
	Serial int
}

// CardPlayer is a player on the table of points.
type CardPlayer struct {
	UserID int64
	Name   string
	Points int64
	Cards  int
	// FreeDay is the day the player was last given the day's tokens, as the
	// midnight that began it, and Tokens how many they hold; only
	// CardPlayerOf reads them.
	FreeDay  time.Time
	FreeUsed int
	Tokens   int
	// SinceLegendary and SinceMythic are the cards the player drew in a
	// row since their last Legendary or better and last Mythic.
	SinceLegendary int
	SinceMythic    int
}

// CardDay is a period as the tokens count it: when it began, the tokens a
// player is given in it, how long one lasts, and how many periods' tokens
// a player who stayed away gathers at most.
type CardDay struct {
	Start  time.Time
	PerDay int
	Every  time.Duration
	Most   int
}

// Next is when the next period's tokens come.
func (d CardDay) Next() time.Time {
	if d.Every > 0 {
		return d.Start.Add(d.Every)
	}
	return d.Start.AddDate(0, 0, 1)
}

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

// cardPlayerRow is a player's row as a draw reads it, locked.
type cardPlayerRow struct {
	last                        *time.Time
	sinceLegendary, sinceMythic int
	shards, tokens              int
	freeDay                     *time.Time
	freeUsed                    int
}

// WelcomeTokens are the tokens a player new to the cards is given when their
// row is made, until WelcomeUntil: the owners' welcome of 2026-11-18, for
// two weeks (the players there then were given 99 once, by hand; 69 for rows made since 2026-11-19).
var (
	WelcomeTokens = 69
	WelcomeUntil  = time.Date(2026, 12, 2, 0, 0, 0, 0, time.FixedZone("Asia/Tashkent", 5*60*60))
)

// welcomeTokens are the tokens a row made now starts with.
func welcomeTokens() int {
	if time.Now().Before(WelcomeUntil) {
		return WelcomeTokens
	}
	return 0
}

// lockCardPlayer makes the player's row if there is none and locks it, so
// that two draws at once are two draws, the second waiting its turn.
func lockCardPlayer(ctx context.Context, tx pgx.Tx, userID int64, name string) (cardPlayerRow, error) {
	var p cardPlayerRow
	if _, err := tx.Exec(ctx, `
		INSERT INTO card_players (user_id, name, tokens) VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO NOTHING`, userID, name, welcomeTokens()); err != nil {
		return p, err
	}
	err := tx.QueryRow(ctx, `SELECT last_draw_at, since_legendary, since_mythic, shards, tokens, free_day, free_used FROM card_players WHERE user_id = $1 FOR UPDATE`, userID).
		Scan(&p.last, &p.sinceLegendary, &p.sinceMythic, &p.shards, &p.tokens, &p.freeDay, &p.freeUsed)
	return p, err
}

// giveCard gives the player a card of the rarity roll picks, at random, of
// collection or of any when it is "", and counts it as addCard does.
func giveCard(ctx context.Context, tx pgx.Tx, userID int64, name, collection string, roll CardRoll, player cardPlayerRow) (CardDraw, error) {
	rarity, guaranteed := roll(player.sinceLegendary, player.sinceMythic)
	card, err := randomCard(ctx, tx, rarity, collection, nil)
	if err != nil {
		return CardDraw{Guaranteed: guaranteed}, err
	}
	return addCard(ctx, tx, userID, name, card, guaranteed)
}

// randomCard is a card of rarity at random, of collection or of any when it
// is "", and never one of but nor a card no draw gives (a limited card or
// one only its event gives).
func randomCard(ctx context.Context, tx pgx.Tx, rarity int, collection string, but []int64) (Card, error) {
	var c Card
	err := tx.QueryRow(ctx, `
		SELECT id, name, anime, image_url, favourites, rarity, attack, health, value, collection
		FROM card_catalog WHERE rarity = $1 AND ($2 = '' OR collection = $2) AND id <> ALL($3::bigint[]) AND id <> ALL($4::bigint[])
		ORDER BY random() LIMIT 1`, rarity, collection, append([]int64{}, but...), undrawnCardIDs()).
		Scan(&c.ID, &c.Name, &c.Anime, &c.ImageURL, &c.Favourites, &c.Rarity, &c.Attack, &c.Health, &c.Value, &c.Collection)
	return c, err
}

// addCard gives the player card and counts it in their wait for a rare
// card, and in their points and cards when it is new to them: a copy of a
// card held adds no points. It counts the copy in the card's copies ever
// drawn, the draw's Serial.
func addCard(ctx context.Context, tx pgx.Tx, userID int64, name string, card Card, guaranteed bool) (CardDraw, error) {
	draw := CardDraw{Card: card, Guaranteed: guaranteed}
	c := &draw.Card
	if err := tx.QueryRow(ctx, `UPDATE card_catalog SET drawn = drawn + 1 WHERE id = $1 RETURNING drawn`, c.ID).Scan(&draw.Serial); err != nil {
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
	err := tx.QueryRow(ctx, `
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
	return s.ownedCards(ctx, userID, rarity, nil)
}

// OwnedCardsOf are those of the player's cards whose ids are given, as
// OwnedCards gives them: an arena team's five, not the whole collection
// (reading whole collections for every team shown was most of the
// database's traffic, as the owners saw on 2026-11-23).
func (s *Store) OwnedCardsOf(ctx context.Context, userID int64, ids []int64) ([]OwnedCard, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return s.ownedCards(ctx, userID, -1, ids)
}

// ownedCards are the player's cards of rarity (all at -1), only those of ids
// unless ids is nil.
func (s *Store) ownedCards(ctx context.Context, userID int64, rarity int, ids []int64) ([]OwnedCard, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, c.anime, c.image_url, c.favourites, c.rarity, c.attack, c.health, c.value, c.collection, o.count, o.melted, o.stars,
			CASE WHEN o.card_id = ANY($3) THEN (
				SELECT count(*) FROM card_owned h
				WHERE h.card_id = o.card_id AND (h.first_at, h.user_id) <= (o.first_at, o.user_id))::int
			ELSE 0 END
		FROM card_owned o JOIN card_catalog c ON c.id = o.card_id
		WHERE o.user_id = $1 AND ($2 < 0 OR c.rarity = $2) AND ($4::bigint[] IS NULL OR o.card_id = ANY($4))
		ORDER BY c.rarity DESC, c.favourites DESC`, userID, rarity, limitedCardIDs(), ids)
	if err != nil {
		return nil, fmt.Errorf("storage: list a player's cards: %w", err)
	}
	cards, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (OwnedCard, error) {
		var o OwnedCard
		err := row.Scan(&o.ID, &o.Name, &o.Anime, &o.ImageURL, &o.Favourites, &o.Rarity, &o.Attack, &o.Health, &o.Value, &o.Collection, &o.Count, &o.Melted, &o.Stars, &o.Serial)
		return o, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list a player's cards: %w", err)
	}
	return cards, nil
}

// OwnedCopy is a card the player holds by its id alone, its catalog numbers
// left to the catalog the caller already has.
type OwnedCopy struct {
	CardID                       int64
	Count, Melted, Stars, Serial int
}

// OwnedCopies are the player's cards as OwnedCards gives them, but by id
// alone, without the catalog's names, animes and pictures, which made most
// of what a collection sent from the database (the owners saw Supabase's
// egress pass its limit on 2026-11-23).
func (s *Store) OwnedCopies(ctx context.Context, userID int64) ([]OwnedCopy, error) {
	return s.ownedCopies(ctx, userID, nil)
}

// OwnedCopiesOf are OwnedCopies of ids alone.
func (s *Store) OwnedCopiesOf(ctx context.Context, userID int64, ids []int64) ([]OwnedCopy, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return s.ownedCopies(ctx, userID, ids)
}

// ownedCopies reads the player's copies, of ids unless nil, in two parts,
// as the owners asked on 2026-11-24 when Supabase's egress passed its limit
// threefold: the plain ones, one copy at ★1 and none melted, most of a
// collection, come as one array of ids, the others row by row.
func (s *Store) ownedCopies(ctx context.Context, userID int64, ids []int64) ([]OwnedCopy, error) {
	const plain = `o.count = 1 AND o.melted = 0 AND o.stars = 1 AND NOT o.card_id = ANY($2)`
	var plainIDs []int64
	if err := s.pool.QueryRow(ctx, `
		SELECT coalesce(array_agg(o.card_id), '{}') FROM card_owned o
		WHERE o.user_id = $1 AND ($3::bigint[] IS NULL OR o.card_id = ANY($3)) AND `+plain,
		userID, limitedCardIDs(), ids).Scan(&plainIDs); err != nil {
		return nil, fmt.Errorf("storage: list a player's copies: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT o.card_id, o.count, o.melted, o.stars,
			CASE WHEN o.card_id = ANY($2) THEN (
				SELECT count(*) FROM card_owned h
				WHERE h.card_id = o.card_id AND (h.first_at, h.user_id) <= (o.first_at, o.user_id))::int
			ELSE 0 END
		FROM card_owned o
		WHERE o.user_id = $1 AND ($3::bigint[] IS NULL OR o.card_id = ANY($3)) AND NOT (`+plain+`)`,
		userID, limitedCardIDs(), ids)
	if err != nil {
		return nil, fmt.Errorf("storage: list a player's copies: %w", err)
	}
	copies, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (OwnedCopy, error) {
		var c OwnedCopy
		return c, row.Scan(&c.CardID, &c.Count, &c.Melted, &c.Stars, &c.Serial)
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list a player's copies: %w", err)
	}
	for _, id := range plainIDs {
		copies = append(copies, OwnedCopy{CardID: id, Count: 1, Stars: 1})
	}
	return copies, nil
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

// CardCatalogStamp is a fingerprint of what CardCatalog reads: it changes
// when any card's name, picture, rarity or numbers change, or a card comes
// or goes, and a few bytes cross the network instead of the whole catalog.
func (s *Store) CardCatalogStamp(ctx context.Context) (string, error) {
	var stamp string
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) || ':' || coalesce(md5(string_agg(
			concat_ws('|', id, name, anime, image_url, favourites, rarity, attack, health, value, collection),
			',' ORDER BY id)), '')
		FROM card_catalog`).Scan(&stamp)
	if err != nil {
		return "", fmt.Errorf("storage: read the card catalog's stamp: %w", err)
	}
	return stamp, nil
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
	err := s.pool.QueryRow(ctx, `SELECT name, points, cards, last_draw_at, free_day, free_used, tokens, since_legendary, since_mythic FROM card_players WHERE user_id = $1`, userID).
		Scan(&p.Name, &p.Points, &p.Cards, &last, &freeDay, &p.FreeUsed, &p.Tokens, &p.SinceLegendary, &p.SinceMythic)
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
