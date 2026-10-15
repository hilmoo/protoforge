package pgsql

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// These tests need a disposable Postgres. Set PROTOFORGE_TEST_DATABASE_URL
// (CI starts a postgres service container); otherwise they are skipped.
// They only create objects in a schema they drop again.
func testURL(t *testing.T) string {
	u := os.Getenv("PROTOFORGE_TEST_DATABASE_URL")
	if u == "" {
		t.Skip("PROTOFORGE_TEST_DATABASE_URL not set")
	}
	return u
}

func TestCommitAndRollback(t *testing.T) {
	url := testURL(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	conn.Exec(ctx, "DROP SCHEMA IF EXISTS protoforge_test CASCADE")
	defer conn.Exec(ctx, "DROP SCHEMA IF EXISTS protoforge_test CASCADE")

	// Success: several statements, including a DO block, in one transaction.
	err = Run(ctx, url, []string{
		"CREATE SCHEMA protoforge_test; CREATE TABLE protoforge_test.t (x int);",
		"DO $$ BEGIN INSERT INTO protoforge_test.t VALUES (1); END $$;",
	})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM protoforge_test.t").Scan(&n); err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}

	// Failure: the second statement fails, so the first must be rolled back.
	err = Run(ctx, url, []string{
		"INSERT INTO protoforge_test.t VALUES (2)",
		"SELECT 1/0",
	})
	if err == nil || !strings.Contains(err.Error(), "post_push_sql[1]") || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("want rollback error, got %v", err)
	}
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM protoforge_test.t").Scan(&n); err != nil || n != 1 {
		t.Fatalf("rollback failed: count=%d err=%v", n, err)
	}
}

func TestMissingURL(t *testing.T) {
	if err := Run(context.Background(), "", []string{"SELECT 1"}); err == nil {
		t.Fatal("want error")
	}
}
