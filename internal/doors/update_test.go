package doors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/db"
)

// Updating replaces what the release ships, keeps everything else and
// backs up what it replaced.
func TestUpdateReplacesShippedFilesOnly(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		w.Write(makeTarGz(t, map[string]string{
			"game-v2/game":        "new binary",
			"game-v2/docs/new.md": "docs",
		}))
	}))
	defer srv.Close()
	tmpl := Template{Name: "Game", Dir: "game", Download: &Download{
		URL: srv.URL + "/{version}/game-{version}.tar.gz", Format: "tar.gz", Subdir: "game-{version}",
		Executables: []string{"game"}, LegacyVersion: "v1",
	}}
	dir := filepath.Join(t.TempDir(), "game")
	os.MkdirAll(filepath.Join(dir, "data"), 0o755)
	os.WriteFile(filepath.Join(dir, "game"), []byte("old binary"), 0o755)
	os.WriteFile(filepath.Join(dir, "data", "world.json"), []byte("save"), 0o644)
	if got := InstalledVersion(tmpl, dir); got != "v1" {
		t.Fatalf("installed before = %q, want the legacy v1", got)
	}

	if err := Update(context.Background(), http.DefaultClient, tmpl, dir, "v2"); err != nil {
		t.Fatal(err)
	}
	if asked != "/v2/game-v2.tar.gz" {
		t.Errorf("downloaded %s", asked)
	}
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }
	if got := read(filepath.Join(dir, "game")); got != "new binary" {
		t.Errorf("game = %q", got)
	}
	if info, _ := os.Stat(filepath.Join(dir, "game")); info.Mode()&0o111 == 0 {
		t.Error("game is not executable")
	}
	if got := read(filepath.Join(dir, "data", "world.json")); got != "save" {
		t.Errorf("save game = %q", got)
	}
	if got := read(filepath.Join(dir, "docs", "new.md")); got != "docs" {
		t.Errorf("new file = %q", got)
	}
	if got := read(filepath.Join(filepath.Dir(dir), ".backup-game", "game")); got != "old binary" {
		t.Errorf("backup = %q", got)
	}
	if got := InstalledVersion(tmpl, dir); got != "v2" {
		t.Errorf("installed after = %q", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(dir))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".update-") {
			t.Errorf("scratch dir %s left behind", e.Name())
		}
	}
}

func TestLatestReleaseAndRecord(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/game/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"tag_name":"v0.2.3","html_url":"https://example/rel","published_at":"2026-10-06T00:14:49Z"}`))
	}))
	defer srv.Close()
	old := githubAPI
	githubAPI = srv.URL
	defer func() { githubAPI = old }()

	tmpl := Template{ID: "game", Name: "Game", Download: &Download{GitHub: "o/game"}}
	rel, err := LatestRelease(context.Background(), http.DefaultClient, tmpl)
	if err != nil || rel.Version != "v0.2.3" || rel.URL != "https://example/rel" {
		t.Fatalf("LatestRelease = %+v, %v", rel, err)
	}

	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	now := time.Now()
	if err := SaveRelease(sqlDB, "game", rel, nil, now); err != nil {
		t.Fatal(err)
	}
	// A failed check later keeps what was found.
	if err := SaveRelease(sqlDB, "game", Release{}, context.DeadlineExceeded, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := LatestReleases(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	if k := got["game"]; k.Version != "v0.2.3" || k.Error == "" || !k.At.Equal(rel.At) {
		t.Errorf("recorded %+v", k)
	}
}

func TestUpdateAvailable(t *testing.T) {
	for _, c := range []struct {
		installed, latest string
		want              bool
	}{
		{"v0.2.0", "v0.2.3", true},
		{"106", "106", false},
		{"1.1.14", "v1.1.14", false},
		{"", "v1", false},
		{"v1", "", false},
	} {
		if got := UpdateAvailable(c.installed, c.latest); got != c.want {
			t.Errorf("UpdateAvailable(%q, %q) = %v", c.installed, c.latest, got)
		}
	}
}

// The templates with a release feed fill in a version.
func TestTemplatesWithFeedHaveVersions(t *testing.T) {
	for _, tm := range Templates {
		d := tm.Download
		if d == nil || d.GitHub == "" {
			continue
		}
		if d.Version == "" || d.LegacyVersion == "" || !strings.Contains(d.URL, "{version}") {
			t.Errorf("%s: version %q, legacy %q, url %q", tm.ID, d.Version, d.LegacyVersion, d.URL)
		}
	}
}
