package tosser

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/archive"
	"git.maik.ch/swissmaik/nullmodem/internal/binkp"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/db"
	"git.maik.ch/swissmaik/nullmodem/internal/mail"
	"git.maik.ch/swissmaik/nullmodem/internal/message"
	"git.maik.ch/swissmaik/nullmodem/internal/netmail"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

// newTestStoresWithArchive mirrors newTestStores, plus an
// archive.Store sharing the same database -- for tests exercising
// handleInboundFile's archiving side effect.
func newTestStoresWithArchive(t *testing.T) (*netmail.Store, *message.Store, *user.Store, *archive.Store) {
	t.Helper()
	dir := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return netmail.NewStore(sqlDB), message.NewStore(sqlDB), user.NewStore(sqlDB), archive.NewStore(sqlDB, filepath.Join(dir, "archive"))
}

func buildTestPacket(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := mail.NewWriter(&buf, mail.PacketHeader{
		OrigAddr: mail.Address{Zone: 21, Net: 3, Node: 100},
		DestAddr: mail.Address{Zone: 21, Net: 3, Node: 194},
		Created:  time.Now(),
	})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteMessage(mail.Message{
		ToName:   "Bob",
		FromName: "Alice",
		Subject:  "Hi",
		Body:     "hello",
	}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.Bytes()
}

// TestHandleInboundFileArchivesReceivedPacket locks in the interception
// point archiving relies on: every inbound file passed through
// handleInboundFile is captured to robot.Archive, whether or not it
// tosses cleanly.
func TestHandleInboundFileArchivesReceivedPacket(t *testing.T) {
	netmailStore, messages, users, archiveStore := newTestStoresWithArchive(t)
	if _, err := users.Register("bob", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	robot := &RobotConfig{Archive: archiveStore}
	packet := buildTestPacket(t)
	res := &Result{}

	err := handleInboundFile(binkp.InboundFile{Name: "12345678.pkt", Size: int64(len(packet))}, bytes.NewReader(packet), nil, netmailStore, messages, users, robot, nil, res, "21:3/100", "host.example.org:24554")
	if err != nil {
		t.Fatalf("handleInboundFile: %v", err)
	}
	if res.Received != 1 {
		t.Fatalf("Result.Received = %d, want 1", res.Received)
	}

	entries, total, err := archiveStore.List(10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(entries) != 1 {
		t.Fatalf("archive entries = %+v (total %d), want exactly one", entries, total)
	}
	e := entries[0]
	if e.Filename != "12345678.pkt" || e.UplinkAddress != "21:3/100" || e.UplinkHost != "host.example.org:24554" {
		t.Fatalf("archived entry = %+v, want the uplink identity passed in", e)
	}
	if e.Outcome != "ok" {
		t.Fatalf("Outcome = %q, want ok", e.Outcome)
	}

	rc, err := archiveStore.Open(e.ID)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()
	var got bytes.Buffer
	if _, err := got.ReadFrom(rc); err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if !bytes.Equal(got.Bytes(), packet) {
		t.Fatal("archived bytes don't match the packet actually received")
	}
}

// TestHandleInboundFileArchivesSkippedFileAsSkipped locks in that a
// file tossing rejects (not just an outright error) is still
// captured, with Outcome reflecting that -- the whole point of
// archiving unconditionally rather than only on hard failure.
func TestHandleInboundFileArchivesSkippedFileAsSkipped(t *testing.T) {
	netmailStore, messages, users, archiveStore := newTestStoresWithArchive(t)
	robot := &RobotConfig{Archive: archiveStore}
	res := &Result{}

	err := handleInboundFile(binkp.InboundFile{Name: "mystery.xyz", Size: 4}, bytes.NewReader([]byte("data")), nil, netmailStore, messages, users, robot, nil, res, "21:3/100", "host.example.org:24554")
	if err != nil {
		t.Fatalf("handleInboundFile: %v", err)
	}

	entries, _, err := archiveStore.List(10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Outcome != "skipped" {
		t.Fatalf("archive entries = %+v, want one with Outcome skipped", entries)
	}
}

// TestRetossReplaysArchivedPacket confirms Retoss can feed a
// previously archived file's raw bytes back through tossing and get
// the same result a live session would have.
func TestRetossReplaysArchivedPacket(t *testing.T) {
	netmailStore, messages, users, archiveStore := newTestStoresWithArchive(t)
	if _, err := users.Register("bob", "password123", user.SLNewUser); err != nil {
		t.Fatalf("Register: %v", err)
	}
	packet := buildTestPacket(t)

	res, err := Retoss([]RetossFile{{Name: "12345678.pkt", Data: packet, UplinkAddress: "21:3/100", UplinkHost: "host.example.org:24554"}},
		[]config.BinkpUplink{{Address: "21:3/100", Host: "host.example.org:24554"}},
		netmailStore, messages, users, &RobotConfig{Archive: archiveStore}, nil)
	if err != nil {
		t.Fatalf("Retoss: %v", err)
	}
	if res.Received != 1 {
		t.Fatalf("Result.Received = %d, want 1", res.Received)
	}
}

func TestRetossWithNoFilesReturnsEmptyResult(t *testing.T) {
	netmailStore, messages, users, _ := newTestStoresWithArchive(t)
	res, err := Retoss(nil, nil, netmailStore, messages, users, nil, nil)
	if err != nil {
		t.Fatalf("Retoss: %v", err)
	}
	if res.Received != 0 {
		t.Fatalf("Result.Received = %d, want 0", res.Received)
	}
}
