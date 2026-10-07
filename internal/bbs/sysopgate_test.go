package bbs

import (
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestSysopMenuAsksForTheSecondFactor(t *testing.T) {
	s := testServer(t)
	maik, _ := s.Users.Register("maik", "password123", user.SLNewUser) // sysop
	setup, _ := s.Users.StartTOTP(maik.ID, "BBS")
	now := time.Now()
	code, _ := totp.GenerateCode(setup.Secret, now.Add(-30*time.Second))
	if _, err := s.Users.ConfirmTOTP(maik.ID, code, now.Add(-30*time.Second)); err != nil {
		t.Fatal(err)
	}
	maik, _ = s.Users.ByID(maik.ID)

	conn := newFakeConn("000000\r\n")
	term := NewTerminal(conn)
	if ok, _ := s.sysopGate(term, maik); ok || !strings.Contains(conn.out.String(), "Wrong code") {
		t.Fatalf("wrong code let in: %q", conn.out.String())
	}
	good, _ := totp.GenerateCode(setup.Secret, now)
	conn = newFakeConn(good + "\r\n")
	term = NewTerminal(conn)
	if ok, err := s.sysopGate(term, maik); !ok || err != nil {
		t.Fatalf("right code refused: %v %q", err, conn.out.String())
	}
	if ok, _ := s.sysopGate(term, maik); !ok {
		t.Fatal("asked twice in one call")
	}

	// Required, and a sysop without two-factor: turned away.
	s.Security = func() config.SecurityConfig { return config.SecurityConfig{RequireAdminTOTP: true} }
	other, _ := s.Users.Register("other", "password123", user.SLNewUser)
	s.Users.SetSecurityLevel(other.ID, user.SLSysop)
	other, _ = s.Users.ByID(other.ID)
	conn = newFakeConn("")
	if ok, _ := s.sysopGate(NewTerminal(conn), other); ok {
		t.Fatal("sysop without two-factor let into the sysop functions")
	}
}
