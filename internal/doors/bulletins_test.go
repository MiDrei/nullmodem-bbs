package doors

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBulletins(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "DATA"), 0o755)
	os.WriteFile(filepath.Join(dir, "DATA", "SCORES.ANS"), []byte("\x1b[1;33mTop players\x1b[0m\r\n"), 0o644)
	if data, _, err := ReadBulletin(dir, "DATA/scores.ans"); err != nil || !strings.Contains(string(data), "Top players") {
		t.Fatalf("case-forgiving read: %v %q", err, data)
	}
	for _, bad := range []string{"../etc/passwd", "/etc/passwd", "a/../../x", ""} {
		if _, _, err := ReadBulletin(dir, bad); err != ErrBadBulletin {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if _, _, err := ReadBulletin(dir, "DATA/NEWS.ANS"); !os.IsNotExist(err) {
		t.Errorf("missing: %v", err)
	}
}

func TestEnableImmortalBaronsBulletins(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "data"), 0o755)
	os.WriteFile(filepath.Join(dir, "data", "bbs.cfg"), []byte("BoardID   Avalon"), 0o644)
	if err := enableImmortalBaronsBulletins(dir, "Maiks Place"); err != nil {
		t.Fatal(err)
	}
	enableImmortalBaronsBulletins(dir, "Maiks Place") // twice: once
	got, _ := os.ReadFile(filepath.Join(dir, "data", "bbs.cfg"))
	if string(got) != "BoardID   Avalon\nBulletinDir      bull\nBBSName          Maiks Place\n" {
		t.Fatalf("bbs.cfg:\n%s", got)
	}
	if tpl, ok := TemplateFor("", "/srv/doors/immortal-barons"); !ok || len(tpl.Bulletins) != 3 || tpl.Daily == "" {
		t.Fatalf("template by dir: %+v %v", tpl.Bulletins, ok)
	}
}
