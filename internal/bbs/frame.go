package bbs

import (
	"regexp"
	"strconv"
	"strings"
)

// Screen updates without the flicker: a screen drawn whole (cleared,
// then written top to bottom) used to be sent again in full on every
// key -- a lightbar step cleared the screen, blanking it for a moment,
// and repainted the art and every row. The Terminal now remembers the
// last such screen line by line and, for the next one, sends only the
// lines that changed, each in place.

const clearHome = "\x1b[2J\x1b[H"

// nonSGR finds a control sequence other than a colour change: a screen
// that moves the cursor itself (the chat, the editor, ANSI art) is
// sent whole.
var nonSGR = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-ln-z@\x60]|\x1b[^\[]`)

var sgrSeq = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// screenLine is one line of a drawn screen: the colours in force at
// its start, and its text.
type screenLine struct {
	state string
	text  string
}

// sgrState follows SGR sequences: the parameters in force since the
// last reset, as one sequence that restores them.
type sgrState []string

func (st *sgrState) apply(text string) {
	for _, m := range sgrSeq.FindAllStringSubmatch(text, -1) {
		params := strings.Split(m[1], ";")
		for _, p := range params {
			if p == "" || p == "0" {
				*st = (*st)[:0]
				continue
			}
			*st = append(*st, p)
		}
	}
}

func (st sgrState) String() string {
	if len(st) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(st, ";") + "m"
}

// visibleWidth is the columns text takes, its escape sequences left out
// (CP437: a byte a column).
func visibleWidth(text string) int {
	return len(sgrSeq.ReplaceAllString(text, ""))
}

// frame turns out, about to be written, into the shortest update of the
// screen: a whole-screen draw becomes just its changed lines when the
// last one is known; anything else is written as it is and forgets the
// screen (the caller drew over it).
func (t *Terminal) frame(out string) string {
	body, whole := strings.CutPrefix(out, clearHome)
	w, h := t.Width(), t.Height()
	if !whole || nonSGR.MatchString(body) || strings.Contains(strings.ReplaceAll(body, "\r\n", ""), "\r") {
		t.screen = nil
		return out
	}
	texts := strings.Split(body, "\r\n")
	if len(texts) > h {
		// It scrolls: rows no longer match lines.
		t.screen = nil
		return out
	}
	lines := make([]screenLine, len(texts))
	var st sgrState
	for i, text := range texts {
		if visibleWidth(text) > w+1 {
			t.screen = nil
			return out
		}
		lines[i] = screenLine{state: st.String(), text: text}
		st.apply(text)
	}
	prev, prevW, prevH := t.screen, t.screenW, t.screenH
	t.screen, t.screenW, t.screenH = lines, w, h
	if prev == nil || prevW != w || prevH != h {
		return out
	}

	var b strings.Builder
	last := len(lines) - 1
	for i, l := range lines[:last] {
		if i < len(prev) && prev[i] == l {
			continue
		}
		// The line in place, then the rest of the row cleared -- in the
		// default colours, as the cleared screen had it.
		b.WriteString("\x1b[" + strconv.Itoa(i+1) + ";1H\x1b[0m" + l.state + l.text + "\x1b[0m\x1b[K")
	}
	// The last line always, everything below it cleared first: the
	// cursor ends where the whole draw left it, in its colours (a
	// prompt's input colour, say).
	l := lines[last]
	b.WriteString("\x1b[" + strconv.Itoa(last+1) + ";1H\x1b[0m\x1b[J" + l.state + l.text)
	return b.String()
}

// forgetScreen: something other than a whole-screen draw changed the
// screen (typing echoed, a door, a transfer), so the next draw is sent
// whole.
func (t *Terminal) forgetScreen() {
	t.screenMu.Lock()
	t.screen = nil
	t.screenMu.Unlock()
}
