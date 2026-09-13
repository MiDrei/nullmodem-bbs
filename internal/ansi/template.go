package ansi

import (
	"regexp"
	"strconv"
	"strings"
)

// Vars maps placeholder names (case-insensitive) to the text they
// expand to when rendering a screen or menu string.
type Vars map[string]string

// placeholderPattern matches {NAME} and {NAME:WIDTH}. WIDTH is signed:
// negative left-aligns (pad/truncate on the right, like a table's text
// column), positive right-aligns (pad/truncate on the left, like a
// numeric column) -- see Render's doc comment.
var placeholderPattern = regexp.MustCompile(`\{([A-Za-z0-9_]+)(?::(-?\d+))?\}`)

// Render replaces every {PLACEHOLDER} or {PLACEHOLDER:WIDTH} token in
// s with its value from vars. An unknown placeholder is left in the
// output verbatim rather than silently removed, so a typo'd or
// not-yet-supported token stays visible on the rendered screen for
// the sysop to notice and fix.
//
// The optional :WIDTH forces the substituted value to exactly that
// many columns (padding with spaces, or truncating if it's longer) --
// unlike {FILL:x}, which only ever distributes whatever width is left
// on a line, this is what a hand-authored template needs for a
// data table's columns to actually line up across rows whose values
// differ in length (e.g. a per-row area name next to fixed-width
// message counts).
func Render(s string, vars Vars) string {
	return placeholderPattern.ReplaceAllStringFunc(s, func(tok string) string {
		m := placeholderPattern.FindStringSubmatch(tok)
		name := strings.ToUpper(m[1])
		val, ok := vars[name]
		if !ok {
			return tok
		}
		if m[2] != "" {
			width, err := strconv.Atoi(m[2])
			if err == nil {
				val = padToWidth(val, width)
			}
		}
		return val
	})
}

// padToWidth returns s truncated or space-padded to exactly |width|
// columns: width < 0 left-aligns (pad/truncate on the right), width
// >= 0 right-aligns (pad/truncate on the left).
func padToWidth(s string, width int) string {
	left := width < 0
	if left {
		width = -width
	}
	if len(s) > width {
		return s[:width]
	}
	if len(s) < width {
		pad := strings.Repeat(" ", width-len(s))
		if left {
			return s + pad
		}
		return pad + s
	}
	return s
}
