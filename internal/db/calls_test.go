package db

import (
	"path/filepath"
	"testing"
)

func TestBackfillCallsFromLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.sqlite")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d.Exec(`INSERT INTO users (username, password_hash) VALUES ('Alice', 'x')`)
	for _, l := range [][2]string{
		{"2026-09-30 10:00:00", "[telnet] node 1: alice logged in"},
		{"2026-09-30 11:00:00", "[ssh] node 2: Alice logged in"},
		{"2026-09-30 12:00:00", "Alice logged into the BBS web portal"},
		{"2026-09-30 12:10:00", "Alice logged into the BBS web portal"}, // same call
		{"2026-09-30 13:00:00", "[telnet] node 1: ghost logged in"},     // no such user
	} {
		d.Exec(`INSERT INTO logs (logged_at, source, level, message) VALUES (?, 'bbs', 'info', ?)`, l[0], l[1])
	}
	d.Exec(`DELETE FROM meta WHERE key = 'calls_backfilled'`)
	d.Close()
	if d, err = Open(path); err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var n, web int
	d.QueryRow(`SELECT COUNT(*), SUM(via = 'web') FROM calls`).Scan(&n, &web)
	if n != 3 || web != 1 {
		t.Fatalf("calls = %d (web %d), want 3 (1)", n, web)
	}
}

func TestMigrateAreaSelections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.sqlite")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d.Exec(`INSERT INTO users (id, username, password_hash) VALUES (7, 'alice', 'x'), (8, 'bob', 'x')`)
	d.Exec(`INSERT INTO message_areas (id, tag, name) VALUES (101, 'a', 'A'), (102, 'b', 'B'), (103, 'c', 'C')`)
	d.Exec(`INSERT INTO qwk_area_selections (user_id, area_id) VALUES (7, 101)`) // alice picked A only; bob nothing (= all)
	d.Exec(`DELETE FROM meta WHERE key = 'area_selections_migrated'`)
	d.Close()
	if d, err = Open(path); err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var aliceOut, bobOut, left int
	d.QueryRow(`SELECT COUNT(*) FROM area_unsubscribed WHERE user_id = 7 AND area_id IN (102, 103)`).Scan(&aliceOut)
	d.QueryRow(`SELECT COUNT(*) FROM area_unsubscribed WHERE user_id = 8`).Scan(&bobOut)
	d.QueryRow(`SELECT COUNT(*) FROM area_unsubscribed WHERE user_id = 7 AND area_id = 101`).Scan(&left)
	if aliceOut != 2 || bobOut != 0 || left != 0 {
		t.Fatalf("alice out %d (want 2), A out %d, bob out %d", aliceOut, left, bobOut)
	}
}
