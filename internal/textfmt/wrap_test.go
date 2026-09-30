package textfmt

import (
	"strings"
	"testing"
)

func TestWrapLongLines(t *testing.T) {
	long := strings.Repeat("word ", 30) // 150 characters
	got := WrapLongLines("Short line.\n"+long+"\n   aligned   table\n", 79)
	for _, l := range strings.Split(got, "\n") {
		if len([]rune(l)) > 79 {
			t.Fatalf("line longer than 79: %q", l)
		}
	}
	if !strings.HasPrefix(got, "Short line.\nword word") || !strings.Contains(got, "\n   aligned   table\n") {
		t.Fatalf("short lines must stay as they were:\n%s", got)
	}
	if strings.Join(strings.Fields(got), " ") != strings.Join(strings.Fields("Short line. "+long+" aligned table"), " ") {
		t.Fatal("wrapping lost or changed words")
	}

	quoted := WrapLongLines(" SW> "+long, 79)
	for _, l := range strings.Split(quoted, "\n") {
		if !strings.HasPrefix(l, " SW> ") || len([]rune(l)) > 79 {
			t.Fatalf("wrapped quote line %q lost its prefix or is too long", l)
		}
	}

	url := "see https://example.org/" + strings.Repeat("x", 100)
	if WrapLongLines(url, 79) != "see\nhttps://example.org/"+strings.Repeat("x", 100) {
		t.Fatalf("a long URL must stay whole: %q", WrapLongLines(url, 79))
	}
	if WrapLongLines("Grüße "+strings.Repeat("ä", 73), 79) != "Grüße "+strings.Repeat("ä", 73) {
		t.Fatal("width counts characters, not bytes: 79 umlaut-heavy characters must fit")
	}
}
