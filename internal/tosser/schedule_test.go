package tosser

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/db"
)

func newTestPollStore(t *testing.T) *UplinkPollStore {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return NewUplinkPollStore(sqlDB)
}

func TestUplinkPollStoreLastPolledAtIsZeroWhenNeverPolled(t *testing.T) {
	s := newTestPollStore(t)
	last, err := s.LastPolledAt("n3.z21.example.org:24554")
	if err != nil {
		t.Fatalf("LastPolledAt: %v", err)
	}
	if !last.IsZero() {
		t.Fatalf("LastPolledAt = %v, want zero for an uplink never polled", last)
	}
}

func TestUplinkPollStoreRecordAttemptAndReread(t *testing.T) {
	s := newTestPollStore(t)
	before := time.Now().Add(-time.Second)

	if err := s.RecordAttempt("n3.z21.example.org:24554"); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	last, err := s.LastPolledAt("n3.z21.example.org:24554")
	if err != nil {
		t.Fatalf("LastPolledAt: %v", err)
	}
	if last.Before(before) {
		t.Fatalf("LastPolledAt = %v, want a time at/after %v", last, before)
	}

	// A different host must not see this uplink's timestamp.
	other, err := s.LastPolledAt("n700.z954.example.org:24554")
	if err != nil {
		t.Fatalf("LastPolledAt (other host): %v", err)
	}
	if !other.IsZero() {
		t.Fatalf("LastPolledAt (other host) = %v, want zero", other)
	}
}

func TestUplinkPollStoreRecordAttemptOverwritesPreviousTimestamp(t *testing.T) {
	s := newTestPollStore(t)
	if err := s.RecordAttempt("n3.z21.example.org:24554"); err != nil {
		t.Fatalf("RecordAttempt (first): %v", err)
	}
	first, err := s.LastPolledAt("n3.z21.example.org:24554")
	if err != nil {
		t.Fatalf("LastPolledAt: %v", err)
	}

	time.Sleep(1100 * time.Millisecond) // SQLite CURRENT_TIMESTAMP has 1s resolution
	if err := s.RecordAttempt("n3.z21.example.org:24554"); err != nil {
		t.Fatalf("RecordAttempt (second): %v", err)
	}
	second, err := s.LastPolledAt("n3.z21.example.org:24554")
	if err != nil {
		t.Fatalf("LastPolledAt: %v", err)
	}

	if !second.After(first) {
		t.Fatalf("second RecordAttempt time %v not after first %v", second, first)
	}
}

func TestIsDueWhenNeverPolled(t *testing.T) {
	uplink := config.BinkpUplink{Host: "n3.z21.example.org:24554"}
	if !IsDue(uplink, time.Time{}, time.Now(), 15*time.Minute) {
		t.Fatal("IsDue = false, want true for an uplink never polled")
	}
}

func TestIsDueUsesGlobalDefaultWhenUplinkHasNoOverride(t *testing.T) {
	now := time.Now()
	uplink := config.BinkpUplink{Host: "n3.z21.example.org:24554"}

	if IsDue(uplink, now.Add(-10*time.Minute), now, 15*time.Minute) {
		t.Fatal("IsDue = true after only 10 of 15 default minutes, want false")
	}
	if !IsDue(uplink, now.Add(-16*time.Minute), now, 15*time.Minute) {
		t.Fatal("IsDue = false after 16 of 15 default minutes, want true")
	}
}

func TestIsDueUsesPerUplinkOverride(t *testing.T) {
	now := time.Now()
	// A hub that only permits polling every 2 hours -- the global
	// 15-minute default must not apply to it.
	uplink := config.BinkpUplink{Host: "n700.z954.example.org:24554", PollIntervalSeconds: 2 * 60 * 60}

	if IsDue(uplink, now.Add(-90*time.Minute), now, 15*time.Minute) {
		t.Fatal("IsDue = true after only 90 of 120 override minutes, want false (override must win over the 15-minute default)")
	}
	if !IsDue(uplink, now.Add(-121*time.Minute), now, 15*time.Minute) {
		t.Fatal("IsDue = false after 121 of 120 override minutes, want true")
	}
}

func TestDialBackoff(t *testing.T) {
	b := &DialBackoff{FirstWait: 5 * time.Minute, MaxWait: time.Hour}
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	if !b.Ready("hub", now) || b.Wait("hub") != 0 {
		t.Fatal("a new host isn't ready")
	}
	want := []time.Duration{5, 10, 20, 40, 60, 60}
	for i, w := range want {
		if n := b.Failed("hub", now); n != i+1 {
			t.Fatalf("failures %d, want %d", n, i+1)
		}
		if got := b.Wait("hub"); got != w*time.Minute {
			t.Errorf("after %d failure(s): wait %v, want %v", i+1, got, w*time.Minute)
		}
		if b.Ready("hub", now.Add(w*time.Minute-time.Second)) || !b.Ready("hub", now.Add(w*time.Minute)) {
			t.Errorf("after %d failure(s): ready at the wrong time", i+1)
		}
	}
	if !b.Ready("other", now) {
		t.Error("one host's failures hold back another")
	}
	b.Worked("hub")
	if !b.Ready("hub", now) || b.Wait("hub") != 0 {
		t.Error("a working session doesn't start over")
	}
}
