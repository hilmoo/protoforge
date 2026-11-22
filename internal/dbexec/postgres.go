package dbexec

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// runPostgres connects to url and executes the statements inside a single
// transaction. Each entry may hold several statements (including DO blocks);
// entries are sent with the simple protocol, exactly like `client.query(sql)`
// with node-postgres.
func runPostgres(ctx context.Context, envName, url string, statements []string) error {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("parse %s: %w", envName, err)
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
