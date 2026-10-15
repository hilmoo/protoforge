// Package pgsql runs post_push_sql (the replacement for a post-push script): every
// statement in one transaction, rolled back on the first error.
package pgsql

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Run connects to url and executes the statements inside a single transaction.
// Each entry may hold several statements (including DO blocks); entries are
// sent with the simple protocol, exactly like `client.query(sql)` with node-postgres.
func Run(ctx context.Context, url string, statements []string) error {
	if url == "" {
		return errors.New("DATABASE_URL is not set (set it in the environment, env_file or env:)")
	}
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(ctx)

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	for i, s := range statements {
		if _, err := tx.Exec(ctx, s); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("post_push_sql[%d]: %w (transaction rolled back)", i, err)
		}
	}
	return tx.Commit(ctx)
}
