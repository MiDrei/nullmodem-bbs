package textclean

import "testing"

func TestLine(t *testing.T) {
	for in, want := range map[string]string{
		"Hello":                  "Hello",
		"\x1b[2JFake sysop msg":  "[2JFake sysop msg",
		"a\tb\r\nc\x7f\u009b31m": "a bc31m",
		"Grüsse aus Zürich":      "Grüsse aus Zürich",
	} {
		if got := Line(in); got != want {
			t.Errorf("Line(%q) = %q, want %q", in, got, want)
		}
	}
	if !HasControl("x\x1b") || HasControl("ok") {
		t.Error("HasControl")
	}
}
