package bbs

import (
	"bytes"
	"fmt"
	"strings"

	"git.maik.ch/nullmodem/kit/ansi"
)

// The full-screen message editor, as callers know it from Mystic and
// Synchronet: the whole message on screen, the cursor moved freely
// with the arrow keys, Home/End, PgUp/PgDn; typing inserts, words wrap
// to the next line at the screen edge. Control keys do the rest (Esc
// can't: a bare Esc is indistinguishable from the start of an arrow
// key until the next key arrives).
//
// Text is CP437, one byte per column, like everything a Telnet/SSH
// caller types. A reply starts with the quoted original (see
// quoteForReply) with the cursor below it.

const fseHint = "^Z Save  ^X Abort  ^Y Del line  ^W Del word  ^L Redraw"

// editBuffer is the text being edited and the cursor in it.
type editBuffer struct {
	lines    [][]byte
	row, col int
	width    int // longest line; typing past it wraps the word
}

func newEditBuffer(initial []string, width int) *editBuffer {
	b := &editBuffer{width: width}
	for _, l := range initial {
		// A quoted line longer than the screen is wrapped like typed text.
		for len(l) > width {
			cut := wrapPoint([]byte(l), width)
			b.lines = append(b.lines, []byte(strings.TrimRight(l[:cut], " ")))
			l = strings.TrimLeft(l[cut:], " ")
		}
		b.lines = append(b.lines, []byte(l))
	}
	if len(b.lines) == 0 {
		b.lines = [][]byte{{}}
	}
	// Writing starts below what's there (a reply's quote).
	b.row = len(b.lines) - 1
	b.col = len(b.lines[b.row])
	return b
}

// wrapPoint is where line, longer than width, breaks: after its last
// space within width, or at width for one long word.
func wrapPoint(line []byte, width int) int {
	if i := bytes.LastIndexByte(line[:width+1], ' '); i > 0 {
		return i + 1
	}
	return width
}

// text is the message: its lines, without trailing empty ones.
func (b *editBuffer) text() []string {
	end := len(b.lines)
	for end > 0 && len(bytes.TrimSpace(b.lines[end-1])) == 0 {
		end--
	}
	out := make([]string, end)
	for i := range out {
		out[i] = strings.TrimRight(string(b.lines[i]), " ")
	}
	return out
}

func (b *editBuffer) cur() []byte { return b.lines[b.row] }

func (b *editBuffer) clampCol() {
	if b.col > len(b.cur()) {
		b.col = len(b.cur())
	}
}

// insert types c at the cursor; it reports whether lines were added
// (a word wrapped).
func (b *editBuffer) insert(c byte) (structural bool) {
	line := b.cur()
	line = append(line[:b.col], append([]byte{c}, line[b.col:]...)...)
	b.lines[b.row] = line
	b.col++
	if len(line) <= b.width {
		return false
	}
	cut := wrapPoint(line, b.width)
	head := bytes.TrimRight(line[:cut], " ")
	tail := append([]byte{}, bytes.TrimLeft(line[cut:], " ")...)
	dropped := len(line) - len(head) - len(tail)
	b.lines[b.row] = head
	b.insertLine(b.row+1, tail)
	if b.col > len(head) {
		b.col -= len(head) + dropped
		if b.col < 0 {
			b.col = 0
		}
		b.row++
	}
	return true
}

func (b *editBuffer) insertLine(at int, line []byte) {
	b.lines = append(b.lines, nil)
	copy(b.lines[at+1:], b.lines[at:])
	b.lines[at] = line
}

func (b *editBuffer) newline() {
	line := b.cur()
	tail := append([]byte{}, line[b.col:]...)
	b.lines[b.row] = line[:b.col]
	b.insertLine(b.row+1, tail)
	b.row++
	b.col = 0
}

