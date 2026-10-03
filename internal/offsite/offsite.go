// Package offsite copies the nightly backups, encrypted, to a storage
// service: an SFTP server (sftp.go) or OpenStack Swift (swift.go).
// Each copy is the backup encrypted with age to the sysop's public key
// ("nullmodem-<time>.tar.gz.age"); the board can't read its copies
// back -- only the sysop's private key can (age -d -i key.txt).
package offsite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"filippo.io/age"

	"git.maik.ch/nullmodem/bbs/internal/backup"
	"git.maik.ch/nullmodem/bbs/internal/config"
)

// Suffix is what an encrypted copy's name ends in.
const Suffix = ".age"

// Target is a place copies go.
type Target interface {
	// Put stores r as name.
	Put(ctx context.Context, name string, r io.Reader) error
	// List returns the names there (only ours: nullmodem-*.age).
	List(ctx context.Context) ([]string, error)
	Delete(ctx context.Context, name string) error
	// Get reads name back (the connection test).
	Get(ctx context.Context, name string) (io.ReadCloser, error)
	Close() error
}

// Open connects to the configured target.
func Open(ctx context.Context, c config.OffsiteConfig) (Target, error) {
	switch c.Kind {
	case "sftp":
		return openSFTP(ctx, c.SFTP)
	case "swift":
		return openSwift(ctx, c.Swift)
	}
	return nil, fmt.Errorf("offsite: no such kind %q (sftp or swift)", c.Kind)
}

// NewKey makes an age key pair: the private key (for the sysop to keep
// safe, never stored here) and its public key (the recipient).
func NewKey() (private, public string, err error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", err
	}
	return id.String(), id.Recipient().String(), nil
}

// CheckRecipient reports whether r is an age public key.
func CheckRecipient(r string) error {
	_, err := age.ParseX25519Recipient(strings.TrimSpace(r))
	return err
}

