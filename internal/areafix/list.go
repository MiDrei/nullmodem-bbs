package areafix

import (
	"regexp"
	"strings"
)

// ParsedArea is one area line recovered from an Areafix/Filefix
// robot's "%LIST" reply -- see ParseAreaListReply.
type ParsedArea struct {
	Tag         string
	Description string
	// Subscribed is true when the reply line itself carried a
	// subscribed-marker ('+' or '*', the conventions seen most often
	// across real hub software), false otherwise. Not authoritative on
	// its own -- cross-check against EchoStore/FileStore.ListForUplink,
	// this system's own record of what it has actually asked for.
	Subscribed bool
}

// ParseAreaListReply heuristically extracts area entries from an
// Areafix/Filefix robot's "%LIST" reply body. Real hub software
// formats this differently -- confirmed live against three: "Clearing
// Houz" uses a bordered ':'-delimited table (see parseColonTableLine),
// a husky/hpt-based robot uses flag-letters+dot-fill+a quoted
// description (see parseMarkerDotsQuotedLine), and others use a plain
// marker-then-whitespace layout (see parseWhitespaceLine) -- so each
// line is tried against each shape in turn and skipped (rather than
// guessed at) if none fit: a table header, a "---" divider, a kludge,
// the reply's own tearline/origin/SEEN-BY/PATH footer.
//
// Always show the reply's raw body alongside this parse (see
// netmail.Store.InboxFromAddress) so the sysop can sanity-check it, or
// fall back to entering tags by hand if a particular hub's list
// format doesn't come through cleanly.
func ParseAreaListReply(body string) []ParsedArea {
	lines := strings.Split(body, "\n")
	var out []ParsedArea
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(strings.TrimRight(lines[i], "\r"))
		if trimmed == "" || isNonAreaLine(trimmed) {
			continue
		}
		if entry, extraLines, ok := parseMarkerDotsQuotedLine(lines, i); ok {
			out = append(out, entry)
			i += extraLines
			continue
		}
		if entry, ok := parseColonTableLine(trimmed); ok {
			out = append(out, entry)
			continue
		}
		if entry, ok := parseWhitespaceLine(trimmed); ok {
			out = append(out, entry)
		}
	}
	return out
}

// markerDotsQuotedLinePattern matches a flag-letters + dot-fill +
// quoted-description row -- confirmed live from a real hub (a husky/
// hpt-based Areafix robot):
//
//	*S   LVLY_ADULT ............... "Mature/18+ topics of discussion"
//
// -- flag letters with no space between them ('*'/'+' = active,
// 'R'/'W'/'M'/'S' = readonly/writeonly/mandatory/rescanable, any
// combination or none), the area tag, a run of literal dots padding
// out to the description, and the description itself in "double
// quotes".
var markerDotsQuotedLinePattern = regexp.MustCompile(`^([*+RWMS]{0,6})\s+([A-Za-z0-9_.\-]{2,40})\s+\.{3,}\s*"(.*)$`)

// parseMarkerDotsQuotedLine tries lines[start] against
// markerDotsQuotedLinePattern. The quoted description sometimes runs
// long enough to wrap onto the next physical line(s) with no closing
// quote yet on lines[start] itself (confirmed live) -- when that
// happens, this keeps appending subsequent lines (trimmed, verbatim)
// until one supplies the closing '"', capped at 5 lines so a reply
// with a genuinely missing closing quote can't consume the rest of
// the message. extraLines reports how many lines beyond start were
// consumed, so the caller's own loop index can skip them. Returns
// ok=false (and extraLines=0) if lines[start] doesn't match the
// pattern at all, or its tag column isn't a plausible area tag, so
// the caller falls back to parseColonTableLine/parseWhitespaceLine.
func parseMarkerDotsQuotedLine(lines []string, start int) (ParsedArea, int, bool) {
	trimmed := strings.TrimSpace(strings.TrimRight(lines[start], "\r"))
	m := markerDotsQuotedLinePattern.FindStringSubmatch(trimmed)
	if m == nil {
		return ParsedArea{}, 0, false
	}
	marker, tag := m[1], m[2]
	if !looksLikeAreaTag(tag) || isHeaderWord(tag) {
		return ParsedArea{}, 0, false
	}

	desc := m[3]
	extraLines := 0
	for !strings.Contains(desc, "\"") && start+extraLines+1 < len(lines) && extraLines < 5 {
		extraLines++
		next := strings.TrimSpace(strings.TrimRight(lines[start+extraLines], "\r"))
		desc += " " + next
	}
	if idx := strings.Index(desc, "\""); idx >= 0 {
		desc = desc[:idx]
	}
	return ParsedArea{Tag: tag, Description: strings.TrimSpace(desc), Subscribed: strings.ContainsAny(marker, "*+")}, extraLines, true
}

