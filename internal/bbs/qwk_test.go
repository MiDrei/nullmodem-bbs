package bbs

import (
	"archive/zip"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"git.maik.ch/nullmodem/kit/qwk"
	"git.maik.ch/swissmaik/nullmodem/internal/qwkdoor"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

// TestDownloadQWKSendsRealPacketToRealRZOverTheBBSConnection is an
// end-to-end regression test through the actual BBS QWK download
// command (not internal/qwk directly): downloadQWK must build a real
// packet containing the caller's actual unread mail, take over the
// connection's raw byte stream, and hand it to a real Zmodem receiver
// correctly -- then mark everything included as read.
func TestDownloadQWKSendsRealPacketToRealRZOverTheBBSConnection(t *testing.T) {
	if _, err := exec.LookPath("sexyz"); err != nil {
		t.Skip("sexyz not installed; skipping real-interop download test (see docs/building-sexyz.md)")
	}

	s := testServer(t)
	s.BBSName = "Test BBS"
	s.SysopName = "Sysop"
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	// ReceiveEcho, not PostMessage: alice's own post would mark itself
	// read for her immediately (see message.Store.PostMessage's own
	// doc comment), leaving it out of her QWK download -- this test
	// wants both messages included.
	if _, _, err := s.Messages.ReceiveEcho(area.ID, "Bob", "Hello", "an echo post\nwith two lines", "", time.Now()); err != nil {
		t.Fatalf("ReceiveEcho: %v", err)
	}
	if _, err := s.Netmail.Receive("Bob", "21:1/1", u.ID, "alice", "", "Hi Alice", "a netmail body", time.Now(), false); err != nil {
		t.Fatalf("Netmail.Receive: %v", err)
	}

	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	term := NewTerminal(pipeConn{serverSide})

	recvDir := t.TempDir()
	cmd := startRZ(t, clientSide, recvDir)

	downloadDone := make(chan error, 1)
	go func() { downloadDone <- s.downloadQWK(term, u) }()

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("sexyz rz exited with error: %v", err)
		}
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		t.Fatal("sexyz rz did not exit in time")
	}

	select {
	case err := <-downloadDone:
		if err != nil {
			t.Fatalf("downloadQWK: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("downloadQWK did not return in time")
	}

	entries, err := filepath.Glob(filepath.Join(recvDir, "*.QWK"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("received files in %s: %v (glob err %v)", recvDir, entries, err)
	}

	messages := mustReadMessagesDATFromZip(t, entries[0])
	if len(messages) != 2 {
		t.Fatalf("packet contains %d messages, want 2", len(messages))
	}

	// Both included items must now be marked read -- the next
	// download shouldn't include them again.
	areaStats, err := s.Messages.ListAreaStats(u.SecurityLevel, u.ID)
	if err != nil {
		t.Fatalf("ListAreaStats: %v", err)
	}
	for _, st := range areaStats {
		if st.Area.ID == area.ID && st.New != 0 {
			t.Fatalf("area %d New = %d after download, want 0", area.ID, st.New)
		}
	}
	inbox, err := s.Netmail.Inbox(u.ID)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	for _, m := range inbox {
		if !m.ReadAt.Valid {
			t.Fatalf("netmail %d still unread after download", m.ID)
		}
	}
}

// TestUploadQWKReplyRoutesRepliesFromRealSexyzOverTheBBSConnection is
// an end-to-end regression test through the actual BBS QWK reply
// upload command: uploadQWKReply must receive a real .REP from a real
// Zmodem sender and route its replies into the right area/netmail.
func TestUploadQWKReplyRoutesRepliesFromRealSexyzOverTheBBSConnection(t *testing.T) {
	if _, err := exec.LookPath("sexyz"); err != nil {
		t.Skip("sexyz not installed; skipping real-interop upload test (see docs/building-sexyz.md)")
	}

	s := testServer(t)
	s.BBSName = "Test BBS"
	alice, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	bob, err := s.Users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	area, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}

	bbsID := qwkdoor.BBSID(s.BBSName)
	repPath := filepath.Join(t.TempDir(), "ALICE.REP")
	replies := []qwk.PackedMessage{
		{Header: qwk.MessageHeader{Number: int(area.ID), To: "All", Subject: "an echo reply"}, Text: "posted from offline"},
		{Header: qwk.MessageHeader{Number: 0, To: "bob", Subject: "a netmail reply"}, Text: "sent from offline"},
	}
	buildTestRepFile(t, repPath, bbsID, replies)

	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	term := NewTerminal(pipeConn{serverSide})

	cmd := startSZ(t, clientSide, repPath)

	uploadDone := make(chan error, 1)
	go func() { uploadDone <- s.uploadQWKReply(term, alice) }()

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("sexyz sz exited with error: %v", err)
		}
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		t.Fatal("sexyz sz did not exit in time")
	}

	select {
	case err := <-uploadDone:
		if err != nil {
			t.Fatalf("uploadQWKReply: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("uploadQWKReply did not return in time")
	}

	posted, err := s.Messages.ListMessages(area.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(posted) != 1 || posted[0].Subject != "an echo reply" || posted[0].Body != "posted from offline" {
		t.Fatalf("posted messages = %+v, want the one echo reply", posted)
	}

	bobInbox, err := s.Netmail.Inbox(bob.ID)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(bobInbox) != 1 || bobInbox[0].Subject != "a netmail reply" || bobInbox[0].Body != "sent from offline" {
		t.Fatalf("bob's inbox = %+v, want the one netmail reply", bobInbox)
	}
}

// mustReadMessagesDATFromZip opens a built .QWK file and decodes its
// MESSAGES.DAT entry via internal/qwk.
func mustReadMessagesDATFromZip(t *testing.T, qwkPath string) []qwk.PackedMessage {
	t.Helper()
	zr, err := zip.OpenReader(qwkPath)
	if err != nil {
		t.Fatalf("opening %s as zip: %v", qwkPath, err)
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if zf.Name != "MESSAGES.DAT" {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			t.Fatalf("opening MESSAGES.DAT in %s: %v", qwkPath, err)
		}
		defer rc.Close()
		messages, err := qwk.ReadMessagesDAT(rc)
		if err != nil {
			t.Fatalf("ReadMessagesDAT: %v", err)
		}
		return messages
	}
	t.Fatalf("%s has no MESSAGES.DAT entry", qwkPath)
	return nil
}

// buildTestRepFile writes a minimal .REP file at path: a zip
// containing one <bbsID>.MSG entry with the given replies, standing
// in for what a real offline reader would produce.
func buildTestRepFile(t *testing.T, path, bbsID string, replies []qwk.PackedMessage) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.Create(bbsID + ".MSG")
	if err != nil {
		t.Fatalf("zip.Create: %v", err)
	}
	if err := qwk.WriteMessagesDAT(w, replies); err != nil {
		t.Fatalf("WriteMessagesDAT: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}
}

// TestBuildQWKPacketForUserRespectsAreaSelection locks in that once a
// user has saved a QWK area selection, buildQWKPacketForUser only
// includes messages from selected areas (netmail is unaffected, it's
// always conference 0) -- and that an unconfigured (empty) selection
// still falls back to "every readable area with new mail", matching
// the original pre-selection behavior.
func TestBuildQWKPacketForUserRespectsAreaSelection(t *testing.T) {
	s := testServer(t)
	s.BBSName = "Test BBS"
	s.SysopName = "Sysop"
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	general, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	other, err := s.Messages.CreateArea("other", "Other Area", "", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	// ReceiveEcho, not PostMessage: alice's own post would mark itself
	// read for her immediately (see message.Store.PostMessage's own
	// doc comment), leaving nothing "new" for this test's area-
	// selection filtering to actually exercise.
	if _, _, err := s.Messages.ReceiveEcho(general.ID, "Bob", "In general", "general body", "", time.Now()); err != nil {
		t.Fatalf("ReceiveEcho: %v", err)
	}
	if _, _, err := s.Messages.ReceiveEcho(other.ID, "Bob", "In other", "other body", "", time.Now()); err != nil {
		t.Fatalf("ReceiveEcho: %v", err)
	}

	// No selection configured yet: both areas' new mail is included.
	dir1 := t.TempDir()
	_, count, _, markRead, err := s.buildQWKPacketForUser(u, dir1)
	if err != nil {
		t.Fatalf("buildQWKPacketForUser (no selection): %v", err)
	}
	if count != 2 {
		t.Fatalf("count with no selection = %d, want 2", count)
	}
	if len(markRead[general.ID]) != 1 || len(markRead[other.ID]) != 1 {
		t.Fatalf("markRead with no selection = %v, want one message in each area", markRead)
	}

	// Select only "other": general's new message must now be excluded.
	if err := s.Messages.SetQWKSelectedAreas(u.ID, []int64{other.ID}); err != nil {
		t.Fatalf("SetQWKSelectedAreas: %v", err)
	}
	dir2 := t.TempDir()
	_, count, _, markRead, err = s.buildQWKPacketForUser(u, dir2)
	if err != nil {
		t.Fatalf("buildQWKPacketForUser (other selected): %v", err)
	}
	if count != 1 {
		t.Fatalf("count with only 'other' selected = %d, want 1", count)
	}
	if len(markRead[general.ID]) != 0 || len(markRead[other.ID]) != 1 {
		t.Fatalf("markRead with only 'other' selected = %v, want only 'other'", markRead)
	}
}

// TestConfigureQWKAreasTogglesAndSaves drives the qwkareas builtin
// through a real Terminal over a net.Pipe: deselect the seeded
// "general" area (toggle #1) then save, and confirm the selection
// actually persisted via message.Store.QWKSelectedAreaIDs.
func TestConfigureQWKAreasTogglesAndSaves(t *testing.T) {
	s := testServer(t)
	u, err := s.Users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	general, err := s.Messages.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	other, err := s.Messages.CreateArea("other", "Other Area", "", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	term := NewTerminal(pipeConn{serverSide})

	done := make(chan error, 1)
	go func() { done <- s.configureQWKAreas(term, u) }()

	// net.Pipe is fully synchronous: the builtin's prompt/menu output
	// would otherwise block on Write with nothing reading it.
	go io.Copy(io.Discard, clientSide)

	go func() {
		// Toggle area #1 (general) off, then save.
		clientSide.Write([]byte("1\r\n"))
		time.Sleep(50 * time.Millisecond)
		clientSide.Write([]byte("S\r\n"))
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("configureQWKAreas: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("configureQWKAreas did not return in time")
	}

	selected, err := s.Messages.QWKSelectedAreaIDs(u.ID)
	if err != nil {
		t.Fatalf("QWKSelectedAreaIDs: %v", err)
	}
	if selected[general.ID] {
		t.Fatalf("selected = %v, general area should have been toggled off", selected)
	}
	if !selected[other.ID] {
		t.Fatalf("selected = %v, other area should still be selected", selected)
	}
}
