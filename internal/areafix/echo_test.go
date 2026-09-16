package areafix

import (
	"path/filepath"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/db"
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
