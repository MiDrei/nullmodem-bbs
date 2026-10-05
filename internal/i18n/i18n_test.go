package i18n

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestChain(t *testing.T) {
	for lang, want := range map[string][]string{
		"de-du": {"de-du", "de", "en"},
		"de":    {"de", "en"},
		"en":    {"en"},
		"":      {"en"},
		"xx":    {"en"},
	} {
		if got := Chain(lang); !reflect.DeepEqual(got, want) {
			t.Errorf("Chain(%q) = %v, want %v", lang, got, want)
		}
	}
}

func TestFallbackAndOverrides(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	if got := c.T("de-du", "login.password"); got == "" || got == "login.password" {
		t.Fatalf("de-du text: %q", got)
	}
	if got := c.T("en", "no.such.key"); got != "no.such.key" {
		t.Fatalf("missing key: %q", got)
	}
	if got := c.T("en", "register.created", "HANDLE", "maik"); got != "Account created. Welcome, maik!" {
		t.Fatalf("placeholder: %q", got)
	}
	if err := c.SetOverrides("de", map[string]string{"login.password": "Kennwort: "}); err != nil {
		t.Fatal(err)
	}
	if got := c.T("de", "login.password"); got != "Kennwort: " {
		t.Fatalf("override: %q", got)
	}
	// The same text as built in is no change.
	def, _ := c.Default("de", "login.handle")
	if err := c.SetOverrides("de", map[string]string{"login.handle": def}); err != nil {
		t.Fatal(err)
	}
	if len(c.Overrides("de")) != 0 {
		t.Fatalf("kept %v", c.Overrides("de"))
	}
	if _, err := os.Stat(filepath.Join(dir, "de.yaml")); !os.IsNotExist(err) {
		t.Fatalf("file not removed: %v", err)
	}
	if err := c.SetOverrides("de", map[string]string{"made.up": "x"}); err == nil {
		t.Fatal("unknown key accepted")
	}
}

// Every language has every English text, with the same placeholders.
func TestCatalogsComplete(t *testing.T) {
	c := New("")
	for _, l := range Languages {
		if l.Code == Fallback {
			continue
		}
		for _, k := range c.Keys() {
			text, ok := c.Default(l.Code, k)
			if !ok {
				if l.Code == "de-du" {
					// de-du may leave a text to de where the two agree.
					if _, ok := c.Default("de", k); ok {
						continue
					}
				}
				t.Errorf("%s: no text for %s", l.Code, k)
				continue
			}
			en, _ := c.Default(Fallback, k)
			if a, b := Placeholders(en), Placeholders(text); !reflect.DeepEqual(a, b) {
				t.Errorf("%s %s: placeholders %v, English has %v", l.Code, k, b, a)
			}
		}
		for k := range c.defaults[l.Code] {
			if _, ok := c.Default(Fallback, k); !ok {
				t.Errorf("%s: %s is not in English", l.Code, k)
			}
		}
	}
}

// Every key the code asks for is in the English catalog.
func TestKeysUsedExist(t *testing.T) {
	uses := []*regexp.Regexp{
		regexp.MustCompile(`\.(?:T|U)\("([a-z0-9_.-]+)"`),
		regexp.MustCompile(`i18n\.T\([^,()]+(?:\(\))?, "([a-z0-9_.-]+)"`),
		regexp.MustCompile(`\{T:([a-z0-9_.-]+)`),
		regexp.MustCompile(`i18n\.Ref\("([a-z0-9_.-]+)"`),
		// The web: t('web.x') in .svelte and .ts, and keys kept in lists.
		regexp.MustCompile(`\bt\(\s*'([a-z0-9_.-]+)'`),
		regexp.MustCompile(`'(web\.[a-z0-9_.-]+)'`),
	}
	plural := regexp.MustCompile(`(?:\.N\("|\btn\(\s*')([a-z0-9_.-]+)['"]`)
	c := New("")
	known := map[string]bool{}
	for _, k := range c.Keys() {
		known[k] = true
	}
	root := filepath.Join("..", "..")
	found := 0
	for _, dir := range []string{"internal", "cmd", "configs/screens", "web/src"} {
		filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if strings.Contains(path, "node_modules") {
				return nil
			}
			if !strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".ans") && !strings.HasSuffix(path, ".svelte") && !strings.HasSuffix(path, ".ts") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			for _, re := range uses {
				for _, m := range re.FindAllStringSubmatch(string(data), -1) {
					if m[1] == "key" || m[1] == "web.x.y" { // examples in comments
						continue
					}
					found++
					if !known[m[1]] && !known[m[1]+".one"] {
						t.Errorf("%s: %q is not in en.yaml", path, m[1])
					}
				}
			}
			for _, m := range plural.FindAllStringSubmatch(string(data), -1) {
				found++
				for _, form := range []string{".one", ".other"} {
					if !known[m[1]+form] {
						t.Errorf("%s: %q is not in en.yaml", path, m[1]+form)
					}
				}
			}
			return nil
		})
	}
	if found < 100 {
		t.Fatalf("only %d uses found -- is the scan broken?", found)
	}
}

// A key bar (…keys, …hint) is drawn on one row: it fits 79 columns.
func TestKeyBarsFit(t *testing.T) {
	c := New("")
	for _, l := range Languages {
		for _, k := range c.Keys() {
			if strings.HasPrefix(k, "web.") || strings.HasPrefix(k, "admin.") {
				continue // the web (and its admin) wraps
			}
			if !strings.HasSuffix(k, "keys") && !strings.HasSuffix(k, "hint") && !strings.HasSuffix(k, "hint_areas") && !strings.HasSuffix(k, "hint_rooms") && !strings.HasSuffix(k, "keys_reply") && !strings.HasSuffix(k, "keys_post") {
				continue
			}
			text, _ := c.Text(l.Code, k)
			view, _ := c.Text(l.Code, "msgs.key_all")
			text = strings.ReplaceAll(text, "{VIEW}", view)
			en, _ := c.Text(Fallback, k)
			if n := len([]rune(text)); n > 79 && n > len(en) {
				t.Errorf("%s %s is %d wide: %q", l.Code, k, n, text)
			}
		}
	}
}

// A merged key still reads as the key it went into, and a sysop's
// change saved under it still counts.
func TestMergedKeys(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	var old, key string
	for o, k := range c.aliases {
		old, key = o, k
		break
	}
	if old == "" {
		t.Skip("no merged keys")
	}
	want, _ := c.Text("de", key)
	if got, ok := c.Text("de", old); !ok || got != want {
		t.Fatalf("Text(%s) = %q, want %q (as %s)", old, got, want, key)
	}
	if err := os.WriteFile(filepath.Join(dir, "de.yaml"), []byte(old+": \"Eigener Text\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.checked = time.Time{}
	if got, _ := c.Text("de", key); got != "Eigener Text" {
		t.Fatalf("override under the old key lost: %q", got)
	}
	if _, ok := c.Default(Fallback, old); !ok {
		t.Fatal("Default of an old key")
	}
	if err := c.SetOverrides("de", map[string]string{old: "Noch einer"}); err != nil {
		t.Fatalf("saving under the old key: %v", err)
	}
	if got := c.Overrides("de"); got[key] != "Noch einer" {
		t.Fatalf("saved %v", got)
	}
}
