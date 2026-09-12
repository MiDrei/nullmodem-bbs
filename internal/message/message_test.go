package message

import (
	"errors"
	"path/filepath"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/db"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func newTestStore(t *testing.T) (*Store, *user.Store) {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return NewStore(sqlDB), user.NewStore(sqlDB)
}

func TestSchemaSeedsGeneralArea(t *testing.T) {
	s, _ := newTestStore(t)

	area, err := s.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if area.Name != "General Discussion" {
		t.Fatalf("seeded area name = %q, want %q", area.Name, "General Discussion")
	}
}

func TestCreateAreaAndRoundTrip(t *testing.T) {
	s, _ := newTestStore(t)

	area, err := s.CreateArea("dev", "Development Talk", "For BBS dev chatter", 10, 50)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if area.MinSLRead != 10 || area.MinSLWrite != 50 {
		t.Fatalf("area SL fields = %+v, want read=10 write=50", area)
	}

	got, err := s.AreaByID(area.ID)
	if err != nil {
		t.Fatalf("AreaByID: %v", err)
	}
	if got.Tag != "dev" {
		t.Fatalf("AreaByID.Tag = %q, want %q", got.Tag, "dev")
	}
}

func TestCreateAreaRejectsDuplicateTag(t *testing.T) {
	s, _ := newTestStore(t)

	if _, err := s.CreateArea("dup", "First", "", 0, 0); err != nil {
		t.Fatalf("first CreateArea: %v", err)
	}
	if _, err := s.CreateArea("DUP", "Second", "", 0, 0); !errors.Is(err, ErrTagTaken) {
		t.Fatalf("second CreateArea = %v, want ErrTagTaken", err)
	}
}

func TestCountAreas(t *testing.T) {
	s, _ := newTestStore(t)

	// The schema seeds one "general" area, so CountAreas starts at 1.
	if n, err := s.CountAreas(); err != nil || n != 1 {
		t.Fatalf("CountAreas() = %d, %v; want 1, nil", n, err)
	}
	if _, err := s.CreateArea("dev", "Dev", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if n, err := s.CountAreas(); err != nil || n != 2 {
		t.Fatalf("CountAreas() = %d, %v; want 2, nil", n, err)
	}
}

func TestAreaCanReadWrite(t *testing.T) {
	a := Area{MinSLRead: 10, MinSLWrite: 50}
	if a.CanRead(5) {
		t.Fatal("CanRead(5) should be false when MinSLRead is 10")
	}
	if !a.CanRead(10) {
		t.Fatal("CanRead(10) should be true when MinSLRead is 10")
	}
	if a.CanWrite(10) {
		t.Fatal("CanWrite(10) should be false when MinSLWrite is 50")
	}
	if !a.CanWrite(50) {
		t.Fatal("CanWrite(50) should be true when MinSLWrite is 50")
	}
}

func TestListAreasFiltersBySecurityLevel(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.CreateArea("sysop-only", "Sysop Only", "", 200, 200); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	areas, err := s.ListAreas(0)
	if err != nil {
		t.Fatalf("ListAreas(0): %v", err)
	}
	for _, a := range areas {
		if a.Tag == "sysop-only" {
			t.Fatalf("ListAreas(0) should not include sysop-only area, got %+v", areas)
		}
	}

	areas, err = s.ListAreas(200)
	if err != nil {
		t.Fatalf("ListAreas(200): %v", err)
	}
	found := false
	for _, a := range areas {
		if a.Tag == "sysop-only" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ListAreas(200) should include sysop-only area, got %+v", areas)
	}
}

func TestAllAreasIgnoresSecurityLevel(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.CreateArea("sysop-only", "Sysop Only", "", 200, 200); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	all, err := s.AllAreas()
	if err != nil {
		t.Fatalf("AllAreas: %v", err)
	}
	// The seeded "general" area plus the one just created.
	if len(all) != 2 {
		t.Fatalf("AllAreas() = %+v, want 2 areas", all)
	}
}

func TestUpdateArea(t *testing.T) {
	s, _ := newTestStore(t)
	area, err := s.CreateArea("dev", "Dev", "old desc", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	updated, err := s.UpdateArea(area.ID, "Dev Talk", "new desc", 10, 20, 5)
	if err != nil {
		t.Fatalf("UpdateArea: %v", err)
	}
	if updated.Name != "Dev Talk" || updated.Description != "new desc" || updated.MinSLRead != 10 ||
		updated.MinSLWrite != 20 || updated.SortOrder != 5 {
		t.Fatalf("UpdateArea result = %+v, want updated fields", updated)
	}
	if updated.Tag != "dev" {
		t.Fatalf("UpdateArea changed tag to %q, want unchanged %q", updated.Tag, "dev")
	}
}

func TestDeleteArea(t *testing.T) {
	s, users := newTestStore(t)
	area, err := s.CreateArea("temp", "Temp", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := s.PostMessage(area.ID, u.ID, "All", "Hi", "body"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	if err := s.DeleteArea(area.ID); err != nil {
		t.Fatalf("DeleteArea: %v", err)
	}
	if _, err := s.AreaByID(area.ID); !errors.Is(err, ErrAreaNotFound) {
		t.Fatalf("AreaByID after delete = %v, want ErrAreaNotFound", err)
	}
	msgs, err := s.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("ListMessages after area delete = %+v, want empty (cascade)", msgs)
	}
}

func TestPostAndListMessages(t *testing.T) {
	s, users := newTestStore(t)
	area, err := s.CreateArea("chat", "Chat", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	posted, err := s.PostMessage(area.ID, u.ID, "All", "Hello", "First post!")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if posted.FromName != "alice" {
		t.Fatalf("posted.FromName = %q, want %q", posted.FromName, "alice")
	}

	msgs, err := s.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Subject != "Hello" || msgs[0].Body != "First post!" {
		t.Fatalf("ListMessages = %+v, want one message with subject Hello", msgs)
	}

	got, err := s.MessageByID(posted.ID)
	if err != nil {
		t.Fatalf("MessageByID: %v", err)
	}
	if got.FromName != "alice" || got.Subject != "Hello" {
		t.Fatalf("MessageByID = %+v, want matching posted message", got)
	}
}

func TestListMessagesOrderedOldestFirst(t *testing.T) {
	s, users := newTestStore(t)
	area, err := s.CreateArea("chat", "Chat", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if _, err := s.PostMessage(area.ID, u.ID, "All", "First", "1"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(area.ID, u.ID, "All", "Second", "2"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	msgs, err := s.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Subject != "First" || msgs[1].Subject != "Second" {
		t.Fatalf("ListMessages order = %+v, want [First, Second]", msgs)
	}
}
