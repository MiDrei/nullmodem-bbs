package db

import (
	"path/filepath"
	"strings"
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

// TestOpenRepairsMessageReadsDanglingForeignKey locks in a real
// production fix: an earlier version of migrateMessagesFromUserIDNullable
// renamed messages to messages_old without legacy_alter_table set,
// which made SQLite silently rewrite message_reads' "REFERENCES
// messages(id)" to "REFERENCES messages_old(id)" (keeping it pointing
// at the renamed table) -- then dropped messages_old a few statements
// later, leaving message_reads permanently referencing a table that
// no longer exists. Every subsequent "mark read" failed with "no such
// table: main.messages_old" from then on; this actually happened on
// the deployed dev database. Open must detect and repair the dangling
// reference, preserving existing rows.
func TestOpenRepairsMessageReadsDanglingForeignKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.sqlite")

	sqlDB, err := Open(path)
	if err != nil {
		t.Fatalf("initial Open: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'alice', 'x')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO messages (id, area_id, from_name, to_name, subject, body)
		 SELECT 1, id, 'Someone', 'All', 'Hi', 'body' FROM message_areas LIMIT 1`,
	); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	// Install the broken CREATE TABLE text directly -- the same shape
	// SQLite's own FK-rewrite-on-rename left behind for real, matching
	// how TestOpenRelaxesNetmailFromUserIDToNullable installs an old-
	// shape table directly rather than trying to reproduce the exact
	// historical migration sequence that produced it.
	const installBroken = `
PRAGMA foreign_keys = OFF;
DROP TABLE message_reads;
CREATE TABLE message_reads (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message_id  INTEGER NOT NULL REFERENCES "messages_old"(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, message_id)
);
INSERT INTO message_reads (user_id, message_id) VALUES (1, 1);
PRAGMA foreign_keys = ON;
`
	if _, err := sqlDB.Exec(installBroken); err != nil {
		t.Fatalf("simulate dangling foreign key: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open after simulated dangling foreign key: %v", err)
	}
	defer reopened.Close()

	var createSQL string
	if err := reopened.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'message_reads'`).Scan(&createSQL); err != nil {
		t.Fatalf("inspecting repaired message_reads: %v", err)
	}
	if strings.Contains(createSQL, "messages_old") {
		t.Fatalf("message_reads schema still references messages_old after repair: %s", createSQL)
	}

	var count int
	if err := reopened.QueryRow(`SELECT COUNT(*) FROM message_reads WHERE user_id = 1 AND message_id = 1`).Scan(&count); err != nil {
		t.Fatalf("selecting preserved read row: %v", err)
	}
	if count != 1 {
		t.Fatalf("preserved read row count = %d, want 1", count)
	}

	if _, err := reopened.Exec(`INSERT OR IGNORE INTO message_reads (user_id, message_id) VALUES (?, ?)`, 1, 1); err != nil {
		t.Fatalf("insert into message_reads after repair: %v", err)
	}
}

// TestOpenRelaxesFilesUploadedByToNullable mirrors
// TestOpenRelaxesNetmailFromUserIDToNullable for
// migrateFilesUploadedByNullable -- including, unlike that one, a
// file_reads row (files(id) has a referencing table the same way
// messages(id) does via message_reads) to confirm the rebuild uses
// legacy_alter_table correctly from the start rather than needing a
// TestOpenRepairsMessageReadsDanglingForeignKey-style follow-up fix.
func TestOpenRelaxesFilesUploadedByToNullable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.sqlite")

	sqlDB, err := Open(path)
	if err != nil {
		t.Fatalf("initial Open: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'alice', 'x')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	var areaID int64
	if err := sqlDB.QueryRow(`SELECT id FROM file_areas WHERE tag = 'general'`).Scan(&areaID); err != nil {
		t.Fatalf("locate seeded file area: %v", err)
	}
	const oldShape = `
PRAGMA foreign_keys = OFF;
DROP TABLE files;
CREATE TABLE files (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    area_id          INTEGER NOT NULL REFERENCES file_areas(id) ON DELETE CASCADE,
    filename         TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    size_bytes       INTEGER NOT NULL,
    storage_path     TEXT NOT NULL,
    uploaded_by      INTEGER NOT NULL REFERENCES users(id),
    uploaded_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    download_count   INTEGER NOT NULL DEFAULT 0,
    UNIQUE (area_id, filename)
);
`
	if _, err := sqlDB.Exec(oldShape); err != nil {
		t.Fatalf("simulate pre-migration schema: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO files (id, area_id, filename, description, size_bytes, storage_path, uploaded_by) VALUES (1, ?, 'old.zip', '', 100, '/tmp/old.zip', 1)`,
		areaID,
	); err != nil {
		t.Fatalf("seed old-shape file: %v", err)
	}
	if _, err := sqlDB.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("re-enable foreign_keys: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO file_reads (user_id, file_id) VALUES (1, 1)`); err != nil {
		t.Fatalf("seed file_reads: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open after simulated pre-migration schema: %v", err)
	}
	defer reopened.Close()

	var filename string
	if err := reopened.QueryRow(`SELECT filename FROM files WHERE id = 1`).Scan(&filename); err != nil {
		t.Fatalf("select preserved row after reopen: %v", err)
	}
	if filename != "old.zip" {
		t.Fatalf("preserved row filename = %q, want %q", filename, "old.zip")
	}

	var readCount int
	if err := reopened.QueryRow(`SELECT COUNT(*) FROM file_reads WHERE user_id = 1 AND file_id = 1`).Scan(&readCount); err != nil {
		t.Fatalf("selecting preserved file_reads row: %v", err)
	}
	if readCount != 1 {
		t.Fatalf("preserved file_reads row count = %d, want 1", readCount)
	}

	if _, err := reopened.Exec(
		`INSERT INTO files (area_id, filename, description, size_bytes, storage_path, uploaded_by, uploaded_by_name) VALUES (?, 'new.zip', '', 5, '/tmp/new.zip', NULL, 'Remote Origin')`,
		areaID,
	); err != nil {
		t.Fatalf("insert with NULL uploaded_by after migration: %v", err)
	}
	if _, err := reopened.Exec(`INSERT INTO file_reads (user_id, file_id) SELECT 1, id FROM files WHERE filename = 'new.zip'`); err != nil {
		t.Fatalf("insert into file_reads after migration: %v (want it to still reference the live files table)", err)
	}
}
