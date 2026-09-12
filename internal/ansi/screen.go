package ansi

import (
	"fmt"
	"os"
)

// LoadScreen reads a screen file — classic CP437 .ANS art or plain
// ASCII text, optionally containing {PLACEHOLDER} tokens for Render —
// and returns its raw bytes as a Go string, unconverted.
//
// This deliberately does NOT run the bytes through DecodeCP437. A
// telnet/SSH client (SyncTERM, PuTTY, ...) understands raw CP437
// bytes natively and is the one actually rendering the art, so the
// server must ship the original bytes unmodified. DecodeCP437 remaps
// the whole 0x00-0x1F range to CP437's decorative glyphs (its correct
// behavior for genuine CP437 text), which would mangle the real
// control bytes an .ANS file also relies on — ESC (0x1B) for color
// codes, CR/LF for line endings — turning them into literal arrow and
// note glyphs instead of executing them. DecodeCP437 is for a
// different job: converting CP437 art to Unicode for a UTF-8-only
// consumer, such as a future web-based screen viewer.
func LoadScreen(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("ansi: read screen %s: %w", path, err)
	}
	return string(data), nil
}
