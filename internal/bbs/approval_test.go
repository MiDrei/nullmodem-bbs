package bbs

import (
	"strings"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/guard"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

func guardedServer(t *testing.T, sec config.SecurityConfig) *Server {
	t.Helper()
	s := testServer(t)
	s.Security = func() config.SecurityConfig { return sec }
	s.Guard = guard.New(s.Users.DB(), func() guard.Settings {
		on, max, window, lockout, maxLockout := sec.GuardSettings()
		return guard.Settings{Enabled: on, MaxFailures: max, Window: window, Lockout: lockout, MaxLockout: maxLockout}
	}, nil)
	s.NewUserSL = 10
	return s
}

func intp(v int) *int { return &v }

func TestFailedLoginsLockTheAddressOut(t *testing.T) {
	s := guardedServer(t, config.SecurityConfig{MaxFailures: intp(3)})
	if _, err := s.Users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn("alice\r\nwrong1\r\nwrong2\r\nwrong3\r\n")
	s.Handle(conn)
	if out := conn.out.String(); !strings.Contains(out, "Too many failed logins") {
		t.Fatalf("no lockout message after 3 failures: %q", out)
	}
	// The next call is turned away before the login prompt.
	conn = newFakeConn("alice\r\npassword123\r\n")
	s.Handle(conn)
	out := conn.out.String()
	if !strings.Contains(out, "Too many failed logins") || strings.Contains(out, "Enter your handle") {
		t.Fatalf("locked-out caller got to the login: %q", out)
	}
}

func TestConnectionLimitPerAddress(t *testing.T) {
	s := guardedServer(t, config.SecurityConfig{MaxConnectionsPerIP: intp(1)})
	s.conns.Open(guard.IP((&fakeConn{}).RemoteAddr().String()), 1) // one already open
	conn := newFakeConn("")
	s.Handle(conn)
	if !strings.Contains(conn.out.String(), "Too many connections") {
		t.Fatalf("second connection not refused: %q", conn.out.String())
	}
}

func TestNewAccountWaitsForApprovalAndMayOnlyWriteToTheSysop(t *testing.T) {
	s := guardedServer(t, config.SecurityConfig{})
	sysop, _ := s.Users.Register("maik", "password123", user.SLNewUser) // the first: sysop
	if !sysop.Validated || sysop.SecurityLevel != user.SLSysop {
		t.Fatalf("first account %+v", sysop)
	}

	conn := newFakeConn("Y\r\n\r\npassword123\r\npassword123\r\nBob Example\r\n")
	bob, ok, err := s.registerNew(NewTerminal(conn), "bob")
	if err != nil || !ok {
		t.Fatalf("registerNew: %v %v", ok, err)
	}
	if bob.Validated || bob.SecurityLevel != 5 {
		t.Fatalf("new account validated %v at SL %d, want waiting at 5", bob.Validated, bob.SecurityLevel)
	}
	if !strings.Contains(conn.out.String(), "waiting for the sysop's approval") {
		t.Error("not told about the approval")
	}

	general, _ := s.Messages.AreaByTag("general")
	conn = newFakeConn("\r\n")
	if err := s.attemptPostMessage(NewTerminal(conn), bob, general, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conn.out.String(), "waiting for the sysop's approval") {
		t.Error("a waiting account could post")
	}
	conn = newFakeConn("\r\n")
	s.showDoors(NewTerminal(conn), bob)
	if !strings.Contains(conn.out.String(), "waiting for the sysop's approval") {
		t.Error("a waiting account got to the doors")
	}

	// Netmail: to someone else refused, to the sysop it goes on.
	s.Users.Register("carol", "password123", user.SLNewUser)
	conn = newFakeConn("carol\r\n\r\n")
	s.composeNetmail(NewTerminal(conn), bob)
	if !strings.Contains(conn.out.String(), "waiting for the sysop's approval") {
		t.Error("a waiting account wrote netmail to another user")
	}
	conn = newFakeConn("maik\r\n")
	s.composeNetmail(NewTerminal(conn), bob)
	if strings.Contains(conn.out.String(), "waiting for the sysop's approval") || !strings.Contains(conn.out.String(), "Subject") {
		t.Errorf("a waiting account couldn't write to the sysop: %q", conn.out.String())
	}

	// Approved: the new-user level, and may post.
	if err := s.Users.Approve(bob.ID, s.NewUserSL); err != nil {
		t.Fatal(err)
	}
	bob, _ = s.Users.ByID(bob.ID)
	if !bob.Validated || bob.SecurityLevel != 10 {
		t.Fatalf("approved: %v SL %d", bob.Validated, bob.SecurityLevel)
	}
}

func TestBlockedHandlesCantRegister(t *testing.T) {
	s := guardedServer(t, config.SecurityConfig{BlockedHandles: []string{"Darth"}})
	s.Users.Register("maik", "password123", user.SLNewUser)
	conn := newFakeConn("darth\r\n")
	s.login(NewTerminal(conn))
	if !strings.Contains(conn.out.String(), "That handle is reserved") {
		t.Fatalf("blocked handle not refused: %q", conn.out.String())
	}
}