// encrypting returns the file at path, encrypted to recipient, as a
// stream.
func encrypting(path, recipient string) (io.ReadCloser, error) {
	rcp, err := age.ParseX25519Recipient(strings.TrimSpace(recipient))
	if err != nil {
		return nil, fmt.Errorf("offsite: the public key: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	go func() {
		defer f.Close()
		w, err := age.Encrypt(pw, rcp)
		if err == nil {
			_, err = io.Copy(w, f)
			if cerr := w.Close(); err == nil {
				err = cerr
			}
		}
		pw.CloseWithError(err)
	}()
	return pr, nil
}

// Status is how the copies are going; kept in the database's meta
// table, so the admin and the health check see it.
type Status struct {
	LastOK    time.Time `json:"last_ok"`
	LastName  string    `json:"last_name"`
	LastTry   time.Time `json:"last_try"`
	LastError string    `json:"last_error"`
	Remote    int       `json:"remote"` // copies there after the last run
}

const statusKey = "offsite_status"

// LoadStatus reads the status.
func LoadStatus(db *sql.DB) Status {
	var s Status
	var raw string
	if db.QueryRow(`SELECT value FROM meta WHERE key = ?`, statusKey).Scan(&raw) == nil {
		json.Unmarshal([]byte(raw), &s)
	}
	return s
}

func saveStatus(db *sql.DB, s Status) {
	raw, _ := json.Marshal(s)
	db.Exec(`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, statusKey, string(raw))
}

// Copier uploads backups as they appear.
type Copier struct {
	DB     *sql.DB
	Config func() config.BackupConfig
	Logger interface {
		Info(format string, args ...any)
		Warn(format string, args ...any)
	}
	// Every is how often it looks (default 10 minutes); after a failed
	// try it waits RetryAfter (default an hour).
	Every, RetryAfter time.Duration
}

// Run copies until ctx ends.
func (c *Copier) Run(ctx context.Context) {
	every := c.Every
	if every == 0 {
		every = 10 * time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		c.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (c *Copier) tick(ctx context.Context) {
	cfg := c.Config()
	if !cfg.Offsite.Enabled {
		return
	}
	st := LoadStatus(c.DB)
	retry := c.RetryAfter
	if retry == 0 {
		retry = time.Hour
	}
	if st.LastError != "" && time.Since(st.LastTry) < retry {
		return
	}
	list, err := backup.List(cfg.Directory())
	if err != nil || len(list) == 0 || list[0].Name == st.LastName {
		return
	}
	if _, err := c.Copy(ctx, cfg, list[0]); err != nil {
		c.Logger.Warn("off-site backup copy: %v", err)
	}
}

// Copy encrypts and uploads one backup, then thins out the copies
// there; it records the outcome.
func (c *Copier) Copy(ctx context.Context, cfg config.BackupConfig, b backup.Info) (Status, error) {
	st := LoadStatus(c.DB)
	st.LastTry = time.Now()
	err := func() error {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
		defer cancel()
		t, err := Open(ctx, cfg.Offsite)
		if err != nil {
			return err
		}
		defer t.Close()
		r, err := encrypting(filepath.Join(cfg.Directory(), b.Name), cfg.Offsite.Recipient)
		if err != nil {
			return err
		}
		defer r.Close()
		if err := t.Put(ctx, b.Name+Suffix, r); err != nil {
			return err
		}
		n, err := prune(ctx, t, cfg.Offsite.Daily(), cfg.Offsite.Weekly())
		st.Remote = n
		return err
	}()
	if err != nil {
		st.LastError = err.Error()
	} else {
		st.LastOK, st.LastName, st.LastError = time.Now(), b.Name, ""
		c.Logger.Info("off-site backup copy: %s uploaded (%d copies there)", b.Name+Suffix, st.Remote)
	}
	saveStatus(c.DB, st)
	return st, err
}

// prune deletes the copies beyond what's kept; it returns how many
// are left.
func prune(ctx context.Context, t Target, daily, weekly int) (int, error) {
	names, err := t.List(ctx)
	if err != nil {
		return 0, err
	}
	var list []backup.Info
	for _, n := range names {
		if tm, ok := backup.TimeOf(n); ok && strings.HasSuffix(n, Suffix) {
			list = append(list, backup.Info{Name: n, Time: tm})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Time.After(list[j].Time) })
	keep := backup.Keep(list, daily, weekly)
	left := 0
	for _, b := range list {
		if keep[b.Name] {
			left++
			continue
		}
		if err := t.Delete(ctx, b.Name); err != nil {
			return left, err
		}
	}
	return left, nil
}

// Remote lists the copies there, newest first.
func Remote(ctx context.Context, cfg config.OffsiteConfig) ([]string, error) {
	t, err := Open(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer t.Close()
	names, err := t.List(ctx)
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, err
}

// Test writes, reads back and deletes a small file there.
func Test(ctx context.Context, cfg config.OffsiteConfig) error {
	if cfg.Recipient != "" {
		if err := CheckRecipient(cfg.Recipient); err != nil {
			return fmt.Errorf("offsite: the public key isn't an age key (age1...): %w", err)
		}
	}
	t, err := Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer t.Close()
	name := fmt.Sprintf("nullmodem-test-%d.txt", time.Now().UnixNano())
	const body = "NullModem BBS off-site test\n"
	if err := t.Put(ctx, name, strings.NewReader(body)); err != nil {
		return fmt.Errorf("writing: %w", err)
	}
	r, err := t.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("reading back: %w", err)
	}
	got, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(got) != body {
		return errors.Join(errors.New("what came back isn't what was written"), err)
	}
	if err := t.Delete(ctx, name); err != nil {
		return fmt.Errorf("deleting: %w", err)
	}
	return nil
}

// ours: a name the copies have.
func ours(name string) bool {
	return strings.HasPrefix(name, "nullmodem-") && strings.HasSuffix(name, Suffix)
}
