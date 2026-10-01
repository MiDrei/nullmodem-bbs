// Package backup writes the nightly backup: one .tar.gz with a
// consistent copy of the database (VACUUM INTO, taken while the
// daemons keep running), the configuration (bbs.yaml, web.yaml, the
// menus and screens a sysop may have edited), the keys and secrets in
// the data directory (JWT secret, SSH host key, push key) and, if
// wanted, the file areas and doors. Paths in the archive are the
// configured ones (data/nullmodem.sqlite, configs/screens/...), the
// same layout as the Docker deployment's directory: restoring is
// unpacking it there (see docs/backup.md).
//
// Older backups are pruned: the newest KeepDaily, plus the newest of
// each of the last KeepWeekly weeks.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Sources is what goes into a backup.
type Sources struct {
	DB *sql.DB
	// DBPath is the database's path, its name in the archive.
	DBPath string
	// DataDir's own small files (secrets and keys) are backed up; not
	// its subdirectories, nor the database files.
	DataDir string
	// ConfigFiles and ConfigDirs are backed up whole.
	ConfigFiles []string
	ConfigDirs  []string
	// BulkDirs (file areas, doors) only with Options.IncludeFiles.
	BulkDirs []string
}

// Options is how a backup is kept.
type Options struct {
	Dir          string
	KeepDaily    int
	KeepWeekly   int
	IncludeFiles bool
}

// Info is one backup on disk.
type Info struct {
	Name string    `json:"name"`
	Size int64     `json:"size"`
	Time time.Time `json:"time"`
}

const timeLayout = "20060102-150405"

var nameRE = regexp.MustCompile(`^nullmodem-(\d{8}-\d{6})\.tar\.gz$`)

// ValidName reports whether name is a backup's file name (and nothing
// that could reach outside the directory).
func ValidName(name string) bool { return nameRE.MatchString(name) }

// maxDataFile: bigger files at the top of the data directory aren't
// secrets or keys.
const maxDataFile = 1 << 20

// ErrRunning: a backup is being written already.
var ErrRunning = errors.New("backup: a backup is already being written")

var running sync.Mutex

// Run writes a backup into opts.Dir and prunes the old ones.
func Run(ctx context.Context, src Sources, opts Options, now time.Time) (Info, error) {
	if !running.TryLock() {
		return Info{}, ErrRunning
	}
	defer running.Unlock()
	if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
		return Info{}, fmt.Errorf("backup: %w", err)
	}
	name := "nullmodem-" + now.Format(timeLayout) + ".tar.gz"
	final := filepath.Join(opts.Dir, name)
	tmpDB := filepath.Join(opts.Dir, ".tmp-"+now.Format(timeLayout)+".sqlite")
	tmpArchive := final + ".tmp"
	defer os.Remove(tmpDB)
	defer os.Remove(tmpArchive)

	os.Remove(tmpDB)
	if _, err := src.DB.ExecContext(ctx, `VACUUM INTO ?`, tmpDB); err != nil {
		return Info{}, fmt.Errorf("backup: copying the database: %w", err)
	}

	f, err := os.OpenFile(tmpArchive, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return Info{}, fmt.Errorf("backup: %w", err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	w := &writer{tw: tw, skip: map[string]bool{}}
	if abs, err := filepath.Abs(opts.Dir); err == nil {
		w.skip[abs] = true
	}

	err = w.file(tmpDB, archivePath(src.DBPath))
	for _, p := range src.ConfigFiles {
		if err == nil {
			err = w.optionalFile(p)
		}
	}
	for _, d := range src.ConfigDirs {
		if err == nil {
			err = w.dir(ctx, d)
		}
	}
	if err == nil && src.DataDir != "" {
		err = w.dataFiles(src.DataDir)
	}
	if opts.IncludeFiles {
		for _, d := range src.BulkDirs {
			if err == nil {
				err = w.dir(ctx, d)
			}
		}
	}
	for _, c := range []io.Closer{tw, gz, f} {
		if cerr := c.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("backup: %w", cerr)
		}
	}
	if err != nil {
		return Info{}, err
	}
	if err := os.Rename(tmpArchive, final); err != nil {
		return Info{}, fmt.Errorf("backup: %w", err)
	}
	fi, err := os.Stat(final)
	if err != nil {
		return Info{}, fmt.Errorf("backup: %w", err)
	}
	if _, err := Prune(opts.Dir, opts.KeepDaily, opts.KeepWeekly); err != nil {
		return Info{}, err
	}
	return Info{Name: name, Size: fi.Size(), Time: now}, nil
}

