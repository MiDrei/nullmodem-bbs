package qwk

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// ConferenceInfo is one CONTROL.DAT conference entry -- conference 0
// is personal/netmail by QWK convention; every other number is a
// message area (see internal/bbs/qwk.go for how this codebase maps
// area IDs to conference numbers).
type ConferenceInfo struct {
	Number int
	// Name is truncated to 255 characters, CONTROL.DAT's own limit
	// under QWKE (extended from the classic format's 13). A reader
	// with no QWKE support simply displays/stores whatever fits its
	// own UI -- CONTROL.DAT is a plain line-based text file, so a
	// longer line here doesn't break its parsing, only (at worst)
	// its own display width.
	Name string
}

// ControlInfo is everything CONTROL.DAT needs. Conferences must
// include conference 0 itself (its own entry, typically named
// something like "Personal") -- WriteControlDAT's "number of
// conferences" line is exactly len(Conferences)-1, per the format's
// own convention of counting every conference beyond the mandatory 0.
type ControlInfo struct {
	BBSName   string
	City      string
	Phone     string
	SysopName string
	// BBSID identifies this system in the packet -- also the expected
	// base filename of an uploaded .REP's single message file (see
	// ParseReplyPacket). Written uppercased; keep it short (spec
	// convention is 8 characters or fewer, matching old DOS filename
	// limits) since it becomes part of a filename.
	BBSID      string
	PacketTime time.Time
	// CallerName is the display name written into CONTROL.DAT itself.
	CallerName string
	// PersonalNames are every other name a message could be addressed
	// to and still mean "this caller" for PERSONAL.NDX purposes (e.g.
	// their login username, when CallerName is instead their real
	// name) -- BuildQWKPacket matches a message's To field against
	// CallerName and every one of these, case-insensitively. CallerName
	// itself never needs repeating here.
	PersonalNames []string
	Conferences   []ConferenceInfo
	// Username, if set, is written into TOREADER.EXT's ALIAS line
	// (see WriteToReaderEXT) -- the caller's actual login handle,
	// kept distinct from CallerName since that may be their real name
	// instead.
	Username string
}

// WriteControlDAT writes CONTROL.DAT: a CRLF text file with lines in
// the QWK format's own fixed, exact order (see this package's doc
// comment for where that order was verified).
func WriteControlDAT(w io.Writer, c ControlInfo) error {
	lines := []string{
		c.BBSName,
		c.City,
		c.Phone,
		c.SysopName + ",Sysop",
		"00000," + strings.ToUpper(c.BBSID),
		c.PacketTime.Format("01-02-2006,15:04:05"),
		strings.ToUpper(c.CallerName),
		"",
		"0",
		"0",
		strconv.Itoa(len(c.Conferences) - 1),
	}
	for _, conf := range c.Conferences {
		lines = append(lines, strconv.Itoa(conf.Number), truncate(conf.Name, 255))
	}
	lines = append(lines, "WELCOME", "NEWS", "GOODBYE")

	for _, line := range lines {
		if _, err := io.WriteString(w, line+"\r\n"); err != nil {
			return fmt.Errorf("qwk: writing CONTROL.DAT: %w", err)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
