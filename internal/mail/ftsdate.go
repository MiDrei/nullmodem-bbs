package mail

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ftscDateSize is the fixed 20-byte field a packed message's date
// stamp occupies (FTS-0001): the formatted string plus its null
// terminator, e.g. "03 Jan 26  23:51:10\x00" (19 chars + NUL).
const ftscDateSize = 20

// formatFTSCDate renders t in FTS-0001's fixed "DD Mon YY  HH:MM:SS"
// form (note the two spaces between the year and the hour -- that's
// how every real implementation writes it, not a typo).
func formatFTSCDate(t time.Time) string {
	month := t.Month().String()[:3]
	return fmt.Sprintf("%02d %s %02d  %02d:%02d:%02d",
		t.Day(), month, t.Year()%100, t.Hour(), t.Minute(), t.Second())
}

// parseFTSCDate parses the "DD Mon YY  HH:MM:SS" form back into a
// time.Time (UTC -- FTS-0001 carries no timezone information, so
// there's nothing more precise to reconstruct). Years are 2-digit;
// values 0-69 are read as 2000-2069 and 70-99 as 1970-1999, the usual
// pivot for FTN-era dates.
func parseFTSCDate(s string) (time.Time, error) {
	s = strings.TrimRight(s, "\x00")
	fields := strings.Fields(s)
	if len(fields) != 4 {
		return time.Time{}, fmt.Errorf("mail: malformed date %q", s)
	}
	day, err := strconv.Atoi(fields[0])
	if err != nil {
		return time.Time{}, fmt.Errorf("mail: malformed date %q: day: %w", s, err)
	}
	month, ok := monthByAbbrev[strings.ToLower(fields[1])]
	if !ok {
		return time.Time{}, fmt.Errorf("mail: malformed date %q: unknown month %q", s, fields[1])
	}
	year, err := strconv.Atoi(fields[2])
	if err != nil {
		return time.Time{}, fmt.Errorf("mail: malformed date %q: year: %w", s, err)
	}
	if year < 70 {
		year += 2000
	} else if year < 100 {
		year += 1900
	}

	clock := strings.Split(fields[3], ":")
	if len(clock) != 3 {
		return time.Time{}, fmt.Errorf("mail: malformed date %q: time %q", s, fields[3])
	}
	hour, err1 := strconv.Atoi(clock[0])
	minute, err2 := strconv.Atoi(clock[1])
	second, err3 := strconv.Atoi(clock[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return time.Time{}, fmt.Errorf("mail: malformed date %q: invalid time", s)
	}

	return time.Date(year, month, day, hour, minute, second, 0, time.UTC), nil
}

var monthByAbbrev = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March,
	"apr": time.April, "may": time.May, "jun": time.June,
	"jul": time.July, "aug": time.August, "sep": time.September,
	"oct": time.October, "nov": time.November, "dec": time.December,
}
