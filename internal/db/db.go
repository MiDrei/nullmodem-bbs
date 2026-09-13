// Package db opens the BBS's SQLite database and applies its schema.
// SQLite in WAL mode comfortably handles a BBS's data volumes (users,
// message bases, file listings); WAL specifically is what lets many
// concurrent node sessions read while one writes without blocking.
package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// Open opens (creating if necessary) the SQLite database at path,
// enables WAL journaling and foreign keys, and applies the schema.
func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("db: mkdir %s: %w", dir, err)
		}
	}

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open %s: %w", path, err)
	}

	// SQLite allows only one writer at a time; a single shared
	// connection avoids "database is locked" errors under our own
	// concurrent goroutines and lets busy_timeout do its job instead.
	sqlDB.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := sqlDB.Exec(pragma); err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("db: %s: %w", pragma, err)
		}
	}

	if _, err := sqlDB.Exec(schema); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("db: apply schema: %w", err)
	}

	if err := ensureColumn(sqlDB, "message_areas", "network", "TEXT NOT NULL DEFAULT ''"); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if err := ensureColumn(sqlDB, "file_areas", "network", "TEXT NOT NULL DEFAULT ''"); err != nil {
		sqlDB.Close()
		return nil, err
	}

	return sqlDB, nil
}

// ensureColumn adds column to table if it isn't already there. SQLite
// has no "ALTER TABLE ... ADD COLUMN IF NOT EXISTS", and schema.sql's
// CREATE TABLE IF NOT EXISTS statements only take effect for a brand
// new database -- one created before a column existed keeps its
// original shape forever unless something like this retrofits it.
func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("db: inspect %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			colType    string
			notNull    int
			dfltValue  sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &primaryKey); err != nil {
			return fmt.Errorf("db: inspect %s: %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("db: inspect %s: %w", table, err)
	}

	if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)); err != nil {
		return fmt.Errorf("db: add column %s.%s: %w", table, column, err)
	}
	return nil
}
