package archive

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return NewStore(sqlDB, filepath.Join(dir, "archive"))
}

func capture(t *testing.T, s *Store, uplinkAddress, uplinkHost, filename, content, outcome, detail string) *Entry {
	t.Helper()
	c, err := s.Begin(uplinkAddress, uplinkHost, filename)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := c.Writer().Write([]byte(content)); err != nil {
		t.Fatalf("write: %v", err)
	}
	e, err := c.Finish(outcome, detail)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	return e
}

func TestBeginFinishRecordsEntryAndBytes(t *testing.T) {
	s := newTestStore(t)
	e := capture(t, s, "21:3/100", "n3.z21.example.org:24554", "12345678.pkt", "hello packet", "ok", "")

	if e.Filename != "12345678.pkt" || e.UplinkAddress != "21:3/100" || e.UplinkHost != "n3.z21.example.org:24554" {
		t.Fatalf("entry = %+v, want the values just captured", e)
	}
	if e.SizeBytes != int64(len("hello packet")) {
		t.Fatalf("SizeBytes = %d, want %d", e.SizeBytes, len("hello packet"))
	}
	if e.Outcome != "ok" || e.Detail != "" {
		t.Fatalf("Outcome/Detail = %q/%q, want ok/empty", e.Outcome, e.Detail)
	}

	rc, err := s.Open(e.ID)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "hello packet" {
		t.Fatalf("archived bytes = %q, want %q", data, "hello packet")
	}
}

func TestFinishRecordsErrorOutcomeAndDetail(t *testing.T) {
	s := newTestStore(t)
	e := capture(t, s, "21:3/100", "host:24554", "bad.tic", "garbage", "error", "tic: missing Area line")

	if e.Outcome != "error" || e.Detail != "tic: missing Area line" {
		t.Fatalf("entry = %+v, want outcome=error with the given detail", e)
	}
}

func TestListReturnsMostRecentFirstWithTotal(t *testing.T) {
	s := newTestStore(t)
	capture(t, s, "21:3/100", "host:24554", "first.pkt", "a", "ok", "")
	capture(t, s, "21:3/100", "host:24554", "second.pkt", "b", "ok", "")
	capture(t, s, "21:3/100", "host:24554", "third.pkt", "c", "ok", "")

	entries, total, err := s.List(2, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	if len(entries) != 2 || entries[0].Filename != "third.pkt" || entries[1].Filename != "second.pkt" {
		t.Fatalf("entries = %+v, want [third.pkt, second.pkt]", entries)
	}
}

func TestDeleteRemovesRowAndFile(t *testing.T) {
	s := newTestStore(t)
	e := capture(t, s, "21:3/100", "host:24554", "gone.pkt", "data", "ok", "")

	if err := s.Delete(e.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.ByID(e.ID); err == nil {
		t.Fatal("ByID after Delete: want error, got nil")
	}
	if _, err := os.Stat(e.storagePathForTest(s)); !os.IsNotExist(err) {
		t.Fatalf("file after Delete: want removed, stat err = %v", err)
	}
}

func TestPruneRemovesOnlyEntriesOlderThanRetentionPeriod(t *testing.T) {
	s := newTestStore(t)
	fresh := capture(t, s, "21:3/100", "host:24554", "fresh.pkt", "data", "ok", "")
	old := capture(t, s, "21:3/100", "host:24554", "old.pkt", "data", "ok", "")

	// Backdate "old" directly in the database, the same way a real
	// entry would age past RetentionPeriod without needing this test
	// to actually wait days.
	if err := backdate(s, old.ID, time.Now().Add(-RetentionPeriod-time.Hour)); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	if err := s.Prune(time.Now()); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if _, err := s.ByID(fresh.ID); err != nil {
		t.Fatalf("fresh entry pruned unexpectedly: %v", err)
	}
	if _, err := s.ByID(old.ID); err == nil {
		t.Fatal("old entry survived Prune")
	}
}

func TestSanitizeFilenameStripsPathSeparators(t *testing.T) {
	if got := sanitizeFilename("../../etc/passwd"); strings.ContainsAny(got, "/\\") {
		t.Fatalf("sanitizeFilename(%q) = %q, want no path separators", "../../etc/passwd", got)
	}
	if got := sanitizeFilename(""); got == "" {
		t.Fatal("sanitizeFilename(\"\") returned empty, want a non-empty fallback")
	}
}

// storagePathForTest and backdate reach past the package's own public
// API for exactly the two things a black-box test can't otherwise
// observe/arrange: the on-disk path a given entry actually used, and
// artificially aging a row without waiting RetentionPeriod for real.
func (e *Entry) storagePathForTest(s *Store) string {
	var path string
	_ = s.db.QueryRow(`SELECT storage_path FROM inbound_archive WHERE id = ?`, e.ID).Scan(&path)
	return path
}

func backdate(s *Store, id int64, at time.Time) error {
	_, err := s.db.Exec(`UPDATE inbound_archive SET received_at = ? WHERE id = ?`, at.UTC().Format("2006-01-02 15:04:05"), id)
	return err
}
