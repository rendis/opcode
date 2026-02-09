package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed migrations/001_initial_schema.sql
var migration001 string

// migration holds a versioned SQL migration.
type migration struct {
	Version int
	Name    string
	SQL     string
}

var migrations = []migration{
	{Version: 1, Name: "initial_schema", SQL: migration001},
}

// runMigrations creates the schema_version table and applies any pending migrations.
func runMigrations(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	var current int
	row := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("read schema_version: %w", err)
	}

	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.Version, err)
		}
		for _, stmt := range splitStatements(m.SQL) {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migration %d (%s): %w", m.Version, m.Name, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version (version, name) VALUES (?, ?)`, m.Version, m.Name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", m.Version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.Version, err)
		}
	}
	return nil
}

// splitStatements splits a SQL script on semicolons, handling comments.
func splitStatements(script string) []string {
	var stmts []string
	for _, raw := range strings.Split(script, ";") {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		// Skip pure comment lines
		lines := strings.Split(s, "\n")
		hasCode := false
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l != "" && !strings.HasPrefix(l, "--") {
				hasCode = true
				break
			}
		}
		if hasCode {
			stmts = append(stmts, s)
		}
	}
	return stmts
}
