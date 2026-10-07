package areafix

import (
	"path/filepath"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/db"
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

// TestFileStoreGrantMirrorsEchoStoreBehavior spot-checks the Grant/
// Revoke/IsGranted/GrantedTags side too -- see echo_test.go's Grant
// tests for the in-depth coverage this mirrors.
func TestFileStoreGrantMirrorsEchoStoreBehavior(t *testing.T) {
	s := newTestFileStore(t)

	granted, err := s.IsGranted("downlink.example.com:24554", "FSX_FILES")
	if err != nil {
		t.Fatalf("IsGranted: %v", err)
	}
	if granted {
		t.Fatal("IsGranted = true before any Grant, want false (default-deny)")
	}

	if err := s.Grant("downlink.example.com:24554", "FSX_FILES"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	granted, err = s.IsGranted("downlink.example.com:24554", "FSX_FILES")
	if err != nil {
		t.Fatalf("IsGranted after Grant: %v", err)
	}
	if !granted {
		t.Fatal("IsGranted = false after Grant, want true")
	}

	if err := s.Revoke("downlink.example.com:24554", "FSX_FILES"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	granted, err = s.IsGranted("downlink.example.com:24554", "FSX_FILES")
	if err != nil {
		t.Fatalf("IsGranted after Revoke: %v", err)
	}
	if granted {
		t.Fatal("IsGranted = true after Revoke, want false")
	}
}
