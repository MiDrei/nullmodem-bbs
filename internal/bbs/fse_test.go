package bbs

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/user"
	"github.com/midrei/nullmodem-kit/ansi"
)

func typeText(b *editBuffer, s string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			b.newline()
		} else {
			b.insert(s[i])
		}
	}
}

func TestEditBufferWrapsWordsAtTheEdge(t *testing.T) {
	b := newEditBuffer(nil, 20)
	typeText(b, "the quick brown fox jumps over the lazy dog")
	got := strings.Join(b.text(), "|")
	want := "the quick brown fox|jumps over the lazy|dog"
	if got != want {
		t.Fatalf("wrapped %q\nwant    %q", got, want)
	}
	if b.row != 2 || b.col != 3 {
		t.Errorf("cursor at %d:%d, want after dog (2:3)", b.row, b.col)
	}
	// One word longer than the line: cut hard.
	b = newEditBuffer(nil, 10)
	typeText(b, "abcdefghijklmno")
	if got := strings.Join(b.text(), "|"); got != "abcdefghij|klmno" {
		t.Fatalf("long word: %q", got)
	}
}

func TestEditBufferEditing(t *testing.T) {
	b := newEditBuffer(nil, 79)
	typeText(b, "first\nsecond")
	// Backspace at the start of a line joins it to the one above.
	b.col = 0
	b.backspace()
	if got := strings.Join(b.text(), "|"); got != "firstsecond" || b.col != 5 {
		t.Fatalf("join: %q col %d", got, b.col)
	}
	b.newline() // split again at the cursor
	if got := strings.Join(b.text(), "|"); got != "first|second" {
		t.Fatalf("split: %q", got)
	}
	// Delete at the end of a line pulls the next one up.
	b.row, b.col = 0, 5
	b.del()
	if got := strings.Join(b.text(), "|"); got != "firstsecond" {
		t.Fatalf("delete-join: %q", got)
	}
	typeText(b, " word")
	b.deleteWord()
	if got := strings.Join(b.text(), "|"); got != "first second" {
		t.Fatalf("ctrl-w: %q", got)
	}
	b.deleteLine()
	if got := b.text(); len(got) != 0 {
		t.Fatalf("delete line left %q", got)
	}
}

func TestEditBufferStartsBelowTheQuote(t *testing.T) {
	b := newEditBuffer([]string{" MA> quoted", ""}, 79)
	if b.row != 1 || b.col != 0 {
		t.Fatalf("cursor %d:%d, want on the line below the quote", b.row, b.col)
	}
	typeText(b, "my answer")
	if got := strings.Join(b.text(), "|"); got != " MA> quoted|my answer" {
		t.Fatalf("%q", got)
	}
}

func TestFullScreenEditorPostsAMessage(t *testing.T) {
	s := testServer(t)
	s.FullScreenEditor = true
	u, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	general, _ := s.Messages.AreaByTag("general")
	// Subject, then: "Hello wrld", left x3, "o", End, Enter, "Bye", Ctrl-Z.
	input := "Greetings\r\n" + "Hello wrld" + "\x1b[D\x1b[D\x1b[D" + "o" + "\x1b[F" + "\r" + "Bye" + "\x1a"
	conn := newFakeConn(input)
	if err := s.postMessage(NewTerminal(conn), u, general); err != nil {
		t.Fatal(err)
	}
	msgs, _ := s.Messages.ListMessages(general.ID)
	last := msgs[len(msgs)-1]
	if last.Subject != "Greetings" || last.Body != "Hello world\nBye" {
		t.Fatalf("posted %q / %q", last.Subject, last.Body)
	}
	out := conn.out.String()
	if !strings.Contains(out, "^Z Save") || !strings.Contains(out, "General Discussion") {
		t.Errorf("editor screen lacks its head or hint line")
	}
}

