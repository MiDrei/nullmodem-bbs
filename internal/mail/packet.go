package mail

import (
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"git.maik.ch/swissmaik/nullmodem/internal/ansi"
)

// capValidWord and capWordValue are FSC-0039's way of letting a
// Type-2+-aware reader distinguish an extended header (carrying real
// zone/point info) from a plain Type-2 one without breaking readers
// that only know Type-2: capValidWord is written byte-swapped at one
// offset and capWordValue in normal order at another; if swapping
// capValidWord yields capWordValue, the extra zone/point fields are
// present and meaningful. An old reader sees a nonsensical "capValid"
// number and safely ignores it.
const (
	capValidWord = 0x0100
	capWordValue = 0x0001
)

// MessageAttr is the FTS-0001 message attribute bit field.
type MessageAttr uint16

const (
	AttrPrivate          MessageAttr = 1 << 0
	AttrCrash            MessageAttr = 1 << 1
	AttrReceived         MessageAttr = 1 << 2
	AttrSent             MessageAttr = 1 << 3
	AttrFileAttach       MessageAttr = 1 << 4
	AttrInTransit        MessageAttr = 1 << 5
	AttrOrphan           MessageAttr = 1 << 6
	AttrKillSent         MessageAttr = 1 << 7
	AttrLocal            MessageAttr = 1 << 8
	AttrHoldForPickup    MessageAttr = 1 << 9
	AttrFileRequest      MessageAttr = 1 << 11
	AttrReturnReceiptReq MessageAttr = 1 << 12
	AttrIsReturnReceipt  MessageAttr = 1 << 13
	AttrAuditRequest     MessageAttr = 1 << 14
	AttrFileUpdateReq    MessageAttr = 1 << 15
)

// PacketHeader identifies the two systems a packet moves mail between
// and when it was created. It's written once at the start of a
// packet and read once before any messages.
type PacketHeader struct {
	OrigAddr, DestAddr Address
	Created            time.Time
	// Password authenticates the packet itself (distinct from a
	// BinkP session's own password -- see internal/binkp) -- rarely
	// used today, but part of the format. Longer than 8 bytes is
	// truncated on write.
	Password string
}

// Message is one packed message: the common wire format for both
// netmail and echomail (the distinction is purely conventional -- a
// personal ToName and AttrPrivate for netmail, an "AREA:tag" first
// body line and no AttrPrivate for echomail; this package doesn't
// enforce either).
type Message struct {
	// OrigAddr and DestAddr are this message's own addresses, which
	// may differ from the packet header's (a packet can carry
	// messages for several destinations, or a point's messages
	// travel via its boss node's packet). A zero Address falls back
	// to the packet header's. A non-zero Zone/Point beyond what the
	// per-message Net/Node fields can carry is written out via INTL/
	// FMPT/TOPT kludge lines automatically prepended to Body
	// (FSC-0035), and parsed back out of them when reading -- callers
	// never need to construct those kludges themselves.
	OrigAddr, DestAddr Address
	Attr               MessageAttr
	Written            time.Time
	ToName, FromName   string
	Subject            string
	// Body is the message text with plain "\n" line endings; writing
	// converts to FTN's bare-CR convention and reading converts back.
	// Any kludge lines -- INTL/FMPT/TOPT this package adds itself,
	// plus anything the caller included (MSGID, AREA, SEEN-BY, ...)
	// -- remain literally part of Body, \x01 markers and all, so a
	// caller that needs them (a tosser matching MSGID/AREA) can find
	// them; this package only interprets INTL/FMPT/TOPT specifically,
	// to resolve OrigAddr/DestAddr's Zone/Point on read.
	Body string
}

// Packet bundles a header with all its messages, for building or
// consuming a whole .pkt file in memory (e.g. handing it to
// binkp.OutboundFile as a byte buffer) rather than streaming.
type Packet struct {
	Header   PacketHeader
	Messages []Message
}

// WriteTo serializes p as a complete .pkt file to w, implementing
// io.WriterTo.
func (p *Packet) WriteTo(w io.Writer) (int64, error) {
	cw := &countingWriter{w: w}
	pw, err := NewWriter(cw, p.Header)
	if err != nil {
		return cw.n, err
	}
	for _, m := range p.Messages {
		if err := pw.WriteMessage(m); err != nil {
			return cw.n, err
		}
	}
	return cw.n, pw.Close()
}

