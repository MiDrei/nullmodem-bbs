// Package qwk implements the QWK offline-mail packet format (message
// download) and its REP reply-upload counterpart -- the classic
// BBS convention for reading/replying to echomail and netmail without
// staying connected. This package only handles the wire format
// (MESSAGES.DAT/CONTROL.DAT/.NDX/.QWK/.REP); internal/bbs owns
// gathering real messages into it and routing an uploaded reply back
// into the right area/netmail.
//
// Every field/byte-offset here was verified during design against the
// format's canonical documentation (wmcbrine.com's mirror of the
// original Sparkware/Patrick Lee/Jeffery Foy specs, cross-checked
// against the Synchronet wiki) rather than reconstructed from memory,
// since a real QWK reader parses these bytes literally.
package qwk

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// blockSize is QWK's fixed logical record length: every part of
// MESSAGES.DAT (the copyright notice in record 0, each message
// header, each message text block) is a multiple of this many bytes.
const blockSize = 128

// lineSep is the byte QWK uses to separate display lines within a
// message's text, instead of CR/LF or FTN's bare CR.
const lineSep = 227

// MessageHeader is one QWK message header record's fields, decoded
// from/encoded to the fixed 128-byte layout below (1-indexed byte
// offsets as the spec itself states them):
//
//	 1      status flag (' ' public unread, '-' public read,
//	        '*' private unread, '+' private read)
//	 2- 8   message number (ASCII digits, space-padded) -- see Number
//	 9-16   date "mm-dd-yy"
//	17-21   time "hh:mm" (24h)
//	22-46   To (25 bytes, space-padded)
//	47-71   From (25 bytes, space-padded)
//	72-96   Subject (25 bytes, space-padded)
//	97-108  password (12 bytes) -- always left blank; this BBS has no
//	        password-protected-message concept to carry
//	109-116 reference (in-reply-to) message number
//	117-122 number of 128-byte blocks, header included -- see Blocks
//	123     kill flag (225 active / 226 kill) -- always 225; nothing
//	        in this package ever marks a message killed
//	124-125 conference number, binary uint16 little-endian
//	126-127 logical message number in the packet, binary uint16 LE
//	128     network tagline flag ('*' or ' ')
type MessageHeader struct {
	Status byte
	// Number is this message's own number within its conference in a
	// downloaded .QWK packet. The .REP reply format repurposes this
	// exact same field to instead hold the destination conference
	// number (see that format's own doc comment) -- ReadMessagesDAT
	// always returns whatever was actually on disk; which meaning
	// applies is the caller's to know (ParseReplyPacket vs a plain
	// .QWK read).
	Number            int
	Written           time.Time
	To, From, Subject string
	// RefNumber is the "in-reply-to" message number, 0 if none.
	RefNumber int
	// Blocks is the total 128-byte block count this message occupies,
	// header included. WriteMessagesDAT computes and overwrites this
	// itself from the actual Text length -- callers never need to set
	// it. ReadMessagesDAT populates it from the on-disk value (and
	// uses it to know how many further blocks to read as Text).
	Blocks        int
	Conference    int
	LogicalNumber int
	HasTagline    bool
}

// PackedMessage is one QWK message: a header plus its text. Text uses
// plain "\n" between lines, like every other message body elsewhere
// in this codebase -- converted to/from QWK's own byte-227 line
// separator at the WriteMessagesDAT/ReadMessagesDAT boundary,
// mirroring how internal/mail/packet.go converts "\n"<->bare-CR at
// its own read/write boundary rather than leaking the wire convention
// into callers.
type PackedMessage struct {
	Header MessageHeader
	Text   string
}

// WriteMessagesDAT writes a complete MESSAGES.DAT: the fixed record-0
// copyright-notice block, then each message's header block followed
// by its text blocks, in order.
func WriteMessagesDAT(w io.Writer, messages []PackedMessage) error {
	if err := writeBlock(w, []byte("Produced by NullModem BBS")); err != nil {
		return fmt.Errorf("qwk: writing record 0: %w", err)
	}
	for i, m := range messages {
		if err := writeMessage(w, m); err != nil {
			return fmt.Errorf("qwk: writing message %d: %w", i, err)
		}
	}
	return nil
}

// ReadMessagesDAT reads a complete MESSAGES.DAT (or a .REP's
// identically-shaped BBSID.MSG -- see ParseReplyPacket), skipping the
// record-0 notice and returning every message that follows.
func ReadMessagesDAT(r io.Reader) ([]PackedMessage, error) {
	br := bufio.NewReader(r)

	var rec0 [blockSize]byte
	if _, err := io.ReadFull(br, rec0[:]); err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf("qwk: reading record 0: %w", err)
	}

	var messages []PackedMessage
	for {
		var hb [blockSize]byte
		if _, err := io.ReadFull(br, hb[:]); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("qwk: reading message %d header: %w", len(messages), err)
		}
		h := decodeHeader(hb)

		textBlocks := h.Blocks - 1
		if textBlocks < 0 {
			textBlocks = 0
		}
		text := make([]byte, 0, textBlocks*blockSize)
		for i := 0; i < textBlocks; i++ {
			var tb [blockSize]byte
			if _, err := io.ReadFull(br, tb[:]); err != nil {
				return nil, fmt.Errorf("qwk: reading message %d text block %d: %w", len(messages), i, err)
			}
			text = append(text, tb[:]...)
		}
		messages = append(messages, PackedMessage{Header: h, Text: decodeText(text)})
	}
	return messages, nil
}

