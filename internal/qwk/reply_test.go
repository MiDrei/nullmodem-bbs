package qwk

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// buildTestRepZip builds a minimal .REP file at path: a zip containing
// one BBSID.MSG entry with the given messages (each Header.Number
// already set to whatever conference number the test wants, per the
// REP format's own repurposing of that field).
func buildTestRepZip(t *testing.T, path, bbsID string, messages []PackedMessage) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.Create(bbsID + ".MSG")
	if err != nil {
		t.Fatalf("zip.Create: %v", err)
	}
	if err := WriteMessagesDAT(w, messages); err != nil {
		t.Fatalf("WriteMessagesDAT: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}
}

func TestParseReplyPacketReadsBBSIDMsg(t *testing.T) {
	messages := []PackedMessage{
		{Header: MessageHeader{Number: 5, To: "All", From: "Alice", Subject: "reply"}, Text: "here's my reply"},
	}
	path := filepath.Join(t.TempDir(), "ALICE.REP")
	buildTestRepZip(t, path, "TESTBBS", messages)

	got, err := ParseReplyPacket(path, "testbbs")
	if err != nil {
		t.Fatalf("ParseReplyPacket: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1", len(got))
	}
	// Number field is repurposed to hold the conference number in a
	// REP -- ParseReplyPacket must hand it back unchanged, not
	// reinterpret it.
	if got[0].Header.Number != 5 {
		t.Fatalf("Header.Number = %d, want 5 (the conference number)", got[0].Header.Number)
	}
	if got[0].Text != "here's my reply" {
		t.Fatalf("Text = %q", got[0].Text)
	}
}

func TestParseReplyPacketRejectsNonZipFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.rep")
	if err := os.WriteFile(path, []byte("not a zip file at all"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := ParseReplyPacket(path, "testbbs"); err == nil {
		t.Fatal("ParseReplyPacket succeeded on a non-zip file, want an error")
	}
}

func TestParseReplyPacketErrorsWhenBBSIDFileMissing(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("WRONGID.MSG")
	_ = WriteMessagesDAT(w, nil)
	zw.Close()

	path := filepath.Join(t.TempDir(), "x.rep")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := ParseReplyPacket(path, "testbbs"); err == nil {
		t.Fatal("ParseReplyPacket succeeded despite no matching BBSID.MSG entry, want an error")
	}
}
