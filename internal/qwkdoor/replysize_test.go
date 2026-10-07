package qwkdoor

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A packet that unpacks to more than MaxReplyBytes is refused before
// anything is read into memory.
func TestParseReplyRefusesAZipBomb(t *testing.T) {
	path := filepath.Join(t.TempDir(), "BOMB.REP")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("MAIKS.MSG")
	chunk := bytes.Repeat([]byte(" "), 1<<20)
	for i := 0; i < MaxReplyBytes>>20+1; i++ {
		w.Write(chunk)
	}
	zw.Close()
	os.WriteFile(path, buf.Bytes(), 0o600)
	if _, err := ParseReply(path, "MAIKS"); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("zip bomb: %v", err)
	}
}
