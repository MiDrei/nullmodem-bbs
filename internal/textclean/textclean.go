// Package textclean keeps control characters out of one-line text --
// names, subjects, chat and one-liner lines -- that goes to other
// callers' terminals: an escape sequence in a subject from the network
// or a chat line typed in the portal would otherwise reach every
// Telnet screen that lists it (clearing it, faking text, or worse on
// clients that act on more than colours). Message bodies are left
// alone: ANSI art in them is wanted.
package textclean

import (
	"strings"
	"unicode"
)

// Line is s with its control characters (C0, DEL, C1) removed; a tab
// becomes a space.
func Line(s string) string {
	if strings.IndexFunc(s, unicode.IsControl) < 0 {
		return s
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, s)
}

// HasControl reports whether s has a control character in it.
func HasControl(s string) bool { return strings.IndexFunc(s, unicode.IsControl) >= 0 }