// archivePath is p as a relative path inside the archive.
func archivePath(p string) string {
	p = filepath.ToSlash(filepath.Clean(p))
	p = strings.TrimLeft(p, "/")
	return strings.TrimPrefix(p, "./")
}

type writer struct {
	tw   *tar.Writer
	skip map[string]bool
}

func (w *writer) file(path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	hdr := &tar.Header{Name: name, Mode: int64(fi.Mode().Perm()), Size: fi.Size(), ModTime: fi.ModTime(), Typeflag: tar.TypeReg}
	if err := w.tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	// A file that grows while being copied: what was there when its
	// size was taken.
	if _, err := io.CopyN(w.tw, f, fi.Size()); err != nil {
		return fmt.Errorf("backup: %s: %w", path, err)
	}
	return nil
}

// optionalFile backs up path if it exists (web.yaml may not).
func (w *writer) optionalFile(path string) error {
	if fi, err := os.Stat(path); err != nil || !fi.Mode().IsRegular() {
		return nil
	}
	return w.file(path, archivePath(path))
}

func (w *writer) dir(ctx context.Context, root string) error {
	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("backup: %w", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if abs, aerr := filepath.Abs(path); aerr == nil && w.skip[abs] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return w.file(path, archivePath(path))
	})
}

// dataFiles backs up the data directory's own small files: secrets and
// keys -- not the live database, nor its backup copies.
func (w *writer) dataFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	for _, e := range entries {
		n := e.Name()
		if !e.Type().IsRegular() || strings.Contains(n, ".sqlite") || strings.Contains(n, ".bak") || strings.HasPrefix(n, ".") {
			continue
		}
		fi, err := e.Info()
		if err != nil || fi.Size() > maxDataFile {
			continue
		}
		if err := w.file(filepath.Join(dir, n), archivePath(filepath.Join(dir, n))); err != nil {
			return err
		}
	}
	return nil
}

// List returns the backups in dir, newest first.
func List(dir string) ([]Info, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []Info{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backup: %w", err)
	}
	out := []Info{}
	for _, e := range entries {
		m := nameRE.FindStringSubmatch(e.Name())
		if m == nil || !e.Type().IsRegular() {
			continue
		}
		t, err := time.ParseInLocation(timeLayout, m[1], time.Local)
		if err != nil {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Info{Name: e.Name(), Size: fi.Size(), Time: t})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out, nil
}

// Prune deletes the backups beyond the newest keepDaily that aren't
// the newest of one of the last keepWeekly weeks; it returns the
// deleted names.
func Prune(dir string, keepDaily, keepWeekly int) ([]string, error) {
	list, err := List(dir)
	if err != nil {
		return nil, err
	}
	keep := map[string]bool{}
	for i, b := range list {
		if i < keepDaily {
			keep[b.Name] = true
		}
	}
	weeks := map[string]bool{}
	for _, b := range list {
		y, wk := b.Time.ISOWeek()
		key := fmt.Sprintf("%d-%02d", y, wk)
		if weeks[key] {
			continue
		}
		if len(weeks) >= keepWeekly {
			break
		}
		weeks[key] = true
		keep[b.Name] = true
	}
	var deleted []string
	for _, b := range list {
		if keep[b.Name] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, b.Name)); err != nil {
			return deleted, fmt.Errorf("backup: %w", err)
		}
		deleted = append(deleted, b.Name)
	}
	return deleted, nil
}

// Logger is what the nightly backup reports to.
type Logger interface {
	Info(format string, args ...any)
	Warn(format string, args ...any)
}

// Nightly writes a backup once a day at the configured hour, unless
// one was already written since then that day. cfg is re-read each
// time (enabled, hour), so changes in the web admin need no restart.
func Nightly(ctx context.Context, cfg func() (enabled bool, hour int, opts Options, src Sources), log Logger) {
	tick := time.NewTicker(5 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		enabled, hour, opts, src := cfg()
		now := time.Now()
		if !enabled || now.Hour() != hour {
			continue
		}
		due := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
		if list, err := List(opts.Dir); err == nil && len(list) > 0 && !list[0].Time.Before(due) {
			continue
		}
		info, err := Run(ctx, src, opts, now)
		if err != nil {
			log.Warn("nightly backup: %v", err)
			continue
		}
		log.Info("nightly backup written: %s (%.1f MB)", info.Name, float64(info.Size)/(1<<20))
	}
}
