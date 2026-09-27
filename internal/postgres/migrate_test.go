package postgres_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/Khr0x/rulefare/internal/postgres"
	"github.com/Khr0x/rulefare/internal/postgres/pgtest"
	"github.com/Khr0x/rulefare/migrations"
)

func files(named map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, sql := range named {
		fsys[name] = &fstest.MapFile{Data: []byte(sql)}
	}
	return fsys
}

var twoTables = map[string]string{
	"0001_first.sql": "CREATE TABLE first (id BIGINT PRIMARY KEY);",
	// Several statements in one file must work.
	"0002_second.sql": "CREATE TABLE second (id BIGINT PRIMARY KEY);\nINSERT INTO second VALUES (1), (2);",
	"README.md":       "ignored",
}

func TestLoadMigrationsSortsAndIgnoresOtherFiles(t *testing.T) {
	got, err := postgres.LoadMigrations(files(map[string]string{
		"0010_later.sql": "SELECT 1;", "0002_early.sql": "SELECT 2;", "notes.txt": "x",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Version != 2 || got[1].Version != 10 || got[0].Name != "0002_early.sql" {
		t.Fatalf("got %+v", got)
	}
	if len(got[0].Checksum) != 64 {
		t.Fatalf("checksum %q is not hex SHA-256", got[0].Checksum)
	}
}

func TestLoadMigrationsRejectsBadNames(t *testing.T) {
	for name, fsys := range map[string]map[string]string{
		"missing digits":    {"1_short.sql": ""},
		"uppercase":         {"0001_Create.sql": ""},
		"no description":    {"0001.sql": ""},
		"version zero":      {"0000_zero.sql": ""},
		"duplicate version": {"0001_a.sql": "", "0001_b.sql": ""},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := postgres.LoadMigrations(files(fsys)); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestEmbeddedMigrationsLoad(t *testing.T) {
	if _, err := postgres.LoadMigrations(migrations.FS); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateFromScratchIsIdempotent(t *testing.T) {
	pool := pgtest.Schema(t)
	ctx := context.Background()

	applied, err := postgres.Migrate(ctx, pool, files(twoTables))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(applied, []int64{1, 2}) {
		t.Fatalf("first run applied %v, want [1 2]", applied)
	}
	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM second").Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("second table rows = %d, err %v", rows, err)
	}

	applied, err = postgres.Migrate(ctx, pool, files(twoTables))
	if err != nil || len(applied) != 0 {
		t.Fatalf("second run applied %v, err %v; want nothing", applied, err)
	}
}

func TestMigrateAppliesOnlyNewFiles(t *testing.T) {
	pool := pgtest.Schema(t)
	ctx := context.Background()
	if _, err := postgres.Migrate(ctx, pool, files(map[string]string{"0001_first.sql": twoTables["0001_first.sql"]})); err != nil {
		t.Fatal(err)
	}
	applied, err := postgres.Migrate(ctx, pool, files(twoTables))
	if err != nil || !slices.Equal(applied, []int64{2}) {
		t.Fatalf("applied %v, err %v; want [2]", applied, err)
	}
}

func TestMigrateRefusesInconsistentHistory(t *testing.T) {
	cases := map[string]struct {
		later map[string]string
		want  string
	}{
		"edited file": {
			later: map[string]string{"0001_first.sql": "CREATE TABLE first (id TEXT);", "0002_second.sql": twoTables["0002_second.sql"]},
			want:  "was modified",
		},
		"removed file": {
			later: map[string]string{"0002_second.sql": twoTables["0002_second.sql"]},
			want:  "missing from this binary",
		},
		"file older than applied": {
			later: map[string]string{
				"0001_first.sql": twoTables["0001_first.sql"], "0002_second.sql": twoTables["0002_second.sql"],
				"0003_late.sql": "SELECT 1;", "0005_newer.sql": "SELECT 1;",
			},
			want: "older than applied",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pool := pgtest.Schema(t)
			ctx := context.Background()
			first := map[string]string{"0001_first.sql": twoTables["0001_first.sql"], "0002_second.sql": twoTables["0002_second.sql"]}
			if name == "file older than applied" {
				first["0005_newer.sql"] = "SELECT 1;"
			}
			if _, err := postgres.Migrate(ctx, pool, files(first)); err != nil {
				t.Fatal(err)
			}
			_, err := postgres.Migrate(ctx, pool, files(tc.later))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestMigrateRollsBackFailedFile(t *testing.T) {
	pool := pgtest.Schema(t)
	ctx := context.Background()
	fsys := files(map[string]string{
		"0001_first.sql":  twoTables["0001_first.sql"],
		"0002_broken.sql": "CREATE TABLE half (id BIGINT);\nSELECT * FROM does_not_exist;",
	})
	applied, err := postgres.Migrate(ctx, pool, fsys)
	if err == nil || !strings.Contains(err.Error(), "0002_broken.sql") {
		t.Fatalf("err = %v, want failure naming 0002_broken.sql", err)
	}
	if !slices.Equal(applied, []int64{1}) {
		t.Fatalf("applied %v, want [1]", applied)
	}
	var halfExists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('half') IS NOT NULL").Scan(&halfExists); err != nil || halfExists {
		t.Fatalf("partial migration left table half: %v, err %v", halfExists, err)
	}
	var recorded int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&recorded); err != nil || recorded != 1 {
		t.Fatalf("recorded migrations = %d, err %v; want 1", recorded, err)
	}
}

func TestMigrateSerializesConcurrentRuns(t *testing.T) {
	pool := pgtest.Schema(t)
	ctx := context.Background()
	const runners = 4
	var wg sync.WaitGroup
	results := make(chan []int64, runners)
	errs := make(chan error, runners)
	for range runners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// CREATE TABLE would fail if two runners applied the same file.
			applied, err := postgres.Migrate(ctx, pool, files(twoTables))
			results <- applied
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var all []int64
	for applied := range results {
		all = append(all, applied...)
	}
	slices.Sort(all)
	if !slices.Equal(all, []int64{1, 2}) {
		t.Fatalf("versions applied across runners = %v, want each exactly once", all)
	}
}

func TestOpenHidesConnectionString(t *testing.T) {
	_, err := postgres.Open(context.Background(), "postgres://user:s3cret@%zz/db")
	if err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("err = %v; want an error without the password", err)
	}
}

func TestOpenPingsServer(t *testing.T) {
	pool, err := postgres.Open(context.Background(), pgtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
}

func TestPending(t *testing.T) {
	pool := pgtest.Schema(t)
	ctx := context.Background()
	if _, err := postgres.Pending(ctx, pool, files(twoTables)); err == nil || !strings.Contains(err.Error(), "never migrated") {
		t.Fatalf("err = %v, want never-migrated error", err)
	}
	if _, err := postgres.Migrate(ctx, pool, files(map[string]string{"0001_first.sql": twoTables["0001_first.sql"]})); err != nil {
		t.Fatal(err)
	}
	if n, err := postgres.Pending(ctx, pool, files(twoTables)); err != nil || n != 1 {
		t.Fatalf("pending = %d, err %v; want 1", n, err)
	}
	if _, err := postgres.Migrate(ctx, pool, files(twoTables)); err != nil {
		t.Fatal(err)
	}
	if n, err := postgres.Pending(ctx, pool, files(twoTables)); err != nil || n != 0 {
		t.Fatalf("pending = %d, err %v; want 0", n, err)
	}
}
