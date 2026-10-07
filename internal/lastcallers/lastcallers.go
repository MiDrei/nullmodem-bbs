// Package lastcallers reads and writes InterBBS Last Callers records:
// each taking part board posts one to a data echo (fsxNet's FSX_DAT)
// whenever a caller logs off, and shows the collected list -- who was
// on which board lately.
//
// Two formats are in use, both ROT47-encoded so they don't read as
// text:
//
//   - The common one (Synchronet, Mystic, NE BBS; subject
//     "ibbslastcall-data", from "ibbslastcall"): seven lines between
//     ">>> BEGIN" and ">>> END" -- alias, BBS, date (MM/DD/YY), time
//     ("07:04a"), the caller's location, the board's system and its
//     address.
//   - Futureland's (subject "IBBSLastCall-Data"): "BEGIN", a header
//     line naming "|"-separated fields (epoch|alias|city|country|
//     client|door|bbs|url|system), records, "END".
//
// This system reads both and writes the common one.
package lastcallers

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/message"
)

// Subject and From are what a record written here is posted under.
const (
	Subject = "ibbslastcall-data"
	From    = "ibbslastcall"
)

// Record is one caller's logoff on some board.
type Record struct {
	Alias    string
	BBS      string
	Location string
	System   string
	Address  string
	// Date and Time are as the board wrote them ("09/30/26", "07:04a"):
	// its local clock, zone unknown.
	Date string
	Time string
}

// IsRecordSubject reports whether a message subject carries records.
func IsRecordSubject(subject string) bool {
	return strings.EqualFold(strings.TrimSpace(subject), Subject)
}

// rot47 is its own inverse.
func rot47(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 33 && c <= 126 {
			b[i] = 33 + (c-33+47)%94
		}
	}
	return string(b)
}

// Parse returns the records in a message body, in either format;
// none if it carries none.
func Parse(body string) []Record {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n"), "\n")
	var out []Record
	for i := 0; i < len(lines); i++ {
		switch strings.TrimSpace(lines[i]) {
		case ">>> BEGIN":
			var f []string
			for i++; i < len(lines) && strings.TrimSpace(lines[i]) != ">>> END"; i++ {
				f = append(f, strings.TrimSpace(rot47(lines[i])))
			}
			if len(f) >= 7 {
				out = append(out, Record{Alias: f[0], BBS: f[1], Date: f[2], Time: f[3], Location: f[4], System: f[5], Address: f[6]})
			}
		case "BEGIN":
			var header []string
			for i++; i < len(lines) && strings.TrimSpace(lines[i]) != "END"; i++ {
				fields := strings.Split(rot47(strings.TrimSpace(lines[i])), "|")
				if header == nil {
					header = fields
					continue
				}
				if r, ok := fromFields(header, fields); ok {
					out = append(out, r)
				}
			}
		}
	}
	return out
}

// fromFields maps a Futureland-style record by its header's names.
func fromFields(header, fields []string) (Record, bool) {
	v := map[string]string{}
	for i, name := range header {
		if i < len(fields) {
			v[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(fields[i])
		}
	}
	if v["alias"] == "" || v["bbs"] == "" {
		return Record{}, false
	}
	r := Record{Alias: v["alias"], BBS: v["bbs"], System: v["system"], Address: v["url"]}
	loc := []string{}
	for _, k := range []string{"city", "country"} {
		if v[k] != "" {
			loc = append(loc, v[k])
		}
	}
	r.Location = strings.Join(loc, ", ")
	if epoch, err := strconv.ParseInt(v["epoch"], 10, 64); err == nil && epoch > 0 {
		t := time.Unix(epoch, 0).UTC()
		r.Date, r.Time = FormatDate(t), FormatTime(t)
	}
	return r, true
}

// FormatDate and FormatTime give a time the way records carry it.
func FormatDate(t time.Time) string { return t.Format("01/02/06") }

func FormatTime(t time.Time) string {
	return fmt.Sprintf("%02d:%02d%c", (t.Hour()+11)%12+1, t.Minute(), "ap"[t.Hour()/12])
}

// Encode is the body of a message carrying r, in the common format.
func Encode(r Record) string {
	var b strings.Builder
	b.WriteString(">>> BEGIN\n")
	for _, f := range []string{r.Alias, r.BBS, r.Date, r.Time, r.Location, r.System, r.Address} {
		// A newline would split the field; ROT47 leaves spaces alone.
		b.WriteString(rot47(strings.Join(strings.Fields(f), " ")))
		b.WriteString("\n")
	}
	b.WriteString(">>> END\n")
	return b.String()
}

// futurelandSubject is the other format's subject.
const futurelandSubject = "IBBSLastCall-Data"

// Recent returns the newest records in the data echo tagged areaTag,
// newest first, at most limit; none if there's no such area.
func Recent(messages *message.Store, areaTag string, limit int) ([]Record, error) {
	area, err := messages.AreaByTag(areaTag)
	if errors.Is(err, message.ErrAreaNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// A message carries one record as a rule; a few spare for the odd
	// one that doesn't parse.
	msgs, err := messages.SubjectBodies(area.ID, []string{Subject, futurelandSubject}, limit+limit/2+5)
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, m := range msgs {
		recs := Parse(m.Body)
		for i := len(recs) - 1; i >= 0 && len(out) < limit; i-- {
			out = append(out, recs[i])
		}
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// PlaceFromTimezone is a caller's rough whereabouts for a record from
// their profile's time zone -- "Europe/Zurich" gives "Zurich" -- as
// callers here have no location field; "" without a zone.
func PlaceFromTimezone(tz string) string {
	i := strings.LastIndex(tz, "/")
	if tz == "" || i < 0 {
		return ""
	}
	return strings.ReplaceAll(tz[i+1:], "_", " ")
}
