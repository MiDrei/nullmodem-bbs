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
	if err := migrateNetmailFromUserIDNullable(sqlDB); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if err := ensureColumn(sqlDB, "netmail_messages", "from_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if err := ensureColumn(sqlDB, "netmail_messages", "sent_at", "TIMESTAMP"); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if err := ensureColumn(sqlDB, "netmail_messages", "crash", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if err := ensureColumn(sqlDB, "message_areas", "pending", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if err := ensureColumn(sqlDB, "file_areas", "pending", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if err := migrateMessagesFromUserIDNullable(sqlDB); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if err := ensureColumn(sqlDB, "messages", "from_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if err := ensureColumn(sqlDB, "messages", "msgid", "TEXT NOT NULL DEFAULT ''"); err != nil {
		sqlDB.Close()
		return nil, err
	}
	// Created here rather than in schema.sql: on an already-existing
	// database, schema.sql runs (see above) before the ensureColumn
	// call just above adds the msgid column, so an index referencing
	// it there would fail the first time this runs against such a
	// database.
	if _, err := sqlDB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_area_msgid ON messages(area_id, msgid) WHERE msgid != ''`); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("db: create idx_messages_area_msgid: %w", err)
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

// migrateNetmailFromUserIDNullable relaxes netmail_messages.from_user_id
// from NOT NULL to nullable, needed so mail internal/tosser receives
// from a remote FTN system (no local sender account) can be stored.
// SQLite can't alter a column's constraints in place, so a database
// created before this change gets the table rebuilt: a new table in
// the current shape, existing rows copied over, then the old one
// dropped. A fresh database already gets the nullable column straight
// from schema.sql, so this is a no-op there.
func migrateNetmailFromUserIDNullable(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(netmail_messages)`)
	if err != nil {
		return fmt.Errorf("db: inspect netmail_messages: %w", err)
	}
	needsRebuild := false
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
			rows.Close()
			return fmt.Errorf("db: inspect netmail_messages: %w", err)
		}
		if name == "from_user_id" && notNull == 1 {
			needsRebuild = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("db: inspect netmail_messages: %w", err)
	}
	rows.Close()
	if !needsRebuild {
		return nil
	}

	const rebuild = `
PRAGMA foreign_keys = OFF;

ALTER TABLE netmail_messages RENAME TO netmail_messages_old;

CREATE TABLE netmail_messages (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    from_user_id   INTEGER REFERENCES users(id),
    from_name      TEXT NOT NULL DEFAULT '',
    from_address   TEXT NOT NULL DEFAULT '',
    to_user_id     INTEGER REFERENCES users(id) ON DELETE CASCADE,
    to_name        TEXT NOT NULL,
    to_address     TEXT NOT NULL DEFAULT '',
    subject        TEXT NOT NULL,
    body           TEXT NOT NULL,
    posted_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    read_at        TIMESTAMP,
    sent_at        TIMESTAMP
);

INSERT INTO netmail_messages (id, from_user_id, from_address, to_user_id, to_name, to_address, subject, body, posted_at, read_at)
SELECT id, from_user_id, from_address, to_user_id, to_name, to_address, subject, body, posted_at, read_at
FROM netmail_messages_old;

DROP TABLE netmail_messages_old;

CREATE INDEX IF NOT EXISTS idx_netmail_to_user_posted ON netmail_messages(to_user_id, posted_at);

PRAGMA foreign_keys = ON;
`
	if _, err := db.Exec(rebuild); err != nil {
		return fmt.Errorf("db: migrating netmail_messages: %w", err)
	}
	return nil
}

// migrateMessagesFromUserIDNullable is migrateNetmailFromUserIDNullable's
// counterpart for messages.from_user_id, needed so echomail
// internal/tosser tosses in from a remote FTN system (no local author
// account) can be stored. See that function's doc comment for why a
// full table rebuild is necessary and why a fresh database needs none
// of this.
func migrateMessagesFromUserIDNullable(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(messages)`)
	if err != nil {
		return fmt.Errorf("db: inspect messages: %w", err)
	}
	needsRebuild := false
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
			rows.Close()
			return fmt.Errorf("db: inspect messages: %w", err)
		}
		if name == "from_user_id" && notNull == 1 {
			needsRebuild = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("db: inspect messages: %w", err)
	}
	rows.Close()
	if !needsRebuild {
		return nil
	}

	const rebuild = `
PRAGMA foreign_keys = OFF;

ALTER TABLE messages RENAME TO messages_old;

CREATE TABLE messages (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    area_id       INTEGER NOT NULL REFERENCES message_areas(id) ON DELETE CASCADE,
    from_user_id  INTEGER REFERENCES users(id),
    from_name     TEXT NOT NULL DEFAULT '',
    to_name       TEXT NOT NULL DEFAULT 'All',
    subject       TEXT NOT NULL,
    body          TEXT NOT NULL,
    posted_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO messages (id, area_id, from_user_id, to_name, subject, body, posted_at)
SELECT id, area_id, from_user_id, to_name, subject, body, posted_at
FROM messages_old;

DROP TABLE messages_old;

CREATE INDEX IF NOT EXISTS idx_messages_area_posted ON messages(area_id, posted_at);

PRAGMA foreign_keys = ON;
`
	if _, err := db.Exec(rebuild); err != nil {
		return fmt.Errorf("db: migrating messages: %w", err)
	}
	return nil
}
