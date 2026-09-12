package ansi

import (
	"fmt"
	"strconv"
	"strings"
)

// Standard ANSI foreground/background colors used by SGR sequences.
const (
	Black = iota
	Red
	Green
	Yellow
	Blue
	Magenta
	Cyan
	White
)

const (
	esc   = "\x1b["
	Reset = esc + "0m"
	Bold  = esc + "1m"
	CRLF  = "\r\n"
)

// FG returns the SGR sequence to set the foreground color. bright
// selects the high-intensity variant (bold color) of the 8 base colors.
func FG(color int, bright bool) string {
	code := 30 + color
	if bright {
		return esc + "1;" + strconv.Itoa(code) + "m"
	}
	return esc + strconv.Itoa(code) + "m"
}

// BG returns the SGR sequence to set the background color.
func BG(color int) string {
	return esc + strconv.Itoa(40+color) + "m"
}

// Goto returns the sequence to move the cursor to a 1-based row/column.
func Goto(row, col int) string {
	return fmt.Sprintf("%s%d;%dH", esc, row, col)
}

// ClearScreen returns the sequence to clear the screen and home the cursor.
func ClearScreen() string {
	return esc + "2J" + esc + "H"
}

// ToCRLF rewrites bare LF line endings to CRLF, which raw telnet/SSH
// terminals require for a proper carriage return.
func ToCRLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", CRLF)
}
