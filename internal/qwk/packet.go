package qwk

import (
	"archive/zip"
	"fmt"
	"os"
	"strings"
)

// BuildQWKPacket writes a complete .QWK packet to disk at path: a zip
// containing CONTROL.DAT, MESSAGES.DAT, one <conf>.NDX per conference
// listed in control.Conferences (zero-padded to 3 digits), PERSONAL.NDX
// (pointers to every message whose To field matches control.CallerName
// or any of control.PersonalNames, case-insensitively and trimmed),
// and TOREADER.EXT (QWKE, see WriteToReaderEXT) when control.Username
// is set.
//
// messages must already be in the order they should appear in
// MESSAGES.DAT -- conference-grouped is conventional but not
// required; each message's own Header.Conference is what actually
// determines which .NDX file it's indexed into, independent of
// ordering.
func BuildQWKPacket(path string, control ControlInfo, messages []PackedMessage) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("qwk: creating %s: %w", path, err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)

	cw, err := zw.Create("CONTROL.DAT")
	if err != nil {
		return fmt.Errorf("qwk: creating CONTROL.DAT entry: %w", err)
	}
	if err := WriteControlDAT(cw, control); err != nil {
		return err
	}

	mw, err := zw.Create("MESSAGES.DAT")
	if err != nil {
		return fmt.Errorf("qwk: creating MESSAGES.DAT entry: %w", err)
	}
	if err := WriteMessagesDAT(mw, messages); err != nil {
		return err
	}

	if control.Username != "" {
		tw, err := zw.Create("TOREADER.EXT")
		if err != nil {
			return fmt.Errorf("qwk: creating TOREADER.EXT entry: %w", err)
		}
		if err := WriteToReaderEXT(tw, control.Username); err != nil {
			return err
		}
	}

	personalNames := append([]string{control.CallerName}, control.PersonalNames...)
	perConference, personal := indexMessages(personalNames, messages)

	for _, conf := range control.Conferences {
		nw, err := zw.Create(fmt.Sprintf("%03d.NDX", conf.Number))
		if err != nil {
			return fmt.Errorf("qwk: creating %03d.NDX entry: %w", conf.Number, err)
		}
		if err := WriteNDX(nw, perConference[conf.Number]); err != nil {
			return err
		}
	}

	pw, err := zw.Create("PERSONAL.NDX")
	if err != nil {
		return fmt.Errorf("qwk: creating PERSONAL.NDX entry: %w", err)
	}
	if err := WriteNDX(pw, personal); err != nil {
		return err
	}

	if err := zw.Close(); err != nil {
		return fmt.Errorf("qwk: finishing %s: %w", path, err)
	}
	return nil
}

// indexMessages computes each message's own MESSAGES.DAT record
// number (1-indexed; record 1 is always the copyright notice, so the
// first message's header starts at record 2) and groups the
// resulting .NDX entries by conference, separately collecting the
// subset addressed to any of names for PERSONAL.NDX.
func indexMessages(names []string, messages []PackedMessage) (perConference map[int][]NDXRecord, personal []NDXRecord) {
	perConference = map[int][]NDXRecord{}
	var trimmed []string
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			trimmed = append(trimmed, n)
		}
	}

	record := 2
	for _, m := range messages {
		rec := NDXRecord{MessageRecordNumber: record, Conference: m.Header.Conference}
		perConference[m.Header.Conference] = append(perConference[m.Header.Conference], rec)
		to := strings.TrimSpace(m.Header.To)
		for _, n := range trimmed {
			if strings.EqualFold(to, n) {
				personal = append(personal, rec)
				break
			}
		}
		record += 1 + len(encodeText(m.Text))
	}
	return perConference, personal
}
