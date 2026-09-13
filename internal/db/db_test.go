package db

import (
	"path/filepath"
	"testing"
)

func TestEnsureColumnAddsMissingColumnIdempotently(t *testing.T) {
	sqlDB, err := Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer sqlDB.Close()

	if _, err := sqlDB.Exec(`CREATE TABLE widgets (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatalf("create widgets: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO widgets (id, name) VALUES (1, 'thing')`); err != nil {
		t.Fatalf("insert widget: %v", err)
	}

	if err := ensureColumn(sqlDB, "widgets", "color", "TEXT NOT NULL DEFAULT ''"); err != nil {
		t.Fatalf("ensureColumn (first call): %v", err)
	}

	var color string
	if err := sqlDB.QueryRow(`SELECT color FROM widgets WHERE id = 1`).Scan(&color); err != nil {
		t.Fatalf("select color after ensureColumn: %v", err)
	}
	if color != "" {
		t.Fatalf("color = %q, want empty default for the pre-existing row", color)
	}

	// Idempotent: calling it again with the column already present
	// must not error (SQLite has no "ADD COLUMN IF NOT EXISTS").
	if err := ensureColumn(sqlDB, "widgets", "color", "TEXT NOT NULL DEFAULT ''"); err != nil {
		t.Fatalf("ensureColumn (second call): %v", err)
	}
}

func TestOpenRetrofitsNetworkColumnOntoPreExistingMessageAreas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.sqlite")

	// Simulate a database created before message_areas.network
	// existed: open normally (creating the full current schema, network
	// column included), then drop the column to emulate the old shape,
	// matching how a real deployed database would look before this
	// migration -- and reopen through Open, which must add it back
	// instead of erroring.
	sqlDB, err := Open(path)
	if err != nil {
		t.Fatalf("initial Open: %v", err)
	}
	if _, err := sqlDB.Exec(`ALTER TABLE message_areas DROP COLUMN network`); err != nil {
		t.Fatalf("simulate pre-migration schema: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open after simulated pre-migration schema: %v", err)
	}
	defer reopened.Close()

	var network string
	if err := reopened.QueryRow(`SELECT network FROM message_areas WHERE tag = 'general'`).Scan(&network); err != nil {
		t.Fatalf("select network after reopen: %v", err)
	}
	if network != "" {
		t.Fatalf("network = %q, want empty default for the pre-existing seeded area", network)
	}
}

func TestOpenRelaxesNetmailFromUserIDToNullable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.sqlite")

	// Simulate a database created before from_user_id was made
	// nullable: rebuild netmail_messages in its old shape (from_user_id
	// NOT NULL, no from_name/sent_at columns) with one existing row,
	// matching how a real deployed database would look before this
	// migration -- then reopen through Open, which must rebuild the
	// table (preserving the row) instead of erroring, and leave
	// from_user_id actually nullable.
	sqlDB, err := Open(path)
	if err != nil {
		t.Fatalf("initial Open: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'alice', 'x')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	const oldShape = `
PRAGMA foreign_keys = OFF;
DROP TABLE netmail_messages;
CREATE TABLE netmail_messages (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    from_user_id   INTEGER NOT NULL REFERENCES users(id),
    from_address   TEXT NOT NULL DEFAULT '',
    to_user_id     INTEGER REFERENCES users(id) ON DELETE CASCADE,
    to_name        TEXT NOT NULL,
    to_address     TEXT NOT NULL DEFAULT '',
    subject        TEXT NOT NULL,
    body           TEXT NOT NULL,
    posted_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    read_at        TIMESTAMP
);
INSERT INTO netmail_messages (id, from_user_id, from_address, to_user_id, to_name, to_address, subject, body)
VALUES (1, 1, '1:234/56.0', NULL, '1:234/99.0', '1:234/99.0', 'Old message', 'body');
PRAGMA foreign_keys = ON;
`
	if _, err := sqlDB.Exec(oldShape); err != nil {
		t.Fatalf("simulate pre-migration schema: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open after simulated pre-migration schema: %v", err)
	}
	defer reopened.Close()

	var subject, fromAddress string
	if err := reopened.QueryRow(`SELECT subject, from_address FROM netmail_messages WHERE id = 1`).Scan(&subject, &fromAddress); err != nil {
		t.Fatalf("select preserved row after reopen: %v", err)
	}
	if subject != "Old message" || fromAddress != "1:234/56.0" {
		t.Fatalf("preserved row = subject %q, from_address %q; want the original values", subject, fromAddress)
	}

	if _, err := reopened.Exec(
		`INSERT INTO netmail_messages (from_user_id, from_name, from_address, to_user_id, to_name, to_address, subject, body)
		 VALUES (NULL, 'Mike Dreier', '21:3/194', 1, 'alice', '', 'From remote', 'hi')`,
	); err != nil {
		t.Fatalf("insert with NULL from_user_id after migration: %v", err)
	}
}
