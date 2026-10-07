package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/db"
)

func TestVerify(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	sqlDB, err := db.Open("data/nullmodem.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if _, err := sqlDB.Exec(`INSERT INTO users (username, password_hash) VALUES ('maik', 'x')`); err != nil {
		t.Fatal(err)
	}
	write(t, "configs/bbs.yaml", "bbs: {}\n")
	src := Sources{DB: sqlDB, DBPath: "data/nullmodem.sqlite", DataDir: "data", ConfigFiles: []string{"configs/bbs.yaml"}}
	opts := Options{Dir: "data/backups", KeepDaily: 7}
	ctx := context.Background()
	info, err := Run(ctx, src, opts, time.Date(2026, 10, 1, 3, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(opts.Dir, info.Name)

	// A sound backup, checked once.
	c, ran := VerifyNewest(ctx, src, opts)
	if !ran || !c.OK || c.Users != 1 || c.Files != 2 {
		t.Fatalf("sound backup: ran %v, %+v", ran, c)
	}
	if _, ran := VerifyNewest(ctx, src, opts); ran {
		t.Error("checked the same backup twice")
	}
	if last, ok := LastCheck(sqlDB); !ok || last.Name != info.Name || !last.OK {
		t.Errorf("recorded %+v", last)
	}

	// Far fewer users than the board has: not plausible.
	for _, u := range []string{"a", "b", "c"} {
		sqlDB.Exec(`INSERT INTO users (username, password_hash) VALUES (?, 'x')`, u)
	}
	if c := Verify(ctx, path, src.DBPath, src.ConfigFiles, sqlDB); c.OK || !strings.Contains(c.Error, "1 users") {
		t.Errorf("implausible backup passed: %+v", c)
	}

	// A config file it should hold.
	if c := Verify(ctx, path, src.DBPath, []string{"configs/web.yaml"}, nil); c.OK || !strings.Contains(c.Error, "configs/web.yaml") {
		t.Errorf("missing file passed: %+v", c)
	}

	// Damaged: cut short.
	b, _ := os.ReadFile(path)
	os.WriteFile(path, b[:len(b)/2], 0o600)
	if c := Verify(ctx, path, src.DBPath, nil, nil); c.OK {
		t.Errorf("truncated backup passed: %+v", c)
	}
	// No temporary database left behind.
	left, _ := filepath.Glob(filepath.Join(opts.Dir, ".verify-*"))
	if len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}
