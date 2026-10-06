package health

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/db"
	"git.maik.ch/nullmodem/bbs/internal/doors"
)

func TestChecksAndTracking(t *testing.T) {
	dir := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(dir, "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	ms := func(t time.Time) int64 { return t.UnixMilli() }
	// mailer silent for 10 minutes, bbs fine, web is us.
	sqlDB.Exec(`INSERT INTO services (name, version, pid, started_at, heartbeat_at) VALUES ('mailer', 'v', 1, ?, ?), ('bbs', 'v', 2, ?, ?), ('web', 'v', 3, ?, ?)`,
		ms(now.Add(-time.Hour)), ms(now.Add(-10*time.Minute)), ms(now.Add(-time.Hour)), ms(now), ms(now.Add(-time.Hour)), ms(now.Add(-time.Hour)))
	// fsxNet hub reached an hour ago, LovlyNet only failing.
	sqlDB.Exec(`INSERT INTO binkp_sessions (direction, peer_address, started_at, storage_path, outcome, detail) VALUES
		('outbound', '21:3/100', ?, 'x', 'ok', ''),
		('outbound', '227:1/1', ?, 'x', 'error', 'dial tcp: connection refused')`,
		now.Add(-time.Hour).Format("2006-01-02 15:04:05"), now.Add(-time.Hour).Format("2006-01-02 15:04:05"))
	// Netmail queued three days ago.
	sqlDB.Exec(`INSERT INTO netmail_messages (from_name, to_name, to_address, subject, body, posted_at) VALUES ('a', 'b', '1:2/3', 's', 'b', ?)`,
		now.Add(-72*time.Hour).Format("2006-01-02 15:04:05"))

	cfg := config.Default()
	cfg.Binkp.Uplinks = []config.BinkpUplink{{Address: "21:3/100", Host: "hub"}, {Address: "227:1/1", Host: "lovly", PollDisabled: true},
		{Address: "21:3/194.1", Downlink: true}, {Address: "1:2/3", Host: "quiet", PollDisabled: true}}
	cfg.Backup.Dir = filepath.Join(dir, "backups")
	env := Env{DB: sqlDB, Config: func() *config.Config { return cfg }, Self: "web", StartedAt: now.Add(-48 * time.Hour),
		Disk: func(string) (uint64, uint64) { return 500 << 20, 100 << 30 }, Now: func() time.Time { return now }}

	found, err := Check(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, p := range found {
		keys = append(keys, p.Key)
	}
	want := "service:mailer uplink:227:1/1 backup disk netmail"
	if strings.Join(keys, " ") != want {
		t.Fatalf("problems %q\nwant %q", strings.Join(keys, " "), want)
	}
	if !strings.Contains(found[1].Detail, "connection refused") {
		t.Errorf("uplink detail %q", found[1].Detail)
	}

	started, over, _ := Track(sqlDB, found, now)
	if len(started) != 5 || len(over) != 0 {
		t.Fatalf("first track: %d started, %d over", len(started), len(over))
	}
	// Same again: nothing new.
	if started, over, _ = Track(sqlDB, found, now); len(started)+len(over) != 0 {
		t.Fatal("announced twice")
	}
	// The mailer is back.
	started, over, _ = Track(sqlDB, found[1:], now)
	if len(started) != 0 || len(over) != 1 || over[0].Key != "service:mailer" {
		t.Fatalf("after the mailer came back: %+v %+v", started, over)
	}
	if cur, _ := Current(sqlDB); len(cur) != 4 {
		t.Fatalf("current %d", len(cur))
	}
}

// A door from a template with a newer release than the installed one
// shows up, once per release.
func TestDoorUpdateProblem(t *testing.T) {
	dir := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(dir, "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	ib := filepath.Join(dir, "doors", "immortal-barons")
	os.MkdirAll(ib, 0o755)
	os.WriteFile(filepath.Join(ib, "immortal-barons"), []byte("x"), 0o755)
	cfg := config.Default()
	cfg.Doors = []config.DoorConfig{{Name: "Immortal Barons", Template: "immortal-barons", Dir: ib, Exe: filepath.Join(ib, "immortal-barons")}}
	env := Env{DB: sqlDB, Config: func() *config.Config { return cfg }, Self: "web", StartedAt: time.Now()}

	check := func() []string {
		found, err := Check(context.Background(), env)
		if err != nil {
			t.Fatal(err)
		}
		var keys []string
		for _, p := range found {
			keys = append(keys, p.Key)
		}
		return keys
	}
	if keys := check(); len(keys) != 0 {
		t.Fatalf("before any check: %v", keys)
	}
	doors.SaveRelease(sqlDB, "immortal-barons", doors.Release{Version: "v0.2.3"}, nil, time.Now())
	if keys := check(); strings.Join(keys, " ") != "door-update:Immortal Barons:v0.2.3" {
		t.Fatalf("with a newer release: %v", keys)
	}
	os.WriteFile(filepath.Join(ib, doors.VersionFile), []byte("v0.2.3\n"), 0o644)
	if keys := check(); len(keys) != 0 {
		t.Fatalf("after updating: %v", keys)
	}
}
