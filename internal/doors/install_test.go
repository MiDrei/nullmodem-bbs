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
	dir, err := Install(context.Background(), http.DefaultClient, tmpl, "", doorsDir, "Maiks Place", "Mike Dreier")
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
	dir, err := Install(context.Background(), http.DefaultClient, tmpl, "", t.TempDir(), "Maiks, Place", "Mike Dreier")
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
	_, err := Install(context.Background(), http.DefaultClient, tmpl, "", doorsDir, "B", "S")
	if !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("err = %v, want ErrAlreadyInstalled", err)
	}
}

func TestInstallLeavesNothingBehindOnABadArchive(t *testing.T) {
	doorsDir := t.TempDir()
	tmpl := Template{Name: "Broken", Dir: "broken", Download: &Download{URL: serve(t, []byte("not a zip")), Format: "zip"}}
	if _, err := Install(context.Background(), http.DefaultClient, tmpl, "", doorsDir, "B", "S"); err == nil {
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
		if !filepath.IsLocal(tm.Dir) {
			t.Errorf("template %s: bad dir %q", tm.ID, tm.Dir)
		}
		switch tm.Kind {
		case "", "dosbox":
			if tm.DOSBoxLaunchCmd == "" {
				t.Errorf("template %s: DOS door without a launch command", tm.ID)
			}
		case "native":
			if tm.Exe == "" || !filepath.IsLocal(tm.Exe) || tm.Download == nil {
				t.Errorf("template %s: native door needs a local exe and a download", tm.ID)
			}
			if !tm.Download.Supports("amd64") {
				t.Errorf("template %s: no amd64 build", tm.ID)
			}
		default:
			t.Errorf("template %s: unknown kind %q", tm.ID, tm.Kind)
		}
	}
}

func TestDownloadResolvesArch(t *testing.T) {
	d := &Download{URL: "https://x/game-{arch}.tgz", Subdir: "game-{arch}", Arch: map[string]string{"amd64": "x86_64"}}
	url, sub, err := d.resolve("amd64", "")
	if err != nil || url != "https://x/game-x86_64.tgz" || sub != "game-x86_64" {
		t.Fatalf("resolve(amd64) = %q, %q, %v", url, sub, err)
	}
	if d.Supports("arm64") {
		t.Fatal("arm64 has no build but Supports said yes")
	}
	plain := &Download{URL: "https://x/dos.zip"}
	if !plain.Supports("riscv64") {
		t.Fatal("an arch-independent download must be available everywhere")
	}
}

func TestInstallKeepsExecutablesExecutable(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, mode := range map[string]int64{"pkg/game": 0o755, "pkg/README": 0o644, "pkg/tool": 0o644} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: 2, Typeflag: tar.TypeReg})
		tw.Write([]byte("hi"))
	}
	tw.Close()
	gz.Close()
	tmpl := Template{Name: "Game", Dir: "game", Download: &Download{
		URL: serve(t, buf.Bytes()), Format: "tar.gz", Subdir: "pkg", Executables: []string{"tool"},
	}}
	dir, err := Install(context.Background(), http.DefaultClient, tmpl, "", t.TempDir(), "B", "S")
	if err != nil {
		t.Fatal(err)
	}
	for name, exec := range map[string]bool{"game": true, "README": false, "tool": true} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode()&0o111 != 0; got != exec {
			t.Errorf("%s executable = %v, want %v (mode %v)", name, got, exec, fi.Mode())
		}
	}
}

func TestSetCtlValuesReplacesAndAppends(t *testing.T) {
	p := filepath.Join(t.TempDir(), "USURP.CTL")
	os.WriteFile(p, []byte(";BBSTYPE PCB15\r\nSYSOPFIRST Bob\r\nBBSTYPE DOORSYS\r\n"), 0o644)
	if err := setCtlValues(p, map[string]string{"SYSOPFIRST": "Mike", "BBSTYPE": "DOOR32", "BBSNAME": "Maiks Place"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	want := ";BBSTYPE PCB15\r\nSYSOPFIRST Mike\r\nBBSTYPE DOOR32\r\nBBSNAME Maiks Place\r\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
