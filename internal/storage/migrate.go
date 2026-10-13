package storage

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// ErrSchemaOutdated means the database is missing migrations this binary needs.
var ErrSchemaOutdated = errors.New("storage: database schema is out of date; run the migrate command")

// migrationLockID is an arbitrary constant identifying this application's
// advisory lock, so two migrators never run at the same time.
const migrationLockID = 7203145500913

const undefinedTable = "42P01"

type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations reads the embedded files, named like 0001_users.sql, in
// version order.
func loadMigrations() ([]migration, error) {
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	migrations := make([]migration, 0, len(names))
	for _, name := range names {
		base := strings.TrimPrefix(name, "migrations/")
		prefix, _, _ := strings.Cut(base, "_")
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("storage: migration %q must start with a number", base)
		}
		content, err := migrationFiles.ReadFile(name)
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, migration{version: version, name: base, sql: string(content)})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	return migrations, nil
}

// Migrate applies every migration the database does not have yet and returns
// how many it applied. Each migration runs in its own transaction, so a
// failure leaves the database at the last good version. Applied migrations
// are recorded in schema_migrations and never run twice.
func (s *Store) Migrate(ctx context.Context) (int, error) {
	migrations, err := loadMigrations()
	if err != nil {
		return 0, err
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("storage: acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return 0, fmt.Errorf("storage: lock migrations: %w", err)
	}
	// Unlock even if ctx was cancelled mid-migration.
	defer conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLockID)

	current, err := prepareVersionTable(ctx, conn.Conn())
	if err != nil {
		return 0, err
	}
	applied := 0
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		if err := applyMigration(ctx, conn.Conn(), m); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

func prepareVersionTable(ctx context.Context, conn *pgx.Conn) (int, error) {
	_, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    integer     PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`)
	if err != nil {
		return 0, fmt.Errorf("storage: create schema_migrations: %w", err)
	}
	return currentVersion(ctx, conn)
}

type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func currentVersion(ctx context.Context, db queryRower) (int, error) {
	var version int
	err := db.QueryRow(ctx, `SELECT coalesce(max(version), 0) FROM schema_migrations`).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("storage: read schema version: %w", err)
	}
	return version, nil
}

func applyMigration(ctx context.Context, conn *pgx.Conn, m migration) error {
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		// Without arguments pgx uses the simple protocol, which allows the
		// several statements a migration file contains.
		if _, err := tx.Exec(ctx, m.sql); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, m.version)
		return err
	})
	if err != nil {
		return fmt.Errorf("storage: apply migration %s: %w", m.name, err)
	}
	return nil
}

// CheckSchema reports ErrSchemaOutdated when the database has not been
// migrated to the version this binary was built with.
func (s *Store) CheckSchema(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	current, err := currentVersion(ctx, s.pool)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == undefinedTable {
		return ErrSchemaOutdated
	}
	if err != nil {
		return err
	}
	if latest := migrations[len(migrations)-1].version; current < latest {
		return ErrSchemaOutdated
	}
	return nil
}
