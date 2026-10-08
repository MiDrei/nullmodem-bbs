package doors

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/midrei/nullmodem-kit/ansi"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
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

// BulletinText is a door's bulletin file ready for the terminal: a
// UTF-8 file (Usurper Reborn's news, with its emoji) turned into CP437
// -- what has no CP437 form, like an emoji, left out -- and plain text
// wrapped at word boundaries to width; ANSI art (CP437, with escape
// sequences) as it is. Lines end in CR LF.
func BulletinText(data []byte, width int) string {
	if i := bytes.IndexByte(data, 0x1a); i >= 0 {
		data = data[:i] // SAUCE
	}
	text := string(data)
	if utf8.Valid(data) && !isASCII(data) {
		text = string(ansi.EncodeCP437(strings.Map(func(r rune) rune {
			// Emoji, their joiners and selectors: no CP437 form, left
			// out rather than shown as "?".
			if r >= 0x80 && string(ansi.EncodeCP437(string(r))) == "?" {
				return -1
			}
			return r
		}, text)))
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if !strings.Contains(text, "\x1b[") {
		var wrapped []string
		for _, l := range lines {
			l = strings.TrimRight(l, " ")
			if len(l) <= width {
				wrapped = append(wrapped, l)
				continue
			}
			wrapped = append(wrapped, ansi.WrapText(l, width)...)
		}
		lines = wrapped
	}
	return strings.Join(lines, "\r\n")
}

func isASCII(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return false
		}
	}
	return true
}
