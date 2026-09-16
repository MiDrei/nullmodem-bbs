// Package tic parses TIC (FTS-0006 "type 2" file distribution) files
// -- the small text sidecar that accompanies each file transferred
// through a file-echo, announcing which local file-echo area it
// belongs to and describing the file itself. This package only
// parses the format; internal/tosser owns correlating a TIC with its
// paired file transfer and actually tossing it (see that package's
// tic_inbound.go).
package tic

import (
	"errors"
	"strconv"
	"strings"
)

// ErrMissingArea is returned by Parse when the required "Area" line
// is absent -- there'd be nothing to route the file to.
var ErrMissingArea = errors.New("tic: missing Area line")

// ErrMissingFile is returned by Parse when the required "File" line
// is absent -- there'd be nothing to correlate this descriptor
// against the actual file transfer.
var ErrMissingFile = errors.New("tic: missing File line")

// File is one TIC descriptor. Real-world TIC files carry more fields
// than this parses (Path/Seenby hop tracking, From/To routing,
// Replaces, Date, ...); only what internal/tosser's inbound toss
// actually uses today is kept here -- add more if/when hub forwarding
// (SEEN-BY dupe suppression) needs them.
type File struct {
	// Area is the file-echo area tag this file belongs to ("Area") --
	// routes the same way an echomail AREA kludge does (see
	// internal/tosser's echoAreaTag/tossEcho).
	Area string
	// Name is the actual filename this TIC describes ("File") --
	// correlates this descriptor with the matching file BinkP
	// transfers alongside it in the same session.
	Name string
	// Description is "Desc" plus every "Ldesc" line, joined with "\n"
	// in the order they appeared -- real hub software uses Desc for a
	// short one-liner and Ldesc for additional detail lines.
	Description string
	// SizeBytes is the file's claimed size ("Size"), 0 if absent or
	// unparseable -- sanity-checked against the actually received
	// file's real size by the caller, not trusted blindly.
	SizeBytes int64
	// CRC32 is the file's claimed CRC-32 ("Crc", hex), meaningful only
	// when HasCRC32 is true -- not every real hub sends this line.
	CRC32    uint32
	HasCRC32 bool
	// Origin is the FTN address that originated this file ("Origin").
	Origin string
	// Password is this TIC's own "Pw" line, checked the same way a
	// packet password is (see config.BinkpUplink.TICPassword).
	Password string
}

// Parse parses a .tic file's contents: simple line-based "Keyword
// value" pairs (FTS-0006), one per line, keyword matched case-
// insensitively and separated from its value by the first run of
// whitespace. An unrecognized keyword is silently ignored -- real hub
// software routinely adds vendor-specific extensions this package has
// no need to understand. Returns ErrMissingArea/ErrMissingFile if
// either required line is absent.
func Parse(data []byte) (File, error) {
	var f File
	var desc string
	var ldesc []string

	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		keyword, value := splitTICLine(line)
		if keyword == "" {
			continue
		}
		switch strings.ToLower(keyword) {
		case "area":
			f.Area = value
		case "file":
			f.Name = value
		case "desc":
			desc = value
		case "ldesc":
			ldesc = append(ldesc, value)
		case "size":
			if n, err := strconv.ParseInt(value, 10, 64); err == nil {
				f.SizeBytes = n
			}
		case "crc":
			if n, err := strconv.ParseUint(value, 16, 32); err == nil {
				f.CRC32 = uint32(n)
				f.HasCRC32 = true
			}
		case "origin":
			f.Origin = value
		case "pw":
			f.Password = value
		}
	}

	if desc != "" {
		ldesc = append([]string{desc}, ldesc...)
	}
	f.Description = strings.Join(ldesc, "\n")

	if f.Area == "" {
		return File{}, ErrMissingArea
	}
	if f.Name == "" {
		return File{}, ErrMissingFile
	}
	return f, nil
}

// splitTICLine splits a trimmed, non-empty line into its keyword
// (everything up to the first whitespace) and value (everything
// after, trimmed) -- a bare keyword with nothing following it (rare,
// but seen live for a flag-only line) returns an empty value rather
// than being rejected.
func splitTICLine(line string) (keyword, value string) {
	idx := strings.IndexAny(line, " \t")
	if idx < 0 {
		return line, ""
	}
	return line[:idx], strings.TrimSpace(line[idx+1:])
}