// ReadPacket reads a complete .pkt file from r.
func ReadPacket(r io.Reader) (*Packet, error) {
	pr, err := NewReader(r)
	if err != nil {
		return nil, err
	}
	p := &Packet{Header: pr.Header}
	for {
		m, err := pr.ReadMessage()
		if err == io.EOF {
			return p, nil
		}
		if err != nil {
			return nil, err
		}
		p.Messages = append(p.Messages, *m)
	}
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.n += int64(n)
	return n, err
}

// Writer streams a packet's header followed by zero or more messages.
// Callers must call Close exactly once after the last message to
// write the packet terminator.
type Writer struct {
	w      io.Writer
	header PacketHeader
}

// NewWriter writes header immediately and returns a Writer ready for
// WriteMessage calls.
func NewWriter(w io.Writer, header PacketHeader) (*Writer, error) {
	if err := writePacketHeader(w, header); err != nil {
		return nil, err
	}
	return &Writer{w: w, header: header}, nil
}

// WriteMessage appends one message to the packet.
func (pw *Writer) WriteMessage(m Message) error {
	return writeMessage(pw.w, pw.header, m)
}

// Close writes the packet's trailing zero word. It does not close the
// underlying writer.
func (pw *Writer) Close() error {
	var terminator [2]byte
	if _, err := pw.w.Write(terminator[:]); err != nil {
		return fmt.Errorf("mail: write packet terminator: %w", err)
	}
	return nil
}

// Reader reads a packet's header once (in NewReader), then its
// messages one at a time.
type Reader struct {
	r      io.Reader
	Header PacketHeader
}

// NewReader reads and validates the packet header at the start of r.
func NewReader(r io.Reader) (*Reader, error) {
	header, err := readPacketHeader(r)
	if err != nil {
		return nil, err
	}
	return &Reader{r: r, Header: header}, nil
}

// ReadMessage returns the next message, or io.EOF once the packet
// terminator is reached.
func (pr *Reader) ReadMessage() (*Message, error) {
	return readMessage(pr.r, pr.Header)
}

func writePacketHeader(w io.Writer, h PacketHeader) error {
	bw := &binWriter{w: w}

	bw.u16(uint16(h.OrigAddr.Node))
	bw.u16(uint16(h.DestAddr.Node))
	bw.u16(uint16(h.Created.Year()))
	bw.u16(uint16(h.Created.Month() - 1))
	bw.u16(uint16(h.Created.Day()))
	bw.u16(uint16(h.Created.Hour()))
	bw.u16(uint16(h.Created.Minute()))
	bw.u16(uint16(h.Created.Second()))
	bw.u16(0) // baud, unused
	bw.u16(2) // packet version -- always 2
	bw.u16(uint16(h.OrigAddr.Net))
	bw.u16(uint16(h.DestAddr.Net))
	bw.u8(0) // product code lo
	bw.u8(0) // product revision major
	bw.bytes(passwordField(h.Password))
	bw.u16(uint16(h.OrigAddr.Zone)) // FSC-0039 QOrgZone
	bw.u16(uint16(h.DestAddr.Zone)) // FSC-0039 QDstZone
	bw.bytes(make([]byte, 2))       // filler
	bw.u16(capValidWord)
	bw.u8(0) // product code hi
	bw.u8(0) // product revision minor
	bw.u16(capWordValue)
	bw.u16(uint16(h.OrigAddr.Zone)) // FSC-0039 OrigZone (redundant copy)
	bw.u16(uint16(h.DestAddr.Zone)) // FSC-0039 DestZone (redundant copy)
	bw.u16(uint16(h.OrigAddr.Point))
	bw.u16(uint16(h.DestAddr.Point))
	bw.i32(0) // product-specific data

	if bw.err != nil {
		return fmt.Errorf("mail: write packet header: %w", bw.err)
	}
	return nil
}

func passwordField(password string) []byte {
	b := make([]byte, 8)
	copy(b, password) // truncates if longer than 8, zero-pads if shorter
	return b
}

