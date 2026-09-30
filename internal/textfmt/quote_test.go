package textfmt

import (
	"strings"
	"testing"
)

func TestInitials(t *testing.T) {
	for name, want := range map[string]string{"SwissMaik": "Sw", "Deon George": "DG", "Mortar M.": "MM", "poindexter FORTRAN": "PF", "": "XX", "bob": "Bo"} {
		if got := Initials(name); got != want {
			t.Errorf("Initials(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestQuoteLines(t *testing.T) {
	body := "\x01MSGID: 1:2/3 4\nHi all,\n\n DG> earlier quote\nthe text.\n\n--- Mystic\n * Origin: x (1:2/3)\nSEEN-BY: 1/2"
	got := strings.Join(QuoteLines(body, "Mortar M.", "poindexter FORTRAN"), "\n")
	want := " -=> Mortar M. wrote to poindexter FORTRAN <=-\n\n MM> Hi all,\n\n DG> earlier quote\n MM> the text."
	if got != want {
		t.Fatalf("QuoteLines =\n%s\nwant\n%s", got, want)
	}
	long := QuoteLines(strings.Repeat("word ", 30), "SwissMaik", "")
	if long[0] != " -=> SwissMaik wrote to All <=-" {
		t.Fatalf("empty To: %q", long[0])
	}
	for _, l := range long[2:] {
		if !strings.HasPrefix(l, " Sw> ") || len([]rune(l)) > LineWidth {
			t.Fatalf("long quote line %q", l)
		}
	}
}
