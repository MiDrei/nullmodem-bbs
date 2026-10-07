package message

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/db"
	"github.com/midrei/nullmodem-bbs/internal/user"
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

	// Alice has never visited: bob's post is new to her, her own isn't
	// (PostMessage marks a poster's own message read for themselves
	// immediately -- see its own doc comment).
	stats, err := s.ListAreaStats(user.SLNewUser, alice.ID)
	if err != nil {
		t.Fatalf("ListAreaStats: %v", err)
	}
	got := statsFor(t, stats, "chat")
	if got.Total != 2 || got.New != 1 || got.Yours != 1 {
		t.Fatalf("alice's stats = %+v, want Total=2 New=1 Yours=1", got)
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
	// Bob's own two posts ("Two", "Three") are read for him
	// immediately; only alice's "One" is new to him.
	if gotBob.New != 1 || gotBob.Yours != 2 {
		t.Fatalf("bob's stats = %+v, want New=1 (only alice's post) Yours=2", gotBob)
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
	msg, created, err := s.ReceiveEcho(area.ID, "Geri Atricks", "Re: Immortal Barons", "body text", "21:3/100 5f3e2a1b", written)
	if err != nil {
		t.Fatalf("ReceiveEcho: %v", err)
	}
	if !created {
		t.Fatal("created = false, want true for a brand new MSGID")
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

// TestReceiveEchoDeduplicatesByMsgIDWithinAnArea locks in a real
// interop fix: a hub that closes its BinkP connection right after
// sending its last file (see internal/binkp's receiveOneFile) instead
// of waiting for our M_GOT resends the same message on its next
// session -- FTS-1026 acknowledges this exact risk. Retossing must
// recognize the duplicate by MSGID rather than storing it twice.
func TestReceiveEchoDeduplicatesByMsgIDWithinAnArea(t *testing.T) {
	s, _ := newTestStore(t)
	area, err := s.CreateArea("dev", "Development Talk", "", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	written := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	first, created, err := s.ReceiveEcho(area.ID, "Geri Atricks", "Re: Immortal Barons", "body text", "21:3/100 5f3e2a1b", written)
	if err != nil {
		t.Fatalf("ReceiveEcho (first): %v", err)
	}
	if !created {
		t.Fatal("created = false on first toss, want true")
	}

	second, created, err := s.ReceiveEcho(area.ID, "Geri Atricks", "Re: Immortal Barons", "body text", "21:3/100 5f3e2a1b", written)
	if err != nil {
		t.Fatalf("ReceiveEcho (resend): %v", err)
	}
	if created {
		t.Fatal("created = true on a resend with an already-seen MSGID, want false")
	}
	if second.ID != first.ID {
		t.Fatalf("resend returned message ID %d, want the original %d", second.ID, first.ID)
	}

	msgs, err := s.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("ListMessages = %+v, want exactly one message stored despite the resend", msgs)
	}
}

// TestReceiveEchoNeverDeduplicatesAnEmptyMsgID covers a system that
// omits MSGID entirely: dropping its mail over a missing kludge would
// be worse than the rare accidental duplicate.
func TestReceiveEchoNeverDeduplicatesAnEmptyMsgID(t *testing.T) {
	s, _ := newTestStore(t)
	area, err := s.CreateArea("dev", "Development Talk", "", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	written := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		_, created, err := s.ReceiveEcho(area.ID, "Geri Atricks", "Re: Immortal Barons", "body text", "", written)
		if err != nil {
			t.Fatalf("ReceiveEcho %d: %v", i, err)
		}
		if !created {
			t.Fatalf("created = false on toss %d with an empty MSGID, want true (never deduplicated)", i)
		}
	}

	msgs, err := s.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("ListMessages = %+v, want two separate messages", msgs)
	}
}

// TestPendingOutboundEchoReturnsOnlyLocalPostsForMatchingNetwork locks
// in outbound echomail support: only locally-posted messages (never
// one tossed in from a remote system) in an area whose network
// matches (case-insensitively) count, and only if not already sent.
func TestPendingOutboundEchoReturnsOnlyLocalPostsForMatchingNetwork(t *testing.T) {
	s, users := newTestStore(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	fsxArea, err := s.CreateArea("fsx_gen", "fsxNet General", "", "fsxNet", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea fsx: %v", err)
	}
	otherNetArea, err := s.CreateArea("hobby_gen", "HobbyNet General", "", "HobbyNet", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea hobby: %v", err)
	}
	localArea, err := s.CreateArea("local", "Local Chat", "", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea local: %v", err)
	}

	localPost, err := s.PostMessage(fsxArea.ID, alice.ID, "All", "Hi from alice", "hello fsxNet")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, _, err := s.ReceiveEcho(fsxArea.ID, "Someone Remote", "Hi from remote", "hello", "21:3/100 abc", time.Now()); err != nil {
		t.Fatalf("ReceiveEcho: %v", err)
	}
	if _, err := s.PostMessage(otherNetArea.ID, alice.ID, "All", "Wrong network", "should not appear"); err != nil {
		t.Fatalf("PostMessage otherNet: %v", err)
	}
	if _, err := s.PostMessage(localArea.ID, alice.ID, "All", "Local only", "no network at all"); err != nil {
		t.Fatalf("PostMessage local: %v", err)
	}

	// Network matching must be case-insensitive: the config-side label
	// (a sysop-set BinkpUplink.Network) won't always match the area's
	// own casing exactly.
	pending, err := s.PendingOutboundEcho("fsxnet")
	if err != nil {
		t.Fatalf("PendingOutboundEcho: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("PendingOutboundEcho = %+v, want exactly 1 (the local fsxNet post)", pending)
	}
	if pending[0].ID != localPost.ID {
		t.Fatalf("PendingOutboundEcho returned message %d, want the local post %d", pending[0].ID, localPost.ID)
	}
	if pending[0].AreaTag != "fsx_gen" {
		t.Fatalf("PendingOutboundEcho AreaTag = %q, want %q", pending[0].AreaTag, "fsx_gen")
	}

	if err := s.MarkSent(pending[0].ID); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	pending, err = s.PendingOutboundEcho("fsxnet")
	if err != nil {
		t.Fatalf("PendingOutboundEcho after MarkSent: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("PendingOutboundEcho after MarkSent = %+v, want empty", pending)
	}
}

func TestPendingOutboundEchoEmptyNetworkMatchesNothing(t *testing.T) {
	s, users := newTestStore(t)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.CreateArea("local", "Local Chat", "", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if _, err := s.PostMessage(area.ID, alice.ID, "All", "Hi", "local only, no network"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	pending, err := s.PendingOutboundEcho("")
	if err != nil {
		t.Fatalf("PendingOutboundEcho: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("PendingOutboundEcho(\"\") = %+v, want empty even though the area's own network is also \"\"", pending)
	}
}

func TestNeighborsWalksAreaInPostedOrder(t *testing.T) {
	s, users := newTestStore(t)
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}

	var ids []int64
	for i := 0; i < 3; i++ {
		m, err := s.PostMessage(area.ID, u.ID, "All", "subject", "body")
		if err != nil {
			t.Fatalf("PostMessage: %v", err)
		}
		ids = append(ids, m.ID)
	}

	// First message: no before, next is the second.
	first, err := s.MessageByID(ids[0])
	if err != nil {
		t.Fatalf("MessageByID: %v", err)
	}
	before, after, err := s.Neighbors(area.ID, first.ID)
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if before != nil {
		t.Fatalf("before first message = %v, want nil", *before)
	}
	if after == nil || *after != ids[1] {
		t.Fatalf("after first message = %v, want %d", after, ids[1])
	}

	// Middle message: before is the first, after is the third.
	mid, err := s.MessageByID(ids[1])
	if err != nil {
		t.Fatalf("MessageByID: %v", err)
	}
	before, after, err = s.Neighbors(area.ID, mid.ID)
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if before == nil || *before != ids[0] {
		t.Fatalf("before middle message = %v, want %d", before, ids[0])
	}
	if after == nil || *after != ids[2] {
		t.Fatalf("after middle message = %v, want %d", after, ids[2])
	}

	// Last message: before is the second, no after.
	last, err := s.MessageByID(ids[2])
	if err != nil {
		t.Fatalf("MessageByID: %v", err)
	}
	before, after, err = s.Neighbors(area.ID, last.ID)
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if before == nil || *before != ids[1] {
		t.Fatalf("before last message = %v, want %d", before, ids[1])
	}
	if after != nil {
		t.Fatalf("after last message = %v, want nil", *after)
	}
}

func TestFirstUnreadPosition(t *testing.T) {
	s, users := newTestStore(t)
	author, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	reader, err := users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}

	// Empty area: position 0.
	pos, err := s.FirstUnreadPosition(area.ID, reader.ID)
	if err != nil {
		t.Fatalf("FirstUnreadPosition (empty): %v", err)
	}
	if pos != 0 {
		t.Fatalf("empty area position = %d, want 0", pos)
	}

	var ids []int64
	for i := 0; i < 5; i++ {
		m, err := s.PostMessage(area.ID, author.ID, "All", "subject", "body")
		if err != nil {
			t.Fatalf("PostMessage: %v", err)
		}
		ids = append(ids, m.ID)
	}

	// Nothing read yet: first unread is the very first message (position 0).
	pos, err = s.FirstUnreadPosition(area.ID, reader.ID)
	if err != nil {
		t.Fatalf("FirstUnreadPosition: %v", err)
	}
	if pos != 0 {
		t.Fatalf("position with nothing read = %d, want 0", pos)
	}

	// Read the first three -- first unread is now the 4th message (position 3).
	for _, id := range ids[:3] {
		if err := s.MarkMessageRead(reader.ID, id); err != nil {
			t.Fatalf("MarkMessageRead: %v", err)
		}
	}
	pos, err = s.FirstUnreadPosition(area.ID, reader.ID)
	if err != nil {
		t.Fatalf("FirstUnreadPosition: %v", err)
	}
	if pos != 3 {
		t.Fatalf("position after reading first 3 = %d, want 3", pos)
	}

	// Read everything: falls back to the last message (position 4).
	for _, id := range ids[3:] {
		if err := s.MarkMessageRead(reader.ID, id); err != nil {
			t.Fatalf("MarkMessageRead: %v", err)
		}
	}
	pos, err = s.FirstUnreadPosition(area.ID, reader.ID)
	if err != nil {
		t.Fatalf("FirstUnreadPosition: %v", err)
	}
	if pos != 4 {
		t.Fatalf("position with everything read = %d, want 4 (last message)", pos)
	}
}

func TestQWKSelectedAreaIDsIsEmptyUntilConfigured(t *testing.T) {
	s, users := newTestStore(t)

	u, err := users.Register("caller", "pw", 10)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	selected, err := s.QWKSelectedAreaIDs(u.ID)
	if err != nil {
		t.Fatalf("QWKSelectedAreaIDs: %v", err)
	}
	if len(selected) != 0 {
		t.Fatalf("selected = %v, want empty before any selection is saved", selected)
	}
}

func TestSetQWKSelectedAreasRoundTripsAndReplacesPreviousSelection(t *testing.T) {
	s, users := newTestStore(t)

	u, err := users.Register("caller", "pw", 10)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	a1, err := s.CreateArea("one", "Area One", "", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	a2, err := s.CreateArea("two", "Area Two", "", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	a3, err := s.CreateArea("three", "Area Three", "", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	if err := s.SetQWKSelectedAreas(u.ID, []int64{a1.ID, a2.ID}); err != nil {
		t.Fatalf("SetQWKSelectedAreas: %v", err)
	}
	selected, err := s.QWKSelectedAreaIDs(u.ID)
	if err != nil {
		t.Fatalf("QWKSelectedAreaIDs: %v", err)
	}
	if !selected[a1.ID] || !selected[a2.ID] || selected[a3.ID] || len(selected) != 2 {
		t.Fatalf("selected = %v, want exactly {%d, %d}", selected, a1.ID, a2.ID)
	}

	// A second call replaces, not merges, the previous selection.
	if err := s.SetQWKSelectedAreas(u.ID, []int64{a3.ID}); err != nil {
		t.Fatalf("SetQWKSelectedAreas (replace): %v", err)
	}
	selected, err = s.QWKSelectedAreaIDs(u.ID)
	if err != nil {
		t.Fatalf("QWKSelectedAreaIDs: %v", err)
	}
	if !selected[a3.ID] || len(selected) != 1 {
		t.Fatalf("selected after replace = %v, want exactly {%d}", selected, a3.ID)
	}

	// An empty slice clears back to "no selection".
	if err := s.SetQWKSelectedAreas(u.ID, nil); err != nil {
		t.Fatalf("SetQWKSelectedAreas (clear): %v", err)
	}
	selected, err = s.QWKSelectedAreaIDs(u.ID)
	if err != nil {
		t.Fatalf("QWKSelectedAreaIDs: %v", err)
	}
	if len(selected) != 0 {
		t.Fatalf("selected after clear = %v, want empty", selected)
	}
}

func TestRenameNetworkMatchesWithoutCaseAndLeavesOthers(t *testing.T) {
	s, _ := newTestStore(t)
	for tag, network := range map[string]string{"a": "fsxNet Echo Areas", "b": "FSXNET ECHO AREAS", "c": "HobbyNet Echo Areas", "d": ""} {
		if _, err := s.CreateArea(tag, tag, "", network, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.RenameNetwork("fsxNet Echo Areas", "fsxNet")
	if err != nil || n != 2 {
		t.Fatalf("RenameNetwork = %d, %v; want 2", n, err)
	}
	if again, _ := s.RenameNetwork("fsxNet Echo Areas", "fsxNet"); again != 0 {
		t.Fatalf("second RenameNetwork changed %d rows, want 0", again)
	}
	areas, err := s.ListAreas(255)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, a := range areas {
		got[a.Tag] = a.Network
	}
	want := map[string]string{"a": "fsxNet", "b": "fsxNet", "c": "HobbyNet Echo Areas", "d": ""}
	for tag, net := range want {
		if got[tag] != net {
			t.Errorf("area %s network = %q, want %q", tag, got[tag], net)
		}
	}
}

func TestHiddenDataAreaStaysOutOfCallersLists(t *testing.T) {
	s, users := newTestStore(t)
	u, _ := users.Register("reader", "password123", user.SLNewUser)
	dat, _ := s.CreateArea("FSX_DAT", "Data", "", "fsxNet", 0, 0)
	if err := s.SetAreaHidden(dat.ID, true); err != nil {
		t.Fatal(err)
	}
	listed, _ := s.ListAreas(user.SLNewUser)
	stats, _ := s.ListAreaStats(user.SLNewUser, u.ID)
	for _, a := range listed {
		if a.ID == dat.ID {
			t.Fatal("hidden area in ListAreas")
		}
	}
	for _, st := range stats {
		if st.Area.ID == dat.ID {
			t.Fatal("hidden area in ListAreaStats")
		}
	}
	if a, _ := s.AreaByID(dat.ID); !a.Hidden {
		t.Fatal("AreaByID lost Hidden")
	}
	all, _ := s.AllAreas()
	found := false
	for _, a := range all {
		found = found || (a.ID == dat.ID && a.Hidden)
	}
	if !found {
		t.Fatal("the admin's AllAreas must still list the hidden area")
	}
}

func TestPostMessageAsKeepsTheGivenSenderAndGoesOut(t *testing.T) {
	s, users := newTestStore(t)
	sysop, _ := users.Register("sysop", "password123", user.SLSysop)
	dat, _ := s.CreateArea("FSX_DAT", "Data", "", "fsxNet", 0, 0)
	m, err := s.PostMessageAs(dat.ID, sysop.ID, "ibbslastcall", "All", "ibbslastcall-data", "body")
	if err != nil || m.FromName != "ibbslastcall" {
		t.Fatalf("PostMessageAs = %+v, %v", m, err)
	}
	pending, _ := s.PendingOutboundEcho("fsxNet")
	if len(pending) != 1 || pending[0].FromName != "ibbslastcall" {
		t.Fatalf("pending = %+v, want it going out as ibbslastcall", pending)
	}
	plain, _ := s.PostMessage(dat.ID, sysop.ID, "All", "Hi", "text")
	if plain.FromName != "sysop" {
		t.Fatalf("an ordinary post shows %q, want the username", plain.FromName)
	}
	got, _ := s.SubjectBodies(dat.ID, []string{"IBBSLASTCALL-DATA"}, 10)
	if len(got) != 1 || got[0].Body != "body" {
		t.Fatalf("SubjectBodies = %+v", got)
	}
}

// A local time (a server with TZ set) is stored as UTC, like
// CURRENT_TIMESTAMP: posted_at is compared and sorted as text.
func TestReceiveEchoStoresUTC(t *testing.T) {
	s, _ := newTestStore(t)
	area, _ := s.AreaByTag("general")
	zurich := time.FixedZone("CEST", 2*60*60)
	m, _, err := s.ReceiveEcho(area.ID, "x", "s", "b", "", time.Date(2026, 10, 1, 19, 26, 0, 0, zurich))
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	s.db.QueryRow(`SELECT substr(posted_at, 1, 19) FROM messages WHERE id = ?`, m.ID).Scan(&raw)
	if raw != "2026-10-01 17:26:00" {
		t.Errorf("stored %q, want the UTC time 2026-10-01 17:26:00", raw)
	}
}

func TestSearchFindsAcrossReadableAreasOnly(t *testing.T) {
	s, users := newTestStore(t)
	bob, _ := users.Register("bob", "password123", user.SLNewUser)
	general, _ := s.AreaByTag("general")
	secret, _ := s.CreateArea("SYSOPS", "Sysops", "", "", 200, 200)
	data, _ := s.CreateArea("FSX_DAT", "Data", "", "fsxNet", 0, 0)
	s.SetAreaHidden(data.ID, true)
	s.PostMessage(general.ID, bob.ID, "All", "Mystic tips", "How do I set up MRC?")
	s.PostMessage(general.ID, bob.ID, "Avon", "hello", "nothing here")
	s.PostMessage(secret.ID, bob.ID, "All", "MRC passwords", "x")
	s.PostMessage(data.ID, bob.ID, "All", "MRC data", "x")

	got, err := s.Search(10, "mrc", 0, 10)
	if err != nil || len(got) != 1 || got[0].Subject != "Mystic tips" {
		t.Fatalf("search mrc: %+v %v", got, err)
	}
	if got, _ := s.Search(10, "AVON", 0, 10); len(got) != 1 || got[0].Subject != "hello" {
		t.Fatalf("by recipient: %+v", got)
	}
	if got, _ := s.Search(10, "bob", general.ID, 10); len(got) != 2 {
		t.Fatalf("by sender in one area: %d", len(got))
	}
	if got, _ := s.Search(255, "mrc", 0, 10); len(got) != 2 {
		t.Fatalf("sysop sees the sysop area too (but no data area): %d", len(got))
	}
}
