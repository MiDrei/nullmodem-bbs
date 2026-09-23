package bbs

import (
	"archive/zip"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/qwk"
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
	if _, err := s.Messages.PostMessage(area.ID, u.ID, "All", "Hello", "an echo post\nwith two lines"); err != nil {
		t.Fatalf("PostMessage: %v", err)
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

	bbsID := qwkBBSID(s.BBSName)
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
