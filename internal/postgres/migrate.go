package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationLockKey identifies the session-level advisory lock that
// serializes migrations across every process sharing a database.
const migrationLockKey int64 = 0x72756c6566617265 // "rulefare"

var migrationName = regexp.MustCompile(`^([0-9]{4,})_([a-z0-9_]+)\.sql$`)

// Migration is one forward-only schema change.
type Migration struct {
	Version  int64
	Name     string
	SQL      string
	Checksum string // hex SHA-256 of SQL
}

// LoadMigrations reads NNNN_description.sql files from the root of fsys,
// sorted by version. Other .sql names and duplicate versions are errors so
// that a typo cannot silently skip a migration; non-.sql files are ignored.
func LoadMigrations(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var migrations []Migration
	seen := map[int64]string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || path.Ext(name) != ".sql" {
			continue
		}
		m := migrationName.FindStringSubmatch(name)
		if m == nil {
			return nil, fmt.Errorf("migration %q must be named NNNN_description.sql", name)
		}
		version, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("migration %q has an invalid version", name)
		}
		if other, dup := seen[version]; dup {
			return nil, fmt.Errorf("migrations %q and %q share version %d", other, name, version)
		}
		seen[version] = name
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", name, err)
		}
		sum := sha256.Sum256(data)
		migrations = append(migrations, Migration{
			Version: version, Name: name, SQL: string(data), Checksum: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	return migrations, nil
}

// Migrate applies every pending migration from fsys and returns the
// versions it applied. It holds an advisory lock for the whole run, applies
// each file and its schema_migrations row in one transaction, and refuses to
// run if an applied migration was edited or removed, or if a new file sorts
// before one that is already applied.
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) (applied []int64, err error) {
	migrations, err := LoadMigrations(fsys)
	if err != nil {
		return nil, err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		return nil, fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		// Unlock even if ctx was cancelled; if that fails, closing the
		// session is what releases a session-level advisory lock.
		if _, unlockErr := conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLockKey); unlockErr != nil {
			conn.Conn().Close(context.Background())
			err = errors.Join(err, fmt.Errorf("release migration lock: %w", unlockErr))
		}
	}()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    BIGINT PRIMARY KEY,
		name       TEXT NOT NULL,
		checksum   TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}

	rows, err := conn.Query(ctx, "SELECT version, name, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	done := map[int64]Migration{}
	var latest int64
	for rows.Next() {
		var m Migration
		if err := rows.Scan(&m.Version, &m.Name, &m.Checksum); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read schema_migrations: %w", err)
		}
		done[m.Version] = m
		latest = m.Version
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}

	byVersion := map[int64]Migration{}
	for _, m := range migrations {
		byVersion[m.Version] = m
	}
	for version, record := range done {
		file, ok := byVersion[version]
		switch {
		case !ok:
			return nil, fmt.Errorf("applied migration %d (%s) is missing from this binary; the database is newer than the code", version, record.Name)
		case file.Checksum != record.Checksum:
			return nil, fmt.Errorf("applied migration %s was modified; add a new migration instead", file.Name)
		}
	}

	for _, m := range migrations {
		if _, ok := done[m.Version]; ok {
			continue
		}
		if m.Version < latest {
			return applied, fmt.Errorf("migration %s is older than applied version %d; renumber it", m.Name, latest)
		}
		if err := apply(ctx, conn, m); err != nil {
			return applied, err
		}
		applied = append(applied, m.Version)
	}
	return applied, nil
}

func apply(ctx context.Context, conn *pgxpool.Conn, m Migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", m.Name, err)
	}
	defer tx.Rollback(context.Background()) // No-op after Commit.
	// No arguments: pgx uses the simple protocol, so a file may hold
	// several statements.
	if _, err := tx.Exec(ctx, m.SQL); err != nil {
		return fmt.Errorf("apply migration %s: %w", m.Name, err)
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)",
		m.Version, m.Name, m.Checksum); err != nil {
		return fmt.Errorf("record migration %s: %w", m.Name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", m.Name, err)
	}
	return nil
}

// Pending reports how many migrations in fsys are not yet applied. It fails
// if schema_migrations does not exist, i.e. the database was never migrated.
func Pending(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) (int, error) {
	migrations, err := LoadMigrations(fsys)
	if err != nil {
		return 0, err
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(&exists); err != nil {
		return 0, fmt.Errorf("check schema_migrations: %w", err)
	}
	if !exists {
		return 0, errors.New("schema_migrations does not exist; the database was never migrated")
	}
	rows, err := pool.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return 0, fmt.Errorf("read schema_migrations: %w", err)
	}
	applied := map[int64]bool{}
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return 0, fmt.Errorf("read schema_migrations: %w", err)
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read schema_migrations: %w", err)
	}
	pending := 0
	for _, m := range migrations {
		if !applied[m.Version] {
			pending++
		}
	}
	return pending, nil
}
