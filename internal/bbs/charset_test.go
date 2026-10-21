package bbs

import (
	"bytes"
	"net"
	"testing"
	"time"
)

// charsetPair is a Terminal on one end of a pipe and the "terminal" on
// the other.
func charsetPair(t *testing.T) (*Terminal, net.Conn) {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() { server.Close(); client.Close() })
	return NewTerminal(pipeConn{server}), client
}

func TestUTF8OutAndIn(t *testing.T) {
	term, client := charsetPair(t)
	term.SetUTF8(true)
	go term.Print("\xc4\xc4 Gr\x81ezi \x1b[1;37m\xdb")
	buf := make([]byte, 64)
	n, _ := client.Read(buf)
	if got, want := string(buf[:n]), "── Grüezi \x1b[1;37m█"; got != want {
		t.Errorf("sent %q, want %q", got, want)
	}
	// Typed: an umlaut in pieces, then Enter.
	go func() {
		client.Write([]byte{0xc3})
		time.Sleep(10 * time.Millisecond)
		client.Write([]byte{0xa4, 'x', '\r'})
	}()
	go func() { // the echo
		b := make([]byte, 64)
		for {
			if _, err := client.Read(b); err != nil {
				return
			}
		}
	}()
	line, err := term.ReadLine(false)
	if err != nil || line != "\x84x" {
		t.Errorf("read %q, %v; want ä as CP437", line, err)
	}
}

func TestCP437IsUntouched(t *testing.T) {
	term, client := charsetPair(t)
	go term.Print("\xc4\x81")
	buf := make([]byte, 8)
	n, _ := client.Read(buf)
	if !bytes.Equal(buf[:n], []byte{0xc4, 0x81}) {
		t.Errorf("sent % x", buf[:n])
	}
}

func TestDetectUTF8(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer string
		utf8   bool
		ok     bool
	}{
		{"UTF-8 terminal", "\x1b[1;2R", true, true},
		{"CP437 terminal", "\x1b[5;3R", false, true},
		{"typed ahead", "a\x1b[1;2Rb", true, true},
		{"no answer", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			term, client := charsetPair(t)
			go func() {
				b := make([]byte, 64)
				n, _ := client.Read(b)
				if string(b[:n]) != cprProbe {
					t.Errorf("probe %q", b[:n])
				}
				if tc.answer != "" {
					client.Write([]byte(tc.answer))
				}
				client.Read(b) // the wipe
			}()
			utf8, ok := term.detectUTF8(200 * time.Millisecond)
			if utf8 != tc.utf8 || ok != tc.ok {
				t.Fatalf("utf8 %v ok %v, want %v %v", utf8, ok, tc.utf8, tc.ok)
			}
			if tc.name == "typed ahead" {
				term.SetUTF8(utf8)
				for _, want := range []byte("ab") {
					k, err := term.ReadKey()
					if err != nil || k.Rune != rune(want) {
						t.Errorf("key %+v %v, want %c", k, err, want)
					}
				}
			}
			if tc.name == "no answer" {
				// Typed later: still there, not lost to the probe.
				go client.Write([]byte("z"))
				k, err := term.ReadKey()
				if err != nil || k.Rune != 'z' {
					t.Errorf("key after the probe %+v %v", k, err)
				}
			}
		})
	}
}
