package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hilmoo/protoforge/internal/config"
)

func TestDatabaseStatus(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "there.db"), nil, 0o644)
	sqlite := &config.Migration{Dialect: "sqlite", Push: true, PostPushSQL: []string{"SELECT 1"}}
	for _, tc := range []struct {
		name string
		m    *config.Migration
		val  string
		ok   bool
		want string
	}{
		{"unset", sqlite, "", false, "not set"},
		{"existing file", sqlite, "file:./there.db", true, "(exists)"},
		{"push creates it", sqlite, "./new.db", true, "will be created by drizzle-kit push"},
		{"no push", &config.Migration{Dialect: "sqlite", Migrate: true}, "./new.db", true, "does not exist yet"},
		{"remote with post_push_sql", sqlite, "libsql://x.turso.io", false, "remote database URL"},
		{"remote, drizzle-kit only", &config.Migration{Dialect: "sqlite", Push: true}, "libsql://x.turso.io", true, "drizzle-kit only"},
		{"memory", sqlite, ":memory:", false, "in-memory"},
		{"postgres", &config.Migration{Dialect: "postgresql"}, "postgres://x", true, "set"},
	} {
		msg, ok := databaseStatus(tc.m, tc.val, dir)
		if ok != tc.ok || !strings.Contains(msg, tc.want) {
			t.Errorf("%s: got (%q, %v), want ok=%v containing %q", tc.name, msg, ok, tc.ok, tc.want)
		}
	}
}
