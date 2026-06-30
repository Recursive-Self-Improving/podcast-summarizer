package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenCreatesFreshDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "bot.db")

	database, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer database.Close()

	if err := database.PingContext(ctx); err != nil {
		t.Fatalf("PingContext returned error: %v", err)
	}
}

func TestOpenUsesWALForFileDatabase(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer database.Close()

	var journalMode string
	if err := database.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q", journalMode)
	}
}

func TestOpenEnforcesForeignKeys(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer database.Close()

	mustExec(t, database, `CREATE TABLE parents (id INTEGER PRIMARY KEY)`)
	mustExec(t, database, `CREATE TABLE children (parent_id INTEGER NOT NULL, FOREIGN KEY(parent_id) REFERENCES parents(id))`)

	_, err = database.ExecContext(ctx, `INSERT INTO children(parent_id) VALUES (999)`)
	if err == nil {
		t.Fatal("expected foreign key violation")
	}
}

func mustExec(t *testing.T, database *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}
