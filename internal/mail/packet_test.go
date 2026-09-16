package mail

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func TestPacketRoundTripSingleMessage(t *testing.T) {
	header := PacketHeader{
		OrigAddr: Address{Zone: 21, Net: 3, Node: 194, Point: 1},
		DestAddr: Address{Zone: 21, Net: 3, Node: 194},
		Created:  time.Date(2026, time.September, 13, 12, 30, 0, 0, time.UTC),
		Password: "secret",
	}
	msg := Message{
		Attr:     AttrPrivate | AttrCrash,
		Written:  time.Date(2026, time.September, 13, 12, 29, 0, 0, time.UTC),
		ToName:   "Mike Dreier",
		FromName: "Sysop",
		Subject:  "Test message",
		Body:     "Hello, world!\nSecond line.\n",
	}

	var buf bytes.Buffer
	pw, err := NewWriter(&buf, header)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := pw.WriteMessage(msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	pr, err := NewReader(&buf)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	if pr.Header.OrigAddr != header.OrigAddr {
		t.Errorf("OrigAddr = %v, want %v", pr.Header.OrigAddr, header.OrigAddr)
	}
	if pr.Header.DestAddr != header.DestAddr {
		t.Errorf("DestAddr = %v, want %v", pr.Header.DestAddr, header.DestAddr)
	}
	if !pr.Header.Created.Equal(header.Created) {
		t.Errorf("Created = %v, want %v", pr.Header.Created, header.Created)
	}
	if pr.Header.Password != header.Password {
		t.Errorf("Password = %q, want %q", pr.Header.Password, header.Password)
	}

	got, err := pr.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if got.ToName != msg.ToName {
		t.Errorf("ToName = %q, want %q", got.ToName, msg.ToName)
	}
	if got.FromName != msg.FromName {
		t.Errorf("FromName = %q, want %q", got.FromName, msg.FromName)
	}
	if got.Subject != msg.Subject {
		t.Errorf("Subject = %q, want %q", got.Subject, msg.Subject)
	}
	// header.OrigAddr carries a point, so the writer must prepend an
	// FMPT kludge -- the reader parses it back into OrigAddr.Point but
	// leaves it in Body, so Body itself gains a prefix here.
	if !strings.HasSuffix(got.Body, msg.Body) {
		t.Errorf("Body = %q, want suffix %q", got.Body, msg.Body)
	}
	if !strings.Contains(got.Body, "\x01FMPT 1") {
		t.Errorf("Body missing expected FMPT kludge, got %q", got.Body)
	}
	if got.Attr != msg.Attr {
		t.Errorf("Attr = %v, want %v", got.Attr, msg.Attr)
	}
	if !got.Written.Equal(msg.Written) {
		t.Errorf("Written = %v, want %v", got.Written, msg.Written)
	}
	// With no explicit per-message address, orig/dest should fall back
	// to the packet header's -- but since header.OrigAddr has a point,
	// the writer must have emitted FMPT and the reader must have
	// parsed it back out.
	if got.OrigAddr != header.OrigAddr {
		t.Errorf("OrigAddr = %v, want %v", got.OrigAddr, header.OrigAddr)
	}
	if got.DestAddr != header.DestAddr {
		t.Errorf("DestAddr = %v, want %v", got.DestAddr, header.DestAddr)
	}

	if _, err := pr.ReadMessage(); err != io.EOF {
		t.Errorf("second ReadMessage: err = %v, want io.EOF", err)
	}
}

func TestPacketRoundTripMultipleMessages(t *testing.T) {
	header := PacketHeader{
		OrigAddr: Address{Zone: 1, Net: 234, Node: 56},
		DestAddr: Address{Zone: 1, Net: 234, Node: 1},
		Created:  time.Now().UTC().Truncate(time.Second),
	}
	msgs := []Message{
		{ToName: "All", FromName: "Alice", Subject: "One", Body: "first"},
		{ToName: "All", FromName: "Bob", Subject: "Two", Body: "second"},
		{ToName: "All", FromName: "Carol", Subject: "Three", Body: "third"},
	}

	var buf bytes.Buffer
	pw, err := NewWriter(&buf, header)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	for _, m := range msgs {
		if err := pw.WriteMessage(m); err != nil {
			t.Fatalf("WriteMessage: %v", err)
		}
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	pr, err := NewReader(&buf)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	for i, want := range msgs {
		got, err := pr.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage %d: %v", i, err)
		}
		if got.Subject != want.Subject || got.Body != want.Body || got.FromName != want.FromName {
			t.Errorf("message %d = %+v, want %+v", i, got, want)
		}
	}
	if _, err := pr.ReadMessage(); err != io.EOF {
		t.Errorf("final ReadMessage: err = %v, want io.EOF", err)
	}
}

func TestPacketPacketHelperRoundTrip(t *testing.T) {
	p := &Packet{
		Header: PacketHeader{
			OrigAddr: Address{Zone: 1, Net: 1, Node: 1},
			DestAddr: Address{Zone: 1, Net: 1, Node: 2},
			Created:  time.Date(2025, time.December, 31, 23, 59, 59, 0, time.UTC),
		},
		Messages: []Message{
			{ToName: "Bob", FromName: "Alice", Subject: "Hi", Body: "hello"},
		},
	}

	var buf bytes.Buffer
	if _, err := p.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	got, err := ReadPacket(&buf)
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(got.Messages))
	}
	if got.Messages[0].Body != "hello" {
		t.Errorf("Body = %q, want %q", got.Messages[0].Body, "hello")
	}
}