// backspace deletes before the cursor, joining with the line above at
// its start; structural when lines were joined.
func (b *editBuffer) backspace() (structural bool) {
	if b.col > 0 {
		line := b.cur()
		b.lines[b.row] = append(line[:b.col-1], line[b.col:]...)
		b.col--
		return false
	}
	if b.row == 0 {
		return false
	}
	prev := b.lines[b.row-1]
	b.col = len(prev)
	b.lines[b.row-1] = append(prev, b.cur()...)
	b.lines = append(b.lines[:b.row], b.lines[b.row+1:]...)
	b.row--
	return true
}

// del deletes at the cursor, joining the next line at the end.
func (b *editBuffer) del() (structural bool) {
	line := b.cur()
	if b.col < len(line) {
		b.lines[b.row] = append(line[:b.col], line[b.col+1:]...)
		return false
	}
	if b.row == len(b.lines)-1 {
		return false
	}
	b.lines[b.row] = append(line, b.lines[b.row+1]...)
	b.lines = append(b.lines[:b.row+1], b.lines[b.row+2:]...)
	return true
}

func (b *editBuffer) deleteLine() {
	if len(b.lines) == 1 {
		b.lines[0] = nil
	} else {
		b.lines = append(b.lines[:b.row], b.lines[b.row+1:]...)
		if b.row >= len(b.lines) {
			b.row = len(b.lines) - 1
		}
	}
	b.clampCol()
}

// deleteWord deletes the word before the cursor (and the spaces after
// it), like a shell's Ctrl-W.
func (b *editBuffer) deleteWord() {
	line := b.cur()
	i := b.col
	for i > 0 && line[i-1] == ' ' {
		i--
	}
	for i > 0 && line[i-1] != ' ' {
		i--
	}
	b.lines[b.row] = append(line[:i], line[b.col:]...)
	b.col = i
}

