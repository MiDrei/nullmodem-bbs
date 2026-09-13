package ansi

import (
	"regexp"
	"strings"
)

var ansiEscapePattern = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

// VisibleWidth returns the number of terminal columns s occupies,
// ignoring ANSI escape sequences (which take no screen space). It
// operates byte-for-byte rather than rune-for-rune, matching the rest
// of this package's raw-CP437-bytes-as-a-Go-string convention (see
// LoadScreen) where one byte is one on-screen glyph.
func VisibleWidth(s string) int {
	return len(ansiEscapePattern.ReplaceAllString(s, ""))
}

// fillToken is one {FILL:x} occurrence found in a line: its byte
// range (for slicing the line around it) and its fill character.
type fillToken struct {
	start, end int
	char       byte
}

const (
	fillPrefix = "{FILL:"
	fillSuffix = "}"
)

// findFillTokens scans line for {FILL:x} occurrences by hand rather
// than with regexp: x can be any single byte, including a raw CP437
// line-drawing byte like 0xCD, and Go's regexp package decodes string
// input as UTF-8 runes even for a byte-range class like [\x00-\xff]
// -- an invalid-UTF-8 byte such as 0xCD decodes to utf8.RuneError
// first and never matches. Since a screen's raw file bytes are
// intentionally not valid UTF-8 (see LoadScreen), regexp cannot do
// this capture reliably, so plain byte-index scanning is used
// instead.
func findFillTokens(line string) []fillToken {
	var tokens []fillToken
	i := 0
	for {
		idx := strings.Index(line[i:], fillPrefix)
		if idx < 0 {
			return tokens
		}
		start := i + idx
		charPos := start + len(fillPrefix)
		if charPos >= len(line) || charPos+1 >= len(line) || line[charPos+1:charPos+2] != fillSuffix {
			// Malformed (missing char or closing brace); skip past
			// this prefix and keep scanning the rest of the line.
			i = start + len(fillPrefix)
			continue
		}
		tokens = append(tokens, fillToken{start: start, end: charPos + 2, char: line[charPos]})
		i = charPos + 2
	}
}

// Layout expands {FILL:x} tokens per line against a target width,
// after ordinary {PLACEHOLDER} substitution via Render (Render leaves
// {FILL:x} untouched since ':' isn't a valid placeholder-name
// character, so the two passes don't interfere). It must run after
// Render because it measures each line's final, resolved width.
//
// A line's remaining width (target minus the visible width of
// everything that isn't a fill token) is split evenly across however
// many {FILL:x} tokens appear on that line, with any leftover column
// going to the last one. One token pads a line out to width -- e.g.
// "AreaName{FILL:.}42 msgs" for a dot-leader listing regardless of
// AreaName's length. Two identical tokens around some text center it
// -- e.g. "{FILL: }{BBSNAME}{FILL: }" keeps a box's content centered
// no matter how long the BBS's name is.
func Layout(s string, width int) string {
	lines := strings.Split(s, "\r\n")
	for i, line := range lines {
		lines[i] = layoutLine(line, width)
	}
	return strings.Join(lines, "\r\n")
}

func layoutLine(line string, width int) string {
	tokens := findFillTokens(line)
	if len(tokens) == 0 {
		return line
	}

	var nonFill strings.Builder
	last := 0
	for _, tok := range tokens {
		nonFill.WriteString(line[last:tok.start])
		last = tok.end
	}
	nonFill.WriteString(line[last:])

	remaining := width - VisibleWidth(nonFill.String())
	if remaining < 0 {
		remaining = 0
	}
	share := remaining / len(tokens)
	extra := remaining % len(tokens)

	var b strings.Builder
	last = 0
	for i, tok := range tokens {
		b.WriteString(line[last:tok.start])
		n := share
		if i == len(tokens)-1 {
			n += extra
		}
		b.WriteString(strings.Repeat(string([]byte{tok.char}), n))
		last = tok.end
	}
	b.WriteString(line[last:])
	return b.String()
}

// WrapText word-wraps s to width columns for plain-text display (a
// message body, not a template), preserving existing line breaks and
// hard-breaking any single word that alone exceeds width -- e.g. a
// long URL -- so it still fits within width instead of relying on the
// terminal's own line wrap, which the project can't assume behaves
// consistently across clients.
func WrapText(s string, width int) []string {
	if width <= 0 {
		width = 1
	}
	var out []string
	for _, paragraph := range strings.Split(s, "\n") {
		paragraph = strings.TrimRight(paragraph, "\r")
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		var line strings.Builder
		for _, word := range words {
			for len(word) > width {
				if line.Len() > 0 {
					out = append(out, line.String())
					line.Reset()
				}
				out = append(out, word[:width])
				word = word[width:]
			}
			switch {
			case line.Len() == 0:
				line.WriteString(word)
			case line.Len()+1+len(word) > width:
				out = append(out, line.String())
				line.Reset()
				line.WriteString(word)
			default:
				line.WriteString(" ")
				line.WriteString(word)
			}
		}
		out = append(out, line.String())
	}
	return out
}

// Center returns s padded with leading spaces so it appears centered
// within width columns. For use directly from Go code that builds
// dynamic content (e.g. a list header) without going through a
// screen template's {FILL:x} tokens.
func Center(s string, width int) string {
	pad := (width - VisibleWidth(s)) / 2
	if pad <= 0 {
		return s
	}
	return strings.Repeat(" ", pad) + s
}
