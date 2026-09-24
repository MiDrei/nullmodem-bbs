package qwk

import (
	"fmt"
	"io"
	"strings"
)

// WriteToReaderEXT writes TOREADER.EXT, the file whose mere presence
// in a .QWK packet is what a reader uses to recognize QWKE support
// (the QWKE 1.02 spec, wmcbrine.com/mmail/specs/qwke.html, defines no
// separate marker line for this in CONTROL.DAT). This package only
// writes the ALIAS line -- the caller's real login handle -- since
// that's the one piece of information callers of this package
// actually have to offer; the format also defines AREA/BULL/ATTACH/
// FILE/KEYWORD/FILTER/TWIT lines for other purposes this codebase has
// no equivalent state for yet.
func WriteToReaderEXT(w io.Writer, username string) error {
	if username == "" {
		return nil
	}
	if _, err := io.WriteString(w, "ALIAS "+username+"\r\n"); err != nil {
		return fmt.Errorf("qwk: writing TOREADER.EXT: %w", err)
	}
	return nil
}

// qwkeKludgeLimit is the classic MESSAGES.DAT header's fixed field
// width for To/From/Subject (25 bytes) -- unlike a conference name,
// this can never simply be made longer, since it's a real fixed-width
// binary field every reader (QWKE-aware or not) decodes at a fixed
// offset. QWKE's kludge lines are the documented workaround: a reader
// that understands them shows/imports the untruncated value instead
// of what's in the header.
const qwkeKludgeLimit = 25

// withQWKEKludges prepends QWKE kludge lines (TO:/FROM:/SUBJ:) ahead
// of text for any of to/from/subject that exceeds the classic 25-byte
// header field width, followed by a blank line -- verified against
// the QWKE 1.02 spec's own example ("TO:PETER ROCCAZISKINZIDONING" /
// "FROM:BOB WILIKERS" / "SUBJ:This is a test of the sys"), which also
// states kludges are only added when a field actually exceeds the
// limit. Fields at or under the limit need no kludge: the header
// alone already carries them in full.
func withQWKEKludges(to, from, subject, text string) string {
	var kludges []string
	if len(to) > qwkeKludgeLimit {
		kludges = append(kludges, "TO:"+to)
	}
	if len(from) > qwkeKludgeLimit {
		kludges = append(kludges, "FROM:"+from)
	}
	if len(subject) > qwkeKludgeLimit {
		kludges = append(kludges, "SUBJ:"+subject)
	}
	if len(kludges) == 0 {
		return text
	}
	return strings.Join(kludges, "\n") + "\n\n" + text
}
