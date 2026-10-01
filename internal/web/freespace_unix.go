//go:build unix

package web

import (
	"path/filepath"

	"golang.org/x/sys/unix"
)

// freeBytes is the free space on the file system dir is on (or would
// be created on: its nearest existing parent), 0 if unknown.
func freeBytes(dir string) uint64 {
	for {
		var st unix.Statfs_t
		if err := unix.Statfs(dir, &st); err == nil {
			return st.Bavail * uint64(st.Bsize)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return 0
		}
		dir = parent
	}
}
