package user

import (
	"errors"
	"path/filepath"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return NewStore(sqlDB)
}

func TestRegisterAndAuthenticate(t *testing.T) {
	s := newTestStore(t)

	u, err := s.Register("Sysop", "correct-horse", SLSysop)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if u.SecurityLevel != SLSysop {
		t.Fatalf("SecurityLevel = %d, want %d", u.SecurityLevel, SLSysop)
	}

	// Username lookup must be case-insensitive.
	got, err := s.Authenticate("sysop", "correct-horse")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("Authenticate returned different user: got id %d, want %d", got.ID, u.ID)
	}
	if got.TotalCalls != 1 {
		t.Fatalf("TotalCalls = %d, want 1", got.TotalCalls)
	}

	if _, err := s.Authenticate("sysop", "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Authenticate with wrong password: got %v, want ErrInvalidCredentials", err)
	}

	if _, err := s.Authenticate("nobody", "whatever"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Authenticate unknown user: got %v, want ErrInvalidCredentials", err)
	}
}

func TestRegisterDuplicateUsernameRejected(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.Register("dupe", "pw1", SLNewUser); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if _, err := s.Register("DUPE", "pw2", SLNewUser); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("second Register: got %v, want ErrUsernameTaken", err)
	}
}

func TestExists(t *testing.T) {
	s := newTestStore(t)

	if ok, err := s.Exists("ghost"); err != nil || ok {
		t.Fatalf("Exists before register = %v, %v; want false, nil", ok, err)
	}
	if _, err := s.Register("ghost", "pw", SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if ok, err := s.Exists("Ghost"); err != nil || !ok {
		t.Fatalf("Exists after register (case-insensitive) = %v, %v; want true, nil", ok, err)
	}
}

func TestFirstUserBecomesSysop(t *testing.T) {
	s := newTestStore(t)

	first, err := s.Register("first", "pw", SLNewUser)
	if err != nil {
		t.Fatalf("Register first: %v", err)
	}
	if first.SecurityLevel != SLSysop {
		t.Fatalf("first user SecurityLevel = %d, want %d (sysop)", first.SecurityLevel, SLSysop)
	}

	second, err := s.Register("second", "pw", SLNewUser)
	if err != nil {
		t.Fatalf("Register second: %v", err)
	}
	if second.SecurityLevel != SLNewUser {
		t.Fatalf("second user SecurityLevel = %d, want %d (requested level)", second.SecurityLevel, SLNewUser)
	}
}

func TestByUsername(t *testing.T) {
	s := newTestStore(t)

	created, err := s.Register("finder", "pw", SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := s.ByUsername("FINDER")
	if err != nil {
		t.Fatalf("ByUsername: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("ByUsername returned id %d, want %d", got.ID, created.ID)
	}

	if _, err := s.ByUsername("nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ByUsername(unknown) = %v, want ErrNotFound", err)
	}
}

func TestListAll(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.Register("first", "pw", SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := s.Register("second", "pw", SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}

	users, err := s.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("ListAll returned %d users, want 2", len(users))
	}
	if users[0].Username != "first" || users[1].Username != "second" {
		t.Fatalf("ListAll not ordered by registration: %+v", users)
	}
}

func TestCount(t *testing.T) {
	s := newTestStore(t)

	if n, err := s.Count(); err != nil || n != 0 {
		t.Fatalf("Count() = %d, %v; want 0, nil", n, err)
	}
	if _, err := s.Register("first", "pw", SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := s.Register("second", "pw", SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if n, err := s.Count(); err != nil || n != 2 {
		t.Fatalf("Count() = %d, %v; want 2, nil", n, err)
	}
}

func TestSetSecurityLevel(t *testing.T) {
	s := newTestStore(t)

	u, err := s.Register("levelup", "pw", SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := s.SetSecurityLevel(u.ID, 100); err != nil {
		t.Fatalf("SetSecurityLevel: %v", err)
	}
	got, err := s.ByID(u.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.SecurityLevel != 100 {
		t.Fatalf("SecurityLevel = %d, want 100", got.SecurityLevel)
	}
}
