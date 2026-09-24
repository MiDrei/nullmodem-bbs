package binkp

import "testing"

func TestQuoteFileNameEscapesSpacesControlCharsAndBackslash(t *testing.T) {
	got := quoteFileName("my file\t.zip\\x")
	want := `my\x20file\x09.zip\x5cx`
	if got != want {
		t.Fatalf("quoteFileName = %q, want %q", got, want)
	}
}

func TestQuoteFileNameLeavesOrdinaryNamesUnchanged(t *testing.T) {
	for _, name := range []string{"00000001.pkt", "readme.txt", "file-name_v2.tic"} {
		if got := quoteFileName(name); got != name {
			t.Fatalf("quoteFileName(%q) = %q, want unchanged", name, got)
		}
	}
}

func TestDequoteFileNameReversesQuoteFileName(t *testing.T) {
	for _, name := range []string{
		"plain.pkt",
		"my file with spaces.zip",
		"tabs\tand\nnewlines.txt",
		`back\slash.zip`,
		"unicode-ish \x01\x02.bin",
	} {
		quoted := quoteFileName(name)
		got := dequoteFileName(quoted)
		if got != name {
			t.Fatalf("dequoteFileName(quoteFileName(%q)=%q) = %q, want %q", name, quoted, got, name)
		}
	}
}

// TestDequoteFileNameAcceptsTheShorterBackslashForm locks in
// dequoteFileName's tolerance for binkd's own strdequote (tools.c),
// which accepts both "\xHH" and the shorter "\HH" (no "x") escape
// form -- some other implementation could plausibly send the shorter
// form even though our own quoteFileName always emits the longer one.
func TestDequoteFileNameAcceptsTheShorterBackslashForm(t *testing.T) {
	got := dequoteFileName(`my\20file.zip`)
	want := "my file.zip"
	if got != want {
		t.Fatalf("dequoteFileName = %q, want %q", got, want)
	}
}

func TestDequoteFileNameLeavesAStrayBackslashAlone(t *testing.T) {
	got := dequoteFileName(`odd\name`)
	want := `odd\name`
	if got != want {
		t.Fatalf("dequoteFileName = %q, want %q (no valid hex escape follows the backslash)", got, want)
	}
}
