package areafix

import (
	"path/filepath"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/db"
)

func newTestFileStore(t *testing.T) *FileStore {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return NewFileStore(sqlDB)
}

// TestFileStoreMirrorsEchoStoreBehavior only spot-checks
// FileStore -- it's byte-for-byte the same logic as EchoStore against
// the parallel file_echo_subscriptions table, already covered in
// depth by echo_test.go.
func TestFileStoreMirrorsEchoStoreBehavior(t *testing.T) {
	s := newTestFileStore(t)

	if err := s.Request("hub.example.com:24554", "FSX_FILES", Outbound); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if err := s.Request("hub.example.com:24554", "FSX_FILES", Outbound); err != nil {
		t.Fatalf("re-Request: %v", err)
	}
	subs, err := s.ListForUplink("hub.example.com:24554", Outbound)
	if err != nil {
		t.Fatalf("ListForUplink: %v", err)
	}
	if len(subs) != 1 || subs[0].AreaTag != "FSX_FILES" {
		t.Fatalf("got %+v, want exactly one FSX_FILES subscription", subs)
	}

	if err := s.Withdraw("hub.example.com:24554", "FSX_FILES", Outbound); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	subs, err = s.ListForUplink("hub.example.com:24554", Outbound)
	if err != nil {
		t.Fatalf("ListForUplink after Withdraw: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("got %d subscriptions after Withdraw, want 0", len(subs))
	}
}
