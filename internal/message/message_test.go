package message

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

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

	area, err := s.CreateArea("dev", "Development Talk", "For BBS dev chatter", "", 10, 50)
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

	if _, err := s.CreateArea("dup", "First", "", "", 0, 0); err != nil {
		t.Fatalf("first CreateArea: %v", err)
	}
	if _, err := s.CreateArea("DUP", "Second", "", "", 0, 0); !errors.Is(err, ErrTagTaken) {
		t.Fatalf("second CreateArea = %v, want ErrTagTaken", err)
	}
}

func TestCountAreas(t *testing.T) {
	s, _ := newTestStore(t)

	// The schema seeds one "general" area, so CountAreas starts at 1.
	if n, err := s.CountAreas(); err != nil || n != 1 {
		t.Fatalf("CountAreas() = %d, %v; want 1, nil", n, err)
	}
	if _, err := s.CreateArea("dev", "Dev", "", "", 0, 0); err != nil {
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
	if _, err := s.CreateArea("sysop-only", "Sysop Only", "", "", 200, 200); err != nil {
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
	if _, err := s.CreateArea("sysop-only", "Sysop Only", "", "", 200, 200); err != nil {
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
	area, err := s.CreateArea("dev", "Dev", "old desc", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	updated, err := s.UpdateArea(area.ID, "Dev Talk", "new desc", "", 10, 20, 5)
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
	area, err := s.CreateArea("temp", "Temp", "", "", 0, 0)
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
	area, err := s.CreateArea("chat", "Chat", "", "", 0, 0)
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
	area, err := s.CreateArea("chat", "Chat", "", "", 0, 0)
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

func statsFor(t *testing.T, stats []AreaWithStats, tag string) AreaWithStats {
	t.Helper()
	for _, st := range stats {
		if st.Area.Tag == tag {
			return st
		}
	}
	t.Fatalf("no area stats for tag %q in %+v", tag, stats)
	return AreaWithStats{}
}

func TestListAreaStatsCountsTotalNewAndYours(t *testing.T) {
	s, users := newTestStore(t)
	area, err := s.CreateArea("chat", "Chat", "", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, err := users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}

	if _, err := s.PostMessage(area.ID, alice.ID, "All", "One", "1"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(area.ID, bob.ID, "All", "Two", "2"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// Alice has never visited: everything in the area is new to her,
	// and one of the two posts is hers.
	stats, err := s.ListAreaStats(user.SLNewUser, alice.ID)
	if err != nil {
		t.Fatalf("ListAreaStats: %v", err)
	}
	got := statsFor(t, stats, "chat")
	if got.Total != 2 || got.New != 2 || got.Yours != 1 {
		t.Fatalf("alice's stats = %+v, want Total=2 New=2 Yours=1", got)
	}

	msgs, err := s.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	for _, m := range msgs {
		if err := s.MarkMessageRead(alice.ID, m.ID); err != nil {
			t.Fatalf("MarkMessageRead: %v", err)
		}
	}
	stats, err = s.ListAreaStats(user.SLNewUser, alice.ID)
	if err != nil {
		t.Fatalf("ListAreaStats after read: %v", err)
	}
	got = statsFor(t, stats, "chat")
	if got.New != 0 {
		t.Fatalf("alice's New after reading every message = %d, want 0", got.New)
	}

	// A message posted after marking the existing ones read is new
	// again, but the read markers for the earlier messages must not
	// affect Bob independently.
	if _, err := s.PostMessage(area.ID, bob.ID, "All", "Three", "3"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	stats, err = s.ListAreaStats(user.SLNewUser, alice.ID)
	if err != nil {
		t.Fatalf("ListAreaStats after new post: %v", err)
	}
	got = statsFor(t, stats, "chat")
	if got.New != 1 || got.Total != 3 {
		t.Fatalf("alice's stats after new post = %+v, want New=1 Total=3", got)
	}

	bobStats, err := s.ListAreaStats(user.SLNewUser, bob.ID)
	if err != nil {
		t.Fatalf("ListAreaStats for bob: %v", err)
	}
	gotBob := statsFor(t, bobStats, "chat")
	if gotBob.New != 3 || gotBob.Yours != 2 {
		t.Fatalf("bob's stats = %+v, want New=3 (never visited) Yours=2", gotBob)
	}
}

func TestNetworksListsDistinctNonEmptyValuesSorted(t *testing.T) {
	s, _ := newTestStore(t)

	if _, err := s.CreateArea("dev", "Dev", "", "fsxNet", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if _, err := s.CreateArea("news", "News", "", "HobbyNet", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if _, err := s.CreateArea("dev2", "Dev2", "", "fsxNet", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if _, err := s.CreateArea("local", "Local", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	got, err := s.Networks()
	if err != nil {
		t.Fatalf("Networks: %v", err)
	}
	want := []string{"HobbyNet", "fsxNet"}
	if len(got) != len(want) {
		t.Fatalf("Networks() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Networks() = %v, want %v", got, want)
		}
	}
}

func TestEnsureAreaCreatesPendingAreaOnFirstUse(t *testing.T) {
	s, _ := newTestStore(t)

	area, created, err := s.EnsureArea("FSXNET_GENERAL", "FSXNET_GENERAL", "fsxNet")
	if err != nil {
		t.Fatalf("EnsureArea: %v", err)
	}
	if !created {
		t.Fatal("created = false, want true for a brand new tag")
	}
	if !area.Pending {
		t.Fatal("Pending = false, want true for a freshly auto-created area")
	}
	if area.Network != "fsxNet" {
		t.Fatalf("Network = %q, want %q", area.Network, "fsxNet")
	}
}

func TestEnsureAreaReturnsExistingAreaUnchanged(t *testing.T) {
	s, _ := newTestStore(t)

	first, created, err := s.EnsureArea("FSXNET_GENERAL", "FSXNET_GENERAL", "fsxNet")
	if err != nil {
		t.Fatalf("EnsureArea (first): %v", err)
	}
	if err := s.ApproveArea(first.ID); err != nil {
		t.Fatalf("ApproveArea: %v", err)
	}

	second, created, err := s.EnsureArea("FSXNET_GENERAL", "some other name", "some other network")
	if err != nil {
		t.Fatalf("EnsureArea (second): %v", err)
	}
	if created {
		t.Fatal("created = true, want false for an already-existing tag")
	}
	if second.Pending {
		t.Fatal("Pending = true, want false -- EnsureArea must not un-approve an already-approved area")
	}
	if second.Name != first.Name || second.Network != first.Network {
		t.Fatalf("EnsureArea (second) = %+v, want the untouched existing area %+v", second, first)
	}
}

func TestEnsureAreaAlsoFindsAManuallyCreatedArea(t *testing.T) {
	s, _ := newTestStore(t)

	manual, err := s.CreateArea("dev", "Development Talk", "", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	found, created, err := s.EnsureArea("dev", "ignored", "ignored")
	if err != nil {
		t.Fatalf("EnsureArea: %v", err)
	}
	if created {
		t.Fatal("created = true, want false for a tag a sysop already created by hand")
	}
	if found.ID != manual.ID || found.Pending {
		t.Fatalf("EnsureArea found = %+v, want the existing non-pending manual area", found)
	}
}

func TestPendingAreaHiddenFromListAreasAndAllAreas(t *testing.T) {
	s, users := newTestStore(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if _, _, err := s.EnsureArea("FSXNET_GENERAL", "FSXNET_GENERAL", ""); err != nil {
		t.Fatalf("EnsureArea: %v", err)
	}

	all, err := s.AllAreas()
	if err != nil {
		t.Fatalf("AllAreas: %v", err)
	}
	for _, a := range all {
		if a.Tag == "FSXNET_GENERAL" {
			t.Fatalf("AllAreas included the pending area %+v, want it excluded", a)
		}
	}

	listed, err := s.ListAreas(user.SLNewUser)
	if err != nil {
		t.Fatalf("ListAreas: %v", err)
	}
	for _, a := range listed {
		if a.Tag == "FSXNET_GENERAL" {
			t.Fatalf("ListAreas included the pending area %+v, want it excluded", a)
		}
	}

	stats, err := s.ListAreaStats(user.SLNewUser, alice.ID)
	if err != nil {
		t.Fatalf("ListAreaStats: %v", err)
	}
	for _, st := range stats {
		if st.Area.Tag == "FSXNET_GENERAL" {
			t.Fatalf("ListAreaStats included the pending area %+v, want it excluded", st)
		}
	}
}

func TestPendingAreasListsOnlyPendingOnes(t *testing.T) {
	s, _ := newTestStore(t)

	if _, _, err := s.EnsureArea("FSXNET_GENERAL", "FSXNET_GENERAL", ""); err != nil {
		t.Fatalf("EnsureArea: %v", err)
	}
	if _, err := s.CreateArea("dev", "Development Talk", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	pending, err := s.PendingAreas()
	if err != nil {
		t.Fatalf("PendingAreas: %v", err)
	}
	if len(pending) != 1 || pending[0].Tag != "FSXNET_GENERAL" {
		t.Fatalf("PendingAreas = %+v, want just the auto-created FSXNET_GENERAL area", pending)
	}
}

func TestApproveAreaMakesItVisible(t *testing.T) {
	s, _ := newTestStore(t)

	area, _, err := s.EnsureArea("FSXNET_GENERAL", "FSXNET_GENERAL", "")
	if err != nil {
		t.Fatalf("EnsureArea: %v", err)
	}
	if err := s.ApproveArea(area.ID); err != nil {
		t.Fatalf("ApproveArea: %v", err)
	}

	reloaded, err := s.AreaByID(area.ID)
	if err != nil {
		t.Fatalf("AreaByID: %v", err)
	}
	if reloaded.Pending {
		t.Fatal("Pending = true after ApproveArea, want false")
	}

	all, err := s.AllAreas()
	if err != nil {
		t.Fatalf("AllAreas: %v", err)
	}
	found := false
	for _, a := range all {
		if a.Tag == "FSXNET_GENERAL" {
			found = true
		}
	}
	if !found {
		t.Fatal("AllAreas did not include the area after approval")
	}

	// Idempotent.
	if err := s.ApproveArea(area.ID); err != nil {
		t.Fatalf("ApproveArea (again): %v", err)
	}
}

func TestReceiveEchoStoresRemoteAuthorWithoutLocalAccount(t *testing.T) {
	s, users := newTestStore(t)
	if _, err := users.Register("alice", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}

	area, err := s.CreateArea("dev", "Development Talk", "", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	written := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	msg, err := s.ReceiveEcho(area.ID, "Geri Atricks", "Re: Immortal Barons", "body text", written)
	if err != nil {
		t.Fatalf("ReceiveEcho: %v", err)
	}
	if !msg.IsFromRemote() {
		t.Fatal("IsFromRemote() = false, want true for a message with no local author")
	}
	if msg.FromName != "Geri Atricks" {
		t.Fatalf("FromName = %q, want %q", msg.FromName, "Geri Atricks")
	}
	if !msg.PostedAt.Equal(written) {
		t.Fatalf("PostedAt = %v, want the message's own Written time %v", msg.PostedAt, written)
	}

	msgs, err := s.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].FromName != "Geri Atricks" {
		t.Fatalf("ListMessages = %+v, want one message from Geri Atricks", msgs)
	}
}
