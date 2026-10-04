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
