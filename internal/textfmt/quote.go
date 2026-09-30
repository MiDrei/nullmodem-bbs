package textfmt

import (
	"regexp"
	"strings"
	"unicode"
)

// Initials is how a reply quotes name: two or more words give the first
// letters of the first and last ("Deon George" -> DG), one word its
// first two ("SwissMaik" -> Sw). Mirrors web/src/lib/reader/quote.ts.
func Initials(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	switch len(words) {
	case 0:
		return "XX"
	case 1:
		r := []rune(words[0])
		out := string(unicode.ToUpper(r[0]))
		if len(r) > 1 {
			out += string(unicode.ToLower(r[1]))
		}
		return out
	}
	first, last := []rune(words[0]), []rune(words[len(words)-1])
	return strings.ToUpper(string(first[0]) + string(last[0]))
}

var alreadyQuoted = regexp.MustCompile(`^ ?[\pL\pN]{0,4}>`)

// QuoteLines is the start of a reply to body by from, addressed to to:
// " -=> from wrote to to <=-", a blank line, then the original's lines
// quoted " XY> " (lines quoted before keep their own prefix), without
// its kludges, tearline, origin, SEEN-BY and PATH, wrapped at
// LineWidth.
func QuoteLines(body, from, to string) []string {
	if strings.TrimSpace(to) == "" {
		to = "All"
	}
	prefix := " " + Initials(from) + "> "
	var quoted []string
	for _, line := range strings.Split(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "\x01"):
			continue
		case line == "---" || strings.HasPrefix(line, "--- "):
			goto done
		case strings.HasPrefix(line, " * Origin:"), strings.HasPrefix(line, "SEEN-BY:"), strings.HasPrefix(line, "PATH:"):
			continue
		case strings.TrimSpace(line) == "":
			quoted = append(quoted, "")
		case alreadyQuoted.MatchString(line):
			quoted = append(quoted, line)
		default:
			quoted = append(quoted, prefix+line)
		}
	}
done:
	for len(quoted) > 0 && quoted[len(quoted)-1] == "" {
		quoted = quoted[:len(quoted)-1]
	}
	for len(quoted) > 0 && quoted[0] == "" {
		quoted = quoted[1:]
	}
	head := " -=> " + from + " wrote to " + strings.TrimSpace(to) + " <=-"
	if len(quoted) == 0 {
		return []string{head}
	}
	return append([]string{head, ""}, strings.Split(WrapLongLines(strings.Join(quoted, "\n"), LineWidth), "\n")...)
}
