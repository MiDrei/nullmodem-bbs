package menu

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Getter looks menus up by name: a fixed Set, or a Watcher that picks
// up changes on disk.
type Getter interface {
	Get(name string) (*Menu, bool)
}

// Watcher serves the menus in Dir, reading them again when a file
// there changes (checked at most every Every) -- so menus edited in
// the web admin apply on the caller's next menu without a restart. A
// directory that doesn't load (a broken file) keeps the last good set.
type Watcher struct {
	Dir   string
	Every time.Duration
	// OnError hears about a set that didn't load.
	OnError func(error)

	mu      sync.Mutex
	set     Set
	stamp   string
	checked time.Time
}

// NewWatcher loads dir once; the error is LoadDir's.
func NewWatcher(dir string) (*Watcher, error) {
	set, err := LoadDir(dir)
	if err != nil {
		return nil, err
	}
	return &Watcher{Dir: dir, Every: 2 * time.Second, set: set, stamp: dirStamp(dir), checked: time.Now()}, nil
}

// Get looks a menu up in the current set.
func (w *Watcher) Get(name string) (*Menu, bool) {
	return w.Set().Get(name)
}

// Set is the current set of menus.
func (w *Watcher) Set() Set {
	w.mu.Lock()
	defer w.mu.Unlock()
	if time.Since(w.checked) < w.Every {
		return w.set
	}
	w.checked = time.Now()
	stamp := dirStamp(w.Dir)
	if stamp == w.stamp {
		return w.set
	}
	set, err := LoadDir(w.Dir)
	if err != nil {
		if w.OnError != nil {
			w.OnError(err)
		}
		w.stamp = stamp // don't retry a broken file every time
		return w.set
	}
	w.set, w.stamp = set, stamp
	return w.set
}

// dirStamp changes whenever a menu file is added, removed or written.
func dirStamp(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var b []byte
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		b = append(b, e.Name()...)
		b = append(b, info.ModTime().Format(time.RFC3339Nano)...)
		b = append(b, byte(info.Size()), byte(info.Size()>>8), byte(info.Size()>>16))
	}
	return string(b)
}