// runFullScreenEditor edits a message body full screen; header is
// shown above it (To, Subject, area). saved is false when the caller
// aborted.
func (s *Server) runFullScreenEditor(term *Terminal, header []string, initial []string) (lines []string, saved bool, err error) {
	width := term.Width()
	top0 := len(header) + 2 // header, separator; text from the next row
	rows := term.Height() - top0 - 1
	if rows < 3 {
		rows = 3
	}
	b := newEditBuffer(initial, width)
	top := 0
	status := ""

	draw := func(i int) string { // screen line for buffer line i
		var l []byte
		if i < len(b.lines) {
			l = b.lines[i]
		}
		return fmt.Sprintf("\x1b[%d;1H%s%s\x1b[K", top0+1+i-top, ansi.Reset, l)
	}
	drawStatus := func() string {
		right := fmt.Sprintf("L%d C%d", b.row+1, b.col+1)
		text := fseHint
		if status != "" {
			text = status
		}
		pad := width - len(text) - len(right)
		if pad < 1 {
			pad = 1
		}
		return fmt.Sprintf("\x1b[%d;1H%s%s%s%s%s\x1b[K", top0+rows+1, ansi.Reset, ansi.FG(ansi.Black, false)+"\x1b[46m",
			text+strings.Repeat(" ", pad)+right+" ", ansi.Reset, "")
	}
	cursor := func() string { return fmt.Sprintf("\x1b[%d;%dH", top0+1+b.row-top, b.col+1) }
	full := func() error {
		var o strings.Builder
		o.WriteString(ansi.ClearScreen() + ansi.Reset)
		for i, h := range header {
			fmt.Fprintf(&o, "\x1b[%d;1H%s", i+1, h)
		}
		fmt.Fprintf(&o, "\x1b[%d;1H%s%s%s", len(header)+1, ansi.FG(ansi.Blue, false), strings.Repeat("\xc4", width), ansi.Reset)
		for i := top; i < top+rows; i++ {
			o.WriteString(draw(i))
		}
		o.WriteString(drawStatus() + cursor())
		return term.Print(o.String())
	}
	// scroll keeps the cursor's line on screen; true if it moved the view.
	scroll := func() bool {
		switch {
		case b.row < top:
			top = b.row
		case b.row >= top+rows:
			top = b.row - rows + 1
		default:
			return false
		}
		return true
	}
	scroll()
	if err := full(); err != nil {
		return nil, false, err
	}

	for {
		key, err := term.ReadKey()
		if err != nil {
			return nil, false, err
		}
		oldRow, oldLen := b.row, len(b.lines)
		structural, redrawAll := false, false
		status = ""
		switch key.Type {
		case KeyChar:
			structural = b.insert(byte(key.Rune))
		case KeyEnter:
			b.newline()
			structural = true
		case KeyBackspace:
			structural = b.backspace()
		case KeyDelete:
			structural = b.del()
		case KeyLeft:
			if b.col > 0 {
				b.col--
			} else if b.row > 0 {
				b.row--
				b.col = len(b.cur())
			}
		case KeyRight:
			if b.col < len(b.cur()) {
				b.col++
			} else if b.row < len(b.lines)-1 {
				b.row++
				b.col = 0
			}
		case KeyUp:
			if b.row > 0 {
				b.row--
				b.clampCol()
			}
		case KeyDown:
			if b.row < len(b.lines)-1 {
				b.row++
				b.clampCol()
			}
		case KeyHome:
			b.col = 0
		case KeyEnd:
			b.col = len(b.cur())
		case KeyPgUp:
			b.row = max(0, b.row-rows)
			b.clampCol()
		case KeyPgDn:
			b.row = min(len(b.lines)-1, b.row+rows)
			b.clampCol()
		case KeyCtrl:
			switch key.Rune {
			case 'z': // save
				text := b.text()
				if len(text) == 0 {
					status = "The message is empty -- nothing to save."
					break
				}
				term.Print(ansi.ClearScreen() + ansi.Reset)
				return text, true, nil
			case 'x': // abort
				status = "Abort this message? (y/N)"
				if err := term.Print(drawStatus() + cursor()); err != nil {
					return nil, false, err
				}
				k, err := term.ReadKey()
				if err != nil {
					return nil, false, err
				}
				if k.Type == KeyChar && (k.Rune == 'y' || k.Rune == 'Y') {
					term.Print(ansi.ClearScreen() + ansi.Reset)
					return nil, false, nil
				}
				status = ""
			case 'y':
				b.deleteLine()
				structural = true
			case 'w':
				b.deleteWord()
			case 'l':
				redrawAll = true
			case 'i': // tab: to the next multiple of 4
				for {
					structural = b.insert(' ') || structural
					if b.col%4 == 0 {
						break
					}
				}
			}
		}

		var o strings.Builder
		switch {
		case redrawAll:
			if err := full(); err != nil {
				return nil, false, err
			}
			continue
		case scroll():
			for i := top; i < top+rows; i++ {
				o.WriteString(draw(i))
			}
		case structural || len(b.lines) != oldLen:
			// From the first changed line down.
			for i := min(oldRow, b.row); i < top+rows; i++ {
				o.WriteString(draw(i))
			}
		default:
			o.WriteString(draw(b.row))
		}
		o.WriteString(drawStatus() + cursor())
		if err := term.Print(o.String()); err != nil {
			return nil, false, err
		}
	}
}

// editorHeader is the full-screen editor's head: where the message
// goes, to whom, and its subject.
func editorHeader(where, to, subject string) []string {
	c, r := ansi.FG(ansi.Cyan, true), ansi.Reset
	return []string{
		c + where + r,
		c + "To: " + r + to + "   " + c + "Subject: " + r + subject,
	}
}

// runEditor edits a message body: full screen when the BBS has it on
// and the caller hasn't chosen the line editor, else line by line.
func (s *Server) runEditor(term *Terminal, header []string, initial []string, lineEditor bool) ([]string, bool, error) {
	if s.FullScreenEditor && !lineEditor {
		return s.runFullScreenEditor(term, header, initial)
	}
	return s.runLineEditor(term, initial)
}
