package menu

import (
	"os"
	"path/filepath"
	"testing"
)

func writeMenuFile(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
}

func TestLoadDirParsesMenusAndItems(t *testing.T) {
	dir := t.TempDir()
	writeMenuFile(t, dir, "main.yaml", `
name: main
title: Main Menu
items:
  - key: W
    label: "Who's online"
    action: "builtin:who"
    min_sl: 0
  - key: S
    label: "Sysop menu"
    action: "goto:sysop"
    min_sl: 200
`)

	set, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	m, ok := set.Get("main")
	if !ok {
		t.Fatal("expected menu 'main' to be loaded")
	}
	if m.Title != "Main Menu" || len(m.Items) != 2 {
		t.Fatalf("unexpected menu: %+v", m)
	}
}

func TestVisibleItemsFiltersBySecurityLevel(t *testing.T) {
	m := &Menu{
		Name: "main",
		Items: []Item{
			{Key: "W", Label: "Who's online", Action: "builtin:who", MinSL: 0},
			{Key: "S", Label: "Sysop menu", Action: "goto:sysop", MinSL: 200},
		},
	}

	visible := m.VisibleItems(10)
	if len(visible) != 1 || visible[0].Key != "W" {
		t.Fatalf("VisibleItems(10) = %+v, want only the SL-0 item", visible)
	}

	visible = m.VisibleItems(255)
	if len(visible) != 2 {
		t.Fatalf("VisibleItems(255) = %+v, want both items", visible)
	}
}

func TestFindIsCaseInsensitiveAndGated(t *testing.T) {
	m := &Menu{
		Items: []Item{
			{Key: "Q", Label: "Quit", Action: "logoff", MinSL: 0},
			{Key: "S", Label: "Sysop menu", Action: "goto:sysop", MinSL: 200},
		},
	}

	if item, ok := m.Find("q", 0); !ok || item.Action != "logoff" {
		t.Fatalf("Find(\"q\", 0) = %+v, %v; want logoff item", item, ok)
	}
	if _, ok := m.Find("s", 10); ok {
		t.Fatal("Find(\"s\", 10) should be gated out below min_sl 200")
	}
	if _, ok := m.Find("s", 200); !ok {
		t.Fatal("Find(\"s\", 200) should be visible at min_sl 200")
	}
	if _, ok := m.Find("Z", 255); ok {
		t.Fatal("Find(\"Z\", 255) should not match any item")
	}
}

func TestLoadDirRejectsDuplicateNames(t *testing.T) {
	dir := t.TempDir()
	writeMenuFile(t, dir, "a.yaml", "name: main\ntitle: A\nitems:\n  - key: Q\n    label: Quit\n    action: logoff\n")
	writeMenuFile(t, dir, "b.yaml", "name: main\ntitle: B\nitems:\n  - key: Q\n    label: Quit\n    action: logoff\n")

	if _, err := LoadDir(dir); err == nil {
		t.Fatal("expected error for duplicate menu name")
	}
}

func TestLoadDirRejectsMissingActionOrKey(t *testing.T) {
	dir := t.TempDir()
	writeMenuFile(t, dir, "bad.yaml", "name: main\ntitle: Bad\nitems:\n  - key: Q\n    label: Quit\n")

	if _, err := LoadDir(dir); err == nil {
		t.Fatal("expected error for item missing action")
	}
}
