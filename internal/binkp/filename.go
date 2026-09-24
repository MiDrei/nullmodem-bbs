package binkp

import "strings"

// quoteFileName escapes control characters, spaces, and a literal
// backslash in name using binkd's own "\xHH" convention (confirmed
// against binkd's source, tools.c's strquote(s, SQ_CNTRL|SQ_SPACE),
// applied to every outbound filename unconditionally in prothlp.c's
// netname_ -- not just ones a sender happens to know need it).
// Without this, a filename containing a space (e.g. a file forwarded
// via TIC under its original, user-chosen name) would desync M_FILE's
// whitespace-delimited argument parsing on both ends.
func quoteFileName(name string) string {
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c < 0x20 || c == 0x7f || c == ' ' || c == '\\' {
			b.WriteByte('\\')
			b.WriteByte('x')
			b.WriteByte(hexDigit(c >> 4))
			b.WriteByte(hexDigit(c & 0xf))
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// dequoteFileName reverses quoteFileName. It also accepts the shorter
// "\HH" form (no "x") binkd's own strdequote (tools.c) accepts
// alongside "\xHH", for interop with any other implementation using
// that shorter form. Bytes that don't match either escape form are
// copied through unchanged, so a name with a stray backslash not
// followed by two hex digits round-trips as-is rather than erroring.
func dequoteFileName(name string) string {
	var b strings.Builder
	for i := 0; i < len(name); {
		if name[i] == '\\' && i+3 < len(name) && name[i+1] == 'x' && isHexDigit(name[i+2]) && isHexDigit(name[i+3]) {
			b.WriteByte(hexDigitValue(name[i+2])<<4 | hexDigitValue(name[i+3]))
			i += 4
			continue
		}
		if name[i] == '\\' && i+2 < len(name) && isHexDigit(name[i+1]) && isHexDigit(name[i+2]) {
			b.WriteByte(hexDigitValue(name[i+1])<<4 | hexDigitValue(name[i+2]))
			i += 3
			continue
		}
		b.WriteByte(name[i])
		i++
	}
	return b.String()
}

func hexDigit(v byte) byte {
	if v < 10 {
		return '0' + v
	}
	return 'a' + v - 10
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func hexDigitValue(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default: // 'A'-'F'
		return c - 'A' + 10
	}
}
