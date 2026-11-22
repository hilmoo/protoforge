package dbexec

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// newDB creates an empty SQLite file (what drizzle-kit push leaves behind).
func newDB(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE seed (x integer)"); err != nil {
		t.Fatal(err)
	}
	return p
}

func count(t *testing.T, path, table string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSQLiteCommitMultiStatement(t *testing.T) {
	dir := t.TempDir()
	p := newDB(t, dir, "a.db")
	err := Run(context.Background(), SQLite, "DB_FILE_NAME", p, dir, []string{
		"CREATE TABLE t (x integer); INSERT INTO t VALUES (1); INSERT INTO t VALUES (2);",
		"INSERT INTO t VALUES (3)",
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, p, "t"); n != 3 {
		t.Fatalf("every statement of an entry must run: count=%d", n)
	}
}

func TestSQLiteRollback(t *testing.T) {
	dir := t.TempDir()
	p := newDB(t, dir, "a.db")
	err := Run(context.Background(), SQLite, "DATABASE_URL", p, dir, []string{
		"INSERT INTO seed VALUES (1)",
		"INSERT INTO nope VALUES (1)",
	})
	if err == nil || !strings.Contains(err.Error(), "post_push_sql[1]") || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("want rollback error, got %v", err)
	}
	if n := count(t, p, "seed"); n != 0 {
		t.Fatalf("rollback failed: count=%d", n)
	}
	// DDL is transactional in SQLite too.
	err = Run(context.Background(), SQLite, "DATABASE_URL", p, dir, []string{"CREATE TABLE ddl (x integer)", "SELECT * FROM nope"})
	if err == nil {
		t.Fatal("want error")
	}
	db, _ := sql.Open("sqlite", p)
	defer db.Close()
	if _, err := db.Exec("SELECT * FROM ddl"); err == nil {
		t.Fatal("CREATE TABLE survived the rollback")
	}
}

func TestSQLiteURLForms(t *testing.T) {
	dir := t.TempDir()
	p := newDB(t, dir, "a.db")
	slash := filepath.ToSlash(p)
	for _, u := range []string{p, "file:" + slash, "sqlite:" + slash, "file:./a.db", "sqlite:./a.db", "./a.db", "a.db", "file:./a.db?mode=rwc"} {
		if err := Run(context.Background(), SQLite, "DATABASE_URL", u, dir, []string{"INSERT INTO seed VALUES (1)"}); err != nil {
			t.Errorf("%q: %v", u, err)
		}
	}
	if n := count(t, p, "seed"); n != 8 {
		t.Errorf("count=%d, want 8", n)
	}
}

func TestSQLiteRelativeToBaseDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "db")
	os.MkdirAll(sub, 0o755)
	p := newDB(t, sub, "local.db")
	got, err := SQLitePath("file:./local.db", sub)
	if err != nil || got != p {
		t.Fatalf("SQLitePath = %q, %v; want %q", got, err, p)
	}
	// The process working directory must not matter.
	if err := Run(context.Background(), SQLite, "DATABASE_URL", "./local.db", sub, []string{"INSERT INTO seed VALUES (1)"}); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteMissingFileIsNotCreated(t *testing.T) {
	dir := t.TempDir()
	err := Run(context.Background(), SQLite, "DB_FILE_NAME", "./missing.db", dir, []string{"SELECT 1"})
	if err == nil || !strings.Contains(err.Error(), "does not exist") || !strings.Contains(err.Error(), "DB_FILE_NAME") {
		t.Fatalf("want a clear missing-file error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "missing.db")); err == nil {
		t.Fatal("database file was created")
	}
}

func TestSQLiteRejectedURLs(t *testing.T) {
	for _, u := range []string{"libsql://x.turso.io", "https://x.turso.io", "http://localhost:8080", ":memory:", "file::memory:", "file:"} {
		err := Run(context.Background(), SQLite, "DATABASE_URL", u, t.TempDir(), []string{"SELECT 1"})
		if err == nil {
			t.Errorf("%q accepted", u)
		}
	}
	if err := Run(context.Background(), SQLite, "MY_DB", "", "", nil); err == nil || !strings.Contains(err.Error(), "MY_DB is not set") {
		t.Errorf("empty value: %v", err)
	}
}
