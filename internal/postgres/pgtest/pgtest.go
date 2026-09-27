// Package pgtest gives integration tests an isolated PostgreSQL schema.
//
// Tests run against RULEFARE_TEST_DATABASE_URL. Without it they are skipped
// locally, but they fail when CI is set so the pipeline can never pass by
// skipping them.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const envURL = "RULEFARE_TEST_DATABASE_URL"

// URL returns the test database URL or skips (fails under CI) without it.
func URL(t testing.TB) string {
	t.Helper()
	url := os.Getenv(envURL)
	if url == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("%s is required in CI", envURL)
		}
		t.Skipf("%s not set; skipping PostgreSQL integration test", envURL)
	}
	return url
}

// SchemaURL creates an empty schema, drops it when the test ends, and
// returns the test database URL with search_path pointing at it. The base
// URL must use the postgres:// form.
func SchemaURL(t testing.TB) string {
	t.Helper()
	base := URL(t)
	ctx := context.Background()

	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	schema := "test_" + hex.EncodeToString(suffix)
	quoted := pgx.Identifier{schema}.Sanitize()

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		conn, err := pgx.Connect(context.Background(), base)
		if err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
			return
		}
		defer conn.Close(context.Background())
		if _, err := conn.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
	})

	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatalf("%s must be a postgres:// URL", envURL)
	}
	q := u.Query()
	q.Set("search_path", schema) // pgx sends unknown parameters as runtime settings.
	u.RawQuery = q.Encode()
	return u.String()
}

// Schema is SchemaURL plus a pool on that schema, closed at test end.
func Schema(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), SchemaURL(t))
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
