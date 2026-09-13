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
