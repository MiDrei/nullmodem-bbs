package community

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHostPort(t *testing.T) {
	for in, want := range map[string]string{
		"bbs.example.org":            "bbs.example.org:23",
		"bbs.example.org:2323":       "bbs.example.org:2323",
		"telnet://bbs.example.org":   "bbs.example.org:23",
		"ssh://bbs.example.org":      "bbs.example.org:22",
		"telnet://bbs.example.org:4": "bbs.example.org:4",
	} {
		h, p, err := hostPort(in)
		if err != nil || h+":"+p != want {
			t.Errorf("%q -> %s:%s %v, want %s", in, h, p, err, want)
		}
	}
	if _, _, err := hostPort("bbs.example.org:99999"); err == nil {
		t.Error("a port out of range")
	}
}

func TestProbeRefusesPrivateNetworks(t *testing.T) {
	for _, a := range []string{"127.0.0.1:22", "localhost:2323", "192.168.1.10:23", "10.0.0.1", "[::1]:23", "100.64.1.1:23"} {
		if up, err := Probe(context.Background(), a); up || !errors.Is(err, ErrPrivate) {
			t.Errorf("%s: up %v, err %v", a, up, err)
		}
	}
}

func TestCheckBBSListRecords(t *testing.T) {
	s := testStore(t)
	up, _ := s.SaveBBS(BBS{Name: "Up", Address: "up.example:23"})
	down, _ := s.SaveBBS(BBS{Name: "Down", Address: "down.example:23"})
	probe := func(_ context.Context, a string) (bool, error) { return a == "up.example:23", nil }
	if err := s.CheckBBSList(context.Background(), time.Hour, probe); err != nil {
		t.Fatal(err)
	}
	u, _ := s.GetBBS(up)
	d, _ := s.GetBBS(down)
	if !u.Online || u.LastUpAt.IsZero() || d.Online || d.CheckedAt.IsZero() || !d.LastUpAt.IsZero() {
		t.Fatalf("up %+v, down %+v", u, d)
	}
	// Checked within the hour: left alone; a changed address: again.
	calls := 0
	count := func(context.Context, string) (bool, error) { calls++; return true, nil }
	s.CheckBBSList(context.Background(), time.Hour, count)
	if calls != 0 {
		t.Fatalf("%d checks within the hour", calls)
	}
	d.Address = "down.example:2323"
	s.SaveBBS(d)
	s.CheckBBSList(context.Background(), time.Hour, count)
	if calls != 1 {
		t.Fatalf("%d checks after the address changed, want 1", calls)
	}
}
