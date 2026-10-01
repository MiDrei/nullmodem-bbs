package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/db"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// contents reads a backup: name -> content.
func contents(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		out[h.Name] = string(b)
	}
}

func TestRunBacksUpDatabaseConfigAndKeys(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	sqlDB, err := db.Open("data/nullmodem.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if _, err := sqlDB.Exec(`INSERT INTO logs (source, level, message) VALUES ('web', 'info', 'kept in the backup')`); err != nil {
		t.Fatal(err)
	}
	write(t, "configs/bbs.yaml", "bbs: {}\n")
	write(t, "configs/screens/MAIN.ANS", "art")
	write(t, "configs/menus/main.yaml", "menu")
	write(t, "data/jwt_secret", "secret")
	write(t, "data/vapid.json", "{}")
	write(t, "data/nullmodem.sqlite.pre-migration-bak", "old db")
	write(t, "data/bbs.yaml.bak-networks-1", "old config")
	write(t, "data/files/fsx/nodelist.zip", "big")
	write(t, "data/binkp-sessions/x.txt", "transcript")

	src := Sources{
		DB: sqlDB, DBPath: "data/nullmodem.sqlite", DataDir: "data",
		ConfigFiles: []string{"configs/bbs.yaml", "configs/web.yaml"},
		ConfigDirs:  []string{"configs/menus", "configs/screens"},
		BulkDirs:    []string{"data/files", "data/doors"},
	}
	now := time.Date(2026, 10, 1, 3, 0, 0, 0, time.Local)
	info, err := Run(context.Background(), src, Options{Dir: "data/backups", KeepDaily: 7, KeepWeekly: 4}, now)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "nullmodem-20261001-030000.tar.gz" {
		t.Errorf("name %q", info.Name)
	}
	got := contents(t, filepath.Join("data/backups", info.Name))
	var names []string
	for n := range got {
		names = append(names, n)
	}
	sort.Strings(names)
	want := "configs/bbs.yaml configs/menus/main.yaml configs/screens/MAIN.ANS data/jwt_secret data/nullmodem.sqlite data/vapid.json"
	if strings.Join(names, " ") != want {
		t.Errorf("archive holds\n%s\nwant\n%s", strings.Join(names, " "), want)
	}

	// The copied database opens and has the data.
	restored := filepath.Join(t.TempDir(), "r.sqlite")
	os.WriteFile(restored, []byte(got["data/nullmodem.sqlite"]), 0o600)
	rdb, err := sql.Open("sqlite", restored)
	if err != nil {
		t.Fatal(err)
	}
	defer rdb.Close()
	var msg string
	if err := rdb.QueryRow(`SELECT message FROM logs`).Scan(&msg); err != nil || msg != "kept in the backup" {
		t.Fatalf("restored database: %q %v", msg, err)
	}

	// With the files, and nothing left behind but the backups.
	info, err = Run(context.Background(), src, Options{Dir: "data/backups", KeepDaily: 7, KeepWeekly: 4, IncludeFiles: true}, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := contents(t, filepath.Join("data/backups", info.Name)); got["data/files/fsx/nodelist.zip"] != "big" {
		t.Errorf("file areas missing with IncludeFiles")
	}
	left, _ := os.ReadDir("data/backups")
	if len(left) != 2 {
		t.Errorf("backup dir holds %d entries, want the 2 backups", len(left))
	}
}

func TestPruneKeepsDailyAndWeekly(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 1, 3, 0, 0, 0, time.Local) // a Thursday
	for d := 0; d < 40; d++ {
		write(t, filepath.Join(dir, "nullmodem-"+start.AddDate(0, 0, -d).Format(timeLayout)+".tar.gz"), "x")
	}
	write(t, filepath.Join(dir, "unrelated.txt"), "keep me")
	if _, err := Prune(dir, 7, 4); err != nil {
		t.Fatal(err)
	}
	list, _ := List(dir)
	var days []string
	for _, b := range list {
		days = append(days, b.Time.Format("01-02"))
	}
	// 7 newest, then the newest of each week until 4 weeks are covered:
	// weeks of 10-01 (already kept), 09-27, 09-20, 09-13.
	want := "10-01 09-30 09-29 09-28 09-27 09-26 09-25 09-20 09-13"
	if strings.Join(days, " ") != want {
		t.Errorf("kept %s\nwant %s", strings.Join(days, " "), want)
	}
	if _, err := os.Stat(filepath.Join(dir, "unrelated.txt")); err != nil {
		t.Error("a file that isn't a backup was deleted")
	}
}

func TestValidName(t *testing.T) {
	for name, ok := range map[string]bool{
		"nullmodem-20261001-030000.tar.gz":      true,
		"../nullmodem.sqlite":                   false,
		"nullmodem-20261001-030000.tar.gz/../x": false,
	} {
		if ValidName(name) != ok {
			t.Errorf("ValidName(%q) = %v", name, !ok)
		}
	}
}
