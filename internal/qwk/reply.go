package qwk

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

// ParseReplyPacket opens an uploaded .REP file at path, confirms it's
// actually a zip by its magic bytes before trusting the extension
// (mirrors internal/tosser's own extractPacketBundle, which does the
// same check for FTS-5005 packet bundles), and reads its single
// <bbsID>.MSG entry (matched case-insensitively) via ReadMessagesDAT.
//
// Per the REP format's own convention, each returned message's
// Header.Number holds the destination conference number rather than
// a real message number -- see MessageHeader's own doc comment; the
// caller (internal/bbs's uploadQWKReply) is the one that knows to
// read it that way.
func ParseReplyPacket(path, bbsID string) ([]PackedMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("qwk: opening %s: %w", path, err)
	}
	magic := make([]byte, 4)
	_, readErr := io.ReadFull(f, magic)
	f.Close()
	if readErr != nil {
		return nil, fmt.Errorf("qwk: reading %s: %w", path, readErr)
	}
	if !bytes.HasPrefix(magic, []byte("PK\x03\x04")) && !bytes.HasPrefix(magic, []byte("PK\x05\x06")) {
		return nil, fmt.Errorf("qwk: %s is not a zip file", path)
	}

	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("qwk: opening %s as zip: %w", path, err)
	}
	defer zr.Close()

	wantName := strings.ToUpper(bbsID) + ".MSG"
	for _, zf := range zr.File {
		if !strings.EqualFold(zf.Name, wantName) {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return nil, fmt.Errorf("qwk: opening %s in %s: %w", zf.Name, path, err)
		}
		defer rc.Close()
		return ReadMessagesDAT(rc)
	}
	return nil, fmt.Errorf("qwk: %s not found in %s", wantName, path)
}
