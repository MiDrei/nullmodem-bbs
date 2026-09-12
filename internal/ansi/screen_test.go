package ansi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadScreenReturnsRawBytesUnconverted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ans")
	// A realistic .ANS snippet: ESC[1;36m color code, a raw CP437
	// box-drawing byte (0xC9), a {PLACEHOLDER}, and a CRLF line ending.
	// LoadScreen must preserve every byte exactly — decoding through
	// DecodeCP437 would remap 0x1B/0x0D/0x0A to CP437's glyphs for
	// those control codes, corrupting the escape sequence and line
	// ending (see the doc comment on LoadScreen for why).
	content := []byte("\x1b[1;36m\xc9{BBSNAME}\r\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write test screen: %v", err)
	}

	got, err := LoadScreen(path)
	if err != nil {
		t.Fatalf("LoadScreen: %v", err)
	}
	if got != string(content) {
		t.Fatalf("LoadScreen() = %q, want raw bytes %q unchanged", got, string(content))
	}
}

func TestLoadScreenMissingFile(t *testing.T) {
	if _, err := LoadScreen(filepath.Join(t.TempDir(), "does-not-exist.ans")); err == nil {
		t.Fatal("expected error for missing screen file")
	}
}