func readPacketHeader(r io.Reader) (PacketHeader, error) {
	br := &binReader{r: r}

	origNode := br.u16()
	destNode := br.u16()
	year := br.u16()
	month := br.u16()
	day := br.u16()
	hour := br.u16()
	minute := br.u16()
	second := br.u16()
	br.u16() // baud, ignored
	pktVer := br.u16()
	origNet := br.u16()
	destNet := br.u16()
	br.u8() // product code lo, ignored
	br.u8() // product revision major, ignored
	passwordBytes := br.bytes(8)
	qOrigZone := br.u16()
	qDestZone := br.u16()
	br.bytes(2) // filler, ignored
	capValid := br.u16()
	br.u8() // product code hi, ignored
	br.u8() // product revision minor, ignored
	capWord := br.u16()
	origZone2 := br.u16()
	destZone2 := br.u16()
	origPoint := br.u16()
	destPoint := br.u16()
	br.i32() // product-specific data, ignored

	if br.err != nil {
		return PacketHeader{}, fmt.Errorf("mail: read packet header: %w", br.err)
	}
	if pktVer != 2 {
		return PacketHeader{}, fmt.Errorf("mail: unsupported packet version %d (want 2)", pktVer)
	}

	var origZone, destZone, origPt, destPt int
	swapped := (capValid >> 8) | (capValid << 8)
	if capWord != 0 && capWord == swapped {
		// Type 2+: prefer the QOrgZone/QDstZone pair, falling back to
		// the redundant second copy if either is zero.
		origZone = int(qOrigZone)
		if origZone == 0 {
			origZone = int(origZone2)
		}
		destZone = int(qDestZone)
		if destZone == 0 {
			destZone = int(destZone2)
		}
		origPt = int(origPoint)
		destPt = int(destPoint)
	}
	// Else: a plain Type-2 header with no capability word -- leave
	// zone/point at 0 rather than trusting bytes a Type-2-only sender
	// never meant as zone/point info.

	return PacketHeader{
		OrigAddr: Address{Zone: origZone, Net: int(origNet), Node: int(origNode), Point: origPt},
		DestAddr: Address{Zone: destZone, Net: int(destNet), Node: int(destNode), Point: destPt},
		Created:  time.Date(int(year), time.Month(month+1), int(day), int(hour), int(minute), int(second), 0, time.UTC),
		Password: strings.TrimRight(string(passwordBytes), "\x00"),
	}, nil
}

func writeMessage(w io.Writer, header PacketHeader, m Message) error {
	orig := m.OrigAddr
	if orig.IsZero() {
		orig = header.OrigAddr
	}
	dest := m.DestAddr
	if dest.IsZero() {
		dest = header.DestAddr
	}

	body := m.Body
	if kludges := addressingKludges(orig, dest); kludges != "" {
		body = kludges + body
	}

	bw := &binWriter{w: w}
	bw.u16(2) // message type -- always 2, the old type-1 format is obsolete
	bw.u16(uint16(orig.Node))
	bw.u16(uint16(dest.Node))
	bw.u16(uint16(orig.Net))
	bw.u16(uint16(dest.Net))
	bw.u16(uint16(m.Attr))
	bw.u16(0) // cost, unused
	bw.dateField(m.Written)
	bw.zstring(m.ToName, 35)
	bw.zstring(m.FromName, 35)
	bw.zstring(m.Subject, 71)
	bw.zstring(toFTNLineEndings(body), 0)

	if bw.err != nil {
		return fmt.Errorf("mail: write message: %w", bw.err)
	}
	return nil
}

// addressingKludges builds the INTL/FMPT/TOPT lines (FSC-0035) needed
// so a message's real zone/point survive the packed-message header,
// which only has room for net/node -- or "" if orig/dest carry no
// information plain net/node can't already express.
func addressingKludges(orig, dest Address) string {
	var b strings.Builder
	if orig.Zone != 0 && dest.Zone != 0 && (orig.Zone != dest.Zone || orig.Point != 0 || dest.Point != 0) {
		fmt.Fprintf(&b, "\x01INTL %s %s\r",
			Address{Zone: dest.Zone, Net: dest.Net, Node: dest.Node},
			Address{Zone: orig.Zone, Net: orig.Net, Node: orig.Node})
	}
	if orig.Point != 0 {
		fmt.Fprintf(&b, "\x01FMPT %d\r", orig.Point)
	}
	if dest.Point != 0 {
		fmt.Fprintf(&b, "\x01TOPT %d\r", dest.Point)
	}
	return b.String()
}

