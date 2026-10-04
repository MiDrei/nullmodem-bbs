package i18n

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"git.maik.ch/nullmodem/kit/ansi"
)

// A screen (.ans, CP437) may take its texts from the catalog: {T:key},
// or like the other placeholders {T:key:-20} left-aligned in 20
// columns, {T:key:6} right-aligned in 6. And a screen may come in each
// language: main.de-du.ans, main.de.ans, then main.ans.

// TextRE matches a screen's {T:key[:width]}.
var TextRE = regexp.MustCompile(`\{T:([a-z0-9_.-]+)(?::(-?\d+))?\}`)

// FillScreen puts the texts in lang, as CP437, into raw's {T:key}s.
func FillScreen(lang, raw string) string {
	if !strings.Contains(raw, "{T:") {
		return raw
	}
	return TextRE.ReplaceAllStringFunc(raw, func(m string) string {
		sub := TextRE.FindStringSubmatch(m)
		text := string(ansi.EncodeCP437(T(lang, sub[1])))
		n, err := strconv.Atoi(sub[2])
		switch {
		case err != nil || n == 0:
		case n < 0 && len(text) >= -n:
			text = text[:-n]
		case n < 0:
			text += strings.Repeat(" ", -n-len(text))
		case len(text) >= n:
			text = text[:n]
		default:
			text = strings.Repeat(" ", n-len(text)) + text
		}
		return text
	})
}

// ScreenNames are the files to try for screen name in lang, best
// first: "main.ans" in "de-du" -> main.de-du.ans, main.de.ans,
// main.en.ans, main.ans.
func ScreenNames(name, lang string) []string {
	name = filepath.Base(name)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	var out []string
	for _, code := range Chain(lang) {
		out = append(out, stem+"."+code+ext)
	}
	return append(out, name)
}

// ScreenLang is the language a screen file is for ("main.de-du.ans"
// -> "de-du"), or "" for the fallback one (main.ans).
func ScreenLang(name string) string {
	stem := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	if i := strings.Index(stem, "."); i >= 0 && Valid(stem[i+1:]) {
		return stem[i+1:]
	}
	return ""
}
