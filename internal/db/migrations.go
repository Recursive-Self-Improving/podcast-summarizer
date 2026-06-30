package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	embeddedmigrations "github.com/Recursive-Self-Improving/podcast-summarizer"
)

const (
	migrationsDir       = "migrations"
	migrationFileSuffix = ".sql"
)

type Migration struct {
	Version string
	SQL     string
}

func RunMigrations(ctx context.Context, database *sql.DB) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, migration := range migrations {
		if err := applyMigration(ctx, database, migration); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, database *sql.DB, migration Migration) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", migration.Version, err)
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM schema_migrations WHERE version = ?`, migration.Version).Scan(&exists); err == nil {
		return tx.Commit()
	} else if err != sql.ErrNoRows {
		return fmt.Errorf("check migration %s: %w", migration.Version, err)
	}

	if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
		return fmt.Errorf("apply migration %s: %w", migration.Version, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES (?)`, migration.Version); err != nil {
		return fmt.Errorf("record migration %s: %w", migration.Version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", migration.Version, err)
	}
	return nil
}

func loadMigrations() ([]Migration, error) {
	entries, err := fs.ReadDir(embeddedmigrations.FS, migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), migrationFileSuffix) {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	migrations := make([]Migration, 0, len(names))
	for _, name := range names {
		path := migrationsDir + "/" + name
		contents, err := fs.ReadFile(embeddedmigrations.FS, path)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		version := strings.TrimSuffix(name, migrationFileSuffix)
		migrations = append(migrations, Migration{Version: version, SQL: string(contents)})
	}
	return migrations, nil
}