func readMessage(r io.Reader, header PacketHeader) (*Message, error) {
	br := &binReader{r: r}

	msgType := br.u16()
	if br.err != nil {
		return nil, br.err
	}
	if msgType == 0 {
		return nil, io.EOF
	}
	if msgType != 2 {
		return nil, fmt.Errorf("mail: unsupported message type %d (want 2)", msgType)
	}

	origNode := br.u16()
	destNode := br.u16()
	origNet := br.u16()
	destNet := br.u16()
	attr := br.u16()
	br.u16() // cost, ignored
	written := br.dateField()
	toName := br.zstring()
	fromName := br.zstring()
	subject := br.zstring()
	body := br.zstring()

	if br.err != nil {
		return nil, fmt.Errorf("mail: read message: %w", br.err)
	}

	orig := Address{Zone: header.OrigAddr.Zone, Net: int(origNet), Node: int(origNode)}
	dest := Address{Zone: header.DestAddr.Zone, Net: int(destNet), Node: int(destNode)}
	body = fromFTNLineEndings(body)
	body = transcodeUTF8ToCP437(body)
	// To/From/Subject are ordinarily ASCII, but not guaranteed to be --
	// a sender's own name/a subject line can carry a real accented
	// character or punctuation (an em dash, a curly quote) sent as
	// UTF-8 rather than CP437 just as easily as a message body can.
	toName = transcodeUTF8ToCP437(toName)
	fromName = transcodeUTF8ToCP437(fromName)
	subject = transcodeUTF8ToCP437(subject)
	orig, dest = scanAddressingKludges(body, orig, dest)

	return &Message{
		OrigAddr: orig,
		DestAddr: dest,
		Attr:     MessageAttr(attr),
		Written:  written,
		ToName:   toName,
		FromName: fromName,
		Subject:  subject,
		Body:     body,
	}, nil
}

// scanAddressingKludges reads any INTL/FMPT/TOPT lines at the very
// start of body (kludges only ever appear there) to refine orig/dest
// beyond what the packed-message header's plain net/node carried --
// see addressingKludges, which is this function's write-side
// counterpart. It does not modify or strip body: a caller that needs
// other kludges (MSGID, AREA, ...) still finds them intact.
func scanAddressingKludges(body string, orig, dest Address) (Address, Address) {
	for _, raw := range strings.Split(body, "\n") {
		if !strings.HasPrefix(raw, "\x01") {
			break
		}
		line := raw[1:]
		switch {
		case strings.HasPrefix(line, "INTL "):
			fields := strings.Fields(line)
			if len(fields) == 3 {
				if a, err := ParseAddress(fields[1]); err == nil {
					dest.Zone, dest.Net, dest.Node = a.Zone, a.Net, a.Node
				}
				if a, err := ParseAddress(fields[2]); err == nil {
					orig.Zone, orig.Net, orig.Node = a.Zone, a.Net, a.Node
				}
			}
		case strings.HasPrefix(line, "FMPT "):
			if p, err := strconv.Atoi(strings.TrimSpace(line[len("FMPT "):])); err == nil {
				orig.Point = p
			}
		case strings.HasPrefix(line, "TOPT "):
			if p, err := strconv.Atoi(strings.TrimSpace(line[len("TOPT "):])); err == nil {
				dest.Point = p
			}
		}
	}
	return orig, dest
}

// toFTNLineEndings converts Go-style line breaks ("\n" or "\r\n") to
// FTN's bare "\r" convention used inside a packed message's body.
func toFTNLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", "\r")
}

// fromFTNLineEndings converts FTN's bare "\r" line breaks back to "\n".
// Collapses "\r\n" first, mirroring toFTNLineEndings' own order: FTS-
// 0001 requires bare CR only inside a packed message's body, but not
// every real-world packer honors that -- a raw .ANS file with its own
// CRLF line endings, embedded unchanged into a packet by a less
// careful ad-distribution tool, was found live turning every single
// line break into a spurious blank line: a naive blind "\r"->"\n"
// replacement converts the CR of an original "\r\n" pair separately
// from the LF right after it, doubling every line break into two.
func fromFTNLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// transcodeUTF8ToCP437 converts s to CP437 bytes (see
// ansi.EncodeCP437) if it looks like it was sent as UTF-8 rather than
// the raw CP437 bytes FTS-0001 assumes -- every byte-oriented piece of
// this codebase (WrapText, VisibleWidth, ansi.ParseGrid, HasArtBytes,
// ...) treats a message body as one CP437 byte per on-screen glyph, so
// UTF-8's multi-byte encoding of anything outside plain ASCII (a
// block/box-drawing character sent by a modern tool exporting Unicode
// ANSI art, or even just an umlaut in genuinely UTF-8-authored prose)
// gets torn apart one raw byte at a time and rendered as unrelated
// CP437 glyphs -- confirmed live against a real fsxNet ad (a "Cyber
// Sword BBS" ad built entirely from Unicode block elements, U+2580-
// U+25AA) that came out as scrambled mojibake.
//
// A field actually IS UTF-8 only when it both validates as UTF-8 (raw
// CP437 high bytes essentially never do, since CP437's 0x80-0xFF
// range doesn't follow UTF-8's continuation-byte structure by
// coincidence) and contains at least one non-ASCII byte -- checking
// validity alone would misfire on pure-ASCII content, which trivially
// validates as UTF-8 without being UTF-8-*encoded* in any meaningful
// sense, and must be left untouched.
func transcodeUTF8ToCP437(s string) string {
	if !hasNonASCIIByte(s) || !utf8.ValidString(s) {
		return s
	}
	return string(ansi.EncodeCP437(s))
}

