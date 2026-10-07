package bbs

import (
	"strings"
	"testing"
)

// A second whole-screen draw sends only the lines that changed, in
// place, and ends where the whole draw would.
func TestFrameSendsOnlyChangedLines(t *testing.T) {
	conn := newFakeConn("")
	term := NewTerminal(conn)
	screen := func(sel int) string {
		var b strings.Builder
		b.WriteString(clearHome + "\x1b[1;34mHEADER ART\x1b[0m\r\n")
		for i := 0; i < 3; i++ {
			if i == sel {
				b.WriteString("\x1b[1;37;44mrow " + string(rune('A'+i)) + "\x1b[0m\r\n")
			} else {
				b.WriteString("row " + string(rune('A'+i)) + "\r\n")
			}
		}
		b.WriteString("\x1b[1;37m[Q] Back")
		return b.String()
	}
	term.Print(screen(0))
	first := conn.out.String()
	if !strings.HasPrefix(first, clearHome) {
		t.Fatalf("first draw not whole: %q", first)
	}
	conn.out.Reset()
	term.Print(screen(1))
	got := conn.out.String()
	if strings.Contains(got, clearHome) || strings.Contains(got, "HEADER ART") || strings.Contains(got, "row C") {
		t.Errorf("unchanged lines sent again: %q", got)
	}
	for _, want := range []string{"\x1b[2;1H\x1b[0mrow A", "\x1b[3;1H\x1b[0m\x1b[1;37;44mrow B", "\x1b[5;1H\x1b[0m\x1b[J\x1b[1;37m[Q] Back"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}

	// Typing echoed onto the screen: the next draw is whole again.
	conn.out.Reset()
	term.forgetScreen()
	term.Print(screen(2))
	if !strings.HasPrefix(conn.out.String(), clearHome) {
		t.Error("draw after the screen changed not whole")
	}

	// A screen moving the cursor itself is always sent whole.
	conn.out.Reset()
	term.Print(clearHome + "a\x1b[5;1Hb")
	conn.out.Reset()
	term.Print(clearHome + "a\x1b[5;1Hc")
	if !strings.HasPrefix(conn.out.String(), clearHome) {
		t.Error("cursor-moving screen diffed")
	}
}
