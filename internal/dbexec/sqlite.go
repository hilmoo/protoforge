package dbexec

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "modernc.org/sqlite" // pure-Go driver (releases are built with CGO_ENABLED=0)
)

var winDrive = regexp.MustCompile(`^/[A-Za-z]:`)

// SQLitePath turns the value of the database env var into a filesystem path.
// Accepted: file:./x.db, file:///abs/x.db, sqlite:./x.db, ./x.db and absolute
// paths. Relative paths resolve against baseDir. Remote libSQL/Turso URLs and
// in-memory databases are rejected: they cannot be shared with drizzle-kit.
func SQLitePath(raw, baseDir string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", errors.New("empty database location")
	}
	lower := strings.ToLower(v)
	for _, p := range []string{"libsql:", "http:", "https:", "ws:", "wss:"} {
		if strings.HasPrefix(lower, p) {
			return "", fmt.Errorf("remote database URL %q is not supported for sqlite post_push_sql (use a local file path or file: URL)", v)
		}
	}
	for _, p := range []string{"file:", "sqlite:"} {
		if strings.HasPrefix(lower, p) {
			v = v[len(p):]
			if strings.HasPrefix(v, "//") {
				v = v[2:] // file:///abs, file://./rel
			}
			break
		}
	}
	if i := strings.IndexByte(v, '?'); i >= 0 {
		v = v[:i]
	}
	if v == "" || v == ":memory:" || strings.HasPrefix(v, ":memory:") {
		return "", errors.New("in-memory SQLite databases are not supported: post_push_sql must run against the file drizzle-kit pushed to")
	}
	if winDrive.MatchString(v) { // file:///C:/x.db
		v = v[1:]
	}
	if u, err := url.PathUnescape(v); err == nil && strings.Contains(raw, "%") {
		v = u
	}
	p := filepath.FromSlash(v)
	if !filepath.IsAbs(p) {
		p = filepath.Join(baseDir, p)
	}
	return filepath.Clean(p), nil
}

func runSQLite(ctx context.Context, envName, raw, baseDir string, statements []string) error {
	path, err := SQLitePath(raw, baseDir)
	if err != nil {
		return fmt.Errorf("%s: %w", envName, err)
	}
	if st, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s: SQLite database %s does not exist (run migration.push first; protoforge does not create it)", envName, path)
		}
		return fmt.Errorf("%s: %w", envName, err)
	} else if st.IsDir() {
		return fmt.Errorf("%s: %s is a directory, not a SQLite database file", envName, path)
	}

	slash := filepath.ToSlash(path)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash // C:/x.db -> /C:/x.db
	}
	dsn := (&url.URL{Scheme: "file", Path: slash, RawQuery: "mode=rw&_pragma=busy_timeout(5000)"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	for i, s := range statements {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("post_push_sql[%d]: %w (transaction rolled back)", i, err)
		}
	}
	return tx.Commit()
}