// hasNonASCIIByte reports whether s contains any byte outside the
// 7-bit ASCII range.
func hasNonASCIIByte(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return true
		}
	}
	return false
}

// binWriter accumulates the first write error across a whole header
// or message, so the caller doesn't need to check one after every
// single field the way a hand-rolled version otherwise would.
type binWriter struct {
	w   io.Writer
	err error
}

func (bw *binWriter) u8(v uint8) {
	if bw.err != nil {
		return
	}
	_, bw.err = bw.w.Write([]byte{v})
}

func (bw *binWriter) u16(v uint16) {
	if bw.err != nil {
		return
	}
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], v)
	_, bw.err = bw.w.Write(buf[:])
}

func (bw *binWriter) i32(v int32) {
	if bw.err != nil {
		return
	}
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], uint32(v))
	_, bw.err = bw.w.Write(buf[:])
}

func (bw *binWriter) bytes(b []byte) {
	if bw.err != nil {
		return
	}
	_, bw.err = bw.w.Write(b)
}

// zstring writes s truncated to maxLen bytes (0 means no limit)
// followed by a NUL terminator -- FTS-0001's variable-length string
// encoding used for To/From/Subject/Text.
func (bw *binWriter) zstring(s string, maxLen int) {
	if bw.err != nil {
		return
	}
	b := []byte(s)
	if maxLen > 0 && len(b) > maxLen {
		b = b[:maxLen]
	}
	if _, bw.err = bw.w.Write(b); bw.err != nil {
		return
	}
	_, bw.err = bw.w.Write([]byte{0})
}

// dateField writes t as the fixed 20-byte FTSC date field.
func (bw *binWriter) dateField(t time.Time) {
	if bw.err != nil {
		return
	}
	b := make([]byte, ftscDateSize)
	copy(b, formatFTSCDate(t)) // 19 bytes; the field's last byte stays 0 as the terminator
	bw.bytes(b)
}

// binReader is binWriter's mirror image: it accumulates the first
// read error so field-by-field code doesn't need to check one after
// every call, checking bw.err/br.err once at the end instead.
type binReader struct {
	r   io.Reader
	err error
}

func (br *binReader) u8() uint8 {
	if br.err != nil {
		return 0
	}
	var buf [1]byte
	_, br.err = io.ReadFull(br.r, buf[:])
	return buf[0]
}

func (br *binReader) u16() uint16 {
	if br.err != nil {
		return 0
	}
	var buf [2]byte
	_, br.err = io.ReadFull(br.r, buf[:])
	return binary.LittleEndian.Uint16(buf[:])
}

func (br *binReader) i32() int32 {
	if br.err != nil {
		return 0
	}
	var buf [4]byte
	_, br.err = io.ReadFull(br.r, buf[:])
	return int32(binary.LittleEndian.Uint32(buf[:]))
}

func (br *binReader) bytes(n int) []byte {
	if br.err != nil {
		return nil
	}
	buf := make([]byte, n)
	_, br.err = io.ReadFull(br.r, buf)
	return buf
}

func (br *binReader) zstring() string {
	if br.err != nil {
		return ""
	}
	var buf []byte
	var b [1]byte
	for {
		if _, err := io.ReadFull(br.r, b[:]); err != nil {
			br.err = err
			return ""
		}
		if b[0] == 0 {
			return string(buf)
		}
		buf = append(buf, b[0])
	}
}

func (br *binReader) dateField() time.Time {
	if br.err != nil {
		return time.Time{}
	}
	raw := br.bytes(ftscDateSize)
	if br.err != nil {
		return time.Time{}
	}
	t, err := parseFTSCDate(string(raw))
	if err != nil {
		br.err = err
		return time.Time{}
	}
	return t
}
