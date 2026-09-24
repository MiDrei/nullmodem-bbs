package areafix

import (
	"path/filepath"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/db"
)

func newTestEchoStore(t *testing.T) *EchoStore {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return NewEchoStore(sqlDB)
}

func TestEchoStoreRequestThenList(t *testing.T) {
	s := newTestEchoStore(t)

	if err := s.Request("hub.example.com:24554", "FSX_GEN", Outbound); err != nil {
		t.Fatalf("Request: %v", err)
	}

	subs, err := s.ListForUplink("hub.example.com:24554", Outbound)
	if err != nil {
		t.Fatalf("ListForUplink: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("got %d subscriptions, want 1", len(subs))
	}
	if subs[0].AreaTag != "FSX_GEN" || subs[0].UplinkHost != "hub.example.com:24554" || subs[0].Direction != Outbound {
		t.Fatalf("unexpected subscription: %+v", subs[0])
	}
}

func TestEchoStoreRequestIsUpsertNotDuplicate(t *testing.T) {
	s := newTestEchoStore(t)

	if err := s.Request("hub.example.com:24554", "FSX_GEN", Outbound); err != nil {
		t.Fatalf("first Request: %v", err)
	}
	if err := s.Request("hub.example.com:24554", "FSX_GEN", Outbound); err != nil {
		t.Fatalf("second Request: %v", err)
	}

	subs, err := s.ListForUplink("hub.example.com:24554", Outbound)
	if err != nil {
		t.Fatalf("ListForUplink: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("got %d subscriptions after re-requesting the same one, want 1 (upsert, not a duplicate row)", len(subs))
	}
}

func TestEchoStoreWithdrawRemovesSubscription(t *testing.T) {
	s := newTestEchoStore(t)

	if err := s.Request("hub.example.com:24554", "FSX_GEN", Outbound); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if err := s.Withdraw("hub.example.com:24554", "FSX_GEN", Outbound); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}

	subs, err := s.ListForUplink("hub.example.com:24554", Outbound)
	if err != nil {
		t.Fatalf("ListForUplink: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("got %d subscriptions after Withdraw, want 0", len(subs))
	}
}

func TestEchoStoreWithdrawAbsentSubscriptionIsNotAnError(t *testing.T) {
	s := newTestEchoStore(t)
	if err := s.Withdraw("hub.example.com:24554", "NEVER_REQUESTED", Outbound); err != nil {
		t.Fatalf("Withdraw of an absent subscription: %v, want nil", err)
	}
}

func TestEchoStoreListForUplinkFiltersByDirection(t *testing.T) {
	s := newTestEchoStore(t)

	if err := s.Request("hub.example.com:24554", "FSX_GEN", Outbound); err != nil {
		t.Fatalf("Request outbound: %v", err)
	}
	if err := s.Request("hub.example.com:24554", "FSX_GEN", Inbound); err != nil {
		t.Fatalf("Request inbound: %v", err)
	}

	out, err := s.ListForUplink("hub.example.com:24554", Outbound)
	if err != nil {
		t.Fatalf("ListForUplink outbound: %v", err)
	}
	in, err := s.ListForUplink("hub.example.com:24554", Inbound)
	if err != nil {
		t.Fatalf("ListForUplink inbound: %v", err)
	}
	if len(out) != 1 || len(in) != 1 {
		t.Fatalf("got %d outbound, %d inbound, want 1 and 1 (same tag/host, different direction, both kept)", len(out), len(in))
	}
}

func TestEchoStoreIsGrantedDefaultsToFalse(t *testing.T) {
	s := newTestEchoStore(t)
	granted, err := s.IsGranted("downlink.example.com:24554", "FSX_GEN")
	if err != nil {
		t.Fatalf("IsGranted: %v", err)
	}
	if granted {
		t.Fatal("IsGranted = true for an area never granted, want false (default-deny)")
	}
}

func TestEchoStoreGrantThenIsGranted(t *testing.T) {
	s := newTestEchoStore(t)
	if err := s.Grant("downlink.example.com:24554", "FSX_GEN"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	granted, err := s.IsGranted("downlink.example.com:24554", "FSX_GEN")
	if err != nil {
		t.Fatalf("IsGranted: %v", err)
	}
	if !granted {
		t.Fatal("IsGranted = false after Grant, want true")
	}
	// A different downlink must not inherit the grant.
	granted, err = s.IsGranted("other.example.com:24554", "FSX_GEN")
	if err != nil {
		t.Fatalf("IsGranted (other host): %v", err)
	}
	if granted {
		t.Fatal("IsGranted = true for a different downlink, want false -- grants are per-downlink")
	}
}

func TestEchoStoreGrantIsUpsertNotDuplicate(t *testing.T) {
	s := newTestEchoStore(t)
	if err := s.Grant("downlink.example.com:24554", "FSX_GEN"); err != nil {
		t.Fatalf("first Grant: %v", err)
	}
	if err := s.Grant("downlink.example.com:24554", "FSX_GEN"); err != nil {
		t.Fatalf("second Grant: %v", err)
	}
	tags, err := s.GrantedTags("downlink.example.com:24554")
	if err != nil {
		t.Fatalf("GrantedTags: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("GrantedTags = %v, want exactly one entry after re-granting the same area", tags)
	}
}

func TestEchoStoreRevokeRemovesGrant(t *testing.T) {
	s := newTestEchoStore(t)
	if err := s.Grant("downlink.example.com:24554", "FSX_GEN"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if err := s.Revoke("downlink.example.com:24554", "FSX_GEN"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	granted, err := s.IsGranted("downlink.example.com:24554", "FSX_GEN")
	if err != nil {
		t.Fatalf("IsGranted: %v", err)
	}
	if granted {
		t.Fatal("IsGranted = true after Revoke, want false")
	}
}

func TestEchoStoreRevokeOfAbsentGrantIsNotAnError(t *testing.T) {
	s := newTestEchoStore(t)
	if err := s.Revoke("downlink.example.com:24554", "NEVER_GRANTED"); err != nil {
		t.Fatalf("Revoke of an absent grant: %v, want nil", err)
	}
}

func TestEchoStoreGrantedTagsIsCaseInsensitive(t *testing.T) {
	s := newTestEchoStore(t)
	if err := s.Grant("downlink.example.com:24554", "fsx_gen"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	granted, err := s.IsGranted("downlink.example.com:24554", "FSX_GEN")
	if err != nil {
		t.Fatalf("IsGranted: %v", err)
	}
	if !granted {
		t.Fatal("IsGranted = false for a differently-cased tag, want true (area tags are case-insensitive)")
	}
	tags, err := s.GrantedTags("downlink.example.com:24554")
	if err != nil {
		t.Fatalf("GrantedTags: %v", err)
	}
	if !tags["FSX_GEN"] {
		t.Fatalf("GrantedTags = %v, want an upper-cased FSX_GEN key", tags)
	}
}
