package qwk

import (
	"fmt"
	"io"
)

// NDXRecord is one entry in a .NDX index file: which MESSAGES.DAT
// record (1-indexed, counting 128-byte blocks from the very start of
// the file -- record 1 is always the copyright notice, so the
// smallest valid value is 2, a message's own header block) a message
// lives at, and which conference it belongs to.
type NDXRecord struct {
	MessageRecordNumber int
	Conference          int
}

// WriteNDX writes one .NDX index file: a sequence of 5-byte records,
// each a 4-byte Microsoft Binary Format float (see mbf.go) holding
// MessageRecordNumber, followed by a 1-byte Conference number.
func WriteNDX(w io.Writer, records []NDXRecord) error {
	for i, rec := range records {
		mbf := float32ToMBF(float32(rec.MessageRecordNumber))
		buf := [5]byte{mbf[0], mbf[1], mbf[2], mbf[3], byte(rec.Conference)}
		if _, err := w.Write(buf[:]); err != nil {
			return fmt.Errorf("qwk: writing .NDX record %d: %w", i, err)
		}
	}
	return nil
}