func TestFullScreenEditorAbortAndEmpty(t *testing.T) {
	s := testServer(t)
	s.FullScreenEditor = true
	u, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	general, _ := s.Messages.AreaByTag("general")
	before, _ := s.Messages.ListMessages(general.ID)

	// Ctrl-Z on nothing: refused; then Ctrl-X, n (go on), Ctrl-X, y.
	conn := newFakeConn("Subj\r\n" + "\x1a" + "\x18n" + "draft" + "\x18y")
	if err := s.postMessage(NewTerminal(conn), u, general); err != nil {
		t.Fatal(err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "The message is empty") || !strings.Contains(out, "Message aborted") {
		t.Fatalf("empty/abort flow: %q", out)
	}
	after, _ := s.Messages.ListMessages(general.ID)
	if len(after) != len(before) {
		t.Fatal("an aborted message was posted")
	}
}

func TestLineEditorPreferenceKeepsTheOldEditor(t *testing.T) {
	s := testServer(t)
	s.FullScreenEditor = true
	u, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	s.Users.SetLineEditor(u.ID, true)
	u, _ = s.Users.ByID(u.ID)
	general, _ := s.Messages.AreaByTag("general")
	conn := newFakeConn("Subj\r\nline one\r\n/S\r\n")
	if err := s.postMessage(NewTerminal(conn), u, general); err != nil {
		t.Fatal(err)
	}
	msgs, _ := s.Messages.ListMessages(general.ID)
	if msgs[len(msgs)-1].Body != "line one" {
		t.Fatalf("line editor body %q", msgs[len(msgs)-1].Body)
	}
}

func TestReadKeyRecognizesEditingKeys(t *testing.T) {
	term := NewTerminal(newFakeConn("\x1b[H\x1b[F\x1b[3~\x1b[5~\x1b[6~\x1b[1~\x1b[4~\x1a\t"))
	want := []KeyType{KeyHome, KeyEnd, KeyDelete, KeyPgUp, KeyPgDn, KeyHome, KeyEnd, KeyCtrl, KeyCtrl}
	for i, w := range want {
		k, err := term.ReadKey()
		if err != nil || k.Type != w {
			t.Fatalf("key %d: %v %v, want %v", i, k, err, w)
		}
	}
}

// Nothing is written into the last row's last column: a terminal that
// wraps at once there (SyncTERM) would scroll the whole editor up on
// every key.
func TestFullScreenEditorKeepsOffTheLastColumn(t *testing.T) {
	s := testServer(t)
	s.FullScreenEditor = true
	u, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	general, _ := s.Messages.AreaByTag("general")
	conn := newFakeConn("Subj\r\n" + "Hello there" + "\x1a")
	term := NewTerminal(conn)
	if err := s.postMessage(term, u, general); err != nil {
		t.Fatal(err)
	}
	last := fmt.Sprintf("\x1b[%d;1H", term.Height())
	out := conn.out.String()
	n := 0
	for i := strings.Index(out, last); i >= 0; i = strings.Index(out, last) {
		out = out[i+len(last):]
		seg := out
		if j := regexp.MustCompile(`\x1b\[\d+;\d+H`).FindStringIndex(seg); j != nil {
			seg = seg[:j[0]]
		}
		if w := visibleWidth(strings.ReplaceAll(seg, "\x1b[K", "")); w > term.Width() {
			t.Fatalf("last row written %d columns wide (max %d): %q", w, term.Width(), seg)
		}
		n++
	}
	if n == 0 {
		t.Fatal("no status bar on the last row")
	}
}

// Writing starts on the row right under the separator.
func TestFullScreenEditorStartsUnderTheRule(t *testing.T) {
	s := testServer(t)
	s.FullScreenEditor = true
	u, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	general, _ := s.Messages.AreaByTag("general")
	conn := newFakeConn("Subj\r\n" + "Hi" + "\x1a")
	if err := s.postMessage(NewTerminal(conn), u, general); err != nil {
		t.Fatal(err)
	}
	out := conn.out.String()
	// Head on rows 1-2, the rule on 3: the text on 4, the cursor there.
	if !strings.Contains(out, "\x1b[3;1H"+ansi.FG(ansi.Blue, false)+"\xc4") || !strings.Contains(out, "\x1b[4;1H"+ansi.Reset+"Hi") || !strings.Contains(out, "\x1b[4;3H") {
		t.Errorf("text not right under the rule: %q", out)
	}
}

// packetConn hands out its input in the pieces the client sent it in,
// and says what's left of the current one (like telnet's Buffered).
type packetConn struct {
	*fakeConn
	packets [][]byte
}

func (c *packetConn) Read(p []byte) (int, error) {
	for len(c.packets) > 0 && len(c.packets[0]) == 0 {
		c.packets = c.packets[1:]
	}
	if len(c.packets) == 0 {
		return 0, io.EOF
	}
	n := copy(p, c.packets[0])
	c.packets[0] = c.packets[0][n:]
	return n, nil
}

func (c *packetConn) Buffered() int {
	if len(c.packets) == 0 {
		return 0
	}
	return len(c.packets[0])
}

func newPacketConn(packets ...string) *packetConn {
	c := &packetConn{fakeConn: newFakeConn("")}
	for _, p := range packets {
		c.packets = append(c.packets, []byte(p))
	}
	return c
}

// The Escape key alone is a key at once; a cursor key's sequence, in
// one piece, is still that key.
func TestReadKeyTellsEscapeFromCursorKeys(t *testing.T) {
	term := NewTerminal(newPacketConn("\x1b", "\x1b[A", "x"))
	for _, want := range []KeyType{KeyEscape, KeyUp, KeyChar} {
		k, err := term.ReadKey()
		if err != nil || k.Type != want {
			t.Fatalf("got %v %v, want %v", k, err, want)
		}
	}
}

// ESC, then S saves -- layout-proof, unlike Ctrl-Z on a QWERTZ keyboard.
func TestFullScreenEditorEscapeMenuSaves(t *testing.T) {
	s := testServer(t)
	s.FullScreenEditor = true
	u, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	general, _ := s.Messages.AreaByTag("general")
	conn := newPacketConn("Menu test\r\n", "Hello", "\x1b", "x", "\x1b", "s")
	if err := s.postMessage(NewTerminal(conn), u, general); err != nil {
		t.Fatal(err)
	}
	msgs, _ := s.Messages.ListMessages(general.ID)
	last := msgs[len(msgs)-1]
	if last.Subject != "Menu test" || last.Body != "Hello" {
		t.Fatalf("posted %q / %q (any other key must go on writing, not type)", last.Subject, last.Body)
	}
	if !strings.Contains(conn.out.String(), "S Save") {
		t.Error("no menu shown")
	}
}
