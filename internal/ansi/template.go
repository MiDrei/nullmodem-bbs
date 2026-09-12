package ansi

import (
	"regexp"
	"strings"
)

// Vars maps placeholder names (case-insensitive) to the text they
// expand to when rendering a screen or menu string.
type Vars map[string]string

var placeholderPattern = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)

// Render replaces every {PLACEHOLDER} token in s with its value from
// vars. An unknown placeholder is left in the output verbatim rather
// than silently removed, so a typo'd or not-yet-supported token stays
// visible on the rendered screen for the sysop to notice and fix.
func Render(s string, vars Vars) string {
	return placeholderPattern.ReplaceAllStringFunc(s, func(tok string) string {
		name := strings.ToUpper(tok[1 : len(tok)-1])
		if val, ok := vars[name]; ok {
			return val
		}
		return tok
	})
}
