package bbs

import (
	"strings"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/doors"
)

// Usurper Reborn writes its news in UTF-8 with emoji: they turn into
// CP437 (the emoji left out), and long lines wrap at word boundaries
// within the width -- an 80-column line wrapped by the terminal left a
// blank line mid-sentence.
func TestBulletinTextUTF8AndWrap(t *testing.T) {
	news := "[06:37] 🎂 Grok the Destroyer the Orc celebrates their 59th birthday!\n" +
		"[06:37] ⚰️ Vex the Merciless has passed away peacefully at the age of 65. The soul moves on...\n" +
		"[06:37] ♥ Über Café\n"
	got := strings.Split(doors.BulletinText([]byte(news), 79), "\r\n")
	if got[0] != "[06:37]  Grok the Destroyer the Orc celebrates their 59th birthday!" {
		t.Errorf("emoji line: %q", got[0])
	}
	if got[1] != "[06:37] Vex the Merciless has passed away peacefully at the age of 65. The soul" || got[2] != "moves on..." {
		t.Errorf("wrapped: %q / %q", got[1], got[2])
	}
	for _, l := range got {
		if len(l) > 79 {
			t.Errorf("%d wide: %q", len(l), l)
		}
	}
	if !strings.Contains(strings.Join(got, "\n"), "\x03 \x9aber Caf\x82") {
		t.Errorf("CP437 letters and symbols lost: %q", got)
	}
	// CP437 ANSI art is left as it is.
	art := "\x1b[1;31m\xdb\xdb\xb0\x1b[0m"
	if got := doors.BulletinText([]byte(art), 79); got != art {
		t.Errorf("art changed: %q", got)
	}
}
