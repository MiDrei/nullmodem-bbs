package guard

import (
	"path/filepath"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/db"
)

func testGuard(t *testing.T) (*Guard, *time.Time) {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	g := New(sqlDB, func() Settings {
		return Settings{Enabled: true, MaxFailures: 3, Window: 10 * time.Minute, Lockout: 15 * time.Minute, MaxLockout: 24 * time.Hour}
	}, nil)
	g.now = func() time.Time { return now }
	return g, &now
}

func TestLockoutAfterFailuresAndEscalation(t *testing.T) {
	g, now := testGuard(t)
	ip := "203.0.113.5"
	for i := 0; i < 2; i++ {
		if v, _ := g.Fail(ip, "bob", "telnet"); v.Blocked {
			t.Fatalf("locked out after %d failures", i+1)
		}
	}
	v, err := g.Fail(ip, "bob", "web")
	if err != nil || !v.Blocked || v.Until.Sub(*now) != 15*time.Minute {
		t.Fatalf("third failure: %+v %v, want 15 minutes", v, err)
	}
	if v, _ := g.Check(ip); !v.Blocked {
		t.Fatal("not locked out")
	}
	if v, _ := g.Check("203.0.113.6"); v.Blocked {
		t.Fatal("another IP locked out")
	}

	// Over: in again; three more failures lock it out four times as long.
	*now = now.Add(16 * time.Minute)
	if v, _ := g.Check(ip); v.Blocked {
		t.Fatal("still locked out after the lockout ended")
	}
	for i := 0; i < 3; i++ {
		v, _ = g.Fail(ip, "bob", "ssh")
	}
	if !v.Blocked || v.Until.Sub(*now) != time.Hour {
		t.Fatalf("second lockout %s, want 1h", v.Until.Sub(*now))
	}

	// Failures outside the window don't add up.
	g2, now2 := testGuard(t)
	for i := 0; i < 5; i++ {
		if v, _ := g2.Fail(ip, "x", "telnet"); v.Blocked {
			t.Fatalf("locked out with failures 11 minutes apart (failure %d)", i+1)
		}
		*now2 = now2.Add(11 * time.Minute)
	}
}

func TestSuccessClearsAndUnlock(t *testing.T) {
	g, _ := testGuard(t)
	ip := "198.51.100.7"
	g.Fail(ip, "a", "telnet")
	g.Fail(ip, "a", "telnet")
	g.Succeed(ip)
	if v, _ := g.Fail(ip, "a", "telnet"); v.Blocked {
		t.Fatal("failures before a good login still counted")
	}
	g.Fail(ip, "a", "telnet")
	g.Fail(ip, "a", "telnet")
	if v, _ := g.Check(ip); !v.Blocked {
		t.Fatal("not locked out")
	}
	if l, _ := g.Lockouts(); len(l) != 1 || l[0].IP != ip {
		t.Fatalf("lockouts %+v", l)
	}
	g.Unlock(ip)
	if v, _ := g.Check(ip); v.Blocked {
		t.Fatal("still locked out after Unlock")
	}
}

func TestAllowAndBlockLists(t *testing.T) {
	g, _ := testGuard(t)
	if _, err := g.AddRule("not an ip", "block", ""); err != ErrBadPattern {
		t.Fatalf("bad pattern: %v", err)
	}
	if p, _ := g.AddRule("203.0.113.77/24", "block", "spam net"); p != "203.0.113.0/24" {
		t.Fatalf("pattern normalized to %q", p)
	}
	g.AddRule("203.0.113.10", "allow", "a friend in that net")
	if v, _ := g.Check("203.0.113.99"); !v.Blocked || !v.Until.IsZero() {
		t.Fatalf("blocked range: %+v", v)
	}
	if v, _ := g.Check("203.0.113.10"); v.Blocked {
		t.Fatal("allowed IP inside a blocked range is blocked")
	}
	// An allowed IP is never locked out.
	for i := 0; i < 10; i++ {
		g.Fail("203.0.113.10", "x", "telnet")
	}
	if v, _ := g.Check("203.0.113.10"); v.Blocked {
		t.Fatal("allowed IP locked out")
	}
	if IP("[::ffff:192.0.2.1]:2323") != "192.0.2.1" || IP("192.0.2.1:5000") != "192.0.2.1" {
		t.Fatal("IP normalization")
	}
}

func TestConnLimit(t *testing.T) {
	var c Conns
	if !c.Open("a", 2) || !c.Open("a", 2) || c.Open("a", 2) {
		t.Fatal("limit of 2 not kept")
	}
	if !c.Open("b", 2) {
		t.Fatal("another IP refused")
	}
	c.Close("a")
	if !c.Open("a", 2) {
		t.Fatal("not open again after a close")
	}
	if !c.Open("z", 0) {
		t.Fatal("0 should mean no limit")
	}
}
