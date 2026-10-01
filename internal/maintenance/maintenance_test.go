package maintenance

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/db"
	"git.maik.ch/nullmodem/bbs/internal/file"
	"git.maik.ch/nullmodem/bbs/internal/message"
	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestRunKeepsWhatItMustAndPreviewMatches(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "t.sqlite")
	sqlDB, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	users, messages := user.NewStore(sqlDB), message.NewStore(sqlDB)
	files := file.NewStore(sqlDB, filepath.Join(dir, "files"))
	nm := netmail.NewStore(sqlDB)
	sysop, _ := users.Register("sysop", "password123", user.SLSysop)

	now := time.Now()
	ago := func(d int) time.Time { return now.Add(-time.Duration(d) * 24 * time.Hour) }
	gen, _ := messages.CreateArea("FSX_GEN", "General", "", "fsxNet", 0, 0)
	dat, _ := messages.CreateArea("FSX_DAT", "Data", "", "fsxNet", 0, 0)
	messages.SetAreaHidden(dat.ID, true)
	keep, _ := messages.CreateArea("KEEP", "Keep all", "", "", 0, 0)
	messages.SetAreaKeep(keep.ID, -1, 0)
	capped, _ := messages.CreateArea("CAPPED", "Capped", "", "", 0, 0)
	messages.SetAreaKeep(capped.ID, 0, 3)

	recv := func(a *message.Area, subj string, when time.Time) {
		if _, _, err := messages.ReceiveEcho(a.ID, "x", subj, "b", "", when); err != nil {
			t.Fatal(err)
		}
	}
	recv(gen, "old", ago(400))      // older than 365: goes
	recv(gen, "year", ago(300))     // stays
	recv(dat, "data old", ago(40))  // data area, 30 days: goes
	recv(dat, "data new", ago(20))  // stays
	recv(keep, "ancient", ago(900)) // area keeps everything
	for i := 0; i < 6; i++ {        // capped at 3: all but the 3 newest go (the fresh one counts)
		recv(capped, "c", ago(30-i))
	}
	recv(capped, "fresh", ago(1))
	// A local post from long ago that never reached the hub: stays.
	m, _ := messages.PostMessage(gen.ID, sysop.ID, "All", "unsent", "b")
	sqlDB.Exec(`UPDATE messages SET posted_at = ? WHERE id = ?`, sqlTime(ago(500)), m.ID)

	// Netmail: read and old goes; unread old, and read but unsent, stay.
	read, _ := nm.Receive("a", "1:2/3", sysop.ID, "sysop", "", "s", "b", ago(100), false)
	nm.MarkRead(read.ID)
	nm.Receive("a", "1:2/3", sysop.ID, "sysop", "", "unread", "b", ago(100), false)

	cfg := config.MaintenanceConfig{}
	ninety := 90
	cfg.NetmailKeepDays = &ninety
	deps := Deps{DB: sqlDB, Files: files, DBPath: dbPath}

	preview := Run(context.Background(), deps, cfg, true)
	if len(preview.Errors) > 0 {
		t.Fatalf("preview errors: %v", preview.Errors)
	}
	var before int
	sqlDB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&before)
	real := Run(context.Background(), deps, cfg, false)
	if len(real.Errors) > 0 {
		t.Fatalf("errors: %v", real.Errors)
	}
	if preview.Messages != real.Messages || preview.Netmail != real.Netmail {
		t.Fatalf("preview %d/%d != run %d/%d", preview.Messages, preview.Netmail, real.Messages, real.Netmail)
	}
	if real.Messages != 6 || real.Netmail != 1 {
		t.Fatalf("deleted %d messages (%v), %d netmail; want 6 and 1", real.Messages, real.MessageAreas, real.Netmail)
	}
	var left []string
	rows, _ := sqlDB.Query(`SELECT subject FROM messages ORDER BY id`)
	for rows.Next() {
		var s string
		rows.Scan(&s)
		left = append(left, s)
	}
	rows.Close()
	if got := strings.Join(left, ","); got != "year,data new,ancient,c,c,fresh,unsent" {
		t.Fatalf("left: %s", got)
	}
	if last, err := Last(sqlDB); err != nil || last != nil {
		t.Fatalf("Last before Save = %v, %v", last, err)
	}
	Save(sqlDB, real)
	if last, _ := Last(sqlDB); last == nil || last.Messages != 6 {
		t.Fatalf("Last = %+v", last)
	}
	if ran, _ := RanOn(sqlDB, now); !ran {
		t.Fatal("RanOn(today) false after a run")
	}
}

// A VACUUM only when enough of the database is unused, and the WAL
// emptied and truncated afterwards -- even while another connection
// (another daemon) has the database open.
func TestCompactVacuumsOnlyWhenWorthItAndTruncatesTheWAL(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "t.sqlite")
	sqlDB, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	other, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	ctx := context.Background()
	exec := func(q string) {
		if _, err := sqlDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE blob (id INTEGER PRIMARY KEY, b BLOB)`)
	exec(`WITH RECURSIVE s(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM s WHERE i < 2000) INSERT INTO blob (b) SELECT randomblob(4000) FROM s`)
	var n int
	other.QueryRow(`SELECT count(*) FROM blob`).Scan(&n) // the other daemon read once

	exec(`DELETE FROM blob WHERE id <= 50`) // ~2.5% unused
	vacuumed, err := compact(ctx, sqlDB)
	if err != nil || vacuumed {
		t.Fatalf("little unused: vacuumed %v, err %v", vacuumed, err)
	}
	exec(`DELETE FROM blob WHERE id <= 1000`) // half unused
	vacuumed, err = compact(ctx, sqlDB)
	if err != nil || !vacuumed {
		t.Fatalf("half unused: vacuumed %v, err %v", vacuumed, err)
	}
	if wal := fileSize(walPath(dbPath)); wal != 0 {
		t.Errorf("WAL is %d bytes after compacting, want 0", wal)
	}
}
