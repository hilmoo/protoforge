// Package dbexec runs post_push_sql (the replacement for a post-push script):
// every statement in one transaction, rolled back on the first error. The
// driver follows migration.dialect.
package dbexec

import (
	"context"
	"fmt"
)

// Dialects accepted by Run.
const (
	Postgres = "postgresql"
	SQLite   = "sqlite"
)

// Run executes the statements in one transaction against the database that url
// (the value of the environment variable envName) points at. baseDir is the
// directory relative SQLite paths resolve against (drizzle-kit's working
// directory). Each entry may hold several statements.
func Run(ctx context.Context, dialect, envName, url, baseDir string, statements []string) error {
	if url == "" {
		return fmt.Errorf("%s is not set (set it in the environment, env_file or env:)", envName)
	}
	switch dialect {
	case Postgres, "":
		return runPostgres(ctx, envName, url, statements)
	case SQLite:
		return runSQLite(ctx, envName, url, baseDir, statements)
	}
	return fmt.Errorf("unsupported dialect %q", dialect)
}
