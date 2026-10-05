package bbs

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"git.maik.ch/nullmodem/kit/ansi"

	"git.maik.ch/nullmodem/bbs/internal/i18n"
	"git.maik.ch/nullmodem/bbs/internal/menu"
)

// A shipped German screen, filled with the English texts, says what
// the English one says, in the same columns.
func TestGermanScreensMatchEnglish(t *testing.T) {
	dir := filepath.Join("..", "..", "configs", "screens")
	des, _ := filepath.Glob(filepath.Join(dir, "*.de.ans"))
	if len(des) == 0 {
		t.Fatal("no German screens")
	}
	escapes := regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
	visible := func(raw string) string { return escapes.ReplaceAllString(raw, "") }
	for _, de := range des {
		en := strings.Replace(de, ".de.ans", ".ans", 1)
		enRaw, err := ansi.LoadScreen(en)
		if err != nil {
			t.Fatalf("%s: %v", en, err)
		}
		deRaw, err := ansi.LoadScreen(de)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := visible(i18n.FillScreen("en", deRaw)), visible(enRaw); got != want {
			t.Errorf("%s with English texts:\n%q\nwant\n%q", filepath.Base(de), got, want)
		}
		// And in German nothing is cut short.
		for _, lang := range []string{"de", "de-du"} {
			for _, m := range i18n.TextRE.FindAllStringSubmatch(deRaw, -1) {
				if m[2] == "" {
					continue
				}
				text := toCP437(i18n.T(lang, m[1]))
				if n, _ := strconv.Atoi(strings.TrimPrefix(m[2], "-")); len(text) > n {
					t.Errorf("%s: %s %q is wider than %d", filepath.Base(de), lang, text, n)
				}
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "main.de.ans")); err != nil {
		t.Fatal(err)
	}
}

// The stock menu screens fit an 80-column terminal in every language, their
// frame closed on every line -- the sysop's entry shown or not.
func TestMenuScreensFit(t *testing.T) {
	dir := filepath.Join("..", "..", "configs", "screens")
	escapes := regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
	// What Terminal.Width lays screens out to on an 80-column terminal:
	// a line as wide as the terminal wraps on many clients.
	width := maxWidth - wrapMargin
	for _, name := range []string{"main", "messages", "files", "community", "sysop", "logoff"} {
		for _, lang := range []string{"en", "de", "de-du"} {
			file := name + ".ans"
			if lang != "en" {
				file = name + ".de.ans"
			}
			raw, err := ansi.LoadScreen(filepath.Join(dir, file))
			if err != nil {
				t.Fatalf("%s: %v", file, err)
			}
			for _, sl := range []int{10, 255} {
				vars := ansi.Vars{"BBSNAME": "Maiks Place BBS", "USERNAME": "SwissMaik", "NODE": "1",
					"SYSOP_ITEM": menu.SysopItem(sl, toCP437(i18n.T(lang, "menu.sysop_item")))}
				out := ansi.Layout(ansi.Render(menu.SysopLines(i18n.FillScreen(lang, raw), sl), vars), width)
				for i, line := range strings.Split(escapes.ReplaceAllString(out, ""), "\r\n") {
					if n := len(line); n > width {
						t.Errorf("%s %s line %d is %d wide: %q", file, lang, i+1, n, line)
					}
					if frame := line[:min(1, len(line))]; (frame == "\xba" || frame == "\xb3") && (len(line) != width || !strings.HasSuffix(line, frame)) {
						t.Errorf("%s %s (SL %d) line %d frame not closed at %d: %q", file, lang, sl, i+1, width, line)
					}
				}
			}
		}
	}
}
