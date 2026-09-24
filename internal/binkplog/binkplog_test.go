package binkplog

import (
	"io"
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
	return NewStore(sqlDB, filepath.Join(dir, "binkp-sessions"))
}

func record(t *testing.T, s *Store, direction, peerAddress, peerHost string, lines []string, outcome, detail string) *Entry {
	t.Helper()
	r, err := s.Begin(direction, peerAddress, peerHost)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	for _, l := range lines {
		r.RecordFrame("send", l)
	}
	e, err := r.Finish(outcome, detail)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	return e
}

func TestBeginFinishRecordsEntryAndTranscript(t *testing.T) {
	s := newTestStore(t)
	e := record(t, s, "outbound", "21:3/194", "bbs.example.com:24554",
		[]string{"M_NUL VER Test/1.0", "M_ADR 21:3/194"}, "ok", "")

	if e.Direction != "outbound" || e.PeerAddress != "21:3/194" || e.PeerHost != "bbs.example.com:24554" {
		t.Fatalf("entry = %+v, want the values just recorded", e)
	}
	if e.Outcome != "ok" || e.Detail != "" {
		t.Fatalf("Outcome/Detail = %q/%q, want ok/empty", e.Outcome, e.Detail)
	}
	if e.SizeBytes == 0 {
		t.Fatal("SizeBytes = 0, want the transcript's real size")
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
	transcript := string(data)
	if !strings.Contains(transcript, "SEND") || !strings.Contains(transcript, "M_NUL VER Test/1.0") ||
		!strings.Contains(transcript, "M_ADR 21:3/194") {
		t.Fatalf("transcript = %q, want both recorded lines with a direction tag", transcript)
	}
}

func TestFinishRecordsErrorOutcomeAndDetail(t *testing.T) {
	s := newTestStore(t)
	e := record(t, s, "outbound", "21:3/194", "bbs.example.com:24554", nil, "error", "handshake: EOF")
	if e.Outcome != "error" || e.Detail != "handshake: EOF" {
		t.Fatalf("Outcome/Detail = %q/%q, want error/\"handshake: EOF\"", e.Outcome, e.Detail)
	}
}

func TestRecentReturnsMostRecentFirst(t *testing.T) {
	s := newTestStore(t)
	first := record(t, s, "outbound", "21:3/194", "host:24554", []string{"M_ADR 21:3/194"}, "ok", "")
	second := record(t, s, "inbound", "1337:1/131", "host2:24554", []string{"M_ADR 1337:1/131"}, "ok", "")

	entries, err := s.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].ID != second.ID || entries[1].ID != first.ID {
		t.Fatalf("entries = %+v, want second before first (most recent first)", entries)
	}
}

func TestPruneRemovesOnlyEntriesOlderThanRetentionPeriod(t *testing.T) {
	s := newTestStore(t)
	fresh := record(t, s, "outbound", "21:3/194", "host:24554", []string{"M_ADR 21:3/194"}, "ok", "")
	old := record(t, s, "outbound", "21:3/194", "host:24554", []string{"M_ADR 21:3/194"}, "ok", "")

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

func TestSanitizeForFilenameStripsPathSeparatorsAndOddCharacters(t *testing.T) {
	if got := sanitizeForFilename("21:3/194@fsxnet ../../etc/passwd"); strings.ContainsAny(got, "/\\ ") {
		t.Fatalf("sanitizeForFilename(...) = %q, want no path separators or spaces", got)
	}
	if got := sanitizeForFilename(""); got == "" {
		t.Fatal("sanitizeForFilename(\"\") returned empty, want a non-empty fallback")
	}
}

func backdate(s *Store, id int64, at time.Time) error {
	_, err := s.db.Exec(`UPDATE binkp_sessions SET started_at = ? WHERE id = ?`, at.UTC().Format("2006-01-02 15:04:05"), id)
	return err
}
