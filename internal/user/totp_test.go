package user

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"git.maik.ch/nullmodem/bbs/internal/db"
)

func TestTOTPSetupVerifyReplayAndRecovery(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	s := NewStore(sqlDB)
	u, _ := s.Register("maik", "password123", SLNewUser)
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)

	setup, err := s.StartTOTP(u.ID, "Maiks Place BBS")
	if err != nil || setup.Secret == "" || len(setup.QR) < 100 {
		t.Fatalf("setup %v %v", setup.Secret, err)
	}
	if on, _ := s.HasTOTP(u.ID); on {
		t.Fatal("on before confirming")
	}
	if _, err := s.ConfirmTOTP(u.ID, "000000", now); err != ErrBadCode {
		t.Fatalf("wrong confirm code: %v", err)
	}
	code := func(at time.Time) string { c, _ := totp.GenerateCode(setup.Secret, at); return c }
	recovery, err := s.ConfirmTOTP(u.ID, code(now), now)
	if err != nil || len(recovery) != recoveryCodes {
		t.Fatalf("confirm: %v %v", recovery, err)
	}
	if on, _ := s.HasTOTP(u.ID); !on {
		t.Fatal("not on after confirming")
	}
	// The confirming code is spent; the next step's code works once.
	if err := s.VerifyTOTP(u.ID, code(now), now); err != ErrBadCode {
		t.Fatalf("replayed code: %v", err)
	}
	later := now.Add(30 * time.Second)
	if err := s.VerifyTOTP(u.ID, code(later), later); err != nil {
		t.Fatalf("fresh code: %v", err)
	}
	if err := s.VerifyTOTP(u.ID, code(later), later.Add(5*time.Second)); err != ErrBadCode {
		t.Fatal("the same code twice")
	}
	// A clock 30 s behind still works.
	much := now.Add(5 * time.Minute)
	if err := s.VerifyTOTP(u.ID, code(much.Add(-30*time.Second)), much); err != nil {
		t.Fatalf("one step of drift: %v", err)
	}
	// Recovery codes: once each.
	if err := s.VerifyTOTP(u.ID, recovery[0], much); err != nil {
		t.Fatalf("recovery code: %v", err)
	}
	if err := s.VerifyTOTP(u.ID, recovery[0], much); err != ErrBadCode {
		t.Fatal("a recovery code twice")
	}
	if n, _ := s.RecoveryCodesLeft(u.ID); n != recoveryCodes-1 {
		t.Fatalf("%d recovery codes left", n)
	}
	s.DisableTOTP(u.ID)
	if err := s.VerifyTOTP(u.ID, "123456", much); err != ErrNoTOTP {
		t.Fatalf("after disabling: %v", err)
	}
}

func TestSetPassword(t *testing.T) {
	sqlDB, _ := db.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	defer sqlDB.Close()
	s := NewStore(sqlDB)
	u, _ := s.Register("bob", "password123", SLNewUser)
	if err := s.SetPassword(u.ID, "short"); err != ErrPasswordTooShort {
		t.Fatalf("short password: %v", err)
	}
	if err := s.SetPassword(u.ID, "a brand new one"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate("bob", "a brand new one"); err != nil {
		t.Fatalf("new password refused: %v", err)
	}
}
