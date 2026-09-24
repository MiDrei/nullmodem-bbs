package qwk

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestBuildQWKPacketProducesExpectedFiles(t *testing.T) {
	control := ControlInfo{
		BBSName:    "Test BBS",
		BBSID:      "testbbs",
		PacketTime: time.Now(),
		CallerName: "Alice",
		Conferences: []ConferenceInfo{
			{Number: 0, Name: "Personal"},
			{Number: 5, Name: "General"},
		},
	}
	messages := []PackedMessage{
		{Header: MessageHeader{To: "Alice", From: "SYSTEM", Subject: "hi", Conference: 0}, Text: "a netmail"},
		{Header: MessageHeader{To: "All", From: "Bob", Subject: "post", Conference: 5}, Text: "an echomail post"},
	}

	path := filepath.Join(t.TempDir(), "TESTBBS.QWK")
	if err := BuildQWKPacket(path, control, messages); err != nil {
		t.Fatalf("BuildQWKPacket: %v", err)
	}

	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("opening built packet as zip: %v", err)
	}
	defer zr.Close()

	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	want := []string{"000.NDX", "005.NDX", "CONTROL.DAT", "MESSAGES.DAT", "PERSONAL.NDX"}
	sort.Strings(want)
	if len(names) != len(want) {
		t.Fatalf("packet contains %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("packet contains %v, want %v", names, want)
		}
	}
}

func TestBuildQWKPacketPersonalNDXMatchesCallerNameOrAliases(t *testing.T) {
	control := ControlInfo{
		BBSID:         "testbbs",
		PacketTime:    time.Now(),
		CallerName:    "Alice Example",
		PersonalNames: []string{"alice"},
		Conferences:   []ConferenceInfo{{Number: 0, Name: "Personal"}},
	}
	messages := []PackedMessage{
		{Header: MessageHeader{To: "Alice Example", Conference: 0}, Text: "matches CallerName"},
		{Header: MessageHeader{To: "alice", Conference: 0}, Text: "matches a PersonalNames alias"},
		{Header: MessageHeader{To: "Someone Else", Conference: 0}, Text: "matches nothing"},
	}
	path := filepath.Join(t.TempDir(), "x.qwk")
	if err := BuildQWKPacket(path, control, messages); err != nil {
		t.Fatalf("BuildQWKPacket: %v", err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("opening built packet: %v", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "PERSONAL.NDX" {
			continue
		}
		rc, _ := f.Open()
		defer rc.Close()
		var buf [10]byte
		n, _ := rc.Read(buf[:])
		if n != 10 { // two 5-byte records
			t.Fatalf("PERSONAL.NDX contains %d bytes, want 10 (2 records)", n)
		}
		return
	}
	t.Fatal("PERSONAL.NDX entry not found")
}

func TestIndexMessagesGroupsByConferenceAndFindsPersonal(t *testing.T) {
	messages := []PackedMessage{
		{Header: MessageHeader{To: "All", Conference: 5}, Text: "short"},
		{Header: MessageHeader{To: "Alice", Conference: 0}, Text: "hi alice"},
		{Header: MessageHeader{To: "  alice  ", Conference: 5}, Text: "another for alice, different conference"},
	}
	perConf, personal := indexMessages([]string{"Alice"}, messages)

	if len(perConf[5]) != 2 {
		t.Fatalf("conference 5 = %+v, want 2 entries", perConf[5])
	}
	if len(perConf[0]) != 1 {
		t.Fatalf("conference 0 = %+v, want 1 entry", perConf[0])
	}
	if len(personal) != 2 {
		t.Fatalf("personal = %+v, want 2 entries (case/whitespace-insensitive match on To)", personal)
	}

	// Record numbers must be strictly increasing and start at 2 (record
	// 1 is the copyright notice).
	if perConf[5][0].MessageRecordNumber != 2 {
		t.Fatalf("first message record number = %d, want 2", perConf[5][0].MessageRecordNumber)
	}
}

// TestBuildQWKPacketIncludesToReaderEXTWhenUsernameIsSet locks in that
// TOREADER.EXT (QWKE) is only added when ControlInfo.Username is set
// -- its presence is what a QWKE-aware reader uses to recognize the
// packet's extended CONTROL.DAT conference names/kludge lines, so a
// classic-only packet (no Username) should stay exactly as before.
func TestBuildQWKPacketIncludesToReaderEXTWhenUsernameIsSet(t *testing.T) {
	control := ControlInfo{
		BBSName:    "Test BBS",
		BBSID:      "testbbs",
		PacketTime: time.Now(),
		CallerName: "Alice",
		Username:   "alice",
		Conferences: []ConferenceInfo{
			{Number: 0, Name: "Personal"},
		},
	}
	path := filepath.Join(t.TempDir(), "TESTBBS.QWK")
	if err := BuildQWKPacket(path, control, nil); err != nil {
		t.Fatalf("BuildQWKPacket: %v", err)
	}

	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("opening built packet as zip: %v", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name != "TOREADER.EXT" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("opening TOREADER.EXT: %v", err)
		}
		defer rc.Close()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatalf("reading TOREADER.EXT: %v", err)
		}
		if buf.String() != "ALIAS alice\r\n" {
			t.Fatalf("TOREADER.EXT contents = %q, want %q", buf.String(), "ALIAS alice\r\n")
		}
		return
	}
	t.Fatal("packet has no TOREADER.EXT entry, want one since Username was set")
}
