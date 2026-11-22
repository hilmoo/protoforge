package syncmig

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hilmoo/protoforge/internal/dbexec"
	_ "modernc.org/sqlite"
)

func compareDirs(t *testing.T, got, want string) {
	t.Helper()
	w, _ := os.ReadDir(want)
	g, _ := os.ReadDir(got)
	if len(w) == 0 || len(w) != len(g) {
		t.Fatalf("file count: got %d want %d", len(g), len(w))
	}
	for _, e := range w {
		wb, _ := os.ReadFile(filepath.Join(want, e.Name()))
		gb, err := os.ReadFile(filepath.Join(got, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gb, wb) {
			t.Errorf("%s differs", e.Name())
		}
	}
}

// TestFixture: expected files were produced by the exact command in the old sync script.
// Covers a final breakpoint without trailing newline, extra spaces, an indented
// (kept) line, CRLF input, a directory without migration.sql and a dot directory.
func TestFixture(t *testing.T) {
	root := t.TempDir()
	to := filepath.Join(root, "out")
	os.MkdirAll(to, 0o755)
	os.WriteFile(filepath.Join(to, "stale.up.sql"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(to, ".keep"), []byte(""), 0o644)
	var warns []string
	_, err := Run(Options{
		From: "../../testdata/fixtures/syncmig/input/drizzle",
		To:   to, Root: root,
		Warn: func(f string, a ...any) { warns = append(warns, f) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 {
		t.Fatalf("want 1 warning (dir without migration.sql), got %v", warns)
	}
	os.Remove(filepath.Join(to, ".keep")) // dotfiles survive the clean, like `rm -rf dir/*`
	compareDirs(t, to, "../../testdata/fixtures/syncmig/expected")
}

// TestSample compares with testdata/sample/server/migrations, produced from the
// sample's drizzle output by the original script (see testdata/oracle).
func TestSample(t *testing.T) {
	out := filepath.Join(t.TempDir(), "m")
	if _, err := Run(Options{From: "../../testdata/sample/db/drizzle", To: out, Root: filepath.Dir(out)}); err != nil {
		t.Fatal(err)
	}
	compareDirs(t, out, "../../testdata/sample/server/migrations")
}

func TestCheckTarget(t *testing.T) {
	root := t.TempDir()
	home, _ := os.UserHomeDir()
	for _, bad := range []string{"/", home, root, filepath.Dir(root), filepath.Join(root, "..", "x")} {
		if err := CheckTarget(bad, root); err == nil {
			t.Errorf("CheckTarget(%q) should fail", bad)
		}
	}
	if err := CheckTarget(filepath.Join(root, "a", "b"), root); err != nil {
		t.Errorf("inside root should pass: %v", err)
	}
}

func TestTransform(t *testing.T) {
	in := "a `b`\n--> statement-breakpoint\nc"
	if got := Transform(in); got != "a \"b\"\nc" {
		t.Fatalf("got %q", got)
	}
	if got := Transform("x\n-->statement-breakpoint"); got != "x\n" {
		t.Fatalf("last line deleted: got %q", got)
	}
	if strings.Contains(Transform("-->\tstatement-breakpoint\n"), "break") {
		t.Fatal("tab after --> should match \\s*")
	}
}

func TestDryRunTouchesNothing(t *testing.T) {
	root := t.TempDir()
	to := filepath.Join(root, "out")
	var ops []string
	_, err := Run(Options{From: "../../testdata/fixtures/syncmig/input/drizzle", To: to, Root: root, DryRun: true,
		Op: func(v, p string) { ops = append(ops, v) }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(to); !os.IsNotExist(err) {
		t.Fatal("dry run created the target")
	}
	if len(ops) != 3 {
		t.Fatalf("ops: %v", ops)
	}
}

// TestSQLiteFixture: real drizzle-kit sqlite-core output (backtick identifiers,
// breakpoints trailing a statement, a PRAGMA table-rebuild migration). The synced
// files are then executed, in order, against a SQLite database through dbexec,
// with a row inserted in between to prove the rebuild keeps data.
func TestSQLiteFixture(t *testing.T) {
	root := t.TempDir()
	to := filepath.Join(root, "out")
	if _, err := Run(Options{From: "../../testdata/fixtures/syncmig/sqlite/input/drizzle", To: to, Root: root}); err != nil {
		t.Fatal(err)
	}
	want := "../../testdata/fixtures/syncmig/sqlite/expected"
	compareDirs(t, to, want)

	dbPath := filepath.Join(root, "app.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("CREATE TABLE seed (x integer)"); err != nil { // materialise the file
		t.Fatal(err)
	}
	apply := func(name string) {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(to, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "`") {
			t.Errorf("%s still has backticks", name)
		}
		if err := dbexec.Run(context.Background(), dbexec.SQLite, "DB_FILE_NAME", dbPath, root, []string{string(b)}); err != nil {
			t.Fatalf("%s does not run on SQLite: %v", name, err)
		}
	}
	apply("20240101000000_init.up.sql")
	if _, err := db.Exec(`INSERT INTO authors (id, created_at, updated_at, name) VALUES ('a1', 1, 1, 'Ann');
INSERT INTO books (id, created_at, updated_at, title, author_id, genre, rating, in_stock) VALUES ('b1', 1, 1, 'T', 'a1', 'poetry', 4.5, 1)`); err != nil {
		t.Fatal(err)
	}
	apply("20240102000000_rating_required.up.sql")
	var title string
	var rating float64
	if err := db.QueryRow("SELECT title, rating FROM books WHERE id = 'b1'").Scan(&title, &rating); err != nil || title != "T" || rating != 4.5 {
		t.Fatalf("row lost in the table rebuild: %q %v %v", title, rating, err)
	}
}
