package web

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// postLineWidth is how long a line of a message written in the web
// portal or the mobile reader may be: FTN readers and Telnet screens
// show 79 columns, and a browser's textarea sends a paragraph as one
// long line.
const postLineWidth = 79

// quotePrefix matches an FTN quote's leading " XY> " (up to a few
// initials, possibly quoted again: "XY>>").
var quotePrefix = regexp.MustCompile(`^ ?[A-Za-z0-9]{0,4}>+ ?`)

// wrapLongLines breaks every line longer than width at the last space
// before it -- shorter lines, and so hand-aligned text, stay exactly as
// they are. A wrapped quote line keeps its " XY> " on each piece. A
// single word longer than the line (a URL) is left whole.
func wrapLongLines(body string, width int) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if utf8.RuneCountInString(line) <= width {
			out = append(out, line)
			continue
		}
		prefix := quotePrefix.FindString(line)
		if !strings.Contains(prefix, ">") {
			prefix = ""
		}
		rest := line[len(prefix):]
		room := width - utf8.RuneCountInString(prefix)
		if room < 20 {
			out = append(out, line)
			continue
		}
		for utf8.RuneCountInString(rest) > room {
			runes := []rune(rest)
			cut := -1
			for i := room; i > 0; i-- {
				if runes[i] == ' ' {
					cut = i
					break
				}
			}
			if cut < 0 {
				break // one long word: leave it
			}
			out = append(out, prefix+strings.TrimRight(string(runes[:cut]), " "))
			rest = strings.TrimLeft(string(runes[cut:]), " ")
		}
		out = append(out, prefix+rest)
	}
	return strings.Join(out, "\n")
}