func TestPacketDifferentZonesGetINTLKludge(t *testing.T) {
	header := PacketHeader{
		OrigAddr: Address{Zone: 21, Net: 3, Node: 194},
		DestAddr: Address{Zone: 21, Net: 3, Node: 1},
		Created:  time.Now().UTC(),
	}
	msg := Message{
		OrigAddr: Address{Zone: 1, Net: 234, Node: 56},
		DestAddr: Address{Zone: 2, Net: 20, Node: 100},
		ToName:   "Someone",
		FromName: "Someone Else",
		Subject:  "Cross-zone",
		Body:     "test body",
	}

	var buf bytes.Buffer
	pw, err := NewWriter(&buf, header)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := pw.WriteMessage(msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	pr, err := NewReader(&buf)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	got, err := pr.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if got.OrigAddr != msg.OrigAddr {
		t.Errorf("OrigAddr = %v, want %v", got.OrigAddr, msg.OrigAddr)
	}
	if got.DestAddr != msg.DestAddr {
		t.Errorf("DestAddr = %v, want %v", got.DestAddr, msg.DestAddr)
	}
	if !strings.Contains(got.Body, "\x01INTL 2:20/100 1:234/56") {
		t.Errorf("Body missing expected INTL kludge, got %q", got.Body)
	}
	if !strings.HasSuffix(got.Body, "test body") {
		t.Errorf("Body should still end with original text, got %q", got.Body)
	}
}

func TestPacketPointAddressesGetFMPTAndTOPT(t *testing.T) {
	header := PacketHeader{
		OrigAddr: Address{Zone: 21, Net: 3, Node: 194},
		DestAddr: Address{Zone: 21, Net: 3, Node: 1},
		Created:  time.Now().UTC(),
	}
	msg := Message{
		OrigAddr: Address{Zone: 21, Net: 3, Node: 194, Point: 5},
		DestAddr: Address{Zone: 21, Net: 3, Node: 1, Point: 7},
		ToName:   "Bob",
		FromName: "Alice",
		Subject:  "Points",
		Body:     "hi",
	}

	var buf bytes.Buffer
	pw, err := NewWriter(&buf, header)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := pw.WriteMessage(msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	pr, err := NewReader(&buf)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	got, err := pr.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if got.OrigAddr.Point != 5 {
		t.Errorf("OrigAddr.Point = %d, want 5", got.OrigAddr.Point)
	}
	if got.DestAddr.Point != 7 {
		t.Errorf("DestAddr.Point = %d, want 7", got.DestAddr.Point)
	}
	if !strings.Contains(got.Body, "\x01FMPT 5") {
		t.Errorf("Body missing FMPT kludge, got %q", got.Body)
	}
	if !strings.Contains(got.Body, "\x01TOPT 7") {
		t.Errorf("Body missing TOPT kludge, got %q", got.Body)
	}
}

func TestPacketNoKludgeWhenNoZoneOrPointInfo(t *testing.T) {
	header := PacketHeader{
		OrigAddr: Address{Zone: 21, Net: 3, Node: 194},
		DestAddr: Address{Zone: 21, Net: 3, Node: 1},
		Created:  time.Now().UTC(),
	}
	msg := Message{
		ToName:   "Bob",
		FromName: "Alice",
		Subject:  "Plain",
		Body:     "no kludges expected",
	}

	var buf bytes.Buffer
	pw, err := NewWriter(&buf, header)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := pw.WriteMessage(msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	pr, err := NewReader(&buf)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	got, err := pr.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if got.Body != msg.Body {
		t.Errorf("Body = %q, want %q (no kludge expected)", got.Body, msg.Body)
	}
}

func TestPacketStringTruncation(t *testing.T) {
	header := PacketHeader{
		OrigAddr: Address{Zone: 1, Net: 1, Node: 1},
		DestAddr: Address{Zone: 1, Net: 1, Node: 2},
		Created:  time.Now().UTC(),
	}
	longName := strings.Repeat("A", 100)
	longSubject := strings.Repeat("B", 100)
	msg := Message{
		ToName:   longName,
		FromName: longName,
		Subject:  longSubject,
		Body:     strings.Repeat("C", 10000), // body has no length limit
	}

	var buf bytes.Buffer
	pw, err := NewWriter(&buf, header)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := pw.WriteMessage(msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	pr, err := NewReader(&buf)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	got, err := pr.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if len(got.ToName) != 35 {
		t.Errorf("ToName len = %d, want 35", len(got.ToName))
	}
	if len(got.FromName) != 35 {
		t.Errorf("FromName len = %d, want 35", len(got.FromName))
	}
	if len(got.Subject) != 71 {
		t.Errorf("Subject len = %d, want 71", len(got.Subject))
	}
	if len(got.Body) != 10000 {
		t.Errorf("Body len = %d, want 10000 (no truncation)", len(got.Body))
	}
}

func TestPacketOldType2HeaderHasNoZonePoint(t *testing.T) {
	// Build a plain, pre-FSC-0039 Type-2 header by hand: same layout
	// but CapValid/CapWord left at zero, so a reader must not invent
	// zone/point info from bytes that were never meant to carry it.
	var buf bytes.Buffer
	bw := &binWriter{w: &buf}
	bw.u16(194) // orig node
	bw.u16(1)   // dest node
	bw.u16(2026)
	bw.u16(8) // month, 0-based = September
	bw.u16(13)
	bw.u16(12)
	bw.u16(0)
	bw.u16(0)
	bw.u16(0) // baud
	bw.u16(2) // pkt ver
	bw.u16(3) // orig net
	bw.u16(3) // dest net
	bw.u8(0)
	bw.u8(0)
	bw.bytes(make([]byte, 8)) // password
	bw.u16(0)                 // QOrigZone
	bw.u16(0)                 // QDestZone
	bw.bytes(make([]byte, 2))
	bw.u16(0) // CapValid = 0 (not FSC-0039 aware)
	bw.u8(0)
	bw.u8(0)
	bw.u16(0) // CapWord = 0
	bw.u16(0) // OrigZone
	bw.u16(0) // DestZone
	bw.u16(0) // OrigPoint
	bw.u16(0) // DestPoint
	bw.i32(0)
	if bw.err != nil {
		t.Fatalf("building raw header: %v", bw.err)
	}

	pr, err := NewReader(&buf)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	if pr.Header.OrigAddr.Zone != 0 || pr.Header.OrigAddr.Point != 0 {
		t.Errorf("OrigAddr = %v, want zone/point 0 for a plain Type-2 header", pr.Header.OrigAddr)
	}
	if pr.Header.OrigAddr.Net != 3 || pr.Header.OrigAddr.Node != 194 {
		t.Errorf("OrigAddr net/node = %d/%d, want 3/194", pr.Header.OrigAddr.Net, pr.Header.OrigAddr.Node)
	}
}

func TestPacketTruncatedInputIsAnError(t *testing.T) {
	if _, err := NewReader(bytes.NewReader(make([]byte, 10))); err == nil {
		t.Error("NewReader on truncated header: want error, got nil")
	}

	header := PacketHeader{
		OrigAddr: Address{Zone: 1, Net: 1, Node: 1},
		DestAddr: Address{Zone: 1, Net: 1, Node: 2},
		Created:  time.Now().UTC(),
	}
	var full bytes.Buffer
	pw, err := NewWriter(&full, header)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := pw.WriteMessage(Message{ToName: "A", FromName: "B", Subject: "C", Body: "D"}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	truncated := bytes.NewReader(full.Bytes()[:60]) // header + a few message bytes only
	pr, err := NewReader(truncated)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	if _, err := pr.ReadMessage(); err == nil {
		t.Error("ReadMessage on truncated message: want error, got nil")
	}
}

func TestPacketUnsupportedVersionIsRejected(t *testing.T) {
	header := PacketHeader{
		OrigAddr: Address{Zone: 1, Net: 1, Node: 1},
		DestAddr: Address{Zone: 1, Net: 1, Node: 2},
		Created:  time.Now().UTC(),
	}
	var buf bytes.Buffer
	if _, err := NewWriter(&buf, header); err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	raw := buf.Bytes()
	raw[18] = 9 // corrupt PktVer field
	raw[19] = 0

	if _, err := NewReader(bytes.NewReader(raw)); err == nil {
		t.Error("NewReader with bad PktVer: want error, got nil")
	}
}

func TestPacketBareCRLineEndingsOnWire(t *testing.T) {
	header := PacketHeader{
		OrigAddr: Address{Zone: 1, Net: 1, Node: 1},
		DestAddr: Address{Zone: 1, Net: 1, Node: 2},
		Created:  time.Now().UTC(),
	}
	msg := Message{ToName: "A", FromName: "B", Subject: "C", Body: "line1\nline2\r\nline3"}

	var buf bytes.Buffer
	pw, err := NewWriter(&buf, header)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := pw.WriteMessage(msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !bytes.Contains(buf.Bytes(), []byte("line1\rline2\rline3")) {
		t.Errorf("wire bytes should use bare CR line endings, got %q", buf.Bytes())
	}
}

// TestFromFTNLineEndingsCollapsesCRLFInsteadOfDoublingIt is a
// regression test for a real production bug: a message body wasn't
// guaranteed to strictly follow FTS-0001's bare-CR-only convention --
// a real fsxNet ANSI-art ad (converted from a raw .ANS file with its
// own CRLF line endings, embedded unchanged by a less careful ad-
// distribution tool) had genuine "\r\n" pairs inside its body. A naive
// blind "\r"->"\n" replacement converts the CR and the LF of that pair
// SEPARATELY, turning every one of its line breaks into two ("\n\n"),
// i.e. an extra blank line after every single row -- confirmed live,
// where it rendered as a diagonal mess of scattered blocks with every
// other row blank.
func TestFromFTNLineEndingsCollapsesCRLFInsteadOfDoublingIt(t *testing.T) {
	got := fromFTNLineEndings("line1\r\nline2\r\nline3")
	want := "line1\nline2\nline3"
	if got != want {
		t.Fatalf("fromFTNLineEndings(%q) = %q, want %q", "line1\\r\\nline2\\r\\nline3", got, want)
	}
}

// TestFromFTNLineEndingsStillHandlesBareCR confirms the common,
// FTS-0001-conformant case (bare CR only, no LF at all) still works
// after fixing the CRLF-doubling bug above.
func TestFromFTNLineEndingsStillHandlesBareCR(t *testing.T) {
	got := fromFTNLineEndings("line1\rline2\rline3")
	want := "line1\nline2\nline3"
	if got != want {
		t.Fatalf("fromFTNLineEndings(%q) = %q, want %q", "line1\\rline2\\rline3", got, want)
	}
}

// TestPacketReadCollapsesCRLFBodyInsteadOfDoublingIt is the same
// regression as TestFromFTNLineEndingsCollapsesCRLFInsteadOfDoublingIt,
// exercised end-to-end through ReadPacket the way tosser.tossInbound
// actually uses it, in case some other layer re-introduces the bug.
func TestPacketReadCollapsesCRLFBodyInsteadOfDoublingIt(t *testing.T) {
	header := PacketHeader{
		OrigAddr: Address{Zone: 1, Net: 1, Node: 1},
		DestAddr: Address{Zone: 1, Net: 1, Node: 2},
		Created:  time.Now().UTC(),
	}
	var buf bytes.Buffer
	pw, err := NewWriter(&buf, header)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	// Bypasses WriteMessage's own toFTNLineEndings (which would
	// correctly collapse this) to simulate a real non-conformant
	// packer embedding a raw CRLF-terminated body as-is.
	raw := Message{ToName: "A", FromName: "B", Subject: "C", Body: "line1\rline2\rline3"}
	if err := pw.WriteMessage(raw); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	wire := buf.Bytes()
	if !bytes.Contains(wire, []byte("line1\rline2\rline3")) {
		t.Fatalf("test setup: expected bare-CR body on the wire, got %q", wire)
	}
	wire = bytes.Replace(wire, []byte("line1\rline2\rline3"), []byte("line1\r\nline2\r\nline3"), 1)

	pkt, err := ReadPacket(bytes.NewReader(wire))
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	if len(pkt.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(pkt.Messages))
	}
	if got, want := pkt.Messages[0].Body, "line1\nline2\nline3"; got != want {
		t.Fatalf("parsed body = %q, want %q (no doubled blank lines)", got, want)
	}
}

// TestTranscodeUTF8ToCP437ConvertsUnicodeArt is a regression test for
// a real production bug: a body sent as UTF-8 (a modern tool exporting
// Unicode block-art characters, or just an umlaut in UTF-8-authored
// prose) went straight through byte-for-byte -- every consumer past
// this point treats a message body as one CP437 byte per glyph, so
// each multi-byte UTF-8 sequence got torn apart and rendered as
// unrelated CP437 glyphs. Confirmed live against a real fsxNet ad
// built entirely from Unicode block elements (U+2580-U+25AA) that
// came out as scrambled mojibake.
func TestTranscodeUTF8ToCP437ConvertsUnicodeArt(t *testing.T) {
	s := "█▄▌ block art" // █▄▌ block art
	got := transcodeUTF8ToCP437(s)
	want := "\xdb\xdc\xdd block art"
	if got != want {
		t.Fatalf("transcodeUTF8ToCP437(%q) = %q, want %q", s, got, want)
	}
}

// TestTranscodeUTF8ToCP437LeavesPlainASCIIUnchanged confirms pure
// ASCII (which trivially validates as UTF-8 without being UTF-8-
// *encoded* in any meaningful sense) is never touched -- the vast
// majority of real messages, which must not pay any conversion cost
// or risk any corruption from this fix.
func TestTranscodeUTF8ToCP437LeavesPlainASCIIUnchanged(t *testing.T) {
	s := "just an ordinary ASCII message\nwith more than one line"
	if got := transcodeUTF8ToCP437(s); got != s {
		t.Fatalf("transcodeUTF8ToCP437(%q) = %q, want unchanged", s, got)
	}
}

// TestTranscodeUTF8ToCP437LeavesRawCP437Unchanged confirms genuine
// raw CP437 bytes (which essentially never validate as UTF-8) pass
// through untouched rather than being mangled by a false-positive
// UTF-8 decode.
func TestTranscodeUTF8ToCP437LeavesRawCP437Unchanged(t *testing.T) {
	s := "block art: \xdb\xdb\xdb and a line: \xc4\xc4\xc4"
	if got := transcodeUTF8ToCP437(s); got != s {
		t.Fatalf("transcodeUTF8ToCP437(%q) = %q, want unchanged (already raw CP437)", s, got)
	}
}

// TestPacketReadTranscodesUTF8FieldsToCP437 exercises the fix end-to-
// end through ReadPacket, across all four text fields (To/From/
// Subject/Body) -- a sender's own name or a subject line can carry
// UTF-8 just as easily as a message body (e.g. a curly quote).
func TestPacketReadTranscodesUTF8FieldsToCP437(t *testing.T) {
	header := PacketHeader{
		OrigAddr: Address{Zone: 1, Net: 1, Node: 1},
		DestAddr: Address{Zone: 1, Net: 1, Node: 2},
		Created:  time.Now().UTC(),
	}
	var buf bytes.Buffer
	pw, err := NewWriter(&buf, header)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	msg := Message{
		ToName:   "All",
		FromName: "Café Owner", // "Café Owner" -- é is UTF-8-encoded here
		Subject:  "NSA’s Supercomputer",
		Body:     "block art: ██",
	}
	if err := pw.WriteMessage(msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	pkt, err := ReadPacket(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	if len(pkt.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(pkt.Messages))
	}
	got := pkt.Messages[0]
	if got.FromName != "Caf\x82 Owner" { // CP437 0x82 is é
		t.Fatalf("FromName = %q, want CP437-transcoded", got.FromName)
	}
	// CP437 has no curly-quote glyph, so EncodeCP437 falls back to '?'
	// -- what matters here is that it's ASCII '?', not the original
	// multi-byte UTF-8 sequence still sitting there uncorrupted-looking
	// but rendering as mojibake on a CP437 terminal.
	if got.Subject != "NSA?s Supercomputer" {
		t.Fatalf("Subject = %q, want %q", got.Subject, "NSA?s Supercomputer")
	}
	if got.Body != "block art: \xdb\xdb" {
		t.Fatalf("Body = %q, want CP437-transcoded", got.Body)
	}
}
