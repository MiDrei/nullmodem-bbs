package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Check is how verifying a backup went: the archive read to its end,
// the database in it whole (SQLite's integrity check) and plausible
// next to the live one.
type Check struct {
	Name     string    `json:"name"`
	At       time.Time `json:"at"`
	OK       bool      `json:"ok"`
	Error    string    `json:"error,omitempty"`
	Files    int       `json:"files"`
	Users    int       `json:"users"`
	Messages int       `json:"messages"`
}

// Verify checks the backup at path: every entry of the archive read
// (gzip's checksum catches a damaged file), the database at dbName in
// it unpacked next to the backup and checked, and the files named in
// required present. live, if set, is the running database: a backup
// with far fewer users or messages than it isn't plausible.
func Verify(ctx context.Context, path, dbName string, required []string, live *sql.DB) Check {
	c := Check{Name: filepath.Base(path), At: time.Now()}
	if err := c.verify(ctx, path, dbName, required, live); err != nil {
		c.Error = err.Error()
		return c
	}
	c.OK = true
	return c
}

func (c *Check) verify(ctx context.Context, path, dbName string, required []string, live *sql.DB) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("not a gzip file: %w", err)
	}
	defer gz.Close()

	// One left behind by a crash or restart mid-check.
	if stale, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".verify-*.sqlite")); len(stale) > 0 {
		for _, f := range stale {
			if info, err := os.Stat(f); err == nil && time.Since(info.ModTime()) > time.Hour {
				os.Remove(f)
			}
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".verify-*.sqlite")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	want := map[string]bool{}
	for _, r := range required {
		want[archivePath(r)] = true
	}
	foundDB := false
	tr := tar.NewReader(gz)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("reading the archive: %w", err)
		}
		c.Files++
		delete(want, h.Name)
		dst := io.Discard
		if h.Name == archivePath(dbName) {
			dst, foundDB = tmp, true
		}
		if _, err := io.Copy(dst, tr); err != nil {
			return fmt.Errorf("reading %s from the archive: %w", h.Name, err)
		}
	}
	if !foundDB {
		return fmt.Errorf("the database %s is missing", archivePath(dbName))
	}
	for name := range want {
		return fmt.Errorf("%s is missing", name)
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	db, err := sql.Open("sqlite", "file:"+tmp.Name()+"?mode=ro")
	if err != nil {
		return fmt.Errorf("opening the database: %w", err)
	}
	defer db.Close()
	var result string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check(1)`).Scan(&result); err != nil {
		return fmt.Errorf("checking the database: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("the database is damaged: %s", result)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&c.Users); err != nil {
		return fmt.Errorf("reading the database: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages`).Scan(&c.Messages); err != nil {
		return fmt.Errorf("reading the database: %w", err)
	}
	if live == nil {
		return nil
	}
	var users, messages int
	live.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&users)
	live.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages`).Scan(&messages)
	if c.Users*2 < users || c.Messages*2 < messages {
		return fmt.Errorf("it holds %d users and %d messages, the board has %d and %d", c.Users, c.Messages, users, messages)
	}
	return nil
}

const checkKey = "backup_check"

// SaveCheck records c as the last check (in the meta table).
func SaveCheck(db *sql.DB, c Check) error {
	b, _ := json.Marshal(c)
	_, err := db.Exec(`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, checkKey, string(b))
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return nil
}

// LastCheck returns the last recorded check, if any.
func LastCheck(db *sql.DB) (Check, bool) {
	var v string
	if db.QueryRow(`SELECT value FROM meta WHERE key = ?`, checkKey).Scan(&v) != nil {
		return Check{}, false
	}
	var c Check
	if json.Unmarshal([]byte(v), &c) != nil {
		return Check{}, false
	}
	return c, true
}

// VerifyNewest checks the newest backup in opts.Dir unless it was
// checked already, and records the result. It reports the check, and
// whether one ran.
func VerifyNewest(ctx context.Context, src Sources, opts Options) (Check, bool) {
	list, err := List(opts.Dir)
	if err != nil || len(list) == 0 {
		return Check{}, false
	}
	if last, ok := LastCheck(src.DB); ok && last.Name == list[0].Name {
		return Check{}, false
	}
	c := Verify(ctx, filepath.Join(opts.Dir, list[0].Name), src.DBPath, src.ConfigFiles[:min(1, len(src.ConfigFiles))], src.DB)
	if ctx.Err() != nil {
		return Check{}, false
	}
	SaveCheck(src.DB, c)
	return c, true
}