// parseColonTableLine handles a bordered ':'-delimited table row --
// confirmed live from a real hub ("Clearing Houz"):
//
//	:*  : FSX_ADS    : FSX: Ads + ANSI Art                : 551 :
//
// -- a subscribed-marker column ('*'=subscribed, '+'=available,
// 'R'/'W'=read/write-only, blank=neither), the area tag column, then
// everything up to an optional trailing purely-numeric "message
// count" column as the description (kept together even if it
// contains its own ':', like "FSX: Ads..." above -- only the marker
// and tag columns are split off explicitly). Returns ok=false if
// trimmed doesn't start with ':' or its second column isn't a
// plausible area tag, so the caller falls back to
// parseWhitespaceLine.
func parseColonTableLine(trimmed string) (ParsedArea, bool) {
	if !strings.HasPrefix(trimmed, ":") {
		return ParsedArea{}, false
	}
	parts := strings.Split(trimmed, ":")
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1] // drop the empty tail from the line's own closing ':'
	}
	if len(parts) < 3 {
		return ParsedArea{}, false
	}
	marker := strings.TrimSpace(parts[1])
	tag := strings.TrimSpace(parts[2])
	if !looksLikeAreaTag(tag) || isHeaderWord(tag) {
		return ParsedArea{}, false
	}

	rest := parts[3:]
	if len(rest) > 0 {
		if last := strings.TrimSpace(rest[len(rest)-1]); last != "" && isAllDigits(last) {
			rest = rest[:len(rest)-1] // drop the trailing message-count column
		}
	}
	description := strings.TrimSpace(strings.Join(rest, ":"))
	return ParsedArea{Tag: tag, Description: description, Subscribed: marker == "*"}, true
}

// parseWhitespaceLine handles the simpler, more common convention: an
// optional leading subscribed-marker ('+' or '*') followed by
// whitespace and the area tag, with anything remaining on the line
// kept as a description. Returns ok=false if the first whitespace-
// delimited token isn't a plausible area tag.
func parseWhitespaceLine(trimmed string) (ParsedArea, bool) {
	subscribed := false
	rest := trimmed
	if strings.HasPrefix(rest, "+") || strings.HasPrefix(rest, "*") {
		subscribed = true
		rest = strings.TrimSpace(rest[1:])
	}

	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ParsedArea{}, false
	}
	tag := fields[0]
	if !looksLikeAreaTag(tag) || isHeaderWord(tag) {
		return ParsedArea{}, false
	}
	description := strings.TrimSpace(strings.TrimPrefix(rest, tag))
	return ParsedArea{Tag: tag, Description: description, Subscribed: subscribed}, true
}

// isNonAreaLine reports whether trimmed is structural reply content
// that's never an area entry: a kludge line, a tearline or its origin
// line, a SEEN-BY/PATH footer line, or a "---"/"==="-style divider.
func isNonAreaLine(trimmed string) bool {
	if strings.HasPrefix(trimmed, "\x01") {
		return true
	}
	if strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "===") {
		return true
	}
	if strings.HasPrefix(trimmed, "* Origin:") {
		return true
	}
	upper := strings.ToUpper(trimmed)
	return strings.HasPrefix(upper, "SEEN-BY:") || strings.HasPrefix(upper, "PATH:")
}

// headerWords are common column-header labels and banner boilerplate
// that would otherwise pass looksLikeAreaTag's shape check (all
// letters, no punctuation) -- confirmed live: a real hub's table
// header row ("AREA" / "DESCRIPTION" / "MSGS") and its banner's own
// "FTN Mailer and Tosser" tagline both parsed as bogus area entries
// before this existed.
var headerWords = map[string]bool{
	"AREA": true, "AREAS": true, "TAG": true, "NAME": true,
	"DESCRIPTION": true, "DESC": true, "MSGS": true, "MESSAGES": true,
	"COUNT": true, "STATUS": true, "FTN": true,
}

func isHeaderWord(tag string) bool {
	return headerWords[strings.ToUpper(tag)]
}

// looksLikeAreaTag reports whether s is shaped like an FTN area tag:
// 2-40 characters, each a letter, digit, '_', '.', or '-', with at
// least one letter (so a lone number -- a count, a page number --
// isn't mistaken for one). An ordinary word from a reply's own prose
// ("Here", in "Here are the list of available echoareas:", confirmed
// live) would otherwise pass this shape check too, so a tag with any
// lowercase letter must also carry a digit/'_'/'.'/'-' -- real area
// tags are conventionally all-uppercase, or otherwise carry one of
// those, which ordinary capitalized English words don't. A single
// trailing '.' is stripped before that check: it's ordinary sentence
// punctuation on the first word of a table row description that
// wrapped onto its own line ("patterns.", confirmed live from a real
// reply whose descriptions ran long enough to wrap), never something
// a real tag ends with.
func looksLikeAreaTag(s string) bool {
	s = strings.TrimSuffix(s, ".")
	if len(s) < 2 || len(s) > 40 {
		return false
	}
	hasLetter, hasLower, hasStructural := false, false, false
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= 'a' && r <= 'z':
			hasLetter, hasLower = true, true
		case r >= '0' && r <= '9', r == '_', r == '.', r == '-':
			hasStructural = true
		default:
			return false
		}
	}
	if !hasLetter {
		return false
	}
	return !hasLower || hasStructural
}

// isAllDigits reports whether s is non-empty and consists only of
// ASCII digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
