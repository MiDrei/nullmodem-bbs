package ansi

import (
	"fmt"
	"html"
	"strconv"
	"strings"
)

// dosPalette are the 16 classic DOS/VGA text-mode colors, in the
// order ANSI SGR foreground codes 30-37 (and their bold/bright
// variants, 38-45) address them -- the same palette real BBS terminal
// clients like SyncTERM render CP437 art with.
var dosPalette = [16]string{
	"#000000", "#AA0000", "#00AA00", "#AA5500",
	"#0000AA", "#AA00AA", "#00AAAA", "#AAAAAA",
	"#555555", "#FF5555", "#55FF55", "#FFFF55",
	"#5555FF", "#FF55FF", "#55FFFF", "#FFFFFF",
}

// htmlState is the SGR (color/attribute) state while converting a raw
// ANSI/CP437 screen to HTML. Foreground brightness is tracked
// separately from the base color (rather than folded into a single
// 0-15 index immediately) because real BBS art sets bold and color in
// either order within one escape (e.g. both "\x1b[1;36m" and
// "\x1b[36;1m" mean bright cyan).
type htmlState struct {
	baseFG    int
	bg        int
	bold      bool
	underline bool
	blink     bool
	reverse   bool
}

func defaultHTMLState() htmlState {
	return htmlState{baseFG: 7, bg: 0}
}

func (s htmlState) effectiveFG() int {
	if s.bold {
		return s.baseFG + 8
	}
	return s.baseFG
}

// ToHTML converts a raw CP437/ANSI screen (as produced by LoadScreen,
// typically after Render/Layout) into an HTML fragment for the web
// admin's screen preview: colored <span> runs matching the classic
// 16-color DOS palette, CP437 bytes mapped to their Unicode glyphs,
// and CRLF turned into <br>. The caller is expected to render the
// result inside a monospace, white-space:pre container so runs of
// spaces stay meaningful.
//
// Only the SGR (color/attribute) escapes that BBS art actually relies
// on are interpreted; other CSI sequences (cursor positioning, clear
// screen, ...) have no meaning for a static, linear HTML preview and
// are silently dropped.
func ToHTML(raw string) string {
	var b strings.Builder
	state := defaultHTMLState()
	spanOpen := false

	openSpan := func() {
		fg, bg := state.effectiveFG(), state.bg
		if state.reverse {
			fg, bg = bg, fg
		}
		styles := []string{
			"color:" + dosPalette[fg],
			"background-color:" + dosPalette[bg],
		}
		if state.underline {
			styles = append(styles, "text-decoration:underline")
		}
		if state.blink {
			styles = append(styles, "animation:ansi-blink 1s steps(1) infinite")
		}
		fmt.Fprintf(&b, `<span style="%s">`, strings.Join(styles, ";"))
		spanOpen = true
	}
	closeSpan := func() {
		if spanOpen {
			b.WriteString("</span>")
			spanOpen = false
		}
	}

	i, n := 0, len(raw)
	for i < n {
		c := raw[i]
		switch {
		case c == 0x1b && i+1 < n && raw[i+1] == '[':
			j := i + 2
			for j < n && !isCSIFinal(raw[j]) {
				j++
			}
			if j >= n {
				i = n
				continue
			}
			if raw[j] == 'm' {
				closeSpan()
				applySGR(&state, raw[i+2:j])
			}
			i = j + 1
		case c == '\r' && i+1 < n && raw[i+1] == '\n':
			closeSpan()
			b.WriteString("<br>")
			i += 2
		case c == '\n':
			closeSpan()
			b.WriteString("<br>")
			i++
		case c == '\r':
			i++
		default:
			if !spanOpen {
				openSpan()
			}
			b.WriteString(html.EscapeString(string(cp437ToRune[c])))
			i++
		}
	}
	closeSpan()
	return b.String()
}

func isCSIFinal(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func applySGR(state *htmlState, params string) {
	if params == "" {
		*state = defaultHTMLState()
		return
	}
	for _, p := range strings.Split(params, ";") {
		code, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		switch {
		case code == 0:
			*state = defaultHTMLState()
		case code == 1:
			state.bold = true
		case code == 2:
			state.bold = false
		case code == 4:
			state.underline = true
		case code == 5:
			state.blink = true
		case code == 7:
			state.reverse = true
		case code == 22:
			state.bold = false
		case code == 24:
			state.underline = false
		case code == 25:
			state.blink = false
		case code == 27:
			state.reverse = false
		case code >= 30 && code <= 37:
			state.baseFG = code - 30
		case code == 39:
			state.baseFG = 7
		case code >= 40 && code <= 47:
			state.bg = code - 40
		case code == 49:
			state.bg = 0
		}
	}
}
