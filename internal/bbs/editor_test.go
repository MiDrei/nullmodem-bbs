package bbs

import (
	"strings"
	"testing"
)

func TestQuoteForReplyKeepsCP437(t *testing.T) {
	// "Grüsse" in CP437 (ü = 0x81), in a line long enough to be wrapped.
	body := "Gr\x81sse " + strings.Repeat("aus Neunkirch ", 8)
	lines := quoteForReply(body, "M\x81ller Hans", "All")
	if lines[0] != " -=> M\x81ller Hans wrote to All <=-" {
		t.Fatalf("head = %q", lines[0])
	}
	if !strings.HasPrefix(lines[2], " MH> Gr\x81sse ") || len(lines) < 5 || !strings.HasPrefix(lines[3], " MH> ") {
		t.Fatalf("quoted = %q", lines)
	}
	for _, l := range lines {
		if strings.Contains(l, "�") || len(l) > 79 {
			t.Fatalf("line %q broken or too long", l)
		}
	}
}
