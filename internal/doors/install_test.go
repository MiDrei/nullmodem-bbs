package doors

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	zw.Close()
	return buf.Bytes()
}

func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg})
		tw.Write([]byte(content))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func serve(t *testing.T, data []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestInstallZipSetsCtlNamesAndStaysInsideTheDoorsDir(t *testing.T) {
	archive := makeZip(t, map[string]string{
		"GAME.EXE":         "MZ",
		"GAME.CTL":         "; comment\r\nSYSOPFIRST Bob\r\nSYSOPLAST Dalton\r\nBBSNAME The TANSTAFL BBS\r\nBBSTYPE DOORSYS\r\n",
		"../../escape.txt": "nope",
	})
	tmpl := Template{Name: "Game", Dir: "game", Download: &Download{URL: serve(t, archive), Format: "zip", CtlFile: "game.ctl"}}
	doorsDir := t.TempDir()
	dir, err := Install(context.Background(), http.DefaultClient, tmpl, doorsDir, "Maiks Place", "Mike Dreier")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if dir != filepath.Join(doorsDir, "game") {
		t.Fatalf("dir = %q", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "GAME.EXE")); err != nil {
		t.Fatalf("GAME.EXE missing: %v", err)
	}
	ctl, err := os.ReadFile(filepath.Join(dir, "GAME.CTL"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"; comment", "SYSOPFIRST Mike", "SYSOPLAST Dreier", "BBSNAME Maiks Place", "BBSTYPE DOORSYS"} {
		if !strings.Contains(string(ctl), want) {
			t.Errorf("GAME.CTL missing %q:\n%s", want, ctl)
		}
	}
	if _, err := os.Stat(filepath.Join(doorsDir, "..", "escape.txt")); err == nil {
		t.Fatal("archive entry escaped the doors directory")
	}
	entries, _ := os.ReadDir(doorsDir)
	if len(entries) != 1 {
		t.Fatalf("doors dir should only hold the door, got %v", entries)
	}
}

func TestInstallTarGzTakesOnlyTheSubdir(t *testing.T) {
	archive := makeTarGz(t, map[string]string{
		"JudgeDredd-main/README.md":         "readme",
		"JudgeDredd-main/GAME/DREDD.EXE":    "MZ",
		"JudgeDredd-main/GAME/ANS/X.ANS":    "art",
		"JudgeDredd-main/GAME/DATA/REG.DAT": "Last Rangers' BBS,Grendil,250504,999\r\n^ comment\r\n",
		"JudgeDredd-main/SOURCE/DREDD.PAS":  "src",
	})
	tmpl := Template{Name: "Dredd", Dir: "dredd", Download: &Download{
		URL: serve(t, archive), Format: "tar.gz", Subdir: "JudgeDredd-main/GAME", Prepare: prepareJudgeDredd,
	}}
	dir, err := Install(context.Background(), http.DefaultClient, tmpl, t.TempDir(), "Maiks, Place", "Mike Dreier")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	for _, p := range []string{"DREDD.EXE", "ANS/X.ANS"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("%s missing: %v", p, err)
		}
	}
	reg, _ := os.ReadFile(filepath.Join(dir, "DATA", "REG.DAT"))
	if !strings.HasPrefix(string(reg), "Maiks Place,Mike Dreier,") || !strings.Contains(string(reg), ",999\r\n^ comment") {
		t.Errorf("REG.DAT = %q", reg)
	}
	for _, p := range []string{"README.md", "DREDD.PAS", "SOURCE"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
			t.Errorf("%s should not have been unpacked", p)
		}
	}
}

func TestInstallNeverOverwritesAnInstalledDoor(t *testing.T) {
	doorsDir := t.TempDir()
	os.MkdirAll(filepath.Join(doorsDir, "dredd"), 0o755)
	os.WriteFile(filepath.Join(doorsDir, "dredd", "SAVEGAME.DAT"), []byte("precious"), 0o644)
	tmpl := Template{Name: "Dredd", Dir: "dredd", Download: &Download{URL: "http://127.0.0.1:1/never", Format: "zip"}}
	_, err := Install(context.Background(), http.DefaultClient, tmpl, doorsDir, "B", "S")
	if !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("err = %v, want ErrAlreadyInstalled", err)
	}
}

func TestInstallLeavesNothingBehindOnABadArchive(t *testing.T) {
	doorsDir := t.TempDir()
	tmpl := Template{Name: "Broken", Dir: "broken", Download: &Download{URL: serve(t, []byte("not a zip")), Format: "zip"}}
	if _, err := Install(context.Background(), http.DefaultClient, tmpl, doorsDir, "B", "S"); err == nil {
		t.Fatal("Install accepted a broken archive")
	}
	entries, _ := os.ReadDir(doorsDir)
	if len(entries) != 0 {
		t.Fatalf("leftovers after a failed install: %v", entries)
	}
}

func TestTemplatesAreValid(t *testing.T) {
	seen := map[string]bool{}
	for _, tm := range Templates {
		if seen[tm.ID] {
			t.Errorf("duplicate template id %q", tm.ID)
		}
		seen[tm.ID] = true
		if err := validateDropFile(Door{Name: tm.Name, DropFile: tm.DropFile}); err != nil {
			t.Error(err)
		}
		if !filepath.IsLocal(tm.Dir) || tm.DOSBoxLaunchCmd == "" {
			t.Errorf("template %s: bad dir or launch command", tm.ID)
		}
	}
}