func writeMessage(w io.Writer, m PackedMessage) error {
	textBlocks := encodeText(m.Text)
	h := m.Header
	h.Blocks = 1 + len(textBlocks)

	hb := encodeHeader(h)
	if _, err := w.Write(hb[:]); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}
	for i, blk := range textBlocks {
		if _, err := w.Write(blk[:]); err != nil {
			return fmt.Errorf("writing text block %d: %w", i, err)
		}
	}
	return nil
}

func encodeHeader(h MessageHeader) [blockSize]byte {
	var b [blockSize]byte
	for i := range b {
		b[i] = ' '
	}
	status := h.Status
	if status == 0 {
		status = ' '
	}
	b[0] = status
	copy(b[1:8], padASCII(strconv.Itoa(h.Number), 7))
	copy(b[8:16], padASCII(h.Written.Format("01-02-06"), 8))
	copy(b[16:21], padASCII(h.Written.Format("15:04"), 5))
	copy(b[21:46], padASCII(h.To, 25))
	copy(b[46:71], padASCII(h.From, 25))
	copy(b[71:96], padASCII(h.Subject, 25))
	// 96:108 password left blank (already space-filled above).
	if h.RefNumber > 0 {
		copy(b[108:116], padASCII(strconv.Itoa(h.RefNumber), 8))
	}
	copy(b[116:122], padASCII(strconv.Itoa(h.Blocks), 6))
	b[122] = 225 // kill flag: 225 = active; this package never marks a message killed
	binary.LittleEndian.PutUint16(b[123:125], uint16(h.Conference))
	binary.LittleEndian.PutUint16(b[125:127], uint16(h.LogicalNumber))
	if h.HasTagline {
		b[127] = '*'
	} else {
		b[127] = ' '
	}
	return b
}

func decodeHeader(b [blockSize]byte) MessageHeader {
	h := MessageHeader{
		Status:        b[0],
		To:            strings.TrimRight(string(b[21:46]), " "),
		From:          strings.TrimRight(string(b[46:71]), " "),
		Subject:       strings.TrimRight(string(b[71:96]), " "),
		Conference:    int(binary.LittleEndian.Uint16(b[123:125])),
		LogicalNumber: int(binary.LittleEndian.Uint16(b[125:127])),
		HasTagline:    b[127] == '*',
	}
	h.Number, _ = strconv.Atoi(strings.TrimSpace(string(b[1:8])))
	h.RefNumber, _ = strconv.Atoi(strings.TrimSpace(string(b[108:116])))
	h.Blocks, _ = strconv.Atoi(strings.TrimSpace(string(b[116:122])))
	dateTime := strings.TrimSpace(string(b[8:16])) + " " + strings.TrimSpace(string(b[16:21]))
	if t, err := time.Parse("01-02-06 15:04", dateTime); err == nil {
		h.Written = t
	}
	return h
}

// encodeText splits text (plain "\n"-separated) into QWK's own byte-
// 227-separated 128-byte blocks, the last space-padded. An empty text
// produces zero blocks -- a message with no body is legitimately just
// its header record.
func encodeText(text string) [][blockSize]byte {
	raw := []byte(strings.ReplaceAll(text, "\n", string([]byte{lineSep})))
	var blocks [][blockSize]byte
	for i := 0; i < len(raw); i += blockSize {
		end := min(i+blockSize, len(raw))
		var blk [blockSize]byte
		for j := range blk {
			blk[j] = ' '
		}
		copy(blk[:], raw[i:end])
		blocks = append(blocks, blk)
	}
	return blocks
}

// decodeText is encodeText's inverse.
func decodeText(raw []byte) string {
	raw = bytes.TrimRight(raw, " \x00")
	if len(raw) == 0 {
		return ""
	}
	return strings.ReplaceAll(string(raw), string([]byte{lineSep}), "\n")
}

// padASCII left-justifies s, space-padded/truncated to exactly width
// bytes -- the format every fixed-width ASCII field in a QWK header
// uses (numeric fields included: a reader trims and parses them the
// same way regardless of which side the padding sits on).
func padASCII(s string, width int) []byte {
	b := []byte(s)
	if len(b) > width {
		b = b[:width]
	}
	out := make([]byte, width)
	for i := range out {
		out[i] = ' '
	}
	copy(out, b)
	return out
}

func writeBlock(w io.Writer, content []byte) error {
	var block [blockSize]byte
	for i := range block {
		block[i] = ' '
	}
	copy(block[:], content)
	_, err := w.Write(block[:])
	return err
}
