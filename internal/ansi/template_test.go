package ansi

import "testing"

func TestRenderSubstitutesKnownPlaceholders(t *testing.T) {
	vars := Vars{"USERNAME": "Alice", "NODE": "1"}
	got := Render("Hello {USERNAME} on node {NODE}!", vars)
	want := "Hello Alice on node 1!"
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestRenderIsCaseInsensitive(t *testing.T) {
	vars := Vars{"USERNAME": "Alice"}
	got := Render("Hi {username}", vars)
	if got != "Hi Alice" {
		t.Fatalf("Render() = %q, want %q", got, "Hi Alice")
	}
}

func TestRenderLeavesUnknownPlaceholdersVerbatim(t *testing.T) {
	vars := Vars{"USERNAME": "Alice"}
	got := Render("{USERNAME} says {TYPO}", vars)
	want := "Alice says {TYPO}"
	if got != want {
		t.Fatalf("Render() = %q, want %q (unknown token should survive)", got, want)
	}
}

func TestRenderPreservesRawCP437AndControlBytes(t *testing.T) {
	// The real pipeline: LoadScreen returns raw, undecoded file bytes
	// (see screen.go for why), and Render then substitutes ASCII
	// placeholders inside that raw byte string. Every non-placeholder
	// byte — the ESC color code, the raw CP437 box-drawing byte 0xC9,
	// the CRLF line ending — must survive completely unchanged.
	raw := "\x1b[1;36m\xc9{BBSNAME}\r\n"
	got := Render(raw, Vars{"BBSNAME": "Maiks Place BBS"})
	want := "\x1b[1;36m\xc9Maiks Place BBS\r\n"
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestRenderNoPlaceholders(t *testing.T) {
	got := Render("plain text", Vars{})
	if got != "plain text" {
		t.Fatalf("Render() = %q, want unchanged input", got)
	}
}

func TestRenderWidthRightAlignsAndPads(t *testing.T) {
	got := Render("[{TOTAL:6}]", Vars{"TOTAL": "5"})
	want := "[     5]"
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestRenderWidthLeftAlignsAndPads(t *testing.T) {
	got := Render("[{AREANAME:-10}]", Vars{"AREANAME": "Chat"})
	want := "[Chat      ]"
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestRenderWidthTruncatesOverflowToPrefix(t *testing.T) {
	// Truncation always keeps the prefix regardless of alignment
	// direction -- alignment only governs which side padding goes on
	// when the value is *shorter* than the requested width.
	got := Render("[{AREANAME:-4}][{TOTAL:4}]", Vars{"AREANAME": "General Discussion", "TOTAL": "123456"})
	want := "[Gene][1234]"
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestRenderWidthUnknownPlaceholderStaysVerbatim(t *testing.T) {
	got := Render("[{NOPE:10}]", Vars{})
	want := "[{NOPE:10}]"
	if got != want {
		t.Fatalf("Render() = %q, want %q (unknown token with width should survive too)", got, want)
	}
}

func TestRenderWidthDoesNotAffectPlainPlaceholders(t *testing.T) {
	got := Render("{BBSNAME}", Vars{"BBSNAME": "Maiks Place BBS"})
	if got != "Maiks Place BBS" {
		t.Fatalf("Render() = %q, want unpadded value for a width-less placeholder", got)
	}
}
