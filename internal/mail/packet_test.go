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
