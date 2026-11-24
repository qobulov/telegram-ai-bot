// Package storage persists users, conversations and messages in PostgreSQL.
//
// Every query that reads or changes a conversation is keyed by the Telegram
// user ID, so one user can never reach another user's history.
package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is a pooled connection to the bot's database. It is safe for
// concurrent use.
type Store struct {
	pool *pgxpool.Pool
}

// Open creates the connection pool and verifies the database is reachable.
// Pool size can be tuned in the URL, for example "?pool_max_conns=10".
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		// The parse error can echo the URL, which contains the password.
		return nil, errors.New("storage: DB_POSTGRES_URL is not a valid PostgreSQL connection string")
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("storage: create pool: %w", err)
	}
	store := &Store{pool: pool}
	if err := store.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	openStores.Lock()
	openStores.all[store] = struct{}{}
	openStores.Unlock()
	return store, nil
}

// openStores are the stores of this process not closed yet, for
// ReleaseConnections.
var openStores = struct {
	sync.Mutex
	all map[*Store]struct{}
}{all: map[*Store]struct{}{}}

// Close releases all connections.
func (s *Store) Close() {
	openStores.Lock()
	delete(openStores.all, s)
	openStores.Unlock()
	s.pool.Close()
}

// ReleaseConnections closes the connections of every open store: the idle
// ones at once, those in use when they are given back. The stores stay
// usable and connect again when next asked. A serverless instance calls it
// when its last request is done, before it is frozen: a frozen instance
// cannot close the connections it holds, and a database that allows few,
// as Aiven's free plan does, then refuses everyone else.
func ReleaseConnections() {
	openStores.Lock()
	defer openStores.Unlock()
	for s := range openStores.all {
		s.pool.Reset()
	}
}

// Ping checks that the database answers.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("storage: database is unreachable: %w", err)
	}
	return nil
}
