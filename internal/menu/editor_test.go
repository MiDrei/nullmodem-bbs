package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheck(t *testing.T) {
	set := Set{"main": {Name: "main"}, "sysop": {Name: "sysop"}}
	ok := &Menu{Name: "games", Items: []Item{{Key: "D", Action: "builtin:doors"}, {Key: "m", Action: "goto:main"}, {Key: "Q", Action: "back"}}}
	if err := Check(ok, set); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []*Menu{
		{Name: "Games", Items: ok.Items},
		{Name: "games"},
		{Name: "games", Items: []Item{{Key: "D", Action: "builtin:doors"}, {Key: "d", Action: "back"}}},
		{Name: "games", Items: []Item{{Key: "D", Action: "builtin:nope"}}},
		{Name: "games", Items: []Item{{Key: "D", Action: "goto:nowhere"}}},
		{Name: "games", Items: []Item{{Key: "two words", Action: "back"}}},
		{Name: "games", Items: []Item{{Key: "D", Action: "shell:rm"}}},
	} {
		if Check(bad, set) == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestNotShown(t *testing.T) {
	m := &Menu{Items: []Item{
		{Key: "M", Label: "Message areas"}, {Key: "V", Label: "Voting booth"}, {Key: "X", Label: "Xmas"}, {Key: "S", Label: "Sysop", MinSL: 200},
	}}
	screen := "[M] Message Areas      V) Polls"
	got := NotShown(m, 10, screen, nil)
	if len(got) != 1 || got[0].Key != "X" {
		t.Fatalf("not shown = %+v", got)
	}
}

func TestWatcherPicksUpChanges(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) {
		os.WriteFile(filepath.Join(dir, "main.yaml"), []byte(body), 0o644)
	}
	write("name: main\ntitle: One\nitems:\n  - key: Q\n    action: logoff\n")
	w, err := NewWatcher(dir)
	if err != nil {
		t.Fatal(err)
	}
	w.Every = 0
	if m, _ := w.Get("main"); m.Title != "One" {
		t.Fatal(m.Title)
	}
	time.Sleep(10 * time.Millisecond)
	write("name: main\ntitle: Two\nitems:\n  - key: Q\n    action: logoff\n")
	if m, _ := w.Get("main"); m.Title != "Two" {
		t.Fatalf("not reloaded: %q", m.Title)
	}
	var reported error
	w.OnError = func(err error) { reported = err }
	time.Sleep(10 * time.Millisecond)
	write("name: main\nitems: [broken")
	if m, _ := w.Get("main"); m.Title != "Two" || reported == nil || !strings.Contains(reported.Error(), "main.yaml") {
		t.Fatalf("a broken file: %q, %v", m.Title, reported)
	}
	if err := Save(dir, &Menu{Name: "main", Title: "Three", Items: []Item{{Key: "Q", Action: "logoff"}}}); err != nil {
		t.Fatal(err)
	}
	if m, _ := w.Get("main"); m.Title != "Three" {
		t.Fatalf("after Save: %q", m.Title)
	}
}

func TestOnlyOnScreen(t *testing.T) {
	m := &Menu{Items: []Item{{Key: "M"}, {Key: "S", MinSL: 200}}}
	got := OnlyOnScreen(m, 10, "[M] Messages  [V] Voting  [S] Sysop  [m] again")
	if strings.Join(got, ",") != "V,S" {
		t.Fatalf("only on screen = %v", got)
	}
}
