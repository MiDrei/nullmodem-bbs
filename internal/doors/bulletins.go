package doors

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MaxBulletin bounds a bulletin file read for showing.
const MaxBulletin = 256 << 10

// ErrBadBulletin: a bulletin path that leaves the door's directory.
var ErrBadBulletin = errors.New("doors: a bulletin is a file inside the door's directory")

// BulletinPath is file (as configured) inside the door directory dir.
func BulletinPath(dir, file string) (string, error) {
	f := filepath.Clean(filepath.FromSlash(strings.TrimSpace(file)))
	if f == "." || filepath.IsAbs(f) || f == ".." || strings.HasPrefix(f, ".."+string(filepath.Separator)) {
		return "", ErrBadBulletin
	}
	return filepath.Join(dir, f), nil
}

// ReadBulletin reads a bulletin (at most MaxBulletin) and when it was
// written; os.ErrNotExist while the door hasn't written it yet. The
// name's case is forgiven (DOS doors write in capitals).
func ReadBulletin(dir, file string) ([]byte, time.Time, error) {
	path, err := BulletinPath(dir, file)
	if err != nil {
		return nil, time.Time{}, err
	}
	if _, err := os.Stat(path); err != nil {
		if alt, ok := findCaseInsensitive(filepath.Dir(path), filepath.Base(path)); ok {
			path = alt
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, time.Time{}, err
	}
	data := make([]byte, min(info.Size(), MaxBulletin))
	n, err := f.Read(data)
	if err != nil && n == 0 {
		return nil, time.Time{}, fmt.Errorf("doors: %w", err)
	}
	return data[:n], info.ModTime(), nil
}
